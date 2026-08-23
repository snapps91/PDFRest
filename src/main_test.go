// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestParsePDFOptionsValid(t *testing.T) {
	values := url.Values{
		"landscape":                 []string{"true"},
		"display_header_footer":     []string{"true"},
		"scale":                     []string{"0.9"},
		"paper_width":               []string{"8.27"},
		"paper_height":              []string{"11.69"},
		"margin_top":                []string{"0.4"},
		"margin_bottom":             []string{"0.5"},
		"margin_left":               []string{"0.6"},
		"margin_right":              []string{"0.7"},
		"print_background":          []string{"false"},
		"page_ranges":               []string{"1-2, 4"},
		"header_template":           []string{`<span class="title"></span>`},
		"footer_template":           []string{`<span class="pageNumber"></span>`},
		"prefer_css_page_size":      []string{"true"},
		"generate_tagged_pdf":       []string{"true"},
		"generate_document_outline": []string{"true"},
		"omit_background":           []string{"true"},
		"wait_for_fonts":            []string{"true"},
	}

	opts, err := parsePDFOptions(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.Landscape == nil || *opts.Landscape != true {
		t.Fatalf("expected landscape=true, got %#v", opts.Landscape)
	}
	if opts.DisplayHeaderFooter == nil || *opts.DisplayHeaderFooter != true {
		t.Fatalf("expected display_header_footer=true, got %#v", opts.DisplayHeaderFooter)
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
	if opts.PageRanges != "1-2, 4" {
		t.Fatalf("expected page_ranges=1-2, 4, got %q", opts.PageRanges)
	}
	if opts.HeaderTemplate != `<span class="title"></span>` || opts.FooterTemplate != `<span class="pageNumber"></span>` {
		t.Fatalf("unexpected header or footer templates: %q / %q", opts.HeaderTemplate, opts.FooterTemplate)
	}
	if opts.PreferCSSPageSize == nil || !*opts.PreferCSSPageSize {
		t.Fatalf("expected prefer_css_page_size=true")
	}
	if opts.GenerateTaggedPDF == nil || !*opts.GenerateTaggedPDF {
		t.Fatalf("expected generate_tagged_pdf=true")
	}
	if opts.GenerateDocumentOutline == nil || !*opts.GenerateDocumentOutline {
		t.Fatalf("expected generate_document_outline=true")
	}
	if opts.OmitBackground == nil || !*opts.OmitBackground {
		t.Fatalf("expected omit_background=true")
	}
	if opts.WaitForFonts == nil || !*opts.WaitForFonts {
		t.Fatalf("expected wait_for_fonts=true")
	}
}

func TestParsePDFOptionsUnits(t *testing.T) {
	values := url.Values{
		"paper_width":  []string{"210mm"},
		"paper_height": []string{"1024px"},
		"margin_top":   []string{"1.27cm"},
	}

	opts, err := parsePDFOptions(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if opts.PaperWidth == nil || !almostEqual(*opts.PaperWidth, 8.2677, 0.01) {
		t.Fatalf("expected paper_width about 8.27in, got %#v", opts.PaperWidth)
	}
	if opts.PaperHeight == nil || !almostEqual(*opts.PaperHeight, 10.6667, 0.01) {
		t.Fatalf("expected paper_height about 10.67in, got %#v", opts.PaperHeight)
	}
	if opts.MarginTop == nil || !almostEqual(*opts.MarginTop, 0.5, 0.001) {
		t.Fatalf("expected margin_top=0.5in, got %#v", opts.MarginTop)
	}
}

func TestParsePDFOptionsPaperFormatTakesPrecedence(t *testing.T) {
	opts, err := parsePDFOptions(url.Values{
		"paper_format": []string{"A4"},
		"paper_width":  []string{"1in"},
		"paper_height": []string{"2in"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.PaperWidth == nil || !almostEqual(*opts.PaperWidth, 8.2677, 0.0001) {
		t.Fatalf("expected A4 width, got %#v", opts.PaperWidth)
	}
	if opts.PaperHeight == nil || !almostEqual(*opts.PaperHeight, 11.6929, 0.0001) {
		t.Fatalf("expected A4 height, got %#v", opts.PaperHeight)
	}
}

func TestParsePDFOptionsInvalid(t *testing.T) {
	cases := []struct {
		name   string
		values url.Values
	}{
		{name: "landscape", values: url.Values{"landscape": []string{"nope"}}},
		{name: "scale", values: url.Values{"scale": []string{"abc"}}},
		{name: "scale_too_small", values: url.Values{"scale": []string{"0.09"}}},
		{name: "scale_too_large", values: url.Values{"scale": []string{"2.01"}}},
		{name: "scale_not_finite", values: url.Values{"scale": []string{"NaN"}}},
		{name: "paper_width", values: url.Values{"paper_width": []string{"x"}}},
		{name: "paper_width_zero", values: url.Values{"paper_width": []string{"0"}}},
		{name: "paper_height", values: url.Values{"paper_height": []string{"x"}}},
		{name: "paper_width_units", values: url.Values{"paper_width": []string{"1qq"}}},
		{name: "paper_height_units", values: url.Values{"paper_height": []string{"1qq"}}},
		{name: "margin_top", values: url.Values{"margin_top": []string{"x"}}},
		{name: "margin_bottom", values: url.Values{"margin_bottom": []string{"x"}}},
		{name: "margin_left", values: url.Values{"margin_left": []string{"x"}}},
		{name: "margin_right", values: url.Values{"margin_right": []string{"x"}}},
		{name: "margin_negative", values: url.Values{"margin_right": []string{"-1"}}},
		{name: "print_background", values: url.Values{"print_background": []string{"x"}}},
		{name: "display_header_footer", values: url.Values{"display_header_footer": []string{"x"}}},
		{name: "prefer_css_page_size", values: url.Values{"prefer_css_page_size": []string{"x"}}},
		{name: "generate_tagged_pdf", values: url.Values{"generate_tagged_pdf": []string{"x"}}},
		{name: "generate_document_outline", values: url.Values{"generate_document_outline": []string{"x"}}},
		{name: "omit_background", values: url.Values{"omit_background": []string{"x"}}},
		{name: "wait_for_fonts", values: url.Values{"wait_for_fonts": []string{"x"}}},
		{name: "paper_format", values: url.Values{"paper_format": []string{"A7"}}},
		{name: "page_ranges_zero", values: url.Values{"page_ranges": []string{"0"}}},
		{name: "page_ranges_reverse", values: url.Values{"page_ranges": []string{"5-2"}}},
		{name: "page_ranges_malformed", values: url.Values{"page_ranges": []string{"1--2"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parsePDFOptions(tc.values); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestBuildPrintToPDFParams(t *testing.T) {
	trueValue := true
	falseValue := false
	scale := 1.25
	options := pdfOptions{
		Landscape:               &trueValue,
		DisplayHeaderFooter:     &trueValue,
		PrintBackground:         &falseValue,
		Scale:                   &scale,
		PageRanges:              "1-3",
		HeaderTemplate:          "<span>header</span>",
		FooterTemplate:          "<span>footer</span>",
		PreferCSSPageSize:       &trueValue,
		GenerateTaggedPDF:       &trueValue,
		GenerateDocumentOutline: &trueValue,
	}

	params := buildPrintToPDFParams(options)
	if params.TransferMode != "ReturnAsStream" {
		t.Fatalf("expected streamed transfer, got %q", params.TransferMode)
	}
	if params.DisplayHeaderFooter == nil || !*params.DisplayHeaderFooter {
		t.Fatal("displayHeaderFooter was not forwarded")
	}
	if params.PrintBackground == nil || *params.PrintBackground {
		t.Fatal("printBackground=false was not forwarded")
	}
	if params.HeaderTemplate != options.HeaderTemplate || params.FooterTemplate != options.FooterTemplate {
		t.Fatal("header/footer templates were not forwarded")
	}
	if params.PreferCSSPageSize == nil || !*params.PreferCSSPageSize || params.GenerateTaggedPDF == nil || !*params.GenerateTaggedPDF || params.GenerateDocumentOutline == nil || !*params.GenerateDocumentOutline {
		t.Fatal("modern Chrome PDF options were not forwarded")
	}
	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	var protocolParams map[string]any
	if err := json.Unmarshal(payload, &protocolParams); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	for _, key := range []string{"displayHeaderFooter", "headerTemplate", "footerTemplate", "preferCSSPageSize", "generateTaggedPDF", "generateDocumentOutline"} {
		if _, ok := protocolParams[key]; !ok {
			t.Fatalf("missing Chrome protocol parameter %q in %s", key, payload)
		}
	}

	defaults := buildPrintToPDFParams(pdfOptions{})
	if defaults.PrintBackground == nil || !*defaults.PrintBackground {
		t.Fatal("existing print_background=true default changed")
	}
	if defaults.GenerateTaggedPDF != nil || defaults.GenerateDocumentOutline != nil {
		t.Fatal("experimental Chrome options must remain opt-in")
	}
}

func almostEqual(got, want, tolerance float64) bool {
	return math.Abs(got-want) <= tolerance
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
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		return nil, 0, nil
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
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		return nil, 0, nil
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
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		return nil, 0, nil
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
	handler := pdfHandler(cfg, stubResolver{err: errors.New("no chrome")}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		return nil, 0, nil
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
	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		return nil, 0, errors.New("render failed")
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

	handler := pdfHandler(cfg, stubResolver{ws: "ws://example"}, func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error) {
		if wsURL != "ws://example" {
			t.Fatalf("unexpected wsURL: %s", wsURL)
		}
		if html != "<html></html>" {
			t.Fatalf("unexpected html: %s", html)
		}
		if options.Landscape == nil || *options.Landscape != true {
			t.Fatalf("expected landscape option to be true")
		}
		return expected, 0, nil
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
