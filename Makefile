.PHONY: build test lint fmt clean

BINARY := agent-sql
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/agent-sql

test:
	go test ./... -count=1

test-short:
	go test ./... -count=1 -short

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .
	goimports -w .

clean:
	rm -f $(BINARY)
	rm -f release/agent-sql-*

# Run in dev mode
dev:
	go run ./cmd/agent-sql $(ARGS)

# Typecheck only (go vet)
vet:
	go vet ./...
