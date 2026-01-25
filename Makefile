IMAGE_NAME := docker.io/snapps91/pdfrest
VERSION := $(shell cat VERSION)

.PHONY: build
build:
	go build -o bin/pdfrest .

.PHONY: image-build
image-build:
	podman build -f Containerfile -t $(IMAGE_NAME):$(VERSION) -t $(IMAGE_NAME):latest .

.PHONY: push
image-push:
	podman push $(IMAGE_NAME):$(VERSION)
	podman push $(IMAGE_NAME):latest

.PHONY: lint
lint:
	golangci-lint run ./...

.PHONY: test
test:
	go test -v ./...
