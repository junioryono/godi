#!/usr/bin/env bash

# Usage: smoke-test-release.sh vMAJOR.MINOR.PATCH
#
# Post-publication check: from an empty module with no replace directives,
# require the core and every integration listed in scripts/modules.txt at the
# released version, then build and run a small program that imports all of
# them. It fails if the published graph does not select exactly that version
# of every godi module.
#
# Environment:
#   SMOKE_GOPROXY    GOPROXY for the consumer (default: https://proxy.golang.org).
#                    Use "direct" to fetch the tags from GitHub without waiting
#                    for the proxy.
#   SMOKE_ATTEMPTS   how many times to try resolving the release (default: 20)
#   SMOKE_DELAY      seconds between attempts (default: 30)

set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${1:-}
attempts=${SMOKE_ATTEMPTS:-20}
delay=${SMOKE_DELAY:-30}

if [[ ! "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
	echo "usage: $0 vMAJOR.MINOR.PATCH" >&2
	exit 2
fi

modules=()
while read -r directory kind; do
	case "$directory" in
		''|'#'*) continue ;;
	esac
	[[ "$kind" == "core" || "$kind" == "integration" ]] || continue
	modules+=("$(awk '$1 == "module" { print $2; exit }' "$root/$directory/go.mod")")
done < "$root/scripts/modules.txt"
core=${modules[0]}

work=$(mktemp -d)
cleanup() {
	chmod -R u+w "$work" 2>/dev/null || true
	rm -rf "$work"
}
trap cleanup EXIT

export GOPROXY=${SMOKE_GOPROXY:-https://proxy.golang.org}
export GOMODCACHE="$work/modcache"
export GOFLAGS=-modcacherw
export GOWORK=off
unset GONOSUMDB GONOSUMCHECK GOPRIVATE GONOPROXY GOINSECURE

mkdir -p "$work/consumer"
cd "$work/consumer"
go mod init example.com/godi-release-smoke >/dev/null 2>&1

specs=()
for module in "${modules[@]}"; do
	specs+=("$module@$version")
done

attempt=1
until go get "${specs[@]}"; do
	if (( attempt >= attempts )); then
		echo "could not resolve the $version release set after $attempts attempts" >&2
		exit 1
	fi
	echo "release set not resolvable yet (attempt $attempt/$attempts); retrying in ${delay}s" >&2
	attempt=$((attempt + 1))
	sleep "$delay"
done

{
	printf 'package main\n\nimport (\n\t"fmt"\n\t"os"\n\n'
	printf '\t"%s"\n' "$core"
	for module in "${modules[@]:1}"; do
		printf '\t_ "%s"\n' "$module"
	done
	cat <<'EOF'
)

type greeter struct{ message string }

func newGreeter() *greeter { return &greeter{message: "godi release smoke test ok"} }

func main() {
	services := godi.NewCollection()
	services.AddSingleton(newGreeter)
	provider, err := services.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer provider.Close()

	g, err := godi.Resolve[*greeter](provider)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(g.message)
}
EOF
} > main.go

go mod tidy
go vet ./...
go build -o smoke .

failed=false
for module in "${modules[@]}"; do
	selected=$(go list -m -f '{{.Version}}{{if .Replace}} (replaced){{end}}' "$module")
	printf '%s %s\n' "$module" "$selected"
	if [[ "$selected" != "$version" ]]; then
		echo "$module resolved to $selected, not $version" >&2
		failed=true
	fi
done
[[ "$failed" == "false" ]]

output=$(./smoke)
printf '%s\n' "$output"
[[ "$output" == "godi release smoke test ok" ]]
echo "clean consumer build of the $version release set succeeded"
