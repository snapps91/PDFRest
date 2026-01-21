// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"net/http"
	"time"
)

func main() {
	cfg := loadConfig()

	// Resolver: discovers Chrome websocket URL unless explicitly provided.
	resolver := newChromeResolver(cfg)

	// Router.
	mux := http.NewServeMux()
	mux.HandleFunc(pathPDF, pdfHandler(cfg, resolver, renderPDF))
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

	runServer(srv, cfg.Addr)
}
