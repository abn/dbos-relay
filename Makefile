.DEFAULT_GOAL := help

export PATH := $(shell go env GOPATH)/bin:$(PATH)

BIN := relay
PKG := ./cmd/$(BIN)
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X github.com/abn/relay/internal/cli.Version=$(VERSION)
GENERATED := internal/store/gen internal/api/gen

.PHONY: help setup build test vet gen drift lint fmt clean check docs/check hooks/require hooks/update verify-live db/up db/down db/url lint/sdk-isolation

##@ Bootstrap

setup: ## Install git hooks and generate local tool shims
	./.agents/bootstrap.sh

##@ Build & Quality

build: ## Build the binary
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(PKG)

dashboard/build: ## Build the web dashboard assets
	node console/build.js

test: ## Run the test suite
	go test ./...

test/conformance: ## Run the end-to-end conformance test suite
	go test -v ./tests/conformance/...

vet: lint/sdk-isolation ## Run static analysis
	go vet ./...
	golangci-lint run

lint/sdk-isolation: ## Verify tests/verifysdk imports no fakes, mocks, or fake clock packages
	@if grep -rnE 'github\.com/abn/relay/internal/.*(fake|mock|clock)' tests/verifysdk/ 2>/dev/null; then \
		echo "ERROR: tests/verifysdk must not import fakes, mocks, or clock packages from internal/" >&2; \
		exit 1; \
	fi

gen: ## Regenerate sqlc and OpenAPI output
	@if [ -f sqlc.yaml ]; then go tool sqlc generate; fi
	@if [ -f api/codegen.yaml ]; then go tool oapi-codegen -config api/codegen.yaml api/spec/openapi-3.0.json; fi

drift: gen ## Fail if generated output differs from the committed version
	@if [ -d internal/store/gen ] || [ -d internal/api/gen ]; then \
	  git diff --exit-code --quiet $$(ls -d $(GENERATED) 2>/dev/null) || \
	  { echo "drift: generated output is stale, run make gen"; exit 1; }; \
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

check: lint vet test drift ## Full quality gate
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

verify-sdk: ## Run real multi-SDK sample app integration suite under Podman compose
	podman compose -f deploy/compose-sdk-apps.yaml up -d
	go test -v -count=1 ./tests/verifysdk/...
	RELAY_TEST_DATABASE_URL="postgres://relay:relay@localhost:5433/relay?sslmode=disable" go test -v -count=1 -run TestLiveDatabase_SDKClientDataPlane ./internal/dataplane/...
	RELAY_TEST_DATABASE_URL="postgres://relay:relay@localhost:5433/relay?sslmode=disable" go test -v -count=1 -run TestRouter_LiveDatabase ./internal/router/...
