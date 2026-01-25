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

func printBanner() {
	banner :=
		`
░█▀▀░█▀█░█▀█░█▀█░█▀█░█░█░░░░░░░░░█▀█░█▀▄░█▀▀░█▀▄░█▀▀░█▀▀░▀█▀
░▀▀█░█░█░█▀█░█▀▀░█▀▀░░█░░░░▄▄▄░░░█▀▀░█░█░█▀▀░█▀▄░█▀▀░▀▀█░░█░
░▀▀▀░▀░▀░▀░▀░▀░░░▀░░░░▀░░░░░░░░░░▀░░░▀▀░░▀░░░▀░▀░▀▀▀░▀▀▀░░▀░
░█▄█░▀█▀░█▀▀░█▀▄░█▀█░░░█▀▀░█▀▀░█▀▄░█░█░▀█▀░█▀▀░█▀▀          
░█░█░░█░░█░░░█▀▄░█░█░░░▀▀█░█▀▀░█▀▄░▀▄▀░░█░░█░░░█▀▀          
░▀░▀░▀▀▀░▀▀▀░▀░▀░▀▀▀░░░▀▀▀░▀▀▀░▀░▀░░▀░░▀▀▀░▀▀▀░▀▀▀          
`
	println(banner)
}

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
	// Print ASCII banner.
	printBanner()
	printVersion()

	// Start server.
	runServer(srv, cfg.Addr)
}
