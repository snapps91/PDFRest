// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
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
				log.Printf("request body close error: %v", err)
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

		// Resolve Chrome websocket endpoint.
		wsURL, err := resolver.wsURL(ctx)
		if err != nil {
			log.Printf("chrome ws error: %v", err)
			http.Error(w, "chrome unavailable", http.StatusServiceUnavailable)
			return
		}

		// Render PDF from HTML.
		pdf, err := renderer(ctx, wsURL, string(body), cfg.PDFWait, options)
		if err != nil {
			log.Printf("render error: %v", err)
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

	if value := getQueryValue(values, "landscape"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return options, fmt.Errorf("invalid landscape")
		}
		options.Landscape = &parsed
	}

	if value := getQueryValue(values, "scale"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid scale")
		}
		options.Scale = &parsed
	}

	if value := getQueryValue(values, "paper_width"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid paper_width")
		}
		options.PaperWidth = &parsed
	}

	if value := getQueryValue(values, "paper_height"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid paper_height")
		}
		options.PaperHeight = &parsed
	}

	if value := getQueryValue(values, "margin_top"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_top")
		}
		options.MarginTop = &parsed
	}

	if value := getQueryValue(values, "margin_bottom"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_bottom")
		}
		options.MarginBottom = &parsed
	}

	if value := getQueryValue(values, "margin_left"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_left")
		}
		options.MarginLeft = &parsed
	}

	if value := getQueryValue(values, "margin_right"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_right")
		}
		options.MarginRight = &parsed
	}

	if value := getQueryValue(values, "print_background"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return options, fmt.Errorf("invalid print_background")
		}
		options.PrintBackground = &parsed
	}

	options.PageRanges = getQueryValue(values, "page_ranges")

	return options, nil
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
