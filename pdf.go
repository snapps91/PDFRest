// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// renderPDF uses a remote Chrome instance via DevTools websocket and prints the given HTML to PDF.
// Logic is unchanged: navigate to about:blank -> set document content -> wait for body -> optional sleep -> PrintToPDF.
func renderPDF(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancel()

	// Create a new tab context (child of allocator ctx).
	ctx, cancel = chromedp.NewContext(allocCtx)
	defer cancel()

	var (
		pdf     []byte
		frameID cdp.FrameID
	)

	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Fetch the main frame and inject the provided HTML.
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			frameID = frameTree.Frame.ID
			return page.SetDocumentContent(frameID, html).Do(ctx)
		}),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Optional wait to allow dynamic content to settle.
			if wait <= 0 {
				return nil
			}
			return chromedp.Sleep(wait).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Print with background enabled, matching original behavior.
			var err error
			print := page.PrintToPDF().WithPrintBackground(true)
			if options.PrintBackground != nil {
				print = print.WithPrintBackground(*options.PrintBackground)
			}
			if options.Landscape != nil {
				print = print.WithLandscape(*options.Landscape)
			}
			if options.Scale != nil {
				print = print.WithScale(*options.Scale)
			}
			if options.PaperWidth != nil {
				print = print.WithPaperWidth(*options.PaperWidth)
			}
			if options.PaperHeight != nil {
				print = print.WithPaperHeight(*options.PaperHeight)
			}
			if options.MarginTop != nil {
				print = print.WithMarginTop(*options.MarginTop)
			}
			if options.MarginBottom != nil {
				print = print.WithMarginBottom(*options.MarginBottom)
			}
			if options.MarginLeft != nil {
				print = print.WithMarginLeft(*options.MarginLeft)
			}
			if options.MarginRight != nil {
				print = print.WithMarginRight(*options.MarginRight)
			}
			if options.PageRanges != "" {
				print = print.WithPageRanges(options.PageRanges)
			}
			pdf, _, err = print.Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, err
	}

	return pdf, nil
}
