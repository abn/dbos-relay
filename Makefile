.DEFAULT_GOAL := help

.PHONY: help setup build test lint fmt clean check docs/check hooks/require hooks/update

##@ Bootstrap

setup: ## Install git hooks and generate local tool shims
	./.agents/bootstrap.sh

##@ Build & Quality

build: ## Build the project (no build stage until the stack is chosen)
	@printf 'build: nothing to build yet, the stack decision is pending\n'

test: ## Run the test suite (no tests until product code exists)
	@printf 'test: no test suite yet\n'

lint: hooks/require ## Run every hook against all files, including the docs bundle check
	pre-commit run --all-files

# The hygiene hooks rewrite files in place and exit non-zero when they do, so
# a fix is not a failure here.
FMT_HOOKS := trailing-whitespace end-of-file-fixer mixed-line-ending

fmt: hooks/require ## Apply formatting fixes
	@for hook in $(FMT_HOOKS); do pre-commit run "$$hook" --all-files || true; done

docs/check: ## Validate the docs bundle against OKF v0.2
	./.agents/scripts/check-okf.py

check: lint test ## Full quality gate
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
