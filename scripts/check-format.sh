#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mapfile_support=false
if [[ -n ${BASH_VERSION:-} ]] && (( BASH_VERSINFO[0] >= 4 )); then
	mapfile_support=true
fi

if [[ "$mapfile_support" == "true" ]]; then
	mapfile -t files < <(git -C "$root" ls-files '*.go')
else
	files=()
	while IFS= read -r file; do files+=("$file"); done < <(git -C "$root" ls-files '*.go')
fi

if (( ${#files[@]} == 0 )); then
	echo "No Go files to check"
	exit 0
fi

unformatted=$(cd "$root" && gofmt -s -l "${files[@]}")
if [[ -n "$unformatted" ]]; then
	echo "Go files need gofmt -s:" >&2
	echo "$unformatted" >&2
	exit 1
fi

echo "Go formatting is valid"
