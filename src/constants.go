// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"time"
)

const (
	// API paths.
	pathPDF     = "/api/v1/pdf"
	pathHealthz = "/healthz"

	// Default server-level timeouts.
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultIdleTimeout       = 60 * time.Second

	// Shutdown timeout (graceful).
	defaultShutdownTimeout = 10 * time.Second

	// Client timeout for the Chrome /json/version endpoint.
	defaultChromeClientTimeout = 5 * time.Second

	// Cache TTL for Chrome websocket discovery.
	defaultWSTTL = 1 * time.Minute

	// Managed Chromium lifecycle defaults.
	defaultChromeIdleTimeout     = 5 * time.Minute
	defaultChromeStartupTimeout  = 10 * time.Second
	defaultChromeShutdownTimeout = 5 * time.Second

	// Response header.
	pdfFilename = "document.pdf"
)

type config struct {
	Addr                  string
	ChromeEndpoint        string
	ChromeWS              string
	ChromeAutoStart       bool
	ChromeBinary          string
	ChromeUserDataDir     string
	ChromeIdleTimeout     time.Duration
	ChromeStartupTimeout  time.Duration
	ChromeShutdownTimeout time.Duration
	RequestTimeout        time.Duration
	MaxBodyBytes          int64
	PDFWait               time.Duration
	CDPPoolSize           int
}

type pdfOptions struct {
	Landscape               *bool
	DisplayHeaderFooter     *bool
	PrintBackground         *bool
	Scale                   *float64
	PaperWidth              *float64
	PaperHeight             *float64
	MarginTop               *float64
	MarginBottom            *float64
	MarginLeft              *float64
	MarginRight             *float64
	PageRanges              string
	HeaderTemplate          string
	FooterTemplate          string
	PreferCSSPageSize       *bool
	GenerateTaggedPDF       *bool
	GenerateDocumentOutline *bool
	OmitBackground          *bool
	WaitForFonts            *bool
}

type wsResolver interface {
	wsURL(ctx context.Context) (string, error)
}

type pdfRenderer func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error)

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}
