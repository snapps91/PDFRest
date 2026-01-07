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

const (
	// API paths.
	pathPDF     = "/api/v1/pdf"
	pathHealthz = "/healthz"

	// Default server-level timeouts.
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 15 * time.Second
	defaultIdleTimeout       = 60 * time.Second

	// Shutdown timeout (graceful).
	defaultShutdownTimeout = 10 * time.Second

	// Client timeout for the Chrome /json/version endpoint.
	defaultChromeClientTimeout = 5 * time.Second

	// Cache TTL for Chrome websocket discovery.
	defaultWSTTL = 1 * time.Minute

	// Response header.
	pdfFilename = "document.pdf"
)

// config holds runtime configuration loaded from env vars.
// Keep it as a "value type": immutable after construction.
type config struct {
	Addr           string
	ChromeEndpoint string
	ChromeWS       string
	RequestTimeout time.Duration
	MaxBodyBytes   int64
	PDFWait        time.Duration
}

type pdfOptions struct {
	Landscape       *bool
	Scale           *float64
	PaperWidth      *float64
	PaperHeight     *float64
	MarginTop       *float64
	MarginBottom    *float64
	MarginLeft      *float64
	MarginRight     *float64
	PrintBackground *bool
	PageRanges      string
}

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

type versionResponse struct {
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func main() {
	cfg := loadConfig()

	// Resolver: discovers Chrome websocket URL unless explicitly provided.
	resolver := newChromeResolver(cfg)

	// Router.
	mux := http.NewServeMux()
	mux.HandleFunc(pathPDF, pdfHandler(cfg, resolver))
	mux.HandleFunc(pathHealthz, healthHandler)

	// Server with sane defaults. Note: WriteTimeout is set to (request timeout + small buffer),
	// so handlers can use the full configured RequestTimeout.
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           loggingMiddleware(mux),
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:       defaultIdleTimeout,
	}

	runServer(srv, cfg.Addr)
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

func runServer(srv *http.Server, addr string) {
	serverErr := make(chan error, 1)

	go func() {
		log.Printf("listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Listen for OS signals.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	// Block until signal or server error.
	select {
	case sig := <-stop:
		log.Printf("shutting down on signal: %s", sig)
	case err := <-serverErr:
		log.Printf("server error: %v", err)
	}

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), defaultShutdownTimeout)
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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	// Health endpoints should be fast and side-effect free.
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func pdfHandler(cfg config, resolver *chromeResolver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only POST is allowed.
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Per-request timeout. This drives both Chrome discovery and PDF rendering.
		ctx, cancel := context.WithTimeout(r.Context(), cfg.RequestTimeout)
		defer cancel()

		// Enforce maximum body size to protect memory.
		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
		defer r.Body.Close()

		body, err := readRequestBody(r.Body)
		if err != nil {
			// Preserve original behavior: map specific read errors to an HTTP status.
			http.Error(w, "invalid request body", mapBodyReadErrorToStatus(err))
			return
		}

		if len(body) == 0 {
			http.Error(w, "empty html", http.StatusBadRequest)
			return
		}

		options, err := parsePDFOptions(r.URL.Query())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Resolve Chrome websocket endpoint.
		wsURL, err := resolver.wsURL(ctx)
		if err != nil {
			log.Printf("chrome ws error: %v", err)
			http.Error(w, "chrome unavailable", http.StatusServiceUnavailable)
			return
		}

		// Render PDF from HTML.
		pdf, err := renderPDF(ctx, wsURL, string(body), cfg.PDFWait, options)
		if err != nil {
			log.Printf("render error: %v", err)
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}

		// Response headers.
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", pdfFilename))

		// Basic hardening headers (does not affect logic).
		// These are safe defaults for an API returning binary content.
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(pdf)
	}
}

// readRequestBody reads the body fully. The MaxBytesReader is already applied at the handler level.
func readRequestBody(r io.Reader) ([]byte, error) {
	// Keep the original semantics: ReadAll then validate len.
	return io.ReadAll(r)
}

// mapBodyReadErrorToStatus keeps the current status mapping logic intact,
// but isolates it into a dedicated function for clarity and testability.
func mapBodyReadErrorToStatus(err error) int {
	status := http.StatusBadRequest

	var maxErr *http.MaxBytesError
	switch {
	case errors.Is(err, http.ErrBodyReadAfterClose), errors.Is(err, io.EOF):
		status = http.StatusBadRequest
	case errors.As(err, &maxErr):
		status = http.StatusRequestEntityTooLarge
	default:
		// Preserve behavior: "invalid request body" + 400 for generic errors.
		status = http.StatusBadRequest
	}
	return status
}

// renderPDF uses a remote Chrome instance via DevTools websocket and prints the given HTML to PDF.
// Logic is unchanged: navigate to about:blank -> set document content -> wait for body -> optional sleep -> PrintToPDF.
func renderPDF(ctx context.Context, wsURL, html string, wait time.Duration, options pdfOptions) ([]byte, error) {
	allocCtx, cancel := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancel()

	// Create a new tab context (child of allocator ctx).
	ctx, cancel = chromedp.NewContext(allocCtx)
	defer cancel()

	var (
		pdf     []byte
		frameID cdp.FrameID
	)

	err := chromedp.Run(ctx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Fetch the main frame and inject the provided HTML.
			frameTree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			frameID = frameTree.Frame.ID
			return page.SetDocumentContent(frameID, html).Do(ctx)
		}),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Optional wait to allow dynamic content to settle.
			if wait <= 0 {
				return nil
			}
			return chromedp.Sleep(wait).Do(ctx)
		}),
		chromedp.ActionFunc(func(ctx context.Context) error {
			// Print with background enabled, matching original behavior.
			var err error
			print := page.PrintToPDF().WithPrintBackground(true)
			if options.PrintBackground != nil {
				print = print.WithPrintBackground(*options.PrintBackground)
			}
			if options.Landscape != nil {
				print = print.WithLandscape(*options.Landscape)
			}
			if options.Scale != nil {
				print = print.WithScale(*options.Scale)
			}
			if options.PaperWidth != nil {
				print = print.WithPaperWidth(*options.PaperWidth)
			}
			if options.PaperHeight != nil {
				print = print.WithPaperHeight(*options.PaperHeight)
			}
			if options.MarginTop != nil {
				print = print.WithMarginTop(*options.MarginTop)
			}
			if options.MarginBottom != nil {
				print = print.WithMarginBottom(*options.MarginBottom)
			}
			if options.MarginLeft != nil {
				print = print.WithMarginLeft(*options.MarginLeft)
			}
			if options.MarginRight != nil {
				print = print.WithMarginRight(*options.MarginRight)
			}
			if options.PageRanges != "" {
				print = print.WithPageRanges(options.PageRanges)
			}
			pdf, _, err = print.Do(ctx)
			return err
		}),
	)
	if err != nil {
		return nil, err
	}

	return pdf, nil
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

	// Store in cache.
	c.setCachedWS(payload.WebSocketDebuggerURL)

	return payload.WebSocketDebuggerURL, nil
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

