#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
gosec_bin=${GOSEC_BIN:-gosec}
govulncheck_bin=${GOVULNCHECK_BIN:-govulncheck}

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

require_tool gosec "$gosec_bin"
require_tool govulncheck "$govulncheck_bin"

for directory in $(awk '!/^#/ && NF { print $1 }' "$root/scripts/modules.txt"); do
	printf '\n==> %s: gosec\n' "$directory"
	(cd "$root/$directory" && "$gosec_bin" -quiet ./... </dev/null)
	printf '\n==> %s: govulncheck\n' "$directory"
	(cd "$root/$directory" && "$govulncheck_bin" ./... </dev/null)
done
