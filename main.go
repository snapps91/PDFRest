package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

type config struct {
	Addr           string
	ChromeEndpoint string
	ChromeWS       string
	RequestTimeout time.Duration
	MaxBodyBytes   int64
	PDFWait        time.Duration
}

type chromeResolver struct {
	endpoint string
	ws       string
	client   *http.Client

	mu       sync.Mutex
	cachedWS string
	cachedAt time.Time
	cacheTTL time.Duration
}

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func main() {
	cfg := loadConfig()

	resolver := &chromeResolver{
		endpoint: cfg.ChromeEndpoint,
		ws:       cfg.ChromeWS,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		cacheTTL: 1 * time.Minute,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/pdf", pdfHandler(cfg, resolver))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			shutdownErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stop:
		log.Printf("shutting down on %s", sig)
	case err := <-shutdownErr:
		log.Printf("server error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}

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

func pdfHandler(cfg config, resolver *chromeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), cfg.RequestTimeout)
		defer cancel()

		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
		defer r.Body.Close()

		body, err := io.ReadAll(r.Body)
		if err != nil {
			status := http.StatusBadRequest
			var maxErr *http.MaxBytesError
			if errors.Is(err, http.ErrBodyReadAfterClose) || errors.Is(err, io.EOF) {
				status = http.StatusBadRequest
			} else if errors.As(err, &maxErr) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, "invalid request body", status)
			return
		}

		if len(body) == 0 {
			http.Error(w, "empty html", http.StatusBadRequest)
			return
		}

		wsURL, err := resolver.wsURL(ctx)
		if err != nil {
			log.Printf("chrome ws error: %v", err)
			http.Error(w, "chrome unavailable", http.StatusServiceUnavailable)
			return
		}

		pdf, err := renderPDF(ctx, wsURL, string(body), cfg.PDFWait)
		if err != nil {
			log.Printf("render error: %v", err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "inline; filename=\"document.pdf\"")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdf)
	}
}

func renderPDF(ctx context.Context, wsURL, html string, wait time.Duration) ([]byte, error) {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancel()

	ctx, cancel = chromedp.NewContext(allocCtx)
	defer cancel()

	var pdf []byte
	var frameID cdp.FrameID

	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			frameID = frameTree.Frame.ID
			return page.SetDocumentContent(frameID, html).Do(ctx)
		}),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if wait <= 0 {
				return nil
			}
			return chromedp.Sleep(wait).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdf, _, err = page.PrintToPDF().WithPrintBackground(true).Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, err
	}

	return pdf, nil
}

func (c *chromeResolver) wsURL(ctx context.Context) (string, error) {
	if c.ws != "" {
		return c.ws, nil
	}

	c.mu.Lock()
	if c.cachedWS != "" && time.Since(c.cachedAt) < c.cacheTTL {
		defer c.mu.Unlock()
		return c.cachedWS, nil
	}
	c.mu.Unlock()

	endpoint := fmt.Sprintf("%s/json/version", c.endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
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

	c.mu.Lock()
	c.cachedWS = payload.WebSocketDebuggerURL
	c.cachedAt = time.Now()
	c.mu.Unlock()

	return payload.WebSocketDebuggerURL, nil
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("%s %s %d %s", r.Method, r.URL.Path, rw.status, time.Since(start))
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
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
