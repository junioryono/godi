#!/usr/bin/env bash

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
benchtime=${BENCHTIME:-1s}
count=${BENCH_COUNT:-1}

printf '# godi benchmark metadata\n'
printf '# timestamp: %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf '# commit: %s\n' "$(git -C "$root" rev-parse HEAD)"
printf '# go: %s\n' "$(go version)"
printf '# GOOS/GOARCH: %s/%s\n' "$(go env GOOS)" "$(go env GOARCH)"
printf '# benchtime/count: %s/%s\n' "$benchtime" "$count"
if command -v uname >/dev/null 2>&1; then
	printf '# system: %s\n' "$(uname -a)"
fi
printf '\n# package benchmarks\n'
(cd "$root" && go test -run='^$' -bench=. -benchmem -benchtime="$benchtime" -count="$count" ./...)
printf '\n# comparative benchmarks\n'
(cd "$root/benchmarks" && go test -run='^$' -bench=. -benchmem -benchtime="$benchtime" -count="$count" .)
