.PHONY: build test test-short test-integration test-docker-up test-docker-down lint fmt clean

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

# Docker integration tests
test-docker-up:
	docker compose -f docker-compose.test.yml up -d
	@echo "Waiting for containers to be healthy..."
	@for svc in pg mysql mariadb mssql; do \
		printf "  %-10s" "$$svc"; \
		retries=0; \
		while [ $$retries -lt 30 ]; do \
			status=$$(docker compose -f docker-compose.test.yml ps --format json $$svc 2>/dev/null | grep -o '"Health":"[^"]*"' | head -1); \
			if echo "$$status" | grep -q healthy; then \
				echo "ready"; \
				break; \
			fi; \
			retries=$$((retries + 1)); \
			sleep 2; \
		done; \
		if [ $$retries -eq 30 ]; then echo "TIMEOUT"; fi; \
	done

test-docker-down:
	docker compose -f docker-compose.test.yml down -v

test-integration: test-docker-up
	@echo "Seeding test databases..."
	./scripts/test-seed.sh
	@echo "Running integration tests..."
	go test ./internal/integration/ -count=1 -v -timeout 120s
	@echo "Tearing down containers..."
	$(MAKE) test-docker-down

# Typecheck only (go vet)
vet:
	go vet ./...
