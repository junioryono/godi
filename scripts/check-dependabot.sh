#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
expected=$(mktemp)
actual=$(mktemp)
trap 'rm -f "$expected" "$actual"' EXIT

awk '!/^#/ && NF {
	if ($1 == ".") print "/"
	else print "/" $1
}' "$root/scripts/modules.txt" | sort > "$expected"

awk '
	/package-ecosystem: gomod/ { gomod = 1; next }
	gomod && /directory:/ {
		value = $2
		gsub(/"/, "", value)
		print value
		gomod = 0
	}
' "$root/.github/dependabot.yml" | sort > "$actual"

if ! diff -u "$expected" "$actual"; then
	echo "Dependabot must cover every Go module in scripts/modules.txt" >&2
	exit 1
fi

if ! grep -q 'package-ecosystem: pip' "$root/.github/dependabot.yml"; then
	echo "Dependabot must cover documentation dependencies" >&2
	exit 1
fi

echo "Dependabot coverage is valid"
