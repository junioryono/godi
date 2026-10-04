#!/usr/bin/env bash

# Usage: prepare-release.sh vMAJOR.MINOR.PATCH
#
# Raises every godi requirement in every module listed in scripts/modules.txt
# (the core floor of each integration plus the integration requirements of the
# test module) to the release version, then tidies each module. Commit the
# result, merge it to main, and only then run the Tag workflow: the tag set is
# created at that commit, so each integration requires the core released with
# it.
#
# The new version is usually not published yet. That is fine for development:
# every godi requirement is paired with a directory replace (../), and
# directory replacements need neither a download nor a go.sum entry.

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${1:-}

if [[ ! "$version" =~ ^v([0-9]+)\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vMAJOR.MINOR.PATCH" >&2
	exit 2
fi
major=${BASH_REMATCH[1]}

cd "$root"
scripts/check-modules.sh >/dev/null

module_major=$(awk '$1 == "module" { print $2; exit }' go.mod | sed -E 's#.*/v([0-9]+)$#\1#')
if [[ "$major" != "$module_major" ]]; then
	echo "refusing $version: module paths target /v$module_major" >&2
	exit 1
fi

latest=$(git tag --list 'v*' --sort=-version:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)
if [[ -n "$latest" ]]; then
	highest=$(printf '%s\n%s\n' "$latest" "$version" | sort -V | tail -n1)
	if [[ "$highest" != "$version" ]]; then
		echo "refusing $version: it is older than the latest release $latest" >&2
		exit 1
	fi
fi

modules=()
while read -r directory _; do
	case "$directory" in
		''|'#'*) continue ;;
	esac
	modules+=("$directory")
done < scripts/modules.txt

for directory in "${modules[@]}"; do
	edits=()
	while read -r path current; do
		[[ "$current" == "$version" ]] && continue
		edits+=("-require=$path@$version")
		printf '%s: %s %s -> %s\n' "$directory" "$path" "$current" "$version"
	done < <(scripts/godi-requires.sh "$directory/go.mod")
	if (( ${#edits[@]} > 0 )); then
		(cd "$directory" && go mod edit "${edits[@]}")
	fi
done

for directory in "${modules[@]}"; do
	printf '==> %s: go mod tidy\n' "$directory"
	(cd "$directory" && go mod tidy </dev/null)
done

scripts/check-release-floors.sh "$version" >/dev/null

cat <<EOF

Release floors now require $version. Next steps:
  1. make verify
  2. commit: git commit -am "chore(release): prepare $version"
  3. open a pull request and merge it to main
  4. run the Tag workflow on main with version $version
EOF
