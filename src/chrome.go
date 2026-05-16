// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// chromeResolver resolves the remote Chrome DevTools websocket URL.
// It supports:
// - Explicit websocket URL via env (CHROME_WS)
// - Discovery via /json/version on the Chrome endpoint, with caching
type chromeResolver struct {
	endpoint string
	ws       string
	client   *http.Client

	mu       sync.Mutex
	cachedWS string
	cachedAt time.Time
	cacheTTL time.Duration
}

func newChromeResolver(cfg config) *chromeResolver {
	return &chromeResolver{
		endpoint: cfg.ChromeEndpoint,
		ws:       cfg.ChromeWS,
		client: &http.Client{
			Timeout: defaultChromeClientTimeout,
		},
		cacheTTL: defaultWSTTL,
	}
}

// wsURL returns the Chrome DevTools websocket URL.
// If CHROME_WS is configured, it is returned directly.
// Otherwise, it discovers it via /json/version and caches the result.
func (c *chromeResolver) wsURL(ctx context.Context) (string, error) {
	// Explicit override always wins.
	if c.ws != "" {
		return c.ws, nil
	}

	// Fast-path cache (locked).
	if ws := c.getCachedWS(); ws != "" {
		return ws, nil
	}

	// Discover via /json/version.
	endpoint := fmt.Sprintf("%s/json/version", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			Warnf("chrome version body close error: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected chrome status: %s", resp.Status)
	}

	var payload versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if payload.WebSocketDebuggerURL == "" {
		return "", errors.New("missing websocket debugger url")
	}

	// Store in cache.
	c.setCachedWS(payload.WebSocketDebuggerURL)

	return payload.WebSocketDebuggerURL, nil
}

// checkChrome verifies connectivity to Chrome without relying on cached websocket values.
func (c *chromeResolver) checkChrome(ctx context.Context) error {
	if c.ws != "" {
		client, err := newCDPClient(ctx, c.ws)
		if err != nil {
			return err
		}
		defer func() {
			if err := client.Close(); err != nil {
				Warnf("chrome websocket close error: %v", err)
			}
		}()

		var version struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		return client.Call(ctx, "", "Browser.getVersion", nil, &version)
	}

	endpoint := fmt.Sprintf("%s/json/version", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			Warnf("chrome health body close error: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected chrome status: %s", resp.Status)
	}

	var payload versionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return err
	}
	if payload.WebSocketDebuggerURL == "" {
		return errors.New("missing websocket debugger url")
	}

	c.setCachedWS(payload.WebSocketDebuggerURL)
	return nil
}

// getCachedWS returns the cached websocket URL if still valid.
func (c *chromeResolver) getCachedWS() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cachedWS == "" {
		return ""
	}
	if time.Since(c.cachedAt) >= c.cacheTTL {
		return ""
	}
	return c.cachedWS
}

// setCachedWS updates cache atomically.
func (c *chromeResolver) setCachedWS(ws string) {
	c.mu.Lock()
	c.cachedWS = ws
	c.cachedAt = time.Now()
	c.mu.Unlock()
}
