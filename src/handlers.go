// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
)

func healthHandler(resolver wsResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Health endpoints should be fast and side-effect free.
		ctx, cancel := context.WithTimeout(r.Context(), defaultChromeClientTimeout)
		defer cancel()

		if checker, ok := resolver.(interface {
			checkChrome(ctx context.Context) error
		}); ok {
			if err := checker.checkChrome(ctx); err != nil {
				http.Error(w, "chrome unavailable", http.StatusServiceUnavailable)
				return
			}
		} else if _, err := resolver.wsURL(ctx); err != nil {
			http.Error(w, "chrome unavailable", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}
}

func pdfHandler(cfg config, resolver wsResolver, renderer pdfRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only POST is allowed.
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Per-request timeout. This drives both Chrome discovery and PDF rendering.
		ctx, cancel := context.WithTimeout(r.Context(), cfg.RequestTimeout)
		defer cancel()

		// Enforce maximum body size to protect memory.
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
		defer func() {
			if err := r.Body.Close(); err != nil {
				Warnf("request body close error: %v", err)
			}
		}()

		body, err := readRequestBody(r.Body)
		if err != nil {
			// Preserve original behavior: map specific read errors to an HTTP status.
			http.Error(w, "invalid request body", mapBodyReadErrorToStatus(err))
			return
		}

		if len(body) == 0 {
			http.Error(w, "empty html", http.StatusBadRequest)
			return
		}

		options, err := parsePDFOptions(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Resolve Chrome and, when it is managed by this service, keep a lease for
		// the whole render so the idle reaper cannot stop it mid-request.
		wsURL, releaseChrome, err := acquireChrome(ctx, resolver)
		if err != nil {
			Errorf("chrome ws error: %v", err)
			http.Error(w, "chrome unavailable", http.StatusServiceUnavailable)
			return
		}
		defer releaseChrome()

		// Render PDF from HTML.
		pdf, pdfTime, err := renderer(ctx, wsURL, string(body), cfg.PDFWait, options)
		if rw, ok := w.(*responseWriter); ok {
			rw.pdfTime = pdfTime
			rw.pdfTimeSet = true
		}
		if err != nil {
			Errorf("render error: %v", err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}

		// Response headers.
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", pdfFilename))

		// Basic hardening headers (does not affect logic).
		// These are safe defaults for an API returning binary content.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdf)
	}
}

type chromeLeaseResolver interface {
	acquire(ctx context.Context) (wsURL string, release func(), err error)
}

func acquireChrome(ctx context.Context, resolver wsResolver) (string, func(), error) {
	if managed, ok := resolver.(chromeLeaseResolver); ok {
		return managed.acquire(ctx)
	}
	wsURL, err := resolver.wsURL(ctx)
	return wsURL, func() {}, err
}

// readRequestBody reads the body fully. The MaxBytesReader is already applied at the handler level.
func readRequestBody(r io.Reader) ([]byte, error) {
	// Keep the original semantics: ReadAll then validate len.
	return io.ReadAll(r)
}

// mapBodyReadErrorToStatus keeps the current status mapping logic intact,
// but isolates it into a dedicated function for clarity and testability.
func mapBodyReadErrorToStatus(err error) int {
	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, http.ErrBodyReadAfterClose), errors.Is(err, io.EOF):
		return http.StatusBadRequest
	case errors.As(err, &maxErr):
		return http.StatusRequestEntityTooLarge
	default:
		// Preserve behavior: "invalid request body" + 400 for generic errors.
		return http.StatusBadRequest
	}
}

