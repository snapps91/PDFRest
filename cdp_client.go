// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

// Package main implements a client for the Chrome DevTools Protocol (CDP).
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	cdpPollInterval = 100 * time.Millisecond
)

// cdpClient manages the connection to the Chrome DevTools Protocol.
// It holds the necessary fields for communication with the CDP.
type cdpClient struct {
	conn   net.Conn
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

const websocketMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

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
	conn, br, err := dialWebSocket(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	return &cdpClient{conn: conn, br: br}, nil
}

// Close terminates the WebSocket connection and cleans up resources.
func (c *cdpClient) Close() error {
	return c.conn.Close()
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
		data, err := c.readMessage()
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
	return c.writeTextMessage(payload)
	// isTimeout checks if the provided error is a timeout error, returning true if it is.
}

func isTimeout(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout()
	}
	return false
}

func dialWebSocket(ctx context.Context, wsURL string) (net.Conn, *bufio.Reader, error) {
	parsed, err := url.Parse(wsURL)
	if err != nil {
		return nil, nil, err
	}
	if parsed.Scheme != "ws" && parsed.Scheme != "wss" {
		return nil, nil, fmt.Errorf("unsupported websocket scheme: %s", parsed.Scheme)
	}

	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "wss" {
			port = "443"
		} else {
			port = "80"
		}
	}
	address := net.JoinHostPort(host, port)

	dialer := &net.Dialer{}
	rawConn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, nil, err
	}

	var conn net.Conn = rawConn
	if parsed.Scheme == "wss" {
		tlsConn := tls.Client(rawConn, &tls.Config{ServerName: host})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			rawConn.Close()
			return nil, nil, err
		}
		conn = tlsConn
	}

	key, err := generateWebSocketKey()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, wsURL, nil)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if parsed.Port() == "" {
		req.Host = host
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", key)

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			conn.Close()
			return nil, nil, err
		}
	}

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, nil, err
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, fmt.Errorf("websocket handshake failed: %s", resp.Status)
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Connection")), "upgrade") {
		conn.Close()
		return nil, nil, errors.New("websocket handshake failed: missing connection upgrade")
	}
	if !strings.Contains(strings.ToLower(resp.Header.Get("Upgrade")), "websocket") {
		conn.Close()
		return nil, nil, errors.New("websocket handshake failed: missing upgrade websocket")
	}
	expectedAccept := computeWebSocketAccept(key)
	if resp.Header.Get("Sec-WebSocket-Accept") != expectedAccept {
		conn.Close()
		return nil, nil, errors.New("websocket handshake failed: invalid accept key")
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		conn.Close()
		return nil, nil, err
	}

	return conn, br, nil
}

