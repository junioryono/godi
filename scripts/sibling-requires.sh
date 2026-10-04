#!/usr/bin/env bash

# Usage: sibling-requires.sh GO_MOD_FILE
# Prints one line per requirement on another integration module of this
# repository: "<directory> <module-path> <version>". Pass - to read the go.mod
# content from stdin (for example from git show TAG:chi/go.mod).
#
# Integration modules are released in lockstep, so a cross-integration
# requirement (chi -> http) must be developed against the sibling source
# (replace => ../<directory>) and released at the same version as the tag set.

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: $0 GO_MOD_FILE|-" >&2
	exit 2
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
root_module=$(awk '$1 == "module" { print $2; exit }' "$root/go.mod")
major_suffix=${root_module##*/}

awk -v suffix="$major_suffix" '
	FNR == NR {
		if ($0 !~ /^#/ && NF && $2 == "integration") {
			integration["github.com/junioryono/godi/" $1 "/" suffix] = $1
		}
		next
	}
	$1 == "require" && $2 == "(" { in_require = 1; next }
	in_require && $1 == ")" { in_require = 0; next }
	in_require && ($1 in integration) { print integration[$1], $1, $2; next }
	$1 == "require" && ($2 in integration) { print integration[$2], $2, $3 }
' "$root/scripts/modules.txt" "$1"
