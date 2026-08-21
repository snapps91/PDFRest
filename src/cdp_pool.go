// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

// This code implements a lightweight session management layer on top of the
// Chrome DevTools Protocol (CDP). The goal of this code is to abstract the
// lifecycle of CDP connections and targets, while providing an optional pooling
// mechanism to reduce connection overhead and improve performance in concurrent
// or repetitive workloads.
//
// The design is based on two core concepts:
//
//  1. cdpSession
//     A cdpSession represents a usable CDP interaction context. Internally it
//     wraps a CDP client connection and, when required, an attached target
//     (for example a browser tab or page). The code distinguishes between
//     “page-level” WebSocket endpoints and “browser-level” endpoints:
//       - When the WebSocket already points to a page, no additional target
//         is created and the session is considered directly usable.
//       - When the WebSocket points to a browser endpoint, a new target is
//         opened and attached, and its lifecycle is explicitly managed.
//
//     The Close method is responsible for deterministic cleanup: if a target
//     was created, it is closed with a bounded timeout, and the underlying
//     CDP client connection is always closed afterwards. Cleanup is designed
//     to be tolerant to errors and safe to call multiple times.
//
//  2. sessionPool
//     The sessionPool provides an optional reuse layer for cdpSession instances.
//     When enabled (size > 0), it maintains a bounded pool of idle sessions
//     associated with a single WebSocket endpoint. This avoids repeated target
//     creation and connection handshakes, which can be expensive in high-
//     throughput scenarios.
//
//     The pool is intentionally conservative:
//       - Pooling is disabled for page-level WebSockets, since those sessions
//         are already bound to a specific target and are not safely reusable.
//       - The pool is tied to exactly one WebSocket URL; if the URL changes,
//         all existing sessions are closed and the pool is reset.
//       - Acquire and release operations are non-blocking: if no idle session
//         is available, a new one is created; if the pool is full on release,
//         the session is closed.
//
// Thread-safety is ensured through a combination of a mutex (to protect pool
// state and WebSocket consistency) and a buffered channel (to store idle
// sessions). Potentially slow operations such as network I/O and session
// creation are performed outside critical sections to minimize lock contention.
//
// Overall, this design favors simplicity, safety, and predictable resource
// usage over aggressive reuse, making it suitable as a building block for
// higher-level automation or tooling built on top of CDP.

package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

type cdpSession struct {
	client    *cdpClient
	sessionID string
	targetID  string
	wsURL     string
}

func newCDPSession(ctx context.Context, wsURL string) (*cdpSession, error) {
	client, err := newCDPClient(ctx, wsURL)
	if err != nil {
		return nil, err
	}

	if isPageWebSocket(wsURL) {
		return &cdpSession{client: client, wsURL: wsURL}, nil
	}

	sessionID, targetID, err := openTargetSession(ctx, client)
	if err != nil {
		_ = client.Close()
		return nil, err
	}

	return &cdpSession{
		client:    client,
		sessionID: sessionID,
		targetID:  targetID,
		wsURL:     wsURL,
	}, nil
}

func (s *cdpSession) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	if s.targetID != "" {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := closeTarget(cleanupCtx, s.client, s.targetID); err != nil {
			Warnf("chrome close target error: %v", err)
		}
	}
	return s.client.Close()
}

type sessionPool struct {
	size     int
	wsURL    string
	sessions chan *cdpSession
	mu       sync.Mutex
}

func newSessionPool(size int) *sessionPool {
	if size < 0 {
		size = 0
	}
	return &sessionPool{size: size}
}

func (p *sessionPool) acquire(ctx context.Context, wsURL string) (*cdpSession, error) {
	if wsURL == "" {
		return nil, errors.New("missing websocket url")
	}
	if p.size <= 0 || isPageWebSocket(wsURL) {
		return newCDPSession(ctx, wsURL)
	}

	p.mu.Lock()
	if p.wsURL == "" {
		p.wsURL = wsURL
		p.sessions = make(chan *cdpSession, p.size)
	} else if p.wsURL != wsURL {
		p.resetLocked()
		p.wsURL = wsURL
		p.sessions = make(chan *cdpSession, p.size)
	}

	var session *cdpSession
	select {
	case session = <-p.sessions:
	default:
	}
	p.mu.Unlock()

	if session != nil {
		return session, nil
	}
	return newCDPSession(ctx, wsURL)
}

func (p *sessionPool) release(session *cdpSession) {
	if session == nil {
		return
	}
	if p.size <= 0 || isPageWebSocket(session.wsURL) {
		_ = session.Close()
		return
	}

	p.mu.Lock()
	if p.sessions == nil || p.wsURL != session.wsURL {
		p.mu.Unlock()
		_ = session.Close()
		return
	}
	select {
	case p.sessions <- session:
		p.mu.Unlock()
		return
	default:
		p.mu.Unlock()
		_ = session.Close()
		return
	}
}

func (p *sessionPool) resetLocked() {
	if p.sessions == nil {
		return
	}
	close(p.sessions)
	for session := range p.sessions {
		_ = session.Close()
	}
	p.sessions = nil
	p.wsURL = ""
}

// close releases every idle CDP session. Callers must ensure that no checked-out
// session is still in use when this is part of a browser shutdown.
func (p *sessionPool) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.resetLocked()
	p.mu.Unlock()
}
