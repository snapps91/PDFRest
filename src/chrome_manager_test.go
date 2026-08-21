// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeChromeProcess struct {
	mu             sync.Mutex
	running        bool
	starts         int
	stops          int
	availableCalls int
	stopEvents     chan struct{}
}

func newFakeChromeProcess() *fakeChromeProcess {
	return &fakeChromeProcess{stopEvents: make(chan struct{}, 10)}
}

func (p *fakeChromeProcess) Available() error {
	p.mu.Lock()
	p.availableCalls++
	p.mu.Unlock()
	return nil
}

func (p *fakeChromeProcess) Start() error {
	p.mu.Lock()
	p.running = true
	p.starts++
	p.mu.Unlock()
	return nil
}

func (p *fakeChromeProcess) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *fakeChromeProcess) Stop(_ context.Context) error {
	p.mu.Lock()
	if p.running {
		p.running = false
		p.stops++
		p.stopEvents <- struct{}{}
	}
	p.mu.Unlock()
	return nil
}

func (p *fakeChromeProcess) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.starts, p.stops, p.availableCalls
}

type fakeChromeEndpoint struct {
	mu          sync.Mutex
	checkErr    error
	wsErr       error
	invalidates int
}

func (r *fakeChromeEndpoint) wsURL(_ context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return "ws://127.0.0.1:9222/devtools/browser/test", r.wsErr
}

func (r *fakeChromeEndpoint) checkChrome(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.checkErr
}

func (r *fakeChromeEndpoint) invalidate() {
	r.mu.Lock()
	r.invalidates++
	r.mu.Unlock()
}

func newTestManagedChrome(idle time.Duration) (*managedChrome, *fakeChromeProcess) {
	process := newFakeChromeProcess()
	manager := &managedChrome{
		resolver:        &fakeChromeEndpoint{},
		process:         process,
		pool:            newSessionPool(0),
		idleTimeout:     idle,
		startupTimeout:  time.Second,
		shutdownTimeout: time.Second,
	}
	return manager, process
}

func TestManagedChromeStartsLazilyAndStopsOnlyAfterRelease(t *testing.T) {
	manager, process := newTestManagedChrome(20 * time.Millisecond)
	defer manager.Close()

	starts, _, _ := process.counts()
	if starts != 0 {
		t.Fatalf("expected lazy startup, got %d starts", starts)
	}

	_, release, err := manager.acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	starts, _, _ = process.counts()
	if starts != 1 {
		t.Fatalf("expected one start, got %d", starts)
	}

	select {
	case <-process.stopEvents:
		t.Fatal("Chromium stopped while a render lease was active")
	case <-time.After(50 * time.Millisecond):
	}

	release()
	waitForStop(t, process.stopEvents)

	_, release, err = manager.acquire(context.Background())
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	release()
	starts, _, _ = process.counts()
	if starts != 2 {
		t.Fatalf("expected restart after idle shutdown, got %d starts", starts)
	}
}

func TestManagedChromeCanceledTimerCannotStopNewLease(t *testing.T) {
	manager, process := newTestManagedChrome(time.Hour)
	defer manager.Close()

	_, firstRelease, err := manager.acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	firstRelease()
	manager.mu.Lock()
	staleGeneration := manager.idleGeneration
	manager.mu.Unlock()

	_, secondRelease, err := manager.acquire(context.Background())
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	manager.stopIfIdle(staleGeneration)
	select {
	case <-process.stopEvents:
		t.Fatal("stale idle timer stopped Chromium during a new lease")
	default:
	}
	secondRelease()
	manager.mu.Lock()
	currentGeneration := manager.idleGeneration
	manager.idleTimer.Stop()
	manager.mu.Unlock()
	manager.stopIfIdle(currentGeneration)
	waitForStop(t, process.stopEvents)

	starts, stops, _ := process.counts()
	if starts != 1 || stops != 1 {
		t.Fatalf("expected one reused process and one idle stop, got starts=%d stops=%d", starts, stops)
	}
}

func TestManagedChromeConcurrentAcquireStartsOneProcess(t *testing.T) {
	manager, process := newTestManagedChrome(time.Hour)
	defer manager.Close()

	const requests = 20
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, release, err := manager.acquire(context.Background())
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			defer release()
		}()
	}
	close(start)
	wg.Wait()

	starts, _, _ := process.counts()
	if starts != 1 {
		t.Fatalf("expected one process for concurrent requests, got %d", starts)
	}
}

func TestManagedChromeHealthDoesNotStartProcess(t *testing.T) {
	manager, process := newTestManagedChrome(time.Second)
	defer manager.Close()

	if err := manager.checkChrome(context.Background()); err != nil {
		t.Fatalf("health check: %v", err)
	}
	starts, _, available := process.counts()
	if starts != 0 || available != 1 {
		t.Fatalf("health check should only validate availability, got starts=%d availability checks=%d", starts, available)
	}
}

func TestManagedChromeAcquireHonorsContextWhileLifecycleIsBusy(t *testing.T) {
	manager, process := newTestManagedChrome(time.Second)

	manager.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	started := time.Now()
	_, _, err := manager.acquire(ctx)
	elapsed := time.Since(started)
	cancel()
	manager.mu.Unlock()
	defer manager.Close()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context deadline, got %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("busy lifecycle ignored request context for %s", elapsed)
	}
	starts, _, _ := process.counts()
	if starts != 0 {
		t.Fatalf("timed-out request unexpectedly started Chromium %d times", starts)
	}
}

func TestManagedChromeStopsFailedStartup(t *testing.T) {
	process := newFakeChromeProcess()
	manager := &managedChrome{
		resolver:        &fakeChromeEndpoint{checkErr: errors.New("not ready")},
		process:         process,
		pool:            newSessionPool(0),
		idleTimeout:     time.Second,
		startupTimeout:  20 * time.Millisecond,
		shutdownTimeout: time.Second,
	}
	defer manager.Close()

	if _, _, err := manager.acquire(context.Background()); err == nil {
		t.Fatal("expected startup error")
	}
	waitForStop(t, process.stopEvents)
}

func TestManagedChromeResolveFailureRestoresIdleTimer(t *testing.T) {
	process := newFakeChromeProcess()
	manager := &managedChrome{
		resolver:        &fakeChromeEndpoint{wsErr: errors.New("discovery failed")},
		process:         process,
		pool:            newSessionPool(0),
		idleTimeout:     20 * time.Millisecond,
		startupTimeout:  time.Second,
		shutdownTimeout: time.Second,
	}
	defer manager.Close()

	if _, _, err := manager.acquire(context.Background()); err == nil {
		t.Fatal("expected websocket discovery error")
	}
	waitForStop(t, process.stopEvents)
}

func waitForStop(t *testing.T, events <-chan struct{}) {
	t.Helper()
	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Chromium to stop")
	}
}

func TestChromeDebugAddress(t *testing.T) {
	address, port, err := chromeDebugAddress("http://localhost:9222")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if address != "127.0.0.1" || port != "9222" {
		t.Fatalf("unexpected address: %s:%s", address, port)
	}

	invalid := []string{"ws://localhost:9222", "http://localhost:9222/debug", "http://"}
	for _, endpoint := range invalid {
		if _, _, err := chromeDebugAddress(endpoint); err == nil {
			t.Fatalf("expected %q to be rejected", endpoint)
		}
	}
}
