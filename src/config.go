// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"os"
	"strconv"
	"time"
)

func loadConfig() config {
	chromeWS := os.Getenv("CHROME_WS")
	chromeAutoStart := getEnvBool("CHROME_AUTO_START", true)
	if chromeWS != "" && chromeAutoStart {
		Warnf("CHROME_WS is set: disabling managed Chromium startup")
		chromeAutoStart = false
	}

	cfg := config{
		Addr:                  getEnv("ADDR", ":8080"),
		ChromeEndpoint:        getEnv("CHROME_ENDPOINT", "http://127.0.0.1:9222"),
		ChromeWS:              chromeWS,
		ChromeAutoStart:       chromeAutoStart,
		ChromeBinary:          os.Getenv("CHROME_BIN"),
		ChromeUserDataDir:     os.Getenv("CHROME_USER_DATA_DIR"),
		ChromeIdleTimeout:     getEnvDuration("CHROME_IDLE_TIMEOUT", defaultChromeIdleTimeout),
		ChromeStartupTimeout:  getEnvDuration("CHROME_STARTUP_TIMEOUT", defaultChromeStartupTimeout),
		ChromeShutdownTimeout: getEnvDuration("CHROME_SHUTDOWN_TIMEOUT", defaultChromeShutdownTimeout),
		RequestTimeout:        getEnvDuration("REQUEST_TIMEOUT", 30*time.Second),
		MaxBodyBytes:          getEnvInt64("MAX_BODY_BYTES", 5*1024*1024),
		PDFWait:               getEnvDuration("PDF_WAIT", 0),
		CDPPoolSize:           getEnvInt("CDP_POOL_SIZE", 4),
	}

	Infof("configuration loaded: %+v", cfg)

	return cfg
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
		Warnf("invalid %s, using default: %v", key, err)
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
		Warnf("invalid %s, using default: %v", key, err)
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		Warnf("invalid %s, using default: %v", key, err)
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		Warnf("invalid %s, using default: %v", key, err)
		return fallback
	}
	return parsed
}
