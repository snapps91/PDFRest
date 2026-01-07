#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-http://127.0.0.1:8080}"
ENDPOINT="${ENDPOINT:-/api/v1/pdf}"
TOTAL_REQUESTS="${TOTAL_REQUESTS:-1000}"
CONCURRENCY="${CONCURRENCY:-100}"
OUT_DIR="${OUT_DIR:-/tmp/pdfrest-bench}"

HTML_PAYLOAD='<!doctype html><html><head><meta charset="utf-8"><title>Bench</title></head><body><h1>Benchmark</h1><p>PDF render test</p></body></html>'

mkdir -p "$OUT_DIR"

printf "Running %s requests with concurrency %s to %s%s\n" "$TOTAL_REQUESTS" "$CONCURRENCY" "$HOST" "$ENDPOINT"

start_ts=$(date +%s)
seq 1 "$TOTAL_REQUESTS" | xargs -P"$CONCURRENCY" -I{} -- \
  curl -sS -X POST "$HOST$ENDPOINT" \
    -H 'Content-Type: text/html; charset=utf-8' \
    --data-binary "$HTML_PAYLOAD" \
    -o "$OUT_DIR/resp-{}.pdf"
end_ts=$(date +%s)

elapsed=$((end_ts - start_ts))
if [ "$elapsed" -eq 0 ]; then
  elapsed=1
fi

rps=$((TOTAL_REQUESTS / elapsed))

printf "Completed in %ss (%s req/s)\n" "$elapsed" "$rps"
printf "Output PDFs: %s\n" "$OUT_DIR"


# Remove all generated PDF files after benchmark
rm -rf "$OUT_DIR"
