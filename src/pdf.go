// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// newPDFRenderer returns a renderer that reuses CDP sessions from the pool.
func newPDFRenderer(pool *sessionPool) pdfRenderer {
	if pool == nil {
		pool = newSessionPool(0)
	}
	return func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		return renderPDF(ctx, pool, wsURL, html, wait, options)
	}
}

// renderPDF uses a Chrome DevTools websocket to load the supplied HTML, apply
// request-scoped PDF preparation, and stream the printed document back.
func renderPDF(ctx context.Context, pool *sessionPool, wsURL, html string, wait time.Duration, options pdfOptions) (pdf []byte, pdfTime time.Duration, err error) {
	session, err := pool.acquire(ctx, wsURL)
	if err != nil {
		return nil, 0, err
	}
	reusable := false
	backgroundOverridden := false
	defer func() {
		if backgroundOverridden {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), defaultChromeClientTimeout)
			cleanupErr := clearBackgroundOverride(cleanupCtx, session.client, session.sessionID)
			cancel()
			if cleanupErr != nil {
				reusable = false
				if err == nil {
					pdf = nil
					err = fmt.Errorf("restore page background: %w", cleanupErr)
				}
			}
		}
		if reusable {
			pool.release(session)
		} else {
			_ = session.Close()
		}
	}()

	client := session.client
	sessionID := session.sessionID

	if err := client.Call(ctx, sessionID, "Page.navigate", map[string]any{
		"url": "about:blank",
	}, nil); err != nil {
		return nil, 0, err
	}

	var frameTree struct {
		FrameTree struct {
			Frame struct {
				ID string `json:"id"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if err := client.Call(ctx, sessionID, "Page.getFrameTree", nil, &frameTree); err != nil {
		return nil, 0, err
	}
	if frameTree.FrameTree.Frame.ID == "" {
		return nil, 0, errors.New("missing frame id")
	}

	if err := client.Call(ctx, sessionID, "Page.setDocumentContent", map[string]any{
		"frameId": frameTree.FrameTree.Frame.ID,
		"html":    html,
	}, nil); err != nil {
		return nil, 0, err
	}

	if err := waitForBody(ctx, client, sessionID); err != nil {
		return nil, 0, err
	}
	if options.WaitForFonts != nil && *options.WaitForFonts {
		if err := waitForFonts(ctx, client, sessionID); err != nil {
			return nil, 0, err
		}
	}
	if err := sleepWithContext(ctx, wait); err != nil {
		return nil, 0, err
	}
	if options.OmitBackground != nil && *options.OmitBackground {
		if err := setTransparentBackground(ctx, client, sessionID); err != nil {
			return nil, 0, err
		}
		backgroundOverridden = true
	}

	params := buildPrintToPDFParams(options)

	var result struct {
		Data   string `json:"data"`
		Stream string `json:"stream"`
	}
	startPDF := time.Now()
	if err := client.Call(ctx, sessionID, "Page.printToPDF", params, &result); err != nil {
		return nil, time.Since(startPDF), err
	}
	pdfTime = time.Since(startPDF)

	if result.Stream != "" {
		pdf, err = readPDFStream(ctx, client, sessionID, result.Stream)
		if err != nil {
			return nil, pdfTime, err
		}
		reusable = true
		return pdf, pdfTime, nil
	}

	if result.Data == "" {
		return nil, pdfTime, errors.New("missing pdf data")
	}
	pdf, err = base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return nil, pdfTime, err
	}

	reusable = true
	return pdf, pdfTime, nil
}

type printToPDFParams struct {
	Landscape               *bool    `json:"landscape,omitempty"`
	DisplayHeaderFooter     *bool    `json:"displayHeaderFooter,omitempty"`
	PrintBackground         *bool    `json:"printBackground,omitempty"`
	Scale                   *float64 `json:"scale,omitempty"`
	PaperWidth              *float64 `json:"paperWidth,omitempty"`
	PaperHeight             *float64 `json:"paperHeight,omitempty"`
	MarginTop               *float64 `json:"marginTop,omitempty"`
	MarginBottom            *float64 `json:"marginBottom,omitempty"`
	MarginLeft              *float64 `json:"marginLeft,omitempty"`
	MarginRight             *float64 `json:"marginRight,omitempty"`
	PageRanges              string   `json:"pageRanges,omitempty"`
	HeaderTemplate          string   `json:"headerTemplate,omitempty"`
	FooterTemplate          string   `json:"footerTemplate,omitempty"`
	PreferCSSPageSize       *bool    `json:"preferCSSPageSize,omitempty"`
	TransferMode            string   `json:"transferMode,omitempty"`
	GenerateTaggedPDF       *bool    `json:"generateTaggedPDF,omitempty"`
	GenerateDocumentOutline *bool    `json:"generateDocumentOutline,omitempty"`
}

func buildPrintToPDFParams(options pdfOptions) printToPDFParams {
	printBackground := options.PrintBackground
	if printBackground == nil {
		printBackground = boolPtr(true)
	}
	return printToPDFParams{
		Landscape:               options.Landscape,
		DisplayHeaderFooter:     options.DisplayHeaderFooter,
		PrintBackground:         printBackground,
		Scale:                   options.Scale,
		PaperWidth:              options.PaperWidth,
		PaperHeight:             options.PaperHeight,
		MarginTop:               options.MarginTop,
		MarginBottom:            options.MarginBottom,
		MarginLeft:              options.MarginLeft,
		MarginRight:             options.MarginRight,
		PageRanges:              options.PageRanges,
		HeaderTemplate:          options.HeaderTemplate,
		FooterTemplate:          options.FooterTemplate,
		PreferCSSPageSize:       options.PreferCSSPageSize,
		TransferMode:            "ReturnAsStream",
		GenerateTaggedPDF:       options.GenerateTaggedPDF,
		GenerateDocumentOutline: options.GenerateDocumentOutline,
	}
}

func waitForFonts(ctx context.Context, client *cdpClient, sessionID string) error {
	var result struct {
		ExceptionDetails any `json:"exceptionDetails"`
	}
	if err := client.Call(ctx, sessionID, "Runtime.evaluate", map[string]any{
		"expression":    "document.fonts ? document.fonts.ready : Promise.resolve()",
		"awaitPromise":  true,
		"returnByValue": true,
	}, &result); err != nil {
		return err
	}
	if result.ExceptionDetails != nil {
		return errors.New("waiting for document fonts failed")
	}
	return nil
}

func setTransparentBackground(ctx context.Context, client *cdpClient, sessionID string) error {
	return client.Call(ctx, sessionID, "Emulation.setDefaultBackgroundColorOverride", map[string]any{
		"color": map[string]any{
			"r": 0,
			"g": 0,
			"b": 0,
			"a": 0,
		},
	}, nil)
}

func clearBackgroundOverride(ctx context.Context, client *cdpClient, sessionID string) error {
	return client.Call(ctx, sessionID, "Emulation.setDefaultBackgroundColorOverride", map[string]any{}, nil)
}

func boolPtr(value bool) *bool {
	return &value
}

func sleepWithContext(ctx context.Context, wait time.Duration) error {
	if wait <= 0 {
		return nil
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// readPDFStream reads all data from a Chrome DevTools Protocol (CDP) IO stream handle
// (typically returned by print-to-PDF or similar operations) and returns the accumulated
// bytes.
//
// The stream is read incrementally using the "IO.read" CDP method until EOF is reached.
// Each chunk may be base64-encoded; when Base64Encoded is true, the data is decoded
// before being appended to the result buffer.
//
// On successful completion, the function attempts to close the remote stream via "IO.close".
// Errors from closing are logged as warnings and do not affect the returned data.
//
// Parameters:
//   - ctx: context used for CDP calls.
//   - client: CDP client used to issue "IO.read" and "IO.close" commands.
//   - sessionID: target session identifier for routing CDP commands.
//   - stream: CDP stream handle to read from (must be non-empty).
//
// Returns the full stream contents as a byte slice, or an error if reading/decoding fails
// or if the stream handle is missing.
func readPDFStream(ctx context.Context, client *cdpClient, sessionID, stream string) ([]byte, error) {
	if stream == "" {
		return nil, errors.New("missing pdf stream handle")
	}

	var buf bytes.Buffer
	for {
		var chunk struct {
			Data          string `json:"data"`
			Base64Encoded bool   `json:"base64Encoded"`
			EOF           bool   `json:"eof"`
		}
		if err := client.Call(ctx, sessionID, "IO.read", map[string]any{
			"handle": stream,
		}, &chunk); err != nil {
			return nil, err
		}
		if chunk.Data != "" {
			if chunk.Base64Encoded {
				decoded, err := base64.StdEncoding.DecodeString(chunk.Data)
				if err != nil {
					return nil, err
				}
				if _, err := buf.Write(decoded); err != nil {
					return nil, err
				}
			} else {
				if _, err := buf.WriteString(chunk.Data); err != nil {
					return nil, err
				}
			}
		}
		if chunk.EOF {
			break
		}
	}

	if err := client.Call(ctx, sessionID, "IO.close", map[string]any{
		"handle": stream,
	}, nil); err != nil {
		Warnf("chrome stream close error: %v", err)
	}

	return buf.Bytes(), nil
}
