# Sales Copilot backend — developer tasks. Run `make help` for the list.
.DEFAULT_GOAL := help

DATABASE_URL ?= postgres://copilot:copilot@localhost:5432/copilot?sslmode=disable

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## Resolve and lock Go dependencies
	go mod tidy

.PHONY: build
build: ## Build all binaries
	go build ./...

.PHONY: run-gateway
run-gateway: ## Run the realtime gateway locally
	go run ./cmd/gateway

.PHONY: test
test: ## Run tests
	go test ./...

.PHONY: lint
lint: ## Run linters (requires golangci-lint)
	golangci-lint run

.PHONY: fmt
fmt: ## Format code
	gofmt -w .

.PHONY: up
up: ## Start local stores (postgres, redis, minio, nats, otel)
	docker compose up -d

.PHONY: down
down: ## Stop local stores
	docker compose down

.PHONY: migrate
migrate: ## Apply SQL migrations to the local database
	@for f in migrations/*.sql; do \
		echo "applying $$f"; \
		psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f $$f; \
	done
