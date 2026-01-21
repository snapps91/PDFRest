// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"log"
	"os"
	"strconv"
	"time"
)

func loadConfig() config {
	return config{
		Addr:           getEnv("ADDR", ":8080"),
		ChromeEndpoint: getEnv("CHROME_ENDPOINT", "http://127.0.0.1:9222"),
		ChromeWS:       os.Getenv("CHROME_WS"),
		RequestTimeout: getEnvDuration("REQUEST_TIMEOUT", 30*time.Second),
		MaxBodyBytes:   getEnvInt64("MAX_BODY_BYTES", 5*1024*1024),
		PDFWait:        getEnvDuration("PDF_WAIT", 0),
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		log.Printf("invalid %s, using default: %v", key, err)
		return fallback
	}
	return parsed
}

func getEnvInt64(key string, fallback int64) int64 {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		log.Printf("invalid %s, using default: %v", key, err)
		return fallback
	}
	return parsed
}
