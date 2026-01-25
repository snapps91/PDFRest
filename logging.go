// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
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

		level, msg := parseLogLine(line)
		payload := map[string]string{
			"time":  time.Now().Format(time.RFC3339Nano),
			"level": level,
			"msg":   msg,
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

func parseLogLine(line string) (string, string) {
	level := "info"
	msg := line
	if !strings.HasPrefix(line, "level=") {
		return level, msg
	}
	splitAt := strings.IndexByte(line, ' ')
	if splitAt == -1 {
		return strings.TrimPrefix(line, "level="), ""
	}
	level = strings.TrimPrefix(line[:splitAt], "level=")
	msg = strings.TrimSpace(line[splitAt+1:])
	return level, msg
}

func logf(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("level=%s %s", level, msg)
}

func Infof(format string, args ...any) {
	logf("info", format, args...)
}

func Warnf(format string, args ...any) {
	logf("warning", format, args...)
}

func Errorf(format string, args ...any) {
	logf("error", format, args...)
}

func Debugf(format string, args ...any) {
	logf("debug", format, args...)
}

// loggingMiddleware logs method/path/status/duration.
// It wraps the ResponseWriter to capture the status code.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		if r.URL.Path == pathPDF && r.Method == http.MethodPost {
			requestID := newRequestID()
			pdfTime := "-"
			if rw.pdfTimeSet {
				pdfTime = rw.pdfTime.String()
			}
			Infof("%s %s %d %s request_id=%s PDF_TIME=%s", r.Method, r.URL.Path, rw.status, time.Since(start), requestID, pdfTime)
			return
		}

		Infof("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

func newRequestID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}

type responseWriter struct {
	http.ResponseWriter
	status     int
	pdfTime    time.Duration
	pdfTimeSet bool
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}
