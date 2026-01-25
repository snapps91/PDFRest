IMAGE_NAME := docker.io/snapps91/pdfrest
VERSION := $(shell cat VERSION)

.PHONY: build

build:
	podman build -f Containerfile -t $(IMAGE_NAME):$(VERSION) -t $(IMAGE_NAME):latest .
