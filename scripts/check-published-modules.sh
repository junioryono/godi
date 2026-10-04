#!/usr/bin/env bash

# Tests the candidate release set the way consumers will see it once tagged.
#
# Release tags are created for every module at one commit, and every module
# requires the other godi modules at the release version (see
# check-release-floors.sh). This script serves the core and the integrations
# of this checkout at that version from a local file-based module proxy, drops
# every replace directive from the integration and test modules, and then
# tidies, verifies, and tests them against that proxy. It catches graphs that
# only work because of local replacements: missing requirements, files that
# fall outside a module zip, and floors that do not resolve to one release.
#
# Third-party modules still come from GOPROXY (with the local module cache as
# a first fallback). A private module cache keeps the candidate modules away
# from any copy of the same version that was already downloaded.

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
work=$(mktemp -d)
cleanup() {
	chmod -R u+w "$work" 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

version=$("$root/scripts/check-release-floors.sh")
"$root/scripts/build-candidate-proxy.sh" "$work/proxy" "$version"

shared_cache=$(go env GOMODCACHE)
upstream=$(go env GOPROXY)
export GOPROXY="file://$work/proxy,file://$shared_cache/cache/download,$upstream"
export GOMODCACHE="$work/modcache"
export GONOSUMDB="github.com/junioryono/godi${GONOSUMDB:+,$GONOSUMDB}"
export GOFLAGS="-modcacherw${GOFLAGS:+ $GOFLAGS}"

for directory in $("$root/scripts/module-matrix.sh" published | tr -d '[]"' | tr ',' ' '); do
	printf '\n==> %s: test candidate release set %s without replacements\n' "$directory" "$version"
	mkdir -p "$work/modules/$(dirname "$directory")"
	cp -R "$root/$directory" "$work/modules/$directory"
	(
		cd "$work/modules/$directory"
		while IFS= read -r module; do
			go mod edit -dropreplace "$module"
		done < <(go mod edit -json | awk -F'"' '/"Old":/ { old = 1; next } old && $2 == "Path" { print $4; old = 0 }')
		if grep -q '^replace' go.mod; then
			echo "$directory/go.mod still contains replace directives" >&2
			exit 1
		fi
		go mod tidy
		go mod verify
		go list -m -f '{{if .Replace}}{{.Path}} is replaced{{end}}' all | grep . && exit 1
		go test ./...
	) </dev/null
done
