#!/usr/bin/env bash

# Usage: check-coverage.sh [module-dir ...]
# With no arguments, covers every module in the coverage matrix. Profiles are
# written to COVERAGE_DIR when set (kept for upload), or a temp dir otherwise.

set -euo pipefail

threshold=${COVERAGE_THRESHOLD:-80}
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
if [[ -n "${COVERAGE_DIR:-}" ]]; then
	mkdir -p "$COVERAGE_DIR"
	work=$(cd "$COVERAGE_DIR" && pwd)
else
	work=$(mktemp -d)
	trap 'rm -rf "$work"' EXIT
fi

modules=("$@")
if (( ${#modules[@]} == 0 )); then
	for directory in $("$root/scripts/module-matrix.sh" coverage | tr -d '[]"' | tr ',' ' '); do
		modules+=("$directory")
	done
fi

for directory in "${modules[@]}"; do
	name=${directory//\//-}
	[[ "$name" == "." ]] && name=root
	profile="$work/$name.out"
	printf '\n==> %s: coverage\n' "$directory"
	(cd "$root/$directory" && go test -covermode=atomic -coverprofile="$profile" ./... </dev/null)
	total=$(cd "$root/$directory" && go tool cover -func="$profile" \
		| awk '/^total:/ { gsub(/%/, "", $3); print $3 }')
	awk -v total="$total" -v threshold="$threshold" 'BEGIN {
		printf "coverage: %.1f%% (minimum %.1f%%)\n", total, threshold
		if (total + 0 < threshold + 0) exit 1
	}'
done
