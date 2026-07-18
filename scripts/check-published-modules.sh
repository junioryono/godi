#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for directory in $("$root/scripts/module-matrix.sh" published | tr -d '[]"' | tr ',' ' '); do
	printf '\n==> %s: test declared dependency graph\n' "$directory"
	cp -R "$root/$directory" "$work/$directory"
	(
		cd "$work/$directory"
		while IFS= read -r module; do
			go mod edit -dropreplace "$module"
		done < <(awk '$1 == "replace" { print $2 }' go.mod)
		go mod tidy
		go mod verify
		go test ./...
	) </dev/null
done
