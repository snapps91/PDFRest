IMAGE_NAME := docker.io/snapps91/pdfrest
VERSION := $(shell cat VERSION)
CHROME_BIN ?= $(shell \
	if [ -x "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" ]; then \
		printf '%s' "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"; \
	elif command -v google-chrome >/dev/null 2>&1; then \
		command -v google-chrome; \
	elif command -v google-chrome-stable >/dev/null 2>&1; then \
		command -v google-chrome-stable; \
	elif command -v chromium >/dev/null 2>&1; then \
		command -v chromium; \
	elif command -v chromium-browser >/dev/null 2>&1; then \
		command -v chromium-browser; \
	else \
		printf '%s' google-chrome; \
	fi)
CHROME_DEBUG_ADDRESS ?= 127.0.0.1
CHROME_DEBUG_PORT ?= 9222
CHROME_USER_DATA_DIR ?= /tmp/pdfrest-chrome

.PHONY: build
build:
	go build -o bin/pdfrest ./src

# Apple Silicon users
.PHONY: image-build
image-build:
	container build -f Containerfile -t $(IMAGE_NAME):$(VERSION) -t $(IMAGE_NAME):latest . --platform linux/amd64,linux/arm64

.PHONY: lint
lint:
	golangci-lint run ./...
	go vet -v ./src
	gofmt -l ./src

.PHONY: test
test:
	go test -v ./src

.PHONY: chrome
chrome:
	@if [ ! -x "$(CHROME_BIN)" ] && ! command -v "$(CHROME_BIN)" >/dev/null 2>&1; then \
		echo "Chrome non trovato. Imposta CHROME_BIN con il percorso del binario." >&2; \
		exit 1; \
	fi
	"$(CHROME_BIN)" \
		--headless \
		--disable-gpu \
		--no-sandbox \
		--remote-debugging-address=$(CHROME_DEBUG_ADDRESS) \
		--remote-debugging-port=$(CHROME_DEBUG_PORT) \
		--user-data-dir=$(CHROME_USER_DATA_DIR) \
		about:blank
