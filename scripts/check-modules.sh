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
root_go=$(awk '$1 == "go" { print $2; exit }' "$root/go.mod")

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

	# The root go directive is the minimum supported Go (scripts/go-matrix.sh).
	go_directive=$(awk '$1 == "go" { print $2; exit }' "$root/$directory/go.mod")
	if [[ "$go_directive" != "$root_go" ]]; then
		echo "$directory/go.mod declares go $go_directive; every module must declare go $root_go" >&2
		exit 1
	fi

	# A module that requires another integration module (chi -> http) must
	# build against the sibling source, never a published copy, so changes to
	# both land and get tested together.
	while read -r dependency dependency_path _; do
		if [[ "$dependency" == "$directory" ]]; then
			echo "$directory/go.mod requires itself" >&2
			exit 1
		fi
		target=$(awk -v path="$dependency_path" '
			$1 == "replace" && $2 == "(" { in_replace = 1; next }
			in_replace && $1 == ")" { in_replace = 0; next }
			{
				start = in_replace ? 1 : ($1 == "replace" ? 2 : 0)
				if (start == 0 || $start != path) next
				for (i = start + 1; i < NF; i++) if ($i == "=>") { print $(i + 1); exit }
			}
		' "$root/$directory/go.mod")
		if [[ "$target" != "../$dependency" ]]; then
			echo "$directory/go.mod requires $dependency_path but does not replace it with ../$dependency" >&2
			exit 1
		fi
	done < <("$root/scripts/sibling-requires.sh" "$root/$directory/go.mod")
done < "$root/scripts/modules.txt"

echo "module inventory and module paths are valid"
