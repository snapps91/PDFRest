# Changelog

All notable changes to this project will be documented in this file.

## 1.3.0
- Moved Chromium lifecycle management into the Go service.
- Added on-demand browser startup, request leases, idle shutdown, and automatic restart.
- Made health checks side-effect free so probes do not start or keep Chromium alive.
- Added configuration for the Chrome binary, profile, and lifecycle timeouts.
- Removed supervisord; `pdfrest` now owns Chromium directly and `tini` only handles init duties.
- Added concurrent lifecycle and race-safety tests.

## 1.2.0
- Reorganized project structure: moved all Go source files into a `src/` subdirectory.
- Kept all existing functionality intact; no behavioral changes.

## 1.1.4
- Implemented a session pooling system for Chrome DevTools Protocol (CDP) WebSocket connections.
- Added streamed PDF reading through the CDP `ReturnAsStream` transfer mode.
- Added the `CDP_POOL_SIZE` configuration option to control reusable CDP sessions.
- Improved rendering throughput by reusing CDP sessions across PDF requests.

## 1.1.3
- Removed external WebSocket dependencies by implementing a native RFC6455 client.
- Preserved the existing CDP request/response flow while switching transports.
- Added PDF_TIME to the /api/v1/pdf call log. This displays the conversion time Chromium takes to generate the PDF.

## 1.1.2
- Replaced chromedp with a minimal CDP websocket client for Chrome automation.
- Kept PDF rendering flow intact while switching to direct CDP calls.
- Updated Chrome health checks to use the new CDP client.

## 1.0.0
- Initial release of pdfrest
