#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

for directory in $(awk '!/^#/ && NF { print $1 }' "$root/scripts/modules.txt"); do
	printf '\n==> %s: verify and tidy check\n' "$directory"
	(
		cd "$root/$directory"
		go mod verify
		go mod tidy -diff
	) </dev/null
done
