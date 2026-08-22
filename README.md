# pdfrest

[![Version](https://img.shields.io/badge/version-1.3.0-blue)](VERSION)
[![CI](https://img.shields.io/github/actions/workflow/status/snapps91/pdfrest/ci.yml?branch=main)](https://github.com/snapps91/pdfrest/actions/workflows/ci.yml)

**HTML in. PDF out.**

pdfrest is a small, production-ready internal service that turns HTML into PDF through a single REST call. It combines a minimal Go API with the rendering quality of headless Chromium.

- **Simple API** — send HTML to one endpoint and receive a PDF.
- **Ready to run** — the container includes the service, Chromium, fonts, and init handling.
- **Lean by design** — no third-party Go modules and no browser automation framework.
- **Efficient at runtime** — Chromium starts on demand, is reused across requests, and stops when idle.

> [!IMPORTANT]
> pdfrest is intended to be called by a trusted backend inside an application architecture. It does not provide authentication or tenant isolation and should not be exposed directly as a public API.

## Quick start

Start the official container:

```bash
docker run --name pdfrest --rm -p 8080:8080 snapps91/pdfrest:latest
```

Send some HTML and save the generated PDF:

```bash
curl --fail --silent --show-error \
  -H 'Content-Type: text/html; charset=utf-8' \
  --data-binary '<!doctype html><h1>Hello PDF</h1><p>Rendered by Chromium.</p>' \
  http://localhost:8080/api/v1/pdf \
  --output hello.pdf
```

That is the complete integration: an HTTP request containing HTML and an `application/pdf` response. On the first request, pdfrest starts Chromium automatically.

The official image is available on [Docker Hub](https://hub.docker.com/r/snapps91/pdfrest).

## Small API, capable runtime

The public interface stays deliberately small while pdfrest handles the browser lifecycle and rendering details internally:

```text
POST HTML -> acquire or start Chromium -> render through CDP -> return PDF -> release
```

Under the hood, pdfrest provides:

- lazy Chromium startup and automatic idle shutdown;
- browser leases that protect active and concurrent renders from shutdown;
- reusable Chrome DevTools Protocol sessions for better throughput;
- streamed PDF transfer from Chromium;
- request timeouts, body-size limits, and graceful shutdown;
- side-effect-free health checks that do not wake an idle browser;
- support for either a locally managed or remote Chromium instance;
- a direct WebSocket/CDP client implemented in Go, without heavy external dependencies.

The result is a focused rendering component that is easy to deploy, predictable to operate, and straightforward to scale horizontally.

## REST API

### `POST /api/v1/pdf`

The request body is treated as HTML regardless of its content type. A successful response contains the generated PDF with:

```http
Content-Type: application/pdf
Content-Disposition: inline; filename="document.pdf"
```

An empty body returns `400 Bad Request`. A body larger than `MAX_BODY_BYTES` returns `413 Request Entity Too Large`.

#### Query parameters

All parameters are optional.

| Parameter | Value | Description |
| --- | --- | --- |
| `landscape` | boolean | Use landscape orientation |
| `scale` | number | Set the page rendering scale |
| `paper_width` | length | Paper width; inches by default, or use `in`, `mm`, or `px` |
| `paper_height` | length | Paper height; inches by default, or use `in`, `mm`, or `px` |
| `margin_top` | number | Top margin in inches |
| `margin_bottom` | number | Bottom margin in inches |
| `margin_left` | number | Left margin in inches |
| `margin_right` | number | Right margin in inches |
| `print_background` | boolean | Print CSS backgrounds; defaults to `true` |
| `page_ranges` | string | Pages to print, for example `1-3,5` |

Example with custom page settings:

```bash
curl --fail --silent --show-error \
  -H 'Content-Type: text/html; charset=utf-8' \
  --data-binary @invoice.html \
  'http://localhost:8080/api/v1/pdf?paper_width=210mm&paper_height=297mm&margin_top=0.5&margin_bottom=0.5' \
  --output invoice.pdf
```

### `GET /healthz`

Returns `200 OK` with the body `ok` when the service is healthy.

```bash
curl --fail http://localhost:8080/healthz
```

The check behaves according to the current browser state:

- when Chromium is stopped, it verifies the executable and endpoint configuration without starting it;
- when Chromium is running, it verifies the live CDP connection.

Health checks never start Chromium or reset its idle timeout.

## Configuration

Configuration is provided through environment variables.

| Variable | Default | Description |
| --- | --- | --- |
| `ADDR` | `:8080` | Address the HTTP server binds to |
| `CHROME_AUTO_START` | `true` | Start and stop a local Chromium process on demand |
| `CHROME_BIN` | auto-detected | Chromium or Chrome executable name or absolute path |
| `CHROME_ENDPOINT` | `http://127.0.0.1:9222` | Local debugging endpoint, or remote endpoint when automatic startup is disabled |
| `CHROME_WS` | empty | Explicit DevTools WebSocket URL; setting it disables managed startup |
| `CHROME_USER_DATA_DIR` | temporary per process | Optional persistent Chromium profile directory |
| `CHROME_IDLE_TIMEOUT` | `5m` | Stop Chromium after this much rendering inactivity; `0` keeps it running |
| `CHROME_STARTUP_TIMEOUT` | `10s` | Maximum time to wait for Chromium to start |
| `CHROME_SHUTDOWN_TIMEOUT` | `5s` | Grace period before Chromium is force-killed |
| `REQUEST_TIMEOUT` | `30s` | Per-request timeout, including a possible browser startup |
| `MAX_BODY_BYTES` | `5242880` | Maximum request body size in bytes (5 MiB) |
| `PDF_WAIT` | `0s` | Optional delay before printing, useful for asynchronously rendered content |
| `CDP_POOL_SIZE` | `4` | Maximum number of reusable idle CDP sessions |

For example, keep Chromium alive for 15 minutes after the latest render:

```bash
docker run --name pdfrest --rm -p 8080:8080 \
  -e CHROME_IDLE_TIMEOUT=15m \
  snapps91/pdfrest:latest
```

### Remote Chromium

To connect to an externally managed browser, disable automatic startup and configure either its HTTP debugging endpoint or WebSocket URL:

```bash
CHROME_AUTO_START=false \
CHROME_ENDPOINT=http://chrome:9222 \
./bin/pdfrest
```

## Run locally

Requirements:

- Go 1.25 or later;
- Chromium or Google Chrome.

Clone and start the service:

```bash
git clone https://github.com/snapps91/pdfrest.git
cd pdfrest
go run ./src
```

The browser executable is detected from common names and locations. Set `CHROME_BIN` when it is installed elsewhere:

```bash
CHROME_BIN=/path/to/chromium go run ./src
```

Chromium remains stopped until the first PDF request. To build or test the project instead:

```bash
make build
make test
```

## License

[MIT](LICENSE)
