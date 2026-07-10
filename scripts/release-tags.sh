#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
new_tag=${1:-}
remote=${RELEASE_REMOTE:-origin}
dry_run=${RELEASE_DRY_RUN:-false}
ref=${GITHUB_REF:-}

if [[ ! "$new_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vMAJOR.MINOR.PATCH" >&2
	exit 2
fi

if [[ "$ref" != "refs/heads/main" ]]; then
	echo "releases must run from refs/heads/main (got ${ref:-unset})" >&2
	exit 1
fi

cd "$root"
scripts/check-modules.sh

head=$(git rev-parse HEAD)
if [[ "$dry_run" != "true" ]]; then
	remote_main=$(git ls-remote "$remote" refs/heads/main | awk '{print $1}')
	if [[ -z "$remote_main" ]]; then
		echo "could not resolve $remote/main" >&2
		exit 1
	fi
	if [[ "$head" != "$remote_main" ]]; then
		echo "HEAD $head does not match $remote/main $remote_main" >&2
		exit 1
	fi
fi

latest=$(git tag --list 'v*' --sort=-version:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)
latest=${latest:-v0.0.0}
IFS=. read -r current_major current_minor current_patch <<< "${latest#v}"
IFS=. read -r major minor patch <<< "${new_tag#v}"

if [[ "$new_tag" == "$latest" ]]; then
	bump=Existing
elif (( major == current_major && minor == current_minor && patch == current_patch + 1 )); then
	bump=Patch
elif (( major == current_major && minor == current_minor + 1 && patch == 0 )); then
	bump=Minor
elif (( major == current_major + 1 && minor == 0 && patch == 0 )); then
	bump=Major
	if [[ ${CONFIRM_MAJOR:-} != "MAJOR" ]]; then
		echo "major releases require CONFIRM_MAJOR=MAJOR" >&2
		exit 1
	fi
else
	echo "$new_tag is not the current release or the next patch, minor, or major after $latest" >&2
	exit 1
fi

module_major=$(awk '$1 == "module" { print $2; exit }' go.mod | sed -E 's#.*/v([0-9]+)$#\1#')
if [[ "$major" != "$module_major" ]]; then
	echo "refusing $new_tag: module paths still target /v$module_major" >&2
	echo "migrate every module path before creating a new major release" >&2
	exit 1
fi

tags=("$new_tag")
while IFS= read -r module; do
	tags+=("$module/$new_tag")
done < <(scripts/module-matrix.sh release | tr -d '[]"' | tr ',' '\n')

expected_commit=$head
if [[ "$bump" == "Existing" ]]; then
	expected_commit=$(git rev-list -n1 "$new_tag")
fi

if [[ -n ${GITHUB_OUTPUT:-} ]]; then
	printf 'current_tag=%s\nnew_tag=%s\nbump=%s\ncommit=%s\n' \
		"$latest" "$new_tag" "$bump" "$expected_commit" >> "$GITHUB_OUTPUT"
fi

printf 'Release plan: %s -> %s (%s)\n' "$latest" "$new_tag" "$bump"
printf '  %s\n' "${tags[@]}"

if [[ "$dry_run" == "true" ]]; then
	exit 0
fi

remote_count=0
for tag in "${tags[@]}"; do
	remote_commit=$(git ls-remote "$remote" "refs/tags/$tag^{}" | awk '{print $1}')
	if [[ -n "$remote_commit" ]]; then
		remote_count=$((remote_count + 1))
		if [[ "$remote_commit" != "$expected_commit" ]]; then
			echo "remote tag $tag points to $remote_commit, not $expected_commit" >&2
			exit 1
		fi
	fi
done

if (( remote_count == ${#tags[@]} )); then
	echo "all release tags already exist at $expected_commit; nothing to do"
	exit 0
fi
if (( remote_count != 0 )); then
	echo "remote has a partial release tag set; refusing to continue" >&2
	exit 1
fi

for tag in "${tags[@]}"; do
	if git rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
		if [[ $(git rev-list -n1 "$tag") != "$expected_commit" ]]; then
			echo "local tag $tag does not point to $expected_commit" >&2
			exit 1
		fi
	else
		git tag -a "$tag" -m "Release $tag"
	fi
done

git push --atomic "$remote" "${tags[@]}"
echo "pushed the complete release tag set atomically"
