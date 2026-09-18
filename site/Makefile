.DEFAULT_GOAL := help

.PHONY: help build site test clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

# site is the deploy target Cloudflare Workers Builds runs: render docs/ into
# wiki/ and ensure the landing page is present. index.html is hand-written and
# committed; wiki/ is generated from ../docs and git-ignored.
site: ## Render docs/ into wiki/ (the Workers Builds entrypoint)
	go run ./cmd/site-wiki ../docs .

build: ## Build the wiki renderer binary
	go build -o bin/site-wiki ./cmd/site-wiki

test: ## Run renderer tests
	go test ./...

clean: ## Remove generated output and build artefacts
	rm -rf wiki bin
