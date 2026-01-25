# PDF REST Micro Service
![Version](https://img.shields.io/badge/version-1.1.2-blue)
![CI](https://img.shields.io/github/actions/workflow/status/snapps91/pdfrest/ci.yml?branch=main)

A minimal, production-ready **internal microservice** that turns raw HTML into a PDF using a running headless Chromium instance via the DevTools protocol.

The service exposes a single REST endpoint at `/api/v1/pdf`, accepts HTML in the request body, and returns a PDF stream as the response.

> ⚠️ **This project is designed to be used as an internal component of an application architecture, not as a public-facing service.**

## Intended usage

This software is **not meant to be used directly by end users** or exposed as a public API.

It is designed to act as a **dedicated rendering component** within a larger application, typically called by a backend service.

## The service idea
The idea behind this service is to provide fast and reliable HTML-to-PDF rendering while keeping operational complexity to a minimum. The goal was to build something focused, predictable, and easy to run in production—especially in internal environments.

The initial inspiration came from using Browsershot by Spatie. Browsershot offers a very convenient way—via Node.js—to take a Blade template or any HTML content and turn it into a PDF by launching a Chrome instance behind the scenes. While this approach works well, it also introduces a full Node.js runtime and browser tooling into the stack, which isn’t always ideal for lightweight or tightly controlled backend environments.

This service follows the same core principle—delegating rendering to Headless Chromium—but implements it as a standalone internal service. It exposes a single, clearly defined endpoint whose only responsibility is converting HTML into a PDF. Rendering is handled through Headless Chromium using the official DevTools protocol, ensuring accurate and consistent output.

One of the key design choices that makes the service both fast and highly scalable is how Chromium is managed. Instead of spawning a new Chromium process for every incoming request, a single Chromium instance is started when the service boots and kept running for its entire lifetime. This completely removes the overhead of repeated browser startups, which is often one of the most expensive parts of HTML-to-PDF rendering.

To support this model, the service includes a custom-built DevTools protocol client, implemented directly over WebSockets and inspired by the chromedp protocol—without relying on heavy external dependencies. This lightweight, in-process client allows the service to communicate efficiently with Chromium while keeping memory usage low and performance predictable.

The API itself is implemented as a lightweight Go server, chosen for its fast startup times and small memory footprint. From an operational perspective, the service is designed to be safe and production-friendly for internal use: it enforces request timeouts and size limits, supports graceful shutdowns, and allows the rendering layer to be scaled horizontally and independently.

Finally, the service is container-ready by design. It ships with an Alpine-based container that runs both Chromium and the API—managed via supervisord—making deployment simple and consistent across environments, while still delivering excellent performance and low resource consumption.

### Typical use case
In my case, this service was designed to support Laravel-based applications by acting as an internal PDF rendering engine. Instead of handling PDF generation directly within the main backend, the responsibility is delegated to a dedicated service.

This architectural approach comes with several practical benefits. First, it keeps backend services lean and focused on what they do best, without forcing them to carry the burden of PDF rendering. It also avoids the need to embed heavy and complex dependencies—such as headless browsers, browser automation tools, JavaScript or TypeScript runtimes, and other rendering-specific tooling—directly into the application.

Another key advantage is operational isolation. The lifecycle of Chromium is fully decoupled from the main backend, which means rendering issues or crashes don’t directly affect core business logic. On top of that, the rendering layer can be scaled horizontally and independently, making it easier to adapt to increased load without impacting the rest of the system.

Within this setup, the Laravel application remains responsible for authentication, authorization, validation, and business logic. It generates or sanitizes the HTML, calls the rendering service only over a trusted internal network, and then either streams the resulting PDF back to the client or stores it for later use.

                           +-----------------------------+
                           |        End User / Client    |
                           +--------------+--------------+
                                          |
                                          |  HTTP request (PDF endpoint)
                                          v
                           +--------------+--------------+
                           |     Backend Application     |
                           |  - Auth / Business logic    |
                           |  - Generate/Sanitize HTML   |
                           +--------------+--------------+
                                          |
                                          |  Internal network call
                                          |  POST /render (HTML payload)
                                          v
        +---------------------------------+------------------------------------+
        |                         PDF Rendering Service                        |
        |                         (Go HTTP API)                                |
        |                                                                      |
        |   +---------------------+        +-------------------------------+   |
        |   |  Request handling   |        |   Limits & Reliability        |   |
        |   |  - Single endpoint  |        |   - Timeouts                  |   |
        |   |  - Stream response  |------->|   - Size limits               |   |
        |   +----------+----------+        |   - Graceful shutdown         |   |
        |              |                   +-------------------------------+   |
        |              |          ^                                            | 
        |              | DevTools Protocol (WebSocket)                         |
        |              v                                                       |
        |   +---------------------+        +-------------------------------+   |
        |   |  Internal DevTools  |<------>|   Headless Chromium (warm)    |   |
        |   |  Client (custom)    |        |   - Started once at boot      |   |
        |   |  - No heavy deps    |        |   - Reused across requests    |   |
        |   |  - Lightweight WS   |        |   - Stable lifecycle          |   |
        |   +---------------------+        +-------------------------------+   |
        |                                                                      |
        |   Container (Alpine) runs:                                           |
        |   - Go API + Chromium, supervised via supervisord                    |
        +---------------------------------+------------------------------------+
                                          |
                                          |  PDF stream (binary)
                                          v
                           +--------------+--------------+
                           |     Backend Application     |
                           |  - Stream to client OR      |
                           |    store (S3/disk/etc.)     |
                           +--------------+--------------+
                                          |
                                          v
                           +--------------+--------------+
                           |        End User / Client    |
                           +-----------------------------+


## Official Docker Hub image
You can pull the official image from Docker Hub:

Docker Hub link: https://hub.docker.com/r/snapps91/pdfrest

```bash
docker pull snapps91/pdfrest:latest
docker run --name pdfrest --rm -p 8080:8080 snapps91/pdfrest:latest
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
* the connection to Chromium is operational

Response: `200 OK` with body `ok`.

```bash
curl -sS http://localhost:8080/healthz
```

## Security considerations ⚠️

This service is **NOT secure by design for public exposure**.

It must be considered a **trusted internal component** and treated accordingly.

### Important notes

❌ **DO NOT expose this service directly to the Internet**
❌ **DO NOT use it as a public PDF rendering API**
❌ **DO NOT accept untrusted input without validation**

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


## License

MIT License. See [LICENSE](LICENSE) for details.
