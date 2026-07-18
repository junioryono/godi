#!/usr/bin/env bash

set -euo pipefail

if [[ $# -eq 0 ]]; then
	echo "usage: $0 command [args ...]" >&2
	exit 2
fi

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

for directory in $(awk '!/^#/ && NF { print $1 }' "$root/scripts/modules.txt"); do
	printf '\n==> %s: %s\n' "$directory" "$*"
	(cd "$root/$directory" && "$@" </dev/null)
done
