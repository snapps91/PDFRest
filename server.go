// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func runServer(srv *http.Server, addr string) {
	serverErr := make(chan error, 1)

	go func() {
		log.Printf("listening on %s", addr)
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
		log.Printf("shutting down on signal: %s", sig)
	case err := <-serverErr:
		log.Printf("server error: %v", err)
	}

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
