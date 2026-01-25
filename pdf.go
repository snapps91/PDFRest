// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"time"
)

// renderPDF uses a remote Chrome instance via DevTools websocket and prints the given HTML to PDF.
// Logic is unchanged: navigate to about:blank -> set document content -> wait for body -> optional sleep -> PrintToPDF.
func renderPDF(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
	client, err := newCDPClient(ctx, wsURL)
	if err != nil {
		return nil, 0, err
	}
	defer func() {
		if err := client.Close(); err != nil {
			Warnf("chrome websocket close error: %v", err)
		}
	}()

	sessionID := ""
	targetID := ""
	if !isPageWebSocket(wsURL) {
		sessionID, targetID, err = openTargetSession(ctx, client)
		if err != nil {
			return nil, 0, err
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := closeTarget(cleanupCtx, client, targetID); err != nil {
				Warnf("chrome close target error: %v", err)
			}
		}()
	}

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
	if err := sleepWithContext(ctx, wait); err != nil {
		return nil, 0, err
	}

	params := printToPDFParams{
		PrintBackground: boolPtr(true),
	}
	if options.PrintBackground != nil {
		params.PrintBackground = options.PrintBackground
	}
	if options.Landscape != nil {
		params.Landscape = options.Landscape
	}
	if options.Scale != nil {
		params.Scale = options.Scale
	}
	if options.PaperWidth != nil {
		params.PaperWidth = options.PaperWidth
	}
	if options.PaperHeight != nil {
		params.PaperHeight = options.PaperHeight
	}
	if options.MarginTop != nil {
		params.MarginTop = options.MarginTop
	}
	if options.MarginBottom != nil {
		params.MarginBottom = options.MarginBottom
	}
	if options.MarginLeft != nil {
		params.MarginLeft = options.MarginLeft
	}
	if options.MarginRight != nil {
		params.MarginRight = options.MarginRight
	}
	if options.PageRanges != "" {
		params.PageRanges = options.PageRanges
	}

	var result struct {
		Data string `json:"data"`
	}
	startPDF := time.Now()
	if err := client.Call(ctx, sessionID, "Page.printToPDF", params, &result); err != nil {
		return nil, time.Since(startPDF), err
	}
	pdfTime := time.Since(startPDF)
	if result.Data == "" {
		return nil, pdfTime, errors.New("missing pdf data")
	}
	pdf, err := base64.StdEncoding.DecodeString(result.Data)
	if err != nil {
		return nil, pdfTime, err
	}

	return pdf, pdfTime, nil
}

type printToPDFParams struct {
	Landscape       *bool    `json:"landscape,omitempty"`
	Scale           *float64 `json:"scale,omitempty"`
	PaperWidth      *float64 `json:"paperWidth,omitempty"`
	PaperHeight     *float64 `json:"paperHeight,omitempty"`
	MarginTop       *float64 `json:"marginTop,omitempty"`
	MarginBottom    *float64 `json:"marginBottom,omitempty"`
	MarginLeft      *float64 `json:"marginLeft,omitempty"`
	MarginRight     *float64 `json:"marginRight,omitempty"`
	PrintBackground *bool    `json:"printBackground,omitempty"`
	PageRanges      string   `json:"pageRanges,omitempty"`
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
