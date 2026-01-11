// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePDFOptionsValid(t *testing.T) {
	values := url.Values{
		"landscape":        []string{"true"},
		"scale":            []string{"0.9"},
		"paper_width":      []string{"8.27"},
		"paper_height":     []string{"11.69"},
		"margin_top":       []string{"0.4"},
		"margin_bottom":    []string{"0.5"},
		"margin_left":      []string{"0.6"},
		"margin_right":     []string{"0.7"},
		"print_background": []string{"false"},
		"page_ranges":      []string{"1-2,4"},
	}

	opts, err := parsePDFOptions(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.Landscape == nil || *opts.Landscape != true {
		t.Fatalf("expected landscape=true, got %#v", opts.Landscape)
	}
	if opts.Scale == nil || *opts.Scale != 0.9 {
		t.Fatalf("expected scale=0.9, got %#v", opts.Scale)
	}
	if opts.PaperWidth == nil || *opts.PaperWidth != 8.27 {
		t.Fatalf("expected paper_width=8.27, got %#v", opts.PaperWidth)
	}
	if opts.PaperHeight == nil || *opts.PaperHeight != 11.69 {
		t.Fatalf("expected paper_height=11.69, got %#v", opts.PaperHeight)
	}
	if opts.MarginTop == nil || *opts.MarginTop != 0.4 {
		t.Fatalf("expected margin_top=0.4, got %#v", opts.MarginTop)
	}
	if opts.MarginBottom == nil || *opts.MarginBottom != 0.5 {
		t.Fatalf("expected margin_bottom=0.5, got %#v", opts.MarginBottom)
	}
	if opts.MarginLeft == nil || *opts.MarginLeft != 0.6 {
		t.Fatalf("expected margin_left=0.6, got %#v", opts.MarginLeft)
	}
	if opts.MarginRight == nil || *opts.MarginRight != 0.7 {
		t.Fatalf("expected margin_right=0.7, got %#v", opts.MarginRight)
	}
	if opts.PrintBackground == nil || *opts.PrintBackground != false {
		t.Fatalf("expected print_background=false, got %#v", opts.PrintBackground)
	}
	if opts.PageRanges != "1-2,4" {
		t.Fatalf("expected page_ranges=1-2,4, got %q", opts.PageRanges)
	}
}

func TestParsePDFOptionsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		values url.Values
	}{
		{name: "landscape", values: url.Values{"landscape": []string{"nope"}}},
		{name: "scale", values: url.Values{"scale": []string{"abc"}}},
		{name: "paper_width", values: url.Values{"paper_width": []string{"x"}}},
		{name: "paper_height", values: url.Values{"paper_height": []string{"x"}}},
		{name: "margin_top", values: url.Values{"margin_top": []string{"x"}}},
		{name: "margin_bottom", values: url.Values{"margin_bottom": []string{"x"}}},
		{name: "margin_left", values: url.Values{"margin_left": []string{"x"}}},
		{name: "margin_right", values: url.Values{"margin_right": []string{"x"}}},
		{name: "print_background", values: url.Values{"print_background": []string{"x"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePDFOptions(tc.values); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestMapBodyReadErrorToStatus(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "body closed", err: http.ErrBodyReadAfterClose, want: http.StatusBadRequest},
		{name: "eof", err: io.EOF, want: http.StatusBadRequest},
		{name: "max bytes", err: &http.MaxBytesError{}, want: http.StatusRequestEntityTooLarge},
		{name: "generic", err: errors.New("boom"), want: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapBodyReadErrorToStatus(tt.err); got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}
}

type stubResolver struct {
	ws  string
	err error
}

func (s stubResolver) wsURL(_ context.Context) (string, error) {
	return s.ws, s.err
}

func TestPDFHandlerMethodNotAllowed(t *testing.T) {
	cfg := config{RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024}
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/pdf", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Result().StatusCode)
	}
}

func TestPDFHandlerEmptyBody(t *testing.T) {
	cfg := config{RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024}
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdf", strings.NewReader(""))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Result().StatusCode)
	}
}

func TestPDFHandlerInvalidOptions(t *testing.T) {
	cfg := config{RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024}
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdf?scale=oops", strings.NewReader("<html></html>"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Result().StatusCode)
	}
}

func TestPDFHandlerResolverError(t *testing.T) {
	cfg := config{RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024}
	handler := pdfHandler(cfg, stubResolver{err: errors.New("no chrome")}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
		return nil, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdf", strings.NewReader("<html></html>"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Result().StatusCode)
	}
}

func TestPDFHandlerRenderError(t *testing.T) {
	cfg := config{RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024}
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
		return nil, errors.New("render failed")
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdf", strings.NewReader("<html></html>"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Result().StatusCode)
	}
}

func TestPDFHandlerSuccess(t *testing.T) {
	cfg := config{RequestTimeout: 2 * time.Second, MaxBodyBytes: 1024}
	expected := []byte("%PDF-1.7")

	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
		if wsURL != "ws://example" {
			t.Fatalf("unexpected wsURL: %s", wsURL)
		}
		if html != "<html></html>" {
			t.Fatalf("unexpected html: %s", html)
		}
		if options.Landscape == nil || *options.Landscape != true {
			t.Fatalf("expected landscape option to be true")
		}
		return expected, nil
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/pdf?landscape=true", strings.NewReader("<html></html>"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Result().StatusCode)
	}
	if ct := rec.Result().Header.Get("Content-Type"); ct != "application/pdf" {
		t.Fatalf("unexpected content type: %s", ct)
	}
	if disp := rec.Result().Header.Get("Content-Disposition"); disp == "" {
		t.Fatalf("missing Content-Disposition")
	}
	if got := rec.Body.Bytes(); string(got) != string(expected) {
		t.Fatalf("unexpected body: %q", string(got))
	}
	if rec.Result().Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("missing X-Content-Type-Options")
	}
	if rec.Result().Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("missing Cache-Control")
	}
}
