#!/usr/bin/env bash

# Usage: godi-requires.sh [go.mod]
# Prints "module-path version" for every require directive that names a godi
# module (github.com/junioryono/godi/...). Reads standard input when no file
# is given, so callers can pipe `git show REF:dir/go.mod` into it.

set -euo pipefail

awk '
	function emit(path, version) {
		if (path ~ /^github\.com\/junioryono\/godi(\/|$)/) print path, version
	}
	/^require[ \t]*\([ \t]*$/ { block = 1; next }
	block && /^[ \t]*\)/ { block = 0; next }
	block && NF >= 2 && $1 !~ /^\/\// { emit($1, $2); next }
	!block && $1 == "require" && NF >= 3 { emit($2, $3) }
' "${1:-/dev/stdin}"