func generateWebSocketKey() (string, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

func computeWebSocketAccept(key string) string {
	h := sha1.New()
	_, _ = io.WriteString(h, key+websocketMagicGUID)
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// readMessage reads and assembles a complete WebSocket message from the connection.
// It handles fragmented messages by reading individual frames and concatenating their payloads.
// It also processes control frames (ping/pong) and validates frame sequences.
//
// Returns the assembled message as a byte slice, or an error if:
// - A frame read fails
// - A continuation frame (0x0) is received without a preceding data frame
// - A data frame (0x1, 0x2) is received while a continuation is pending
// - An unsupported opcode is encountered
// - The connection is closed (0x8 opcode returns io.EOF)
//
// Control frames (ping 0x9, pong 0xA) are handled transparently and do not
// affect message assembly.
func (c *cdpClient) readMessage() ([]byte, error) {
	var message []byte
	collecting := false

	for {
		fin, opcode, payload, err := c.readFrame()
		if err != nil {
			return nil, err
		}

		switch opcode {
		case 0x0:
			if !collecting {
				return nil, errors.New("websocket continuation without start frame")
			}
			message = append(message, payload...)
		case 0x1:
			if collecting {
				return nil, errors.New("websocket data frame while continuation pending")
			}
			collecting = true
			message = append(message, payload...)
		case 0x2:
			return nil, errors.New("unexpected binary websocket frame")
		case 0x8:
			closePayload := payload
			if len(closePayload) > 125 {
				closePayload = nil
			}
			_ = c.writeControlFrame(0x8, closePayload)
			return nil, io.EOF
		case 0x9:
			if err := c.writeControlFrame(0xA, payload); err != nil {
				return nil, err
			}
			continue
		case 0xA:
			continue
		default:
			return nil, fmt.Errorf("unsupported websocket opcode: 0x%x", opcode)
		}

		if fin {
			return message, nil
		}
	}
}

// readFrame reads and parses a single WebSocket frame from c.br.
//
// It reads the 2-byte base header, extracts FIN, opcode, MASK, and the initial
// payload length, then reads any extended length bytes (16-bit for 126, 64-bit
// for 127) as defined by RFC 6455. If the 64-bit length does not fit into an
// int on the current platform, it returns an error.
//
// If the MASK bit is set, it reads the 4-byte masking key, reads the payload,
// and applies the masking operation in-place.
//
// It returns the FIN flag, opcode, unmasked payload bytes, and any I/O or
// protocol/size error encountered.
func (c *cdpClient) readFrame() (bool, byte, []byte, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(c.br, header); err != nil {
		return false, 0, nil, err
	}

	fin := (header[0] & 0x80) != 0
	opcode := header[0] & 0x0F
	masked := (header[1] & 0x80) != 0
	payloadLen := int(header[1] & 0x7F)

	if masked {
		return false, 0, nil, errors.New("server websocket frames must not be masked")
	}

	switch payloadLen {
	case 126:
		ext := make([]byte, 2)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return false, 0, nil, err
		}
		payloadLen = int(ext[0])<<8 | int(ext[1])
	case 127:
		ext := make([]byte, 8)
		if _, err := io.ReadFull(c.br, ext); err != nil {
			return false, 0, nil, err
		}
		length := uint64(ext[0])<<56 | uint64(ext[1])<<48 | uint64(ext[2])<<40 | uint64(ext[3])<<32 |
			uint64(ext[4])<<24 | uint64(ext[5])<<16 | uint64(ext[6])<<8 | uint64(ext[7])
		if length > uint64(int(^uint(0)>>1)) {
			return false, 0, nil, errors.New("websocket frame too large")
		}
		payloadLen = int(length)
	}

	payload := make([]byte, payloadLen)
	if payloadLen > 0 {
		if _, err := io.ReadFull(c.br, payload); err != nil {
			return false, 0, nil, err
		}
	}

	return fin, opcode, payload, nil
}

func (c *cdpClient) writeTextMessage(payload []byte) error {
	return c.writeFrame(0x1, payload, true)
}

func (c *cdpClient) writeControlFrame(opcode byte, payload []byte) error {
	if len(payload) > 125 {
		return errors.New("websocket control frame too large")
	}
	return c.writeFrame(opcode, payload, true)
}

// writeFrame constructs and writes a single masked WebSocket frame to the underlying
// connection. It encodes the FIN bit and opcode in the first byte, chooses the
// appropriate payload length encoding (7-bit, 16-bit, or 64-bit), generates a random
// 4-byte masking key, and applies the mask to the payload as required for client-to-server
// frames (RFC 6455). The final frame is written atomically via c.conn.Write.
// It returns any error encountered while generating the mask key or writing to the connection.
func (c *cdpClient) writeFrame(opcode byte, payload []byte, fin bool) error {
	maskKey := [4]byte{}
	if _, err := rand.Read(maskKey[:]); err != nil {
		return err
	}

	payloadLen := len(payload)
	headerLen := 2
	if payloadLen >= 126 && payloadLen <= 65535 {
		headerLen += 2
	} else if payloadLen > 65535 {
		headerLen += 8
	}
	headerLen += 4

	frame := make([]byte, headerLen+payloadLen)
	if fin {
		frame[0] = 0x80 | opcode
	} else {
		frame[0] = opcode
	}

	offset := 2
	switch {
	case payloadLen <= 125:
		frame[1] = 0x80 | byte(payloadLen)
	case payloadLen <= 65535:
		frame[1] = 0x80 | 126
		frame[offset] = byte(payloadLen >> 8)
		frame[offset+1] = byte(payloadLen)
		offset += 2
	default:
		frame[1] = 0x80 | 127
		length := uint64(payloadLen)
		frame[offset] = byte(length >> 56)
		frame[offset+1] = byte(length >> 48)
		frame[offset+2] = byte(length >> 40)
		frame[offset+3] = byte(length >> 32)
		frame[offset+4] = byte(length >> 24)
		frame[offset+5] = byte(length >> 16)
		frame[offset+6] = byte(length >> 8)
		frame[offset+7] = byte(length)
		offset += 8
	}

	copy(frame[offset:], maskKey[:])
	offset += 4

	for i := 0; i < payloadLen; i++ {
		frame[offset+i] = payload[i] ^ maskKey[i%4]
	}

	_, err := c.conn.Write(frame)
	return err
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
