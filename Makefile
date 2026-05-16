IMAGE_NAME := docker.io/snapps91/pdfrest
VERSION := $(shell cat VERSION)

.PHONY: build
build:
	go build -o bin/pdfrest ./src

.PHONY: image-build
image-build:
	podman build -f Containerfile -t $(IMAGE_NAME):$(VERSION) -t $(IMAGE_NAME):latest .

.PHONY: lint
lint:
	golangci-lint run ./...
	go vet -v ./src
	gofmt -l ./src

.PHONY: test
test:
	go test -v ./src
