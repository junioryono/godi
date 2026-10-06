SHELL := /usr/bin/env bash

GOLANGCI_LINT_VERSION ?= v2.14.0
GOSEC_VERSION ?= v2.29.0
GOVULNCHECK_VERSION ?= v1.8.0
ACTIONLINT_VERSION ?= v1.7.12
BENCH_COUNT ?= 1

TOOLS_DIR ?= $(CURDIR)/.tools
TOOLS_BIN := $(abspath $(TOOLS_DIR))/bin
GOLANGCI_LINT_BIN := $(TOOLS_BIN)/golangci-lint-$(GOLANGCI_LINT_VERSION)
GOSEC_BIN := $(TOOLS_BIN)/gosec-$(GOSEC_VERSION)
GOVULNCHECK_BIN := $(TOOLS_BIN)/govulncheck-$(GOVULNCHECK_VERSION)
ACTIONLINT_BIN := $(TOOLS_BIN)/actionlint-$(ACTIONLINT_VERSION)

# A local gate shares the machine with everything else on it; CI's runs as it
# always did. Measured in a sibling Go project on a 12-core, 32 GiB Mac on
# 2026-10-05: `go test -race ./...` at go's default -p (one package per CPU)
# peaked at 3.36 GiB resident over 96 processes, and several gates at once pushed
# swap to 39 GiB, stalling DNS and the VPN client. At -p 4 the same step peaked
# at 0.99 GiB over 56 processes and finished sooner (2049s against 2299s). So
# outside CI each module's race tests run at most TEST_PARALLEL packages at once,
# and the test and lint steps run under nice. nice rather than macOS's
# taskpolicy: a package took 37s under nice -n 10 and 27s to 119s unprioritised
# as the machine's load moved, but 205s at the utility QoS and 485s at background
# QoS, which throttle disk I/O as well as CPU. Override either on the command
# line (`make verify TEST_PARALLEL= NICE=` lifts both, and still takes the lock
# below). Under CI both are forced empty, whatever the environment says, and the
# recipes $(strip) the command so that it is then CI's byte for byte.
ifeq ($(CI),)
TEST_PARALLEL ?= 4
NICE ?= nice -n 10
GO_TEST_P := $(if $(TEST_PARALLEL),-p $(TEST_PARALLEL))
else
override NICE :=
override GO_TEST_P :=
endif

# One gate at a time per machine, across projects: `verify` and `verify-ci` take
# this lock and a run waits for whichever gate holds it (see
# scripts/with-check-lock.sh). The path is shared with other repositories' gates
# on purpose, per user, so XDG_CACHE_HOME counts only when it is absolute: a
# relative one would give every checkout a lock of its own. It is a courtesy, not
# a gate: without a lock tool, or a lock that cannot be taken, the run goes ahead
# and says so.
GATE_LOCK ?= $(or $(filter /%,$(XDG_CACHE_HOME)),$(HOME)/.cache)/dev-gate.lock

# A dry run (-n), a question (-q) or a touch (-t) runs no step, so it takes no
# lock: GNU make runs a recipe line naming $(MAKE) even under -n, and the lock
# would make `make -n verify` wait behind a whole gate to print commands. The
# short flags are not always MAKEFLAGS's first word (measured on GNU Make 3.81:
# `-n` gives "n", `--no-print-directory -s -n` gives " --no-print-directory -sn",
# and `GATE_LOCK=/tmp/x` alone gives "GATE_LOCK=/tmp/x"), so the flags are read
# from the words before the first variable assignment or "--", and only from a
# word made of nothing but make's short-flag letters and an optional leading "-".
MAKE_FLAG_LETTERS := B d e i I j k l L n p q r R s S t v w
make_flags_before_vars = $(if $1,$(if $(or $(filter --,$(firstword $1)),$(findstring =,$(firstword $1))),,$(firstword $1) $(call make_flags_before_vars,$(wordlist 2,$(words $1),$1))))
make_drop_letters = $(if $2,$(call make_drop_letters,$(subst $(firstword $2),,$1),$(wordlist 2,$(words $2),$2)),$1)
make_flag_words = $(foreach w,$(call make_flags_before_vars,$1),$(if $(filter-out -,$(call make_drop_letters,$(w),$(MAKE_FLAG_LETTERS))),,$(w)))
MAKE_RUNS_NOTHING = $(strip $(foreach flag,n q t,$(findstring $(flag),$(call make_flag_words,$(MAKEFLAGS)))))

.PHONY: verify verify-unlocked verify-ci verify-ci-unlocked module-check floor-check dependency-check workflow-check format-check tidy-check build vet test test-cover lint docs benchmark published-check security vulncheck tool-updates prepare-release release-smoke tools clean

verify:
	$(if $(MAKE_RUNS_NOTHING),$(MAKE) --no-print-directory verify-unlocked,@scripts/with-check-lock.sh "$(GATE_LOCK)" $(MAKE) --no-print-directory verify-unlocked)

verify-unlocked: module-check floor-check dependency-check workflow-check format-check tidy-check build vet test lint

verify-ci:
	$(if $(MAKE_RUNS_NOTHING),$(MAKE) --no-print-directory verify-ci-unlocked,@scripts/with-check-lock.sh "$(GATE_LOCK)" $(MAKE) --no-print-directory verify-ci-unlocked)

verify-ci-unlocked: verify-unlocked test-cover docs published-check security

module-check:
	@scripts/check-modules.sh

floor-check:
	@scripts/check-release-floors.sh >/dev/null

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
	@$(strip $(NICE) scripts/for-each-module.sh go test -race $(GO_TEST_P) ./...)

test-cover:
	@scripts/check-coverage.sh

lint: $(GOLANGCI_LINT_BIN)
	@$(strip $(NICE) scripts/for-each-module.sh "$(GOLANGCI_LINT_BIN)" run ./...)

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

vulncheck: $(GOVULNCHECK_BIN)
	@SECURITY_SCANNERS=govulncheck GOVULNCHECK_BIN="$(GOVULNCHECK_BIN)" scripts/security.sh

tool-updates:
	@scripts/check-tool-updates.sh

# Usage: make prepare-release VERSION=vX.Y.Z
prepare-release:
	@test -n "$(VERSION)" || { echo "usage: make prepare-release VERSION=vX.Y.Z" >&2; exit 2; }
	@scripts/prepare-release.sh "$(VERSION)"

# Usage: make release-smoke VERSION=vX.Y.Z (after the tags are published)
release-smoke:
	@test -n "$(VERSION)" || { echo "usage: make release-smoke VERSION=vX.Y.Z" >&2; exit 2; }
	@scripts/smoke-test-release.sh "$(VERSION)"

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