// loggingMiddleware logs method/path/status/duration.
// It wraps the ResponseWriter to capture the status code.
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

func parsePDFOptions(values map[string][]string) (pdfOptions, error) {
	options := pdfOptions{}

	if value := getQueryValue(values, "landscape"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return options, fmt.Errorf("invalid landscape")
		}
		options.Landscape = &parsed
	}

	if value := getQueryValue(values, "scale"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid scale")
		}
		options.Scale = &parsed
	}

	if value := getQueryValue(values, "paper_width"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid paper_width")
		}
		options.PaperWidth = &parsed
	}

	if value := getQueryValue(values, "paper_height"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid paper_height")
		}
		options.PaperHeight = &parsed
	}

	if value := getQueryValue(values, "margin_top"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_top")
		}
		options.MarginTop = &parsed
	}

	if value := getQueryValue(values, "margin_bottom"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_bottom")
		}
		options.MarginBottom = &parsed
	}

	if value := getQueryValue(values, "margin_left"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_left")
		}
		options.MarginLeft = &parsed
	}

	if value := getQueryValue(values, "margin_right"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return options, fmt.Errorf("invalid margin_right")
		}
		options.MarginRight = &parsed
	}

	if value := getQueryValue(values, "print_background"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return options, fmt.Errorf("invalid print_background")
		}
		options.PrintBackground = &parsed
	}

	options.PageRanges = getQueryValue(values, "page_ranges")

	return options, nil
}

func getQueryValue(values map[string][]string, key string) string {
	if values == nil {
		return ""
	}
	if list, ok := values[key]; ok && len(list) > 0 {
		return list[0]
	}
	return ""
}
