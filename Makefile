# IdeaVault developer commands. Run `make help`.
SHELL := /bin/bash
.DEFAULT_GOAL := help

-include .env
export

POSTGRES_PORT ?= 5432
DATABASE_URL ?= postgres://ideavault:ideavault@localhost:$(POSTGRES_PORT)/ideavault?sslmode=disable
TEST_DATABASE_URL ?= postgres://ideavault:ideavault@localhost:$(POSTGRES_PORT)/ideavault_test?sslmode=disable
API_DIR := apps/api
WEB_DIR := apps/web

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- setup
.PHONY: setup
setup: ## Install dependencies (Go modules, npm packages, Playwright browser)
	cd $(API_DIR) && go mod download
	cd $(WEB_DIR) && npm ci && npx playwright install chromium
	@test -f .env || (cp .env.example .env && echo "Created .env from .env.example")

.PHONY: up
up: ## Start PostgreSQL (pgvector) and Redis
	docker compose up -d postgres redis
	@echo "Waiting for PostgreSQL…"; for i in $$(seq 1 40); do docker compose exec -T postgres pg_isready -U ideavault -d ideavault >/dev/null 2>&1 && break; sleep 1; done; echo ready

.PHONY: down
down: ## Stop local infrastructure
	docker compose down

.PHONY: reset-db
reset-db: ## Drop and recreate the local databases (DESTROYS local data)
	docker compose down -v && $(MAKE) up

# ---------------------------------------------------------------- run
.PHONY: api
api: ## Run the API on :8080 (auto-migrates)
	cd $(API_DIR) && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/api

.PHONY: web
web: ## Run the web app on :3000
	cd $(WEB_DIR) && npm run dev

.PHONY: dev
dev: up ## Run infrastructure + API + web together (Ctrl+C stops both)
	@trap 'kill 0' INT TERM; $(MAKE) api & $(MAKE) web & wait

.PHONY: seed
seed: ## Seed a demo vault through the API (local development only)
	API=http://localhost:$${PORT:-8080} node tests/fixtures/seed/seed.mjs

# ---------------------------------------------------------------- database
.PHONY: migrate
migrate: ## Apply migrations
	cd $(API_DIR) && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate up

.PHONY: migrate-status
migrate-status: ## Show migration status
	cd $(API_DIR) && DATABASE_URL="$(DATABASE_URL)" go run ./cmd/migrate status

.PHONY: migrate-verify
migrate-verify: ## Prove migrations are reversible (up → down → up) on the test database
	cd $(API_DIR) && DATABASE_URL="$(TEST_DATABASE_URL)" go run ./cmd/migrate verify

# ---------------------------------------------------------------- quality
.PHONY: fmt
fmt: ## Format Go and TypeScript
	cd $(API_DIR) && gofmt -w .
	cd $(WEB_DIR) && npx prettier --write "app/**/*.{ts,tsx}" "features/**/*.{ts,tsx}" "components/**/*.{ts,tsx}" "lib/**/*.ts" --log-level warn

.PHONY: lint
lint: ## Lint Go (vet, gofmt) and web (eslint, tsc)
	cd $(API_DIR) && test -z "$$(gofmt -l .)" && go vet ./...
	cd $(WEB_DIR) && npm run lint && npm run typecheck

.PHONY: test
test: test-api test-web ## Run all unit + integration tests

.PHONY: test-api
test-api: ## Go unit + integration tests (needs `make up` for DB-backed tests)
	cd $(API_DIR) && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./... -count=1

.PHONY: test-web
test-web: ## Frontend unit tests (Vitest)
	cd $(WEB_DIR) && npm test

.PHONY: e2e
e2e: ## Playwright end-to-end tests (starts API in offline mode + web)
	cd $(WEB_DIR) && npx playwright test

.PHONY: eval
eval: ## Agent evaluation cases against the offline planner
	cd $(API_DIR) && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go test ./internal/integration -run TestAgentEval -count=1 -v

.PHONY: eval-models
eval-models: ## Benchmark models: EVAL_MODELS=groq/openai/gpt-oss-120b,groq/openai/gpt-oss-20b make eval-models
	cd $(API_DIR) && TEST_DATABASE_URL="$(TEST_DATABASE_URL)" go run ./cmd/eval models

# ---------------------------------------------------------------- contracts & build
.PHONY: contracts
contracts: ## Regenerate packages/contracts/openapi.json and web TypeScript types
	cd $(API_DIR) && go run ./cmd/openapi > ../../packages/contracts/openapi.json
	cd $(WEB_DIR) && npm run gen:api

.PHONY: build
build: ## Build API binary and web app
	cd $(API_DIR) && go build -o bin/api ./cmd/api && go build -o bin/migrate ./cmd/migrate
	cd $(WEB_DIR) && npm run build

.PHONY: docker-api
docker-api: ## Build the API container image
	docker build -f $(API_DIR)/Dockerfile -t ideavault-api:local .

.PHONY: secrets-check
secrets-check: ## Scan commits and staged changes for secrets (gitleaks; ignores your untracked .env)
	@if command -v gitleaks >/dev/null; then gitleaks git --redact -v . && gitleaks git --pre-commit --staged --redact .; \
	else docker run --rm -v "$$PWD:/repo" -w /repo zricethezav/gitleaks:v8.21.2 git --redact -v /repo; fi
