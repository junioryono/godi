#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
selection=${1:-all}
first=true

printf '['
while read -r directory kind; do
	case "$directory" in
		''|'#'*) continue ;;
	esac

	case "$selection" in
		all) ;;
		coverage)
			[[ "$kind" == "benchmark" || "$kind" == "test" ]] && continue
			;;
		release)
			[[ "$kind" != "integration" ]] && continue
			;;
		published)
			# Integration modules only: test modules compose the sibling
			# adapters and assert this branch's behavior, which published
			# adapter versions cannot satisfy until after a release.
			[[ "$kind" != "integration" ]] && continue
			;;
		*)
			echo "unknown module selection: $selection" >&2
			exit 2
			;;
	esac

	if [[ "$first" == "false" ]]; then
		printf ','
	fi
	printf '"%s"' "$directory"
	first=false
done < "$root/scripts/modules.txt"
printf ']\n'
