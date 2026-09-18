.DEFAULT_GOAL := help

export PATH := $(shell go env GOPATH)/bin:$(PATH)

BIN := relay
PKG := ./cmd/$(BIN)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/abn/relay/internal/cli.Version=$(VERSION)
GENERATED := internal/store/gen internal/api/gen

.PHONY: help setup build build/java build/typescript dashboard/build dashboard/check site site/clean test test/conformance vet lint/sdk-isolation lint/examples-isolation gen drift lint fmt docs/check check clean hooks/require hooks/update help db/up db/down db/url verify-live verify-sdk

##@ Bootstrap

setup: ## Install git hooks and generate local tool shims
	./.agents/bootstrap.sh

##@ Build & Quality

build: ## Build the binary
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(PKG)
	(cd examples/golang && CGO_ENABLED=0 go build -o bin/app .)

build/java: ## Build the Java sample application jar
	@if command -v mvn >/dev/null 2>&1; then \
		(cd examples/java && mvn clean package -DskipTests); \
	else \
		podman run --rm -v $(CURDIR)/examples/java:/app:z -w /app docker.io/library/maven:3.9-eclipse-temurin-21-alpine mvn clean package -DskipTests; \
	fi

build/typescript: ## Build the TypeScript sample application
	@if command -v npm >/dev/null 2>&1; then \
		(cd examples/typescript && npm ci && npm run build); \
	else \
		podman run --rm -v $(CURDIR)/examples/typescript:/app:z -w /app docker.io/library/node:22-slim sh -c "npm ci && npm run build"; \
	fi

dashboard/build: ## Build the web dashboard assets
	node console/build.js

dashboard/check: ## Validate dashboard production bundle syntax
	@command -v node >/dev/null 2>&1 || { echo "ERROR: node is required for dashboard/check" >&2; exit 1; }
	node --check internal/dashboard/dist/assets/app.js

site: ## Build static landing page and documentation site
	$(MAKE) -C site site

site/clean: ## Clean generated site documentation
	$(MAKE) -C site clean

test: ## Run the test suite
	go test -p 1 ./...
	@if [ -n "$$RELAY_TEST_DATABASE_URL" ]; then \
		go test -v ./tests/conformance/... ./tests/chaos/...; \
	fi

test/conformance: ## Run the end-to-end conformance test suite
	go test -v ./tests/conformance/...

vet: lint/sdk-isolation lint/examples-isolation ## Run static analysis
	go vet ./...
	golangci-lint run

lint/sdk-isolation: ## Verify the tests/verifysdk test binary links no fakes, mocks, or fake clock packages
	@deps=$$(go list -deps -test ./tests/verifysdk/... | grep -E 'github\.com/abn/relay/internal/.*(fake|mock|clock)' || true); \
	if [ -n "$$deps" ]; then \
		echo "ERROR: tests/verifysdk links fake/mock/clock packages:" >&2; echo "$$deps" >&2; exit 1; \
	fi

