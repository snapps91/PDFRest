# PDF REST Micro Service

A minimal, production-ready **internal microservice** that turns raw HTML into a PDF using a running headless Chromium instance via the DevTools protocol.

The service exposes a single REST endpoint at `/api/v1/pdf`, accepts HTML in the request body, and returns a PDF stream as the response.

> ⚠️ **This project is designed to be used as an internal component of an application architecture, not as a public-facing service.**

## Intended usage

This software is **not meant to be used directly by end users** or exposed as a public API.

It is designed to act as a **dedicated rendering component** within a larger application, typically called by a backend service.

### Typical use case

In my specific case, this service was built to be used by **Laravel-based applications** as an internal PDF rendering engine.

This architectural choice provides several advantages:

* Keeps backend services **lightweight and focused**
* Avoids embedding **heavy dependencies** such as:

  * Headless browsers
  * Browser automation libraries
  * JavaScript / TypeScript runtimes
  * Rendering-specific tooling
* Improves **operational isolation**:

  * Chromium lifecycle is decoupled from the main backend
  * Rendering failures do not directly impact core business logic
* Enables **horizontal scaling** of the rendering layer independently

In this model, the Laravel application:

* Handles authentication, authorization, validation, and business logic
* Generates or sanitizes the HTML
* Calls this service **only from a trusted internal network**
* Streams the resulting PDF back to the client or stores it

## Why this service

This service is designed for fast, reliable HTML-to-PDF rendering with minimal operational overhead:

* **Single responsibility**: one endpoint that converts HTML to PDF
* **Headless Chromium**: uses the official DevTools protocol for accurate rendering
* **Lightweight Go server**: low memory usage and fast startup
* **Production-friendly (internal)**:

  * Request timeouts
  * Request size limits
  * Graceful shutdown
* **Container-ready**: ships with an Alpine-based container that runs both Chromium and the API via supervisord

## How it works

1. Chromium is started in headless mode with a remote debugging port
2. The Go service connects to Chromium through the DevTools websocket
3. The service injects the provided HTML into a blank page
4. Chromium renders the page and generates a PDF via `PrintToPDF`
5. The API returns the PDF bytes with `application/pdf` content type

## REST API

### `POST /api/v1/pdf`

* **Request body**: raw HTML (`text/html`) or any content type; the body is treated as HTML
* **Response**: `application/pdf` with an inline `Content-Disposition` header
* **Query parameters (optional)**:

  * `landscape` (bool)
  * `scale` (float)
  * `paper_width` (float, inches)
  * `paper_height` (float, inches)
  * `margin_top` (float, inches)
  * `margin_bottom` (float, inches)
  * `margin_left` (float, inches)
  * `margin_right` (float, inches)
  * `print_background` (bool)
  * `page_ranges` (string, e.g. `1-3,5`)

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

### `GET /healthz`

Basic health check.

Verifies that:

* the HTTP service is running
* the connection to Chromium is operational

Response: `200 OK` with body `ok`.

```bash
curl -sS http://localhost:8080/healthz
```

## Security considerations ⚠️

This service is **NOT secure by design for public exposure**.

It must be considered a **trusted internal component** and treated accordingly.

### Important notes

* ❌ **DO NOT expose this service directly to the Internet**
* ❌ **DO NOT use it as a public PDF rendering API**
* ❌ **DO NOT accept untrusted input without validation**

### Recommended setup

* Run the service:

  * On a private network
  * Inside a protected container environment
  * Behind an internal load balancer if needed
* Always place a **solid backend in front of it** (e.g. Laravel, Go, Java, etc.)
* Let the backend handle:

  * Authentication & authorization
  * Input validation & sanitization
  * Rate limiting
  * Access control
  * Audit logging

Think of this service as a **PDF rendering engine**, not as an API gateway.

## Configuration

All configuration is done via environment variables:

| Variable          | Default                 | Description                              |
| ----------------- | ----------------------- | ---------------------------------------- |
| `ADDR`            | `:8080`                 | Address the HTTP server binds to         |
| `CHROME_ENDPOINT` | `http://127.0.0.1:9222` | Chromium debugging endpoint              |
| `CHROME_WS`       | empty                   | Optional explicit DevTools websocket URL |
| `REQUEST_TIMEOUT` | `30s`                   | Per-request timeout for rendering        |
| `MAX_BODY_BYTES`  | `5242880`               | Max request body size in bytes (5 MiB)   |
| `PDF_WAIT`        | `0s`                    | Optional delay before printing           |

---

## Running locally

You need a running Chromium instance with remote debugging enabled:

```bash
chromium \
  --headless \
  --disable-gpu \
  --no-sandbox \
  --remote-debugging-address=127.0.0.1 \
  --remote-debugging-port=9222
```

Then run the service:

```bash
export CHROME_ENDPOINT=http://127.0.0.1:9222
go run .
```

## Container image (Alpine + supervisord)

The provided `Containerfile` builds a statically linked binary and runs both Chromium and the API with supervisord.

```bash
docker build -t pdfrest .
docker run -p 8080:8080 pdfrest
```

> ⚠️ Even when containerized, the same **security considerations apply**: keep it internal.

## License

MIT License. See [LICENSE](LICENSE) for details.