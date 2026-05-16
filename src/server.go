// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// runServer starts the provided HTTP server and blocks until it receives either:
//   - an OS interrupt/termination signal (SIGINT, SIGTERM), or
//   - a non-graceful server error from ListenAndServe.
//
// It logs the listening address, then attempts a graceful shutdown using
// srv.Shutdown with a timeout defined by defaultShutdownTimeout.
func runServer(srv *http.Server, addr string) {
	serverErr := make(chan error, 1)

	go func() {
		Infof("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Listen for OS signals.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Block until signal or server error.
	select {
	case sig := <-stop:
		Infof("shutting down on signal: %s", sig)
	case err := <-serverErr:
		Errorf("server error: %v", err)
	}

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		Errorf("shutdown error: %v", err)
	}
}
