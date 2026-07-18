#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
failed=false

while IFS= read -r entry; do
	file=${entry%%:*}
	rest=${entry#*:}
	line_number=${rest%%:*}
	line=${rest#*:}
	spec=$(printf '%s\n' "$line" | sed -E 's/^[[:space:]]*uses:[[:space:]]*([^[:space:]]+).*/\1/')

	[[ "$spec" == ./* ]] && continue
	ref=${spec##*@}
	if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]] || [[ ! "$line" =~ \#[[:space:]]v[0-9]+ ]]; then
		echo "$file:$line_number must pin $spec to a 40-character SHA with a '# vN' comment" >&2
		failed=true
	fi
done < <(grep -R -n -E '^[[:space:]]*uses:' "$root/.github/workflows" --include='*.yml')

if [[ "$failed" == "true" ]]; then
	exit 1
fi

echo "GitHub Actions are pinned to immutable commits"