lint/examples-isolation: ## Verify examples contain no websocket libraries or fake protocol frames
	@if grep -rnE --exclude-dir=node_modules --exclude-dir=target --exclude-dir=__pycache__ --exclude-dir=.venv '(import\s+websocket|from\s+websockets|require\(["'\'']ws["'\'']\)|import\s+.*\s+from\s+["'\'']ws["'\'']|net/http/WebSocket|"type"\s*:\s*"executor_info")' examples/ 2>/dev/null; then \
		echo "ERROR: examples/ must not import websocket libraries or hand-craft executor_info wire frames" >&2; \
		exit 1; \
	fi

gen: ## Regenerate sqlc and OpenAPI output
	@if [ -f sqlc.yaml ]; then go tool sqlc generate; fi
	@if [ -f api/codegen.yaml ]; then go tool oapi-codegen -config api/codegen.yaml api/spec/openapi-3.0.json; fi

drift: ## Fail if generated output differs from the committed version
	rm -rf $(GENERATED)
	$(MAKE) gen
	@if [ -n "$$(git status --porcelain -- $(GENERATED))" ]; then \
	  git status --porcelain -- $(GENERATED); \
	  echo "drift: generated output is stale, run make gen"; exit 1; \
	fi
	@echo "drift: ok"

lint: hooks/require ## Run every hook against all files, including the docs bundle check
	pre-commit run --all-files

# The hygiene hooks rewrite files in place and exit non-zero when they do, so
# a fix is not a failure here.
FMT_HOOKS := trailing-whitespace end-of-file-fixer mixed-line-ending

fmt: hooks/require ## Apply formatting fixes
	@for hook in $(FMT_HOOKS); do pre-commit run "$$hook" --all-files || true; done

docs/check: ## Validate the docs bundle against OKF v0.2
	./.agents/scripts/check-okf.py

check: lint vet drift ## Full quality gate
	@printf 'check: ok\n'

clean: ## Remove build artefacts
	rm -rf bin dist out

##@ Utilities

# Every target that shells out to pre-commit depends on this, so a missing
# tool says what to do instead of "pre-commit: No such file".
hooks/require:
	@command -v pre-commit >/dev/null || { printf 'pre-commit is not installed, run make setup\n' >&2; exit 1; }

hooks/update: hooks/require ## Update pinned hook revisions
	pre-commit autoupdate

# The escaped slash is load-bearing: an unescaped one ends the regex literal
# on any POSIX awk, so bare make would die on mawk and busybox awk.
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} \
	  /^[a-zA-Z0-9_\/-]+:.*##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } \
	  /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Database

db/up: ## Start the local PostgreSQL service
	podman compose -f deploy/compose.yaml up -d

db/down: ## Stop and remove the local PostgreSQL service
	podman compose -f deploy/compose.yaml down -v

db/url: ## Print the local test database URL
	@echo "postgres://relay:relay@localhost:5433/relay?sslmode=disable"

verify-live: ## Run live database verification suite against real PostgreSQL
	@if [ -z "$$RELAY_TEST_DATABASE_URL" ]; then \
		echo "verify-live: RELAY_TEST_DATABASE_URL is not set (required, no mock fallback)" >&2; \
		exit 1; \
	fi
	go test -v -count=1 -run TestLiveDatabase_Reachable ./internal/store/...
	go test -v -count=1 -run TestLiveDatabase_SDKClientDataPlane ./internal/dataplane/...
	go test -v -count=1 -run TestChaos_LiveDatabase ./tests/chaos/...
	go test -v -count=1 -run TestConformance_EndToEndSuite ./tests/conformance/...
	go test -v -count=1 -run TestRouter_LiveDatabase ./internal/router/...
	go test -v -count=1 ./internal/declarative/...

verify-sdk: build build/java build/typescript lint/sdk-isolation lint/examples-isolation ## Run real multi-SDK sample app integration suite under Podman compose
	@if [ ! -f deploy/.env ] || [ -z "$$(grep RELAY_API_KEY deploy/.env 2>/dev/null)" ]; then \
		KEY="dbos_sec_$$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"; \
		echo "RELAY_API_KEY=$$KEY" > deploy/.env; \
	fi
	podman compose --env-file deploy/.env -f deploy/compose-sdk-apps.yaml up -d
	RELAY_VERIFY_SDK=1 RELAY_API_KEY=$$(grep RELAY_API_KEY deploy/.env | cut -d= -f2) go test -v -count=1 ./tests/verifysdk/...
	RELAY_TEST_DATABASE_URL="postgres://relay:relay@localhost:5433/relay?sslmode=disable" go test -v -count=1 -run TestLiveDatabase_SDKClientDataPlane ./internal/dataplane/...
	RELAY_TEST_DATABASE_URL="postgres://relay:relay@localhost:5433/relay?sslmode=disable" go test -v -count=1 -run TestRouter_LiveDatabase ./internal/router/...
