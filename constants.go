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

	// Response header.
	pdfFilename = "document.pdf"
)

type config struct {
	Addr           string
	ChromeEndpoint string
	ChromeWS       string
	RequestTimeout time.Duration
	MaxBodyBytes   int64
	PDFWait        time.Duration
}

type pdfOptions struct {
	Landscape       *bool
	Scale           *float64
	PaperWidth      *float64
	PaperHeight     *float64
	MarginTop       *float64
	MarginBottom    *float64
	MarginLeft      *float64
	MarginRight     *float64
	PrintBackground *bool
	PageRanges      string
}

type wsResolver interface {
	wsURL(ctx context.Context) (string, error)
}

type pdfRenderer func(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, time.Duration, error)

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}
