// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

type jsonLogWriter struct {
	out io.Writer
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write writes the given byte slice p to the jsonLogWriter.
// It locks the writer to ensure thread safety, then attempts to
// write the data to an internal buffer. It processes the buffer
// line by line, encoding each line as a JSON object with a timestamp,
// log level, and message. If a line is successfully processed, it
// is written to the output. The function returns the number of bytes
// written and any error encountered during the process.
func (w *jsonLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.buf.Write(p); err != nil {
		return len(p), err
	}

	for {
		data := w.buf.Bytes()
		idx := bytes.IndexByte(data, '\n')
		if idx == -1 {
			break
		}

		line := string(data[:idx])
		w.buf.Next(idx + 1)
		if line == "" {
			continue
		}

		payload := map[string]string{
			"time":  time.Now().Format(time.RFC3339Nano),
			"level": "info",
			"msg":   line,
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return len(p), err
		}
		if _, err := w.out.Write(append(encoded, '\n')); err != nil {
			return len(p), err
		}
	}

	return len(p), nil
}

func init() {
	log.SetFlags(0)
	log.SetOutput(&jsonLogWriter{out: os.Stderr})
}

// loggingMiddleware logs method/path/status/duration.
// It wraps the ResponseWriter to capture the status code.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}
