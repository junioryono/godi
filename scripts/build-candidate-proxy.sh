#!/usr/bin/env bash

# Usage: build-candidate-proxy.sh PROXY_DIR vMAJOR.MINOR.PATCH
#
# Writes a file-based Go module proxy (usable as GOPROXY=file://PROXY_DIR)
# that serves every released godi module -- the core plus each integration in
# scripts/modules.txt -- from this checkout at the given version. It lets the
# candidate release set be resolved exactly as consumers will resolve the
# tags, without replace directives and before anything is published.
#
# Files are taken from the working tree: tracked files plus untracked files
# that are not ignored. Like the module zips served by proxy.golang.org, a
# module's zip omits nested modules.

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
proxy=${1:-}
version=${2:-}

if [[ -z "$proxy" || ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 PROXY_DIR vMAJOR.MINOR.PATCH" >&2
	exit 2
fi
if ! command -v zip >/dev/null 2>&1; then
	echo "zip is required to build the candidate module proxy" >&2
	exit 1
fi

mkdir -p "$proxy"
proxy=$(cd "$proxy" && pwd)
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

all_modules=()
released=()
while read -r directory kind; do
	case "$directory" in
		''|'#'*) continue ;;
	esac
	all_modules+=("$directory")
	[[ "$kind" == "core" || "$kind" == "integration" ]] && released+=("$directory")
done < "$root/scripts/modules.txt"

timestamp=$(date -u +%Y-%m-%dT%H:%M:%SZ)

for directory in "${released[@]}"; do
	module_path=$(awk '$1 == "module" { print $2; exit }' "$root/$directory/go.mod")
	if [[ "$module_path" =~ [A-Z] ]]; then
		echo "$module_path needs case-encoding, which this script does not implement" >&2
		exit 1
	fi

	# Nested module directories are excluded from this module's zip.
	nested=()
	for other in "${all_modules[@]}"; do
		[[ "$other" == "$directory" ]] && continue
		if [[ "$directory" == "." || "$other" == "$directory/"* ]]; then
			nested+=("$other")
		fi
	done

	content="$stage/$module_path@$version"
	mkdir -p "$content"
	pathspec=.
	[[ "$directory" != "." ]] && pathspec=$directory
	while IFS= read -r file; do
		relative=$file
		[[ "$directory" != "." ]] && relative=${file#"$directory/"}
		skip=false
		for other in "${nested[@]}"; do
			if [[ "$file" == "$other/"* ]]; then
				skip=true
				break
			fi
		done
		[[ "$skip" == "true" ]] && continue
		# Skip deleted tracked files and symlinks, which module zips cannot hold.
		[[ -f "$root/$file" && ! -L "$root/$file" ]] || continue
		mkdir -p "$content/$(dirname "$relative")"
		cp "$root/$file" "$content/$relative"
	done < <(git -C "$root" ls-files --cached --others --exclude-standard -- "$pathspec" | sort -u)

	target="$proxy/$module_path/@v"
	mkdir -p "$target"
	rm -f "$target/$version.zip"
	(cd "$stage" && zip -q -X -D -r "$target/$version.zip" "$module_path@$version")
	cp "$root/$directory/go.mod" "$target/$version.mod"
	printf '{"Version":"%s","Time":"%s"}\n' "$version" "$timestamp" > "$target/$version.info"
	printf '%s\n' "$version" > "$target/list"
	printf 'candidate %s@%s\n' "$module_path" "$version"
done
