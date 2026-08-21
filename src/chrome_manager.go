// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	chromeReadinessPollInterval = 100 * time.Millisecond
	chromeLockPollInterval      = 10 * time.Millisecond
)

type chromeEndpointResolver interface {
	wsResolver
	checkChrome(ctx context.Context) error
	invalidate()
}

// managedChrome starts Chromium on the first render, tracks active renders and
// stops the process only after the configured period with no active leases.
type managedChrome struct {
	resolver chromeEndpointResolver
	process  chromeProcessController
	pool     *sessionPool

	idleTimeout     time.Duration
	startupTimeout  time.Duration
	shutdownTimeout time.Duration

	mu             sync.Mutex
	active         int
	idleTimer      *time.Timer
	idleGeneration uint64
	closed         bool
}

func newManagedChrome(cfg config, resolver chromeEndpointResolver, pool *sessionPool) *managedChrome {
	return &managedChrome{
		resolver:        resolver,
		process:         newChromeProcess(cfg),
		pool:            pool,
		idleTimeout:     cfg.ChromeIdleTimeout,
		startupTimeout:  cfg.ChromeStartupTimeout,
		shutdownTimeout: cfg.ChromeShutdownTimeout,
	}
}

func (m *managedChrome) acquire(ctx context.Context) (string, func(), error) {
	if err := m.lockWithContext(ctx); err != nil {
		return "", nil, err
	}
	defer m.mu.Unlock()

	if m.closed {
		return "", nil, errors.New("Chromium manager is closed")
	}
	m.cancelIdleTimerLocked()

	if err := m.ensureStartedLocked(ctx); err != nil {
		m.scheduleIdleStopLocked()
		return "", nil, err
	}
	wsURL, err := m.resolver.wsURL(ctx)
	if err != nil {
		m.scheduleIdleStopLocked()
		return "", nil, err
	}

	m.active++
	var once sync.Once
	release := func() {
		once.Do(m.release)
	}
	return wsURL, release, nil
}

// wsURL preserves the resolver interface for health-handler compatibility.
// Rendering code uses acquire so the returned endpoint remains leased.
func (m *managedChrome) wsURL(ctx context.Context) (string, error) {
	wsURL, release, err := m.acquire(ctx)
	if err != nil {
		return "", err
	}
	release()
	return wsURL, nil
}

func (m *managedChrome) ensureStartedLocked(ctx context.Context) error {
	if m.process.Running() {
		return nil
	}

	// A crashed or previously stopped browser leaves pooled sockets and discovery
	// data unusable. Clear both before creating the next process.
	if m.pool != nil {
		m.pool.close()
	}
	m.resolver.invalidate()

	if err := m.process.Start(); err != nil {
		return err
	}
	Infof("Chromium started on demand")

	startupCtx := ctx
	cancel := func() {}
	if m.startupTimeout > 0 {
		startupCtx, cancel = context.WithTimeout(ctx, m.startupTimeout)
	}
	defer cancel()

	var lastErr error
	ticker := time.NewTicker(chromeReadinessPollInterval)
	defer ticker.Stop()
	for {
		if err := m.resolver.checkChrome(startupCtx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if !m.process.Running() {
			m.resolver.invalidate()
			return fmt.Errorf("Chromium exited during startup: %w", lastErr)
		}

		select {
		case <-startupCtx.Done():
			m.stopProcessLocked("startup failure")
			return fmt.Errorf("Chromium startup failed: %w (last probe: %v)", startupCtx.Err(), lastErr)
		case <-ticker.C:
		}
	}
}

func (m *managedChrome) release() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.active == 0 {
		Warnf("unbalanced Chromium lease release")
		return
	}
	m.active--
	m.scheduleIdleStopLocked()
}

func (m *managedChrome) scheduleIdleStopLocked() {
	if m.active != 0 || m.closed || m.idleTimeout <= 0 || !m.process.Running() {
		return
	}

	m.cancelIdleTimerLocked()
	m.idleGeneration++
	generation := m.idleGeneration
	m.idleTimer = time.AfterFunc(m.idleTimeout, func() {
		m.stopIfIdle(generation)
	})
}

func (m *managedChrome) stopIfIdle(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed || m.active != 0 || generation != m.idleGeneration {
		return
	}
	m.idleTimer = nil
	m.stopProcessLocked("idle timeout")
}

func (m *managedChrome) cancelIdleTimerLocked() {
	m.idleGeneration++
	if m.idleTimer != nil {
		m.idleTimer.Stop()
		m.idleTimer = nil
	}
}

func (m *managedChrome) stopProcessLocked(reason string) {
	if m.pool != nil {
		m.pool.close()
	}

	if m.process.Running() {
		ctx := context.Background()
		cancel := func() {}
		if m.shutdownTimeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, m.shutdownTimeout)
		}
		err := m.process.Stop(ctx)
		cancel()
		if err != nil {
			Warnf("stop Chromium after %s: %v", reason, err)
		}
		Infof("Chromium stopped after %s", reason)
	}
	m.resolver.invalidate()
}

// checkChrome intentionally does not start Chromium. A stopped managed browser
// is healthy when its executable/configuration is available; a running one is
// probed so genuine runtime failures remain visible.
func (m *managedChrome) checkChrome(ctx context.Context) error {
	if err := m.lockWithContext(ctx); err != nil {
		return err
	}
	defer m.mu.Unlock()

	if m.closed {
		return errors.New("Chromium manager is closed")
	}
	if !m.process.Running() {
		return m.process.Available()
	}
	return m.resolver.checkChrome(ctx)
}

func (m *managedChrome) lockWithContext(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.mu.TryLock() {
		return nil
	}

	ticker := time.NewTicker(chromeLockPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if m.mu.TryLock() {
				return nil
			}
		}
	}
}

func (m *managedChrome) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return
	}
	m.closed = true
	m.cancelIdleTimerLocked()
	m.stopProcessLocked("service shutdown")
}
