// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"net/http"
	"os"
	"strings"
	"time"
)

func printVersion() {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		Warnf("unable to read VERSION file: %v", err)
		return
	}
	version := strings.TrimSpace(string(data))
	Infof("software version: %s", version)
}

func main() {
	// Print ASCII banner.
	printVersion()

	cfg := loadConfig()

	// Resolver: discovers Chrome websocket URL unless explicitly provided.
	resolver := newChromeResolver(cfg)
	pool := newSessionPool(cfg.CDPPoolSize)
	rendererPdf := newPDFRenderer(pool)

	// Router.
	mux := http.NewServeMux()
	mux.HandleFunc(pathPDF, pdfHandler(cfg, resolver, rendererPdf))
	mux.HandleFunc(pathHealthz, healthHandler(resolver))

	// Server with sane defaults. Note: WriteTimeout is set to (request timeout + small buffer),
	// so handlers can use the full configured RequestTimeout.
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       defaultIdleTimeout,
	}

	// Start server.
	runServer(srv, cfg.Addr)
}
