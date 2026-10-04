#!/usr/bin/env bash

# Usage: check-release-floors.sh [--ref REF] [vMAJOR.MINOR.PATCH]
#
# Every godi module is released at one commit with one version, so every
# module must require the other godi modules (the core and, for the test
# module, the integrations) at exactly the same version: the release floor.
# Without a version argument the floors only have to agree with each other;
# with one they must equal it. With --ref, go.mod files are read from that git
# revision instead of the working tree.
#
# On success the floor version is printed to standard output.

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
ref=""
if [[ "${1:-}" == "--ref" ]]; then
	ref=${2:?--ref requires a git revision}
	shift 2
fi
expected=${1:-}

if [[ -n "$expected" && ! "$expected" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 [--ref REF] [vMAJOR.MINOR.PATCH]" >&2
	exit 2
fi

read_file() {
	if [[ -n "$ref" ]]; then
		git -C "$root" show "$ref:$1"
	else
		cat "$root/$1"
	fi
}

floors=$(mktemp)
trap 'rm -f "$floors"' EXIT

while read -r directory _; do
	case "$directory" in
		''|'#'*) continue ;;
	esac
	modfile=go.mod
	[[ "$directory" != "." ]] && modfile="$directory/go.mod"
	read_file "$modfile" | "$root/scripts/godi-requires.sh" \
		| awk -v file="$modfile" '{ print file, $1, $2 }' >> "$floors"
done < <(read_file scripts/modules.txt)

if [[ ! -s "$floors" ]]; then
	echo "no module requires a godi module; nothing to check" >&2
	exit 1
fi

versions=$(awk '{ print $3 }' "$floors" | sort -u)
if [[ $(printf '%s\n' "$versions" | wc -l) -ne 1 ]]; then
	echo "godi release floors disagree; every module must require one version:" >&2
	awk '{ printf "  %s: %s %s\n", $1, $2, $3 }' "$floors" >&2
	echo "run 'make prepare-release VERSION=vX.Y.Z' to align them" >&2
	exit 1
fi

if [[ -n "$expected" && "$versions" != "$expected" ]]; then
	echo "godi release floors are $versions, not the release version $expected${ref:+ (at $ref)}" >&2
	echo "run 'make prepare-release VERSION=$expected', merge the result to main, then tag" >&2
	exit 1
fi

echo "godi release floors agree on $versions${ref:+ at $ref}" >&2
printf '%s\n' "$versions"
