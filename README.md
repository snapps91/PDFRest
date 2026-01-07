# pdfrest

A minimal, production-ready microservice that turns raw HTML into a PDF using a running headless Chromium instance via the DevTools protocol. The service exposes a single REST endpoint at `/api/v1/pdf`, accepts HTML in the request body, and returns a PDF stream as the response.

## Why this service

This service is designed for fast, reliable HTML-to-PDF rendering with minimal operational overhead:

- **Single responsibility**: one endpoint that converts HTML to PDF.
- **Headless Chromium**: uses the official DevTools protocol for accurate rendering.
- **Lightweight Go server**: low memory usage and fast startup.
- **Production-friendly**: timeouts, request size limits, and graceful shutdown.
- **Container-ready**: ships with an Alpine-based container that runs both Chromium and the API via supervisord.

## How it works

1. Chromium is started in headless mode with a remote debugging port.
2. The Go service connects to Chromium through the DevTools websocket.
3. The service injects the provided HTML into a blank page.
4. Chromium renders the page and generates a PDF via `PrintToPDF`.
5. The API returns the PDF bytes with `application/pdf` content type.

## REST API

### `POST /api/v1/pdf`

- **Request body**: raw HTML (`text/html`) or any content type; the body is treated as HTML.
- **Response**: `application/pdf` with an inline `Content-Disposition` header.
- **Query parameters (optional)**:
  - `landscape` (bool)
  - `scale` (float)
  - `paper_width` (float, inches)
  - `paper_height` (float, inches)
  - `margin_top` (float, inches)
  - `margin_bottom` (float, inches)
  - `margin_left` (float, inches)
  - `margin_right` (float, inches)
  - `print_background` (bool)
  - `page_ranges` (string, e.g. `1-3,5`)

Example:

```bash
curl -sS -X POST http://localhost:8080/api/v1/pdf \
  -H 'Content-Type: text/html; charset=utf-8' \
  --data-binary @- \
  -o /tmp/test.pdf <<'HTML'
<!doctype html>
<html>
  <head><meta charset="utf-8"><title>PDF Test</title></head>
  <body><h1>Hello PDF</h1><p>Rendered by Chromium.</p></body>
</html>
HTML
```

Example with PDF options:

```bash
curl -sS -X POST "http://localhost:8080/api/v1/pdf?landscape=true&scale=0.9&paper_width=8.27&paper_height=11.69&margin_top=0.4&margin_bottom=0.4&margin_left=0.4&margin_right=0.4&print_background=true&page_ranges=1-2" \
  -H 'Content-Type: text/html; charset=utf-8' \
  --data-binary '<!doctype html><html><body><h1>PDF Options</h1></body></html>' \
  -o /tmp/options.pdf
```

### `GET /healthz`

Basic health check.

```bash
curl -sS http://localhost:8080/healthz
```

## Configuration

All configuration is done via environment variables:

| Variable | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | Address the HTTP server binds to. |
| `CHROME_ENDPOINT` | `http://127.0.0.1:9222` | Chromium debugging endpoint. |
| `CHROME_WS` | empty | Optional explicit DevTools websocket URL. If set, discovery is skipped. |
| `REQUEST_TIMEOUT` | `30s` | Per-request timeout for rendering. |
| `MAX_BODY_BYTES` | `5242880` | Max request body size in bytes (5 MiB). |
| `PDF_WAIT` | `0s` | Optional delay before printing (useful for async rendering). |

## Running locally

You need a running Chromium instance with remote debugging enabled, then run the server:

```bash
# example for local Chromium
chromium \
  --headless \
  --disable-gpu \
  --no-sandbox \
  --remote-debugging-address=127.0.0.1 \
  --remote-debugging-port=9222

# run the service
export CHROME_ENDPOINT=http://127.0.0.1:9222
go run .
```

## Container image (Alpine + supervisord)

The provided `Dockerfile` builds a statically linked binary and runs both Chromium and the API with supervisord.

```bash
docker build -t pdfrest .
docker run -p 8080:8080 pdfrest
```

## Implementation notes

- The server uses `chromedp` to connect to a remote Chromium instance.
- HTML is injected into `about:blank` and rendered via `page.SetDocumentContent`.
- PDF generation uses `page.PrintToPDF` with background printing enabled.
- The websocket URL is cached briefly to reduce overhead on repeated requests.
- The server enforces body size limits and request timeouts.

## File layout

- `main.go`: HTTP server, request handling, and PDF rendering logic.
- `supervisord.conf`: runs Chromium and the Go server in the container.
- `Dockerfile`: multi-stage build and Alpine runtime setup.
- `go.mod`: Go module definition.

## License

MIT
