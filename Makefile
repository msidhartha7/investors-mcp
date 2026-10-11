BINARY := investors-mcp

# Load MONGODB_URI / MONGODB_DB (and friends) from .env.local when present.
-include .env.local
export

.DEFAULT_GOAL := help
.PHONY: help build run test vet lint fmt check inspect claude-add db-up db-down clean env-check

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build the server binary
	go build -o $(BINARY) .

run: build env-check ## Build and run the server over stdio
	./$(BINARY)

test: ## Run tests
	go test ./...

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

fmt: ## Format code
	gofmt -w .

check: vet lint test ## Vet, lint, and test

inspect: build env-check db-up ## Open the server in the MCP Inspector
	npx @modelcontextprotocol/inspector ./$(BINARY)

claude-add: build env-check ## Register the server with Claude Code (as "investors")
	claude mcp add investors -e MONGODB_URI="$(MONGODB_URI)" -e MONGODB_DB="$(MONGODB_DB)" -- $(CURDIR)/$(BINARY)

db-up: ## Start the local MongoDB container
	scripts/local-mongo.sh up

db-down: ## Stop the local MongoDB container
	scripts/local-mongo.sh down

clean: ## Remove the built binary
	rm -f $(BINARY)

env-check:
	@test -n "$(MONGODB_URI)" || { echo "MONGODB_URI is not set (expected in .env.local)"; exit 1; }
	@test -n "$(MONGODB_DB)" || { echo "MONGODB_DB is not set (expected in .env.local)"; exit 1; }
