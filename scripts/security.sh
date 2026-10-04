#!/usr/bin/env bash

set -euo pipefail

# SECURITY_SCANNERS selects the scanners to run (default: both).

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
gosec_bin=${GOSEC_BIN:-gosec}
govulncheck_bin=${GOVULNCHECK_BIN:-govulncheck}
scanners=" ${SECURITY_SCANNERS:-gosec govulncheck} "

require_tool() {
	local name=$1
	local executable=$2
	if [[ "$executable" == */* ]]; then
		if [[ ! -x "$executable" ]]; then
			echo "$name is required at $executable; run 'make security'" >&2
			exit 1
		fi
	elif ! command -v "$executable" >/dev/null 2>&1; then
		echo "$name is required; run 'make security'" >&2
		exit 1
	fi
}

[[ "$scanners" == *" gosec "* ]] && require_tool gosec "$gosec_bin"
[[ "$scanners" == *" govulncheck "* ]] && require_tool govulncheck "$govulncheck_bin"

for directory in $(awk '!/^#/ && NF { print $1 }' "$root/scripts/modules.txt"); do
	if [[ "$scanners" == *" gosec "* ]]; then
		printf '\n==> %s: gosec\n' "$directory"
		(cd "$root/$directory" && "$gosec_bin" -quiet ./... </dev/null)
	fi
	if [[ "$scanners" == *" govulncheck "* ]]; then
		printf '\n==> %s: govulncheck\n' "$directory"
		(cd "$root/$directory" && "$govulncheck_bin" ./... </dev/null)
	fi
done
