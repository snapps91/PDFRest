# PDF REST Micro Service
![Version](https://img.shields.io/badge/version-1.3.0-blue)
![CI](https://img.shields.io/github/actions/workflow/status/snapps91/pdfrest/ci.yml?branch=main)

A minimal, production-ready **internal microservice** that turns raw HTML into a PDF using headless Chromium via the DevTools protocol.

The service exposes a single REST endpoint at `/api/v1/pdf`, accepts HTML in the request body, and returns a PDF stream as the response.

> ⚠️ **This project is designed to be used as an internal component of an application architecture, not as a public-facing service.**

## Intended usage

This software is **not meant to be used directly by end users** or exposed as a public API.

It is designed to act as a **dedicated rendering component** within a larger application, typically called by a backend service.

## Service 
The idea behind this service is to provide fast and reliable HTML-to-PDF rendering while keeping operational complexity to a minimum. The goal was to build something focused, predictable, and easy to run in production—especially in internal environments.

One of the key design choices that makes the service both fast and resource-efficient is how Chromium is managed. Chromium is started lazily by the Go service when the first PDF request arrives, reused across requests, and stopped after a configurable period with no rendering activity. A later request starts it again automatically. Active and concurrent renderings hold a lease on the browser, so the idle timeout can never stop it while work is in progress.

To support this model, the service includes a custom-built DevTools protocol client, implemented directly over WebSockets and inspired by the chromedp protocol—without relying on heavy external dependencies. This lightweight, in-process client allows the service to communicate efficiently with Chromium while keeping memory usage low and performance predictable.

The API itself is implemented as a lightweight Go server, chosen for its fast startup times and small memory footprint. From an operational perspective, the service is designed to be safe and production-friendly for internal use: it enforces request timeouts and size limits, supports graceful shutdowns, and allows the rendering layer to be scaled horizontally and independently.

The health endpoint does not wake a stopped browser, so liveness and readiness probes do not defeat the memory-saving behavior. When Chromium is running, the same endpoint still verifies the live CDP connection.

Finally, the service is container-ready by design. It ships with an Alpine-based image where the Go service directly owns the Chromium child process; a minimal init forwards signals and reaps processes, but no separate process supervisor manages or keeps Chromium alive. Graceful service shutdown also terminates Chromium and its renderer processes.

## Official Docker Hub image
You can pull the official image from Docker Hub:

Docker Hub link: https://hub.docker.com/r/snapps91/pdfrest

```bash
docker pull snapps91/pdfrest:latest
docker run --name pdfrest --rm -p 8080:8080 \
  -e CHROME_IDLE_TIMEOUT=5m \
  snapps91/pdfrest:latest
```


## REST API

### `POST /api/v1/pdf`

* **Request body**: raw HTML (`text/html`) or any content type; the body is treated as HTML
* **Response**: `application/pdf` with an inline `Content-Disposition` header
* **Query parameters (optional)**:

  * `landscape` (bool)
  * `scale` (float)
  * `paper_width` (float, inches by default; suffix `mm` or `px` to convert)
  * `paper_height` (float, inches by default; suffix `mm` or `px` to convert)
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
* the managed Chromium executable and endpoint configuration are available, when currently stopped
* the CDP connection is operational, when Chromium is running

The check never starts Chromium and does not reset its idle timeout.

Response: `200 OK` with body `ok`.

```bash
curl -sS http://localhost:8080/healthz
```

## Configuration

All configuration is done via environment variables:

| Variable                  | Default                 | Description                                                                 |
| ------------------------- | ----------------------- | --------------------------------------------------------------------------- |
| `ADDR`                    | `:8080`                 | Address the HTTP server binds to                                            |
| `CHROME_AUTO_START`       | `true`                  | Start and stop a local Chromium process on demand                           |
| `CHROME_BIN`              | auto-detected           | Chromium/Chrome executable name or absolute path                            |
| `CHROME_ENDPOINT`         | `http://127.0.0.1:9222` | Local debugging endpoint, or the remote endpoint when automatic start is off |
| `CHROME_WS`               | empty                   | Explicit DevTools websocket URL; automatically disables managed startup     |
| `CHROME_USER_DATA_DIR`    | temporary per process   | Optional persistent Chromium profile directory                              |
| `CHROME_IDLE_TIMEOUT`     | `5m`                    | Stop Chromium after this much rendering inactivity; `0` keeps it running    |
| `CHROME_STARTUP_TIMEOUT`  | `10s`                   | Maximum time to wait for a newly started Chromium                           |
| `CHROME_SHUTDOWN_TIMEOUT` | `5s`                    | Grace period before Chromium is force-killed                                |
| `REQUEST_TIMEOUT`         | `30s`                   | Per-request timeout, including a possible browser startup                   |
| `MAX_BODY_BYTES`          | `5242880`               | Max request body size in bytes (5 MiB)                                      |
| `PDF_WAIT`                | `0s`                    | Optional delay before printing                                              |
| `CDP_POOL_SIZE`           | `4`                     | Maximum number of reusable idle CDP sessions                                |

---

## Running locally

Install Chromium or Google Chrome, then run the service. The executable is auto-detected from common names and locations; use `CHROME_BIN` when it is elsewhere:

```bash
git clone https://github.com/snapps91/PDFRest.git 
cd PDFRest
go run ./src
```

Chromium remains stopped until the first `POST /api/v1/pdf`. To use an externally managed or remote Chrome instead, set `CHROME_AUTO_START=false` and configure `CHROME_ENDPOINT` or `CHROME_WS`.

## License

MIT License. See [LICENSE](LICENSE) for details.
