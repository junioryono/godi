#!/usr/bin/env bash

# Prints the Go toolchains CI tests as a JSON array.
#
# Policy: godi supports the two most recent Go minor releases, like Go itself.
#   - minimum: the minor named by the root go.mod `go` directive, tested at
#     its latest patch ("1.N.x")
#   - latest:  the exact toolchain pinned in .go-version, which every
#     single-toolchain CI job also uses
# When a new Go minor ships, bump .go-version; when a minor drops out of
# support, raise the go directive of every module.

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

go_directive=$(awk '$1 == "go" { print $2; exit }' "$root/go.mod")
latest=$(tr -d '[:space:]' < "$root/.go-version")

if [[ ! "$go_directive" =~ ^1\.([0-9]+)(\.[0-9]+)?$ ]]; then
	echo "unexpected go directive in go.mod: $go_directive" >&2
	exit 1
fi
minimum_minor=${BASH_REMATCH[1]}
if [[ ! "$latest" =~ ^1\.([0-9]+)\.[0-9]+$ ]]; then
	echo ".go-version must name an exact release such as 1.27.1 (got $latest)" >&2
	exit 1
fi
latest_minor=${BASH_REMATCH[1]}

if (( latest_minor < minimum_minor )); then
	echo ".go-version $latest is older than the go directive $go_directive" >&2
	exit 1
fi
if (( latest_minor - minimum_minor > 1 )); then
	echo "go directive $go_directive is more than one minor behind .go-version $latest;" >&2
	echo "raise the go directive: only the two latest Go minors are supported" >&2
	exit 1
fi

if (( latest_minor == minimum_minor )); then
	printf '["%s"]\n' "$latest"
else
	printf '["1.%s.x","%s"]\n' "$minimum_minor" "$latest"
fi
