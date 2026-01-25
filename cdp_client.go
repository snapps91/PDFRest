// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

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

type cdpClient struct {
	conn   net.Conn
	rw     io.ReadWriter
	nextID int64
	mu     sync.Mutex
	br     *bufio.Reader
}

type cdpRequest struct {
	ID        int64  `json:"id"`
	Method    string `json:"method"`
	Params    any    `json:"params,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
}

type cdpResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
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

func (c *cdpClient) Close() error {
	err := c.conn.Close()
	if c.br != nil {
		ws.PutReader(c.br)
	}
	return err
}

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

func closeTarget(ctx context.Context, client *cdpClient, targetID string) error {
	if targetID == "" {
		return nil
	}
	return client.Call(ctx, "", "Target.closeTarget", map[string]any{
		"targetId": targetID,
	}, nil)
}

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
