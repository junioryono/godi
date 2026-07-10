#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
expected=$(mktemp)
actual=$(mktemp)
trap 'rm -f "$expected" "$actual"' EXIT

awk '!/^#/ && NF { print $1 }' "$root/scripts/modules.txt" | sort > "$expected"

find "$root" -name go.mod -not -path '*/_venv/*' -not -path '*/_build/*' -print \
	| while IFS= read -r modfile; do
		if [[ "$modfile" == "$root/go.mod" ]]; then
			printf '.\n'
		else
			directory=${modfile%/go.mod}
			printf '%s\n' "${directory#"$root/"}"
		fi
	done \
	| sort > "$actual"

if ! diff -u "$expected" "$actual"; then
echo "scripts/modules.txt must list every Go module exactly once" >&2
	exit 1
fi

root_module=$(awk '$1 == "module" { print $2; exit }' "$root/go.mod")
if [[ ! "$root_module" =~ ^github\.com/junioryono/godi/v[2-9][0-9]*$ ]]; then
	echo "root go.mod declares unexpected module path: $root_module" >&2
	exit 1
fi
major_suffix=${root_module##*/}

while read -r directory kind; do
	case "$directory" in
		''|'#'*) continue ;;
	esac

	case "$kind" in
		core) expected_path="$root_module" ;;
		integration) expected_path="github.com/junioryono/godi/$directory/$major_suffix" ;;
		test) expected_path="github.com/junioryono/godi/integrationtests" ;;
		benchmark) expected_path="$root_module/benchmarks" ;;
		*)
			echo "unknown module kind '$kind' for $directory" >&2
			exit 1
			;;
	esac

	declared=$(awk '$1 == "module" { print $2; exit }' "$root/$directory/go.mod")
	if [[ "$declared" != "$expected_path" ]]; then
		echo "$directory/go.mod declares $declared; expected $expected_path" >&2
		exit 1
	fi
done < "$root/scripts/modules.txt"

echo "module inventory and module paths are valid"
