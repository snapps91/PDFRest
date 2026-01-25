// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

// Package main implements a client for the Chrome DevTools Protocol (CDP).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

const (
	cdpPollInterval = 100 * time.Millisecond
)

// cdpClient manages the connection to the Chrome DevTools Protocol.
// It holds the necessary fields for communication with the CDP.
type cdpClient struct {
	conn   net.Conn
	rw     io.ReadWriter
	nextID int64
	mu     sync.Mutex
	br     *bufio.Reader
}

// cdpRequest represents a request sent to the Chrome DevTools Protocol.
// It contains the method and parameters for the request.
type cdpRequest struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

// cdpResponse represents a response received from the Chrome DevTools Protocol.
// It includes the result and any errors that occurred during the request.
type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
	// cdpError represents an error response from the Chrome DevTools Protocol.
	// It contains the error code and message.
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpReadWriter struct {
	r io.Reader
	w io.Writer
}

func (rw *cdpReadWriter) Read(p []byte) (int, error) {
	return rw.r.Read(p)
}

func (rw *cdpReadWriter) Write(p []byte) (int, error) {
	return rw.w.Write(p)
}

// newCDPClient establishes a new WebSocket connection to the specified URL
// and returns a pointer to a cdpClient instance along with any error encountered.
//
// Parameters:
//   - ctx: A context.Context to control the lifetime of the connection.
//   - wsURL: A string representing the WebSocket URL to connect to.
//
// Returns:
//   - A pointer to a cdpClient if the connection is successful.
//   - An error if the connection fails.
func newCDPClient(ctx context.Context, wsURL string) (*cdpClient, error) {
	conn, br, _, err := ws.Dial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	rw := io.ReadWriter(conn)
	if br != nil {
		rw = &cdpReadWriter{r: br, w: conn}
	}
	return &cdpClient{conn: conn, rw: rw, br: br}, nil
}

// Close terminates the WebSocket connection and cleans up resources.
func (c *cdpClient) Close() error {
	err := c.conn.Close()
	if c.br != nil {
		ws.PutReader(c.br)
	}
	return err
}

// Call sends a single Chrome DevTools Protocol (CDP) request and blocks until the
// matching response is received or ctx is canceled.
//
// The request is issued with a monotonically increasing internal ID and includes
// the provided sessionID (if non-empty), method name, and params. The call is
// serialized with a mutex to ensure only one in-flight request/response exchange
// occurs at a time on the underlying transport.
//
// While waiting, Call reads messages and ignores those that are not responses
// (resp.ID == 0) or whose ID does not match the current request. If the response
// contains a protocol error, Call returns it as a formatted Go error.
//
// If result is non-nil and the response includes a non-empty Result payload,
// Call unmarshals the payload into result.
//
// Returns any marshaling, transport read/write, unmarshaling, context, or CDP
// protocol error encountered.
func (c *cdpClient) Call(ctx context.Context, sessionID, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := atomic.AddInt64(&c.nextID, 1)
	req := cdpRequest{
		ID:        id,
		Method:    method,
		Params:    params,
		SessionID: sessionID,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if err := c.write(ctx, payload); err != nil {
		return err
	}

	for {
		msg, err := c.read(ctx)
		if err != nil {
			return err
		}
		var resp cdpResponse
		if err := json.Unmarshal(msg, &resp); err != nil {
			return err
		}
		if resp.ID == 0 {
			continue
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("cdp %s error %d: %s", method, resp.Error.Code, resp.Error.Message)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return err
			}
		}
		return nil
	}
}

// read reads the next complete WebSocket message payload from the server.
//
// The call respects ctx for cancellation and deadlines. If ctx has a deadline,
// the underlying connection read deadline is set accordingly; otherwise a short
// polling deadline (cdpPollInterval) is used so the method can periodically
// re-check ctx.
//
// On a successful read, it returns the message payload bytes. If a read times
// out and ctx is still active, it retries. Any non-timeout read error, or any
// context cancellation/deadline error, is returned.
func (c *cdpClient) read(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if deadline, ok := ctx.Deadline(); ok {
			if err := c.conn.SetReadDeadline(deadline); err != nil {
				return nil, err
			}
		} else {
			if err := c.conn.SetReadDeadline(time.Now().Add(cdpPollInterval)); err != nil {
				return nil, err
			}
		}
		data, _, err := wsutil.ReadServerData(c.rw)
		if err == nil {
			return data, nil
		}
		if isTimeout(err) && ctx.Err() == nil {
			continue
		}
		return nil, err
	}
}

// write sends a payload to the CDP server over the WebSocket connection.
// It respects the context deadline if one is set and returns an error if the
// context is already cancelled or if the write operation fails.
func (c *cdpClient) write(ctx context.Context, payload []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
	}
	return wsutil.WriteClientText(c.rw, payload)
	// isTimeout checks if the provided error is a timeout error, returning true if it is.
}

func isTimeout(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func isPageWebSocket(wsURL string) bool {
	return strings.Contains(wsURL, "/devtools/page/")
}

// openTargetSession creates a new target and attaches to it, returning the session ID and target ID.
// It first creates a target with a blank URL using Target.createTarget, then attaches to the created
// target using Target.attachToTarget with flattening enabled. Returns an error if target ID or session ID
// is missing or if either CDP protocol call fails.
func openTargetSession(ctx context.Context, client *cdpClient) (string, string, error) {
	var created struct {
		TargetID string `json:"targetId"`
	}
	if err := client.Call(ctx, "", "Target.createTarget", map[string]any{
		"url": "about:blank",
	}, &created); err != nil {
		return "", "", err
	}
	if created.TargetID == "" {
		return "", "", errors.New("cdp target id missing")
	}

	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := client.Call(ctx, "", "Target.attachToTarget", map[string]any{
		"targetId": created.TargetID,
		"flatten":  true,
	}, &attached); err != nil {
		return "", "", err
	}
	if attached.SessionID == "" {
		return "", "", errors.New("cdp session id missing")
	}

	return attached.SessionID, created.TargetID, nil
}

// closeTarget closes a target with the given targetID using the Chrome DevTools Protocol.
// If targetID is empty, it returns nil without making any API call.
// It returns an error if the Target.closeTarget RPC call fails.
func closeTarget(ctx context.Context, client *cdpClient, targetID string) error {
	if targetID == "" {
		return nil
	}
	return client.Call(ctx, "", "Target.closeTarget", map[string]any{
		"targetId": targetID,
	}, nil)
}

// waitForBody polls the client at regular intervals to check if the document body
// is ready. It returns nil once the body is available, or an error if the context
// is cancelled or if checking the body fails.
func waitForBody(ctx context.Context, client *cdpClient, sessionID string) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		ready, err := hasBody(ctx, client, sessionID)
		if err != nil {
			return err
		}
		if ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// hasBody checks whether the DOM document contains a body element.
// It first retrieves the root document node, then queries for a body element
// within that root. Returns true if a body element is found, false otherwise.
// Returns an error if any DOM operation fails.
func hasBody(ctx context.Context, client *cdpClient, sessionID string) (bool, error) {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := client.Call(ctx, sessionID, "DOM.getDocument", map[string]any{
		"depth": 1,
	}, &doc); err != nil {
		return false, err
	}
	if doc.Root.NodeID == 0 {
		return false, nil
	}

	var query struct {
		NodeID int `json:"nodeId"`
	}
	if err := client.Call(ctx, sessionID, "DOM.querySelector", map[string]any{
		"nodeId":   doc.Root.NodeID,
		"selector": "body",
	}, &query); err != nil {
		return false, err
	}
	return query.NodeID != 0, nil
}
