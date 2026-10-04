#!/usr/bin/env bash

# Reports pinned development tools with a newer release. Dependabot cannot see
# these pins: the *_VERSION variables in the Makefile and the Go toolchain in
# .go-version. Prints a Markdown table and exits 1 when any pin is outdated
# (the scheduled Tool Updates workflow turns that into an issue).

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

# Makefile variable -> Go module that provides the tool.
tools=(
	"GOLANGCI_LINT_VERSION github.com/golangci/golangci-lint/v2"
	"GOSEC_VERSION github.com/securego/gosec/v2"
	"GOVULNCHECK_VERSION golang.org/x/vuln"
	"ACTIONLINT_VERSION github.com/rhysd/actionlint"
)

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Query outside any module so local go.mod files cannot influence @latest.
cd "$work"

outdated=false
rows=()

for entry in "${tools[@]}"; do
	read -r variable module <<< "$entry"
	pinned=$(awk -F'[ \t]*[?:]?=[ \t]*' -v name="$variable" '$1 == name { print $2; exit }' "$root/Makefile")
	if [[ -z "$pinned" ]]; then
		echo "$variable not found in Makefile" >&2
		exit 1
	fi
	latest=$(GOFLAGS=-mod=mod go list -m -f '{{.Version}}' "$module@latest")
	status=current
	if [[ "$pinned" != "$latest" ]]; then
		status=outdated
		outdated=true
	fi
	rows+=("| \`$variable\` (Makefile) | $module | $pinned | $latest | $status |")
done

# Go toolchain: the newest stable release of the pinned toolchain's minor and
# the newest stable release overall.
pinned_go=$(tr -d '[:space:]' < "$root/.go-version")
releases=$(go list -m -versions golang.org/toolchain | tr ' ' '\n' \
	| sed -nE 's/^v0\.0\.1-go([0-9]+\.[0-9]+\.[0-9]+)\.linux-amd64$/\1/p' | sort -t. -k1,1n -k2,2n -k3,3n -u)
latest_go=$(printf '%s\n' "$releases" | tail -n1)
status=current
if [[ "$pinned_go" != "$latest_go" ]]; then
	status=outdated
	outdated=true
	pinned_minor=${pinned_go%.*}
	if [[ "${latest_go%.*}" != "$pinned_minor" ]]; then
		status="outdated: new Go minor; also raise the go directive per the version policy"
	fi
fi
rows+=("| Go toolchain (\`.go-version\`) | golang.org/toolchain | $pinned_go | $latest_go | $status |")

echo "| Pin | Source | Pinned | Latest | Status |"
echo "| --- | --- | --- | --- | --- |"
printf '%s\n' "${rows[@]}"

[[ "$outdated" == "false" ]]
