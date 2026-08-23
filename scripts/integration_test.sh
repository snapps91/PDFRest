#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-pdfrest:integration-test}"
PORT="${PORT:-18080}"
CONTAINER_NAME="${CONTAINER_NAME:-pdfrest-integration-test-$$}"
BASE_URL="http://127.0.0.1:${PORT}"
TMP_DIR="$(mktemp -d)"

cleanup() {
  status=$?
  trap - EXIT

  if [ "$status" -ne 0 ]; then
    docker logs "$CONTAINER_NAME" 2>/dev/null || true
  fi

  docker rm --force "$CONTAINER_NAME" >/dev/null 2>&1 || true
  rm -rf "$TMP_DIR"
  exit "$status"
}
trap cleanup EXIT

docker build --file Containerfile --tag "$IMAGE" .
docker run \
  --detach \
  --name "$CONTAINER_NAME" \
  --publish "127.0.0.1:${PORT}:8080" \
  "$IMAGE" >/dev/null

healthy=false
for _ in $(seq 1 60); do
  if curl --fail --silent "$BASE_URL/healthz" --output "$TMP_DIR/health.txt"; then
    healthy=true
    break
  fi

  if [ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME")" != "true" ]; then
    break
  fi

  sleep 1
done

if [ "$healthy" != "true" ] || [ "$(<"$TMP_DIR/health.txt")" != "ok" ]; then
  echo "Application did not become healthy" >&2
  exit 1
fi

status_code="$(curl \
  --silent \
  --output "$TMP_DIR/method-not-allowed.txt" \
  --write-out '%{http_code}' \
  "$BASE_URL/api/v1/pdf")"
if [ "$status_code" != "405" ]; then
  echo "Expected GET /api/v1/pdf to return 405, got $status_code" >&2
  exit 1
fi

status_code="$(curl \
  --silent \
  --request POST \
  --output "$TMP_DIR/empty-body.txt" \
  --write-out '%{http_code}' \
  "$BASE_URL/api/v1/pdf")"
if [ "$status_code" != "400" ]; then
  echo "Expected an empty PDF request to return 400, got $status_code" >&2
  exit 1
fi

HTML_PAYLOAD='<!doctype html><html><head><meta charset="utf-8"><title>Integration test</title></head><body><h1>pdfrest</h1><p>End-to-end test</p></body></html>'

curl \
  --fail \
  --silent \
  --show-error \
  --dump-header "$TMP_DIR/headers.txt" \
  --header 'Content-Type: text/html; charset=utf-8' \
  --data-binary "$HTML_PAYLOAD" \
  "$BASE_URL/api/v1/pdf?paper_width=210mm&paper_height=297mm&print_background=true" \
  --output "$TMP_DIR/document.pdf"

if ! grep -Eiq '^content-type:[[:space:]]*application/pdf[[:space:]]*$' "$TMP_DIR/headers.txt"; then
  echo "PDF response has an unexpected Content-Type" >&2
  sed -n '1,20p' "$TMP_DIR/headers.txt" >&2
  exit 1
fi

if ! grep -Eiq '^content-disposition:[[:space:]]*inline;[[:space:]]*filename="document\.pdf"[[:space:]]*$' "$TMP_DIR/headers.txt"; then
  echo "PDF response has an unexpected Content-Disposition" >&2
  sed -n '1,20p' "$TMP_DIR/headers.txt" >&2
  exit 1
fi

if [ "$(LC_ALL=C head -c 5 "$TMP_DIR/document.pdf")" != "%PDF-" ]; then
  echo "Response body is not a PDF document" >&2
  exit 1
fi

pdf_size="$(wc -c <"$TMP_DIR/document.pdf")"
if [ "$pdf_size" -lt 1000 ]; then
  echo "Generated PDF is unexpectedly small: $pdf_size bytes" >&2
  exit 1
fi

echo "End-to-end test passed; generated PDF size: $pdf_size bytes"
