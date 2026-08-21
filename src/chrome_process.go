// Copyright 2026 - Giacomo Failla <failla.giacomo@gmail.com>
// MIT License. See LICENSE file for details.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const chromeForcedKillWait = time.Second

type chromeProcessController interface {
	Available() error
	Start() error
	Running() bool
	Stop(ctx context.Context) error
}

// chromeProcess owns the Chromium child process started by the service.
type chromeProcess struct {
	cfg config

	mu            sync.Mutex
	cmd           *exec.Cmd
	done          chan error
	userDataDir   string
	removeDataDir bool
}

func newChromeProcess(cfg config) *chromeProcess {
	return &chromeProcess{cfg: cfg}
}

func (p *chromeProcess) Available() error {
	if _, err := resolveChromeBinary(p.cfg.ChromeBinary); err != nil {
		return err
	}
	_, _, err := chromeDebugAddress(p.cfg.ChromeEndpoint)
	return err
}

func (p *chromeProcess) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd != nil {
		return nil
	}

	binary, err := resolveChromeBinary(p.cfg.ChromeBinary)
	if err != nil {
		return err
	}
	address, port, err := chromeDebugAddress(p.cfg.ChromeEndpoint)
	if err != nil {
		return err
	}

	userDataDir := p.cfg.ChromeUserDataDir
	removeDataDir := false
	if userDataDir == "" {
		userDataDir, err = os.MkdirTemp("", "pdfrest-chrome-")
		if err != nil {
			return fmt.Errorf("create Chromium profile: %w", err)
		}
		removeDataDir = true
	} else if err := os.MkdirAll(userDataDir, 0o700); err != nil {
		return fmt.Errorf("create Chromium profile %q: %w", userDataDir, err)
	}

	args := []string{
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--remote-debugging-address=" + address,
		"--remote-debugging-port=" + port,
		"--disable-dev-shm-usage",
		"--disable-background-networking",
		"--disable-extensions",
		"--disable-default-apps",
		"--no-first-run",
		"--disable-sync",
		"--user-data-dir=" + userDataDir,
		"about:blank",
	}
	cmd := exec.Command(binary, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	// Isolate Chromium and all of its helpers in their own process group so a
	// forced shutdown cannot leave renderer processes behind.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		if removeDataDir {
			_ = os.RemoveAll(userDataDir)
		}
		return fmt.Errorf("start Chromium: %w", err)
	}

	done := make(chan error, 1)
	p.cmd = cmd
	p.done = done
	p.userDataDir = userDataDir
	p.removeDataDir = removeDataDir

	go p.wait(cmd, done, userDataDir, removeDataDir)
	return nil
}

func (p *chromeProcess) wait(cmd *exec.Cmd, done chan error, userDataDir string, removeDataDir bool) {
	err := cmd.Wait()

	p.mu.Lock()
	if p.cmd == cmd {
		p.cmd = nil
		p.done = nil
		p.userDataDir = ""
		p.removeDataDir = false
	}
	p.mu.Unlock()

	if removeDataDir {
		if err := os.RemoveAll(userDataDir); err != nil {
			Warnf("remove temporary Chromium profile: %v", err)
		}
	}

	// Publish completion only after Running reports false and temporary resources
	// are gone, so an immediate acquire cannot observe the old process as alive.
	done <- err
	close(done)
}

func (p *chromeProcess) Running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cmd != nil
}

func (p *chromeProcess) Stop(ctx context.Context) error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	if cmd == nil || done == nil {
		return nil
	}

	if err := signalChromeProcessGroup(cmd, syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal Chromium: %w", err)
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		if err := signalChromeProcessGroup(cmd, syscall.SIGKILL); err != nil {
			return fmt.Errorf("kill Chromium: %w", err)
		}
		timer := time.NewTimer(chromeForcedKillWait)
		defer timer.Stop()
		select {
		case <-done:
			return fmt.Errorf("Chromium required a forced kill: %w", ctx.Err())
		case <-timer.C:
			return fmt.Errorf("Chromium did not exit after a forced kill: %w", ctx.Err())
		}
	}
}

func signalChromeProcessGroup(cmd *exec.Cmd, signal syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, signal)
	if errors.Is(err, syscall.ESRCH) || errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}

func resolveChromeBinary(configured string) (string, error) {
	if configured != "" {
		if strings.ContainsRune(configured, os.PathSeparator) {
			info, err := os.Stat(configured)
			if err != nil {
				return "", fmt.Errorf("find Chromium binary %q: %w", configured, err)
			}
			if info.IsDir() || info.Mode().Perm()&0o111 == 0 {
				return "", fmt.Errorf("Chromium binary %q is not executable", configured)
			}
			return configured, nil
		}
		path, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("find Chromium binary %q: %w", configured, err)
		}
		return path, nil
	}

	candidates := []string{"chromium", "chromium-browser", "google-chrome-stable", "google-chrome"}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}

	macChrome := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	if info, err := os.Stat(macChrome); err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
		return macChrome, nil
	}

	return "", errors.New("Chromium executable not found; set CHROME_BIN")
}

func chromeDebugAddress(endpoint string) (string, string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", "", fmt.Errorf("parse CHROME_ENDPOINT: %w", err)
	}
	if parsed.Scheme != "http" {
		return "", "", fmt.Errorf("managed CHROME_ENDPOINT must use http")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", "", fmt.Errorf("CHROME_ENDPOINT must not contain a path")
	}

	address := parsed.Hostname()
	if address == "" {
		return "", "", fmt.Errorf("CHROME_ENDPOINT is missing a host")
	}
	if strings.EqualFold(address, "localhost") {
		address = "127.0.0.1"
	}
	port := parsed.Port()
	if port == "" {
		port = "80"
	}
	return address, port, nil
}