func parsePDFOptions(values map[string][]string) (pdfOptions, error) {
	options := pdfOptions{}

	var err error
	if options.Landscape, err = parseOptionalBool(values, "landscape"); err != nil {
		return options, err
	}
	if options.DisplayHeaderFooter, err = parseOptionalBool(values, "display_header_footer"); err != nil {
		return options, err
	}
	if options.PrintBackground, err = parseOptionalBool(values, "print_background"); err != nil {
		return options, err
	}
	if options.PreferCSSPageSize, err = parseOptionalBool(values, "prefer_css_page_size"); err != nil {
		return options, err
	}
	if options.GenerateTaggedPDF, err = parseOptionalBool(values, "generate_tagged_pdf"); err != nil {
		return options, err
	}
	if options.GenerateDocumentOutline, err = parseOptionalBool(values, "generate_document_outline"); err != nil {
		return options, err
	}
	if options.OmitBackground, err = parseOptionalBool(values, "omit_background"); err != nil {
		return options, err
	}
	if options.WaitForFonts, err = parseOptionalBool(values, "wait_for_fonts"); err != nil {
		return options, err
	}

	if value := getQueryValue(values, "scale"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil || !isFinite(parsed) || parsed < 0.1 || parsed > 2 {
			return options, fmt.Errorf("invalid scale")
		}
		options.Scale = &parsed
	}

	if value := getQueryValue(values, "paper_width"); value != "" {
		parsed, err := parseLength(value)
		if err != nil || parsed <= 0 {
			return options, fmt.Errorf("invalid paper_width")
		}
		options.PaperWidth = &parsed
	}

	if value := getQueryValue(values, "paper_height"); value != "" {
		parsed, err := parseLength(value)
		if err != nil || parsed <= 0 {
			return options, fmt.Errorf("invalid paper_height")
		}
		options.PaperHeight = &parsed
	}

	if options.MarginTop, err = parseOptionalMargin(values, "margin_top"); err != nil {
		return options, err
	}
	if options.MarginBottom, err = parseOptionalMargin(values, "margin_bottom"); err != nil {
		return options, err
	}
	if options.MarginLeft, err = parseOptionalMargin(values, "margin_left"); err != nil {
		return options, err
	}
	if options.MarginRight, err = parseOptionalMargin(values, "margin_right"); err != nil {
		return options, err
	}

	if value := getQueryValue(values, "paper_format"); value != "" {
		format, ok := paperFormats[strings.ToLower(strings.TrimSpace(value))]
		if !ok {
			return options, fmt.Errorf("invalid paper_format")
		}
		// Match Chrome tooling semantics: a named format takes precedence over
		// custom paper dimensions when both are supplied.
		width, height := format.width, format.height
		options.PaperWidth = &width
		options.PaperHeight = &height
	}

	options.PageRanges = getQueryValue(values, "page_ranges")
	if err := validatePageRanges(options.PageRanges); err != nil {
		return options, fmt.Errorf("invalid page_ranges")
	}
	options.HeaderTemplate = getQueryValue(values, "header_template")
	options.FooterTemplate = getQueryValue(values, "footer_template")

	return options, nil
}

func parseOptionalBool(values map[string][]string, key string) (*bool, error) {
	value := getQueryValue(values, key)
	if value == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, fmt.Errorf("invalid %s", key)
	}
	return &parsed, nil
}

func parseOptionalMargin(values map[string][]string, key string) (*float64, error) {
	value := getQueryValue(values, key)
	if value == "" {
		return nil, nil
	}
	parsed, err := parseLength(value)
	if err != nil || parsed < 0 {
		return nil, fmt.Errorf("invalid %s", key)
	}
	return &parsed, nil
}

type paperSize struct {
	width  float64
	height float64
}

var paperFormats = map[string]paperSize{
	"letter":  {width: 8.5, height: 11},
	"legal":   {width: 8.5, height: 14},
	"tabloid": {width: 11, height: 17},
	"ledger":  {width: 17, height: 11},
	"a0":      {width: 33.1102, height: 46.811},
	"a1":      {width: 23.3858, height: 33.1102},
	"a2":      {width: 16.5354, height: 23.3858},
	"a3":      {width: 11.6929, height: 16.5354},
	"a4":      {width: 8.2677, height: 11.6929},
	"a5":      {width: 5.8268, height: 8.2677},
	"a6":      {width: 4.1339, height: 5.8268},
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validatePageRanges(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	for _, item := range strings.Split(value, ",") {
		parts := strings.Split(strings.TrimSpace(item), "-")
		if len(parts) < 1 || len(parts) > 2 {
			return fmt.Errorf("invalid range")
		}
		start, err := parsePageNumber(parts[0])
		if err != nil {
			return err
		}
		if len(parts) == 2 {
			end, err := parsePageNumber(parts[1])
			if err != nil || start > end {
				return fmt.Errorf("invalid range")
			}
		}
	}
	return nil
}

func parsePageNumber(value string) (int64, error) {
	page, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || page < 1 {
		return 0, fmt.Errorf("invalid page number")
	}
	return page, nil
}

func parseLength(value string) (float64, error) {
	const (
		mmPerInch = 25.4
		cmPerInch = 2.54
		pxPerInch = 96.0
	)

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("empty length")
	}

	lower := strings.ToLower(trimmed)
	var divisor float64 = 1
	var number string
	switch {
	case strings.HasSuffix(lower, "mm"):
		divisor = mmPerInch
		number = strings.TrimSpace(trimmed[:len(trimmed)-2])
	case strings.HasSuffix(lower, "cm"):
		divisor = cmPerInch
		number = strings.TrimSpace(trimmed[:len(trimmed)-2])
	case strings.HasSuffix(lower, "px"):
		divisor = pxPerInch
		number = strings.TrimSpace(trimmed[:len(trimmed)-2])
	case strings.HasSuffix(lower, "in"):
		number = strings.TrimSpace(trimmed[:len(trimmed)-2])
	default:
		number = trimmed
	}

	parsed, err := strconv.ParseFloat(number, 64)
	if err != nil || !isFinite(parsed) {
		return 0, fmt.Errorf("invalid length")
	}
	return parsed / divisor, nil
}

func getQueryValue(values map[string][]string, key string) string {
	if values == nil {
		return ""
	}
	if list, ok := values[key]; ok && len(list) > 0 {
		return list[0]
	}
	return ""
}
