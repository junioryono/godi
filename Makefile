SHELL := /usr/bin/env bash

GOLANGCI_LINT_VERSION ?= v2.12.2
GOSEC_VERSION ?= v2.27.1
GOVULNCHECK_VERSION ?= v1.6.0
ACTIONLINT_VERSION ?= v1.7.12
BENCH_COUNT ?= 1

TOOLS_DIR ?= $(CURDIR)/.tools
TOOLS_BIN := $(abspath $(TOOLS_DIR))/bin
GOLANGCI_LINT_BIN := $(TOOLS_BIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOSEC_BIN := $(TOOLS_BIN)/gosec-$(GOSEC_VERSION)
GOVULNCHECK_BIN := $(TOOLS_BIN)/govulncheck-$(GOVULNCHECK_VERSION)
ACTIONLINT_BIN := $(TOOLS_BIN)/actionlint-$(ACTIONLINT_VERSION)

.PHONY: verify verify-ci module-check dependency-check workflow-check format-check tidy-check build vet test test-cover lint docs benchmark published-check security tools clean

verify: module-check dependency-check workflow-check format-check tidy-check build vet test lint

verify-ci: BENCH_COUNT = 3
verify-ci: verify test-cover docs published-check security benchmark

module-check:
	@scripts/check-modules.sh

dependency-check:
	@scripts/check-dependabot.sh

workflow-check: $(ACTIONLINT_BIN)
	@"$(ACTIONLINT_BIN)"
	@scripts/check-actions-pinned.sh

format-check:
	@scripts/check-format.sh

tidy-check:
	@scripts/check-tidy.sh

build:
	@scripts/for-each-module.sh go build ./...

vet:
	@scripts/for-each-module.sh go vet ./...

test:
	@scripts/for-each-module.sh go test -race ./...

test-cover:
	@scripts/check-coverage.sh

lint: $(GOLANGCI_LINT_BIN)
	@scripts/for-each-module.sh "$(GOLANGCI_LINT_BIN)" run ./...

docs:
	@build_dir=$$(mktemp -d); \
		trap 'rm -rf "$$build_dir"' EXIT; \
		$(MAKE) -C docs html BUILDDIR="$$build_dir" SPHINXOPTS="-W --keep-going"

benchmark:
	@BENCH_COUNT="$(BENCH_COUNT)" scripts/run-benchmarks.sh

published-check:
	@scripts/check-published-modules.sh

security: $(GOSEC_BIN) $(GOVULNCHECK_BIN)
	@GOSEC_BIN="$(GOSEC_BIN)" GOVULNCHECK_BIN="$(GOVULNCHECK_BIN)" scripts/security.sh

tools: $(GOLANGCI_LINT_BIN) $(GOSEC_BIN) $(GOVULNCHECK_BIN) $(ACTIONLINT_BIN)

$(GOLANGCI_LINT_BIN):
	@mkdir -p "$(TOOLS_BIN)"
	@GOBIN="$(TOOLS_BIN)" go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@mv "$(TOOLS_BIN)/golangci-lint" "$@"

$(GOSEC_BIN):
	@mkdir -p "$(TOOLS_BIN)"
	@GOBIN="$(TOOLS_BIN)" go install github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION)
	@mv "$(TOOLS_BIN)/gosec" "$@"

$(GOVULNCHECK_BIN):
	@mkdir -p "$(TOOLS_BIN)"
	@GOBIN="$(TOOLS_BIN)" go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@mv "$(TOOLS_BIN)/govulncheck" "$@"

$(ACTIONLINT_BIN):
	@mkdir -p "$(TOOLS_BIN)"
	@GOBIN="$(TOOLS_BIN)" go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
	@mv "$(TOOLS_BIN)/actionlint" "$@"

clean:
	@$(MAKE) -C docs clean
