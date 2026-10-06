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
# below). Under CI, NICE and the -p flag are forced empty whatever the environment
# or command line says, and the recipes $(strip) the command so that it is then
# CI's byte for byte.
ifeq ($(CI),)
TEST_PARALLEL ?= 4
NICE ?= nice -n 10
GO_TEST_P := $(if $(TEST_PARALLEL),-p $(TEST_PARALLEL))
else
override NICE :=
override GO_TEST_P :=
endif

# One gate at a time per machine, across projects: `verify` and `verify-ci` take
# a lock and a run waits for whichever gate holds it (see
# scripts/with-check-lock.sh). Left empty, GATE_LOCK means the path every
# repository's gate shares, which the script works out in the shell: make's word
# functions would split an XDG_CACHE_HOME holding spaces into another path. It is
# a courtesy, not a gate: without a lock tool, or a lock that cannot be made, the
# run goes ahead and says so.
GATE_LOCK ?=

# A dry run (-n), a question (-q) or a touch (-t) runs no step, so it takes no
# lock and goes straight to the steps, as `make -n verify` always did. Two parts
# keep it so. First, the locked recipe reaches make through GATE_SUBMAKE rather
# than naming $(MAKE): GNU make runs a line naming $(MAKE) even under -n, -q and
# -t, and does not treat a line that reaches it through another variable as
# recursive (measured on GNU Make 3.81 and 4.4.1), so under any of those modes
# the locked recipe is printed or skipped, never run. Second, when the Makefile
# is read and those flags can be seen, verify and verify-ci become plain
# prerequisites of the steps, so a dry run lists the steps and `make -q verify`
# answers as it did. The short flags are not always MAKEFLAGS's first word
# (measured on GNU Make 3.81: `-n` gives "n", `-n -I dir` gives "nI dir",
# `--no-print-directory -s -n` gives " --no-print-directory -sn"; GNU Make 4.x
# puts them first and may add "-Idir" words and, after "--", assignments), so
# they are read from the words before the first "--" or assignment: from the
# first word when it has no leading "-", or else from the first word after the
# long options make itself writes, when that word has one leading "-". Either
# way the word must be argument-free option letters followed only by letters of
# options that take an argument, the shape make writes; any other shape, such as
# a hand-written MAKEFLAGS make would never produce, reads as a real run and
# takes the lock.
GATE_SUBMAKE = $(MAKE)
MAKE_NOARG_FLAG_LETTERS := b B d e h i k L m n p q r R s S t v w
MAKE_ARG_FLAG_LETTERS := C f I j l o O W
MAKE_WRITTEN_LONG_OPTIONS := --no-print-directory --print-directory --warn-undefined-variables --trace --no-silent
make_words_before_vars = $(if $1,$(if $(or $(filter --,$(firstword $1)),$(findstring =,$(firstword $1))),,$(firstword $1) $(call make_words_before_vars,$(wordlist 2,$(words $1),$1))))
make_drop_letters = $(if $2,$(call make_drop_letters,$(subst $(firstword $2),,$1),$(wordlist 2,$(words $2),$2)),$1)
make_after_long_options = $(if $(filter --%,$(firstword $1)),$(if $(filter $(MAKE_WRITTEN_LONG_OPTIONS),$(firstword $1)),$(call make_after_long_options,$(wordlist 2,$(words $1),$1))),$(filter -%,$(firstword $1)))
make_flag_bundle = $(if $(call make_drop_letters,$1,$(MAKE_NOARG_FLAG_LETTERS) $(MAKE_ARG_FLAG_LETTERS)),,$(if $(filter $(call make_drop_letters,$1,$(MAKE_ARG_FLAG_LETTERS))%,$1),$1))
make_short_flags = $(call make_flag_bundle,$(patsubst -%,%,$(if $(filter -%,$(firstword $1)),$(call make_after_long_options,$1),$(firstword $1))))
MAKE_RUNS_NOTHING := $(strip $(foreach flag,n q t,$(findstring $(flag),$(call make_short_flags,$(call make_words_before_vars,$(MAKEFLAGS))))))

.PHONY: verify verify-unlocked verify-ci verify-ci-unlocked module-check floor-check dependency-check workflow-check format-check tidy-check build vet test test-cover lint docs benchmark published-check security vulncheck tool-updates prepare-release release-smoke tools clean

ifeq ($(MAKE_RUNS_NOTHING),)
verify:
	@scripts/with-check-lock.sh "$(GATE_LOCK)" $(GATE_SUBMAKE) --no-print-directory verify-unlocked

verify-ci:
	@scripts/with-check-lock.sh "$(GATE_LOCK)" $(GATE_SUBMAKE) --no-print-directory verify-ci-unlocked
else
verify: verify-unlocked

verify-ci: verify-ci-unlocked
endif

verify-unlocked: module-check floor-check dependency-check workflow-check format-check tidy-check build vet test lint

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
