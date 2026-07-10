#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
tag=${1:-}
require_head=${REQUIRE_TAGGED_HEAD:-true}

if [[ ! "$tag" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
	echo "release tag must use strict vMAJOR.MINOR.PATCH form: ${tag:-unset}" >&2
	exit 1
fi
tag_major=${BASH_REMATCH[1]}

cd "$root"
if ! git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	echo "release tag does not exist locally: $tag" >&2
	exit 1
fi

root_commit=$(git rev-list -n1 "$tag")
if [[ "$require_head" == "true" && $(git rev-parse HEAD) != "$root_commit" ]]; then
	echo "checked-out commit does not match $tag ($root_commit)" >&2
	exit 1
fi

module_path=$(git show "$tag:go.mod" | awk '$1 == "module" { print $2; exit }')
expected_root="github.com/junioryono/godi/v$tag_major"
if [[ "$module_path" != "$expected_root" ]]; then
	echo "$tag contains module path $module_path; expected $expected_root" >&2
	exit 1
fi

for module in $(scripts/module-matrix.sh release | tr -d '[]"' | tr ',' ' '); do
	module_tag="$module/$tag"
	if ! git rev-parse -q --verify "refs/tags/$module_tag" >/dev/null; then
		echo "missing integration release tag: $module_tag" >&2
		exit 1
	fi
	module_commit=$(git rev-list -n1 "$module_tag")
	if [[ "$module_commit" != "$root_commit" ]]; then
		echo "$module_tag points to $module_commit, not $root_commit" >&2
		exit 1
	fi

	declared=$(git show "$tag:$module/go.mod" | awk '$1 == "module" { print $2; exit }')
	expected="github.com/junioryono/godi/$module/v$tag_major"
	if [[ "$declared" != "$expected" ]]; then
		echo "$module_tag contains module path $declared; expected $expected" >&2
		exit 1
	fi
done

if [[ -n ${GITHUB_OUTPUT:-} ]]; then
	printf 'tag=%s\ncommit=%s\nmodule_path=%s\n' "$tag" "$root_commit" "$module_path" >> "$GITHUB_OUTPUT"
fi

echo "release tag set is complete and points to $root_commit"
