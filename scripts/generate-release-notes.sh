#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
current_tag=${1:-}
module_path=${2:-}
release_notes=${3:-release_notes.md}

if [[ ! "$current_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ || -z "$module_path" ]]; then
	echo "usage: $0 vMAJOR.MINOR.PATCH module-path [output-file]" >&2
	exit 2
fi

cd "$root"
version=${current_tag#v}
previous_tag=$(git describe --tags --match 'v[0-9]*' --abbrev=0 "${current_tag}^" 2>/dev/null || true)

if [[ -n ${GITHUB_OUTPUT:-} ]]; then
	{
		printf 'current_tag=%s\n' "$current_tag"
		printf 'version=%s\n' "$version"
		printf 'previous_tag=%s\n' "$previous_tag"
	} >> "$GITHUB_OUTPUT"
fi

if [[ -n "$previous_tag" ]]; then
	commits=$(git log "$previous_tag..$current_tag" --pretty=format:'%s|%h')
else
	commits=$(git log "$current_tag" --pretty=format:'%s|%h')
fi

breaking_pattern='^[a-z]+(\([^)]+\))?!:'
fix_pattern='^fix(\([^)]+\))?:'
scoped_fix_pattern='^fix\([^)]+\):'
feature_pattern='^feat(\([^)]+\))?:'
scoped_feature_pattern='^feat\([^)]+\):'

breaking=""
fixes=""
features=""
while IFS='|' read -r message hash; do
	if [[ "$message" =~ $breaking_pattern ]] || [[ "$message" == *"BREAKING CHANGE"* ]]; then
		clean_message=$(printf '%s\n' "$message" | sed -E 's/^[a-z]+(\([^)]+\))?!?: ?//')
		breaking="${breaking}* ${clean_message} (${hash})\n"
	fi

	if [[ "$message" =~ $fix_pattern ]]; then
		if [[ "$message" =~ $scoped_fix_pattern ]]; then
			scope=$(printf '%s\n' "$message" | sed -E 's/^fix\(([^)]+)\):.*/\1/')
			clean_message=$(printf '%s\n' "$message" | sed -E 's/^fix\([^)]+\): ?//')
			fixes="${fixes}* **${scope}:** ${clean_message} ([${hash}](https://github.com/junioryono/godi/commit/${hash}))\n"
		else
			clean_message=${message#fix:}
			clean_message=${clean_message# }
			fixes="${fixes}* ${clean_message} ([${hash}](https://github.com/junioryono/godi/commit/${hash}))\n"
		fi
	fi

	if [[ "$message" =~ $feature_pattern ]]; then
		if [[ "$message" =~ $scoped_feature_pattern ]]; then
			scope=$(printf '%s\n' "$message" | sed -E 's/^feat\(([^)]+)\):.*/\1/')
			clean_message=$(printf '%s\n' "$message" | sed -E 's/^feat\([^)]+\): ?//')
			features="${features}* **${scope}:** ${clean_message} ([${hash}](https://github.com/junioryono/godi/commit/${hash}))\n"
		else
			clean_message=${message#feat:}
			clean_message=${clean_message# }
			features="${features}* ${clean_message} ([${hash}](https://github.com/junioryono/godi/commit/${hash}))\n"
		fi
	fi
done <<< "$commits"

{
	printf '# Release %s\n\n' "$version"
	if [[ -n "$breaking" ]]; then
		printf '## BREAKING CHANGES\n\n%b\n' "$breaking"
	fi
	if [[ -n "$fixes" ]]; then
		printf '## Bug Fixes\n\n%b\n' "$fixes"
	fi
	if [[ -n "$features" ]]; then
		printf '## Features\n\n%b\n' "$features"
	fi
	printf '## Installation\n\n```bash\ngo get %s@%s\n```\n' "$module_path" "$current_tag"
	printf '\n## All Changes\n\n'
	if [[ -n "$previous_tag" ]]; then
		git log "$previous_tag..$current_tag" --pretty=format:'* %s'
	else
		git log "$current_tag" --pretty=format:'* %s'
	fi
} > "$release_notes"
