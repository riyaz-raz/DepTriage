.PHONY: build test lint snapshot

BINARY := deptriage
VERSION ?= dev

build:
	go build -ldflags "-X main.version=$(VERSION)" -o $(BINARY) ./cmd/deptriage

test:
	go test ./...

lint:
	go vet ./...
	gofmt -w $$(find cmd internal -name '*.go')

snapshot:
	go test ./... -update
