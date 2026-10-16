#!/usr/bin/env bash
# verify-changed.sh: check what this checkout changed, and nothing else.
#
# `make verify` runs it. It is the local check while working; CI runs the full
# gate (`make verify-ci`'s steps: the race tests of every module on every
# supported Go, lint, coverage, docs, the candidate release set, security) on
# every push, and `make verify-full` runs the local half of it here. Full gates
# run again and again on one shared Mac are what starved it into a kernel panic
# on 2026-10-15, so this checks the changed packages and leaves the tree to CI.
#
# WHAT CHANGED is everything between the merge base with the base ref
# (VERIFY_BASE, default origin/main) and the working tree: commits on this
# branch, staged and unstaged edits, and untracked files git does not ignore. A
# merge base that cannot be found is could-not-tell, never "nothing changed":
# the script says so and exits 2.
#
# WHAT IT RUNS, for each module (a directory with a go.mod) a change is in:
#   - gofmt -s -l on every changed .go file;
#   - go build ./... once any package of the module changed;
#   - go mod tidy -diff when its go.mod or go.sum changed;
#   - go vet, go test and golangci-lint on the changed packages: a package is
#     the directory of a changed .go file, or the nearest directory above a
#     changed non-Go file that holds Go files (an embedded asset, a script a
#     test executes), with any testdata component cut off first, because the go
#     tool ignores testdata. .claude/ and the Makefile are read by the tests in
#     scripts/, so they select it. go test runs without -race, at -p 2 and
#     GOMAXPROCS=4: -race costs many times the run (21x for one package of a
#     sibling project, 23 s against 491 s, measured 2026-10-15) and CI runs it.
#     golangci-lint gets packages, never files (a file alone is type-checked
#     without the rest of its package), and --allow-serial-runners, because its
#     lock is machine-wide and a second run without it fails rather than waits.
# A changed workflow runs actionlint and scripts/check-actions-pinned.sh, as
# `make workflow-check` does. A changed docs/ file is left to CI's docs job.
#
# GOLANGCI_LINT and ACTIONLINT name the binaries (make passes the pinned ones);
# otherwise they are looked up on PATH.
#
# Exit status: 0 when every check that ran passed, 1 when one failed, 2 when the
# change could not be read. A required tool that is missing is a failure. Every
# step runs even after an earlier one failed, so one run reports everything.

set -uo pipefail

say() {
	printf 'verify: %s\n' "$*"
}

cannot_tell() {
	printf 'verify: could not tell what changed: %s\n' "$*" >&2
	exit 2
}

if ! root=$(git rev-parse --show-toplevel 2>/dev/null); then
	cannot_tell "this is not a git checkout"
fi
if ! cd "$root"; then
	cannot_tell "could not enter $root"
fi

base_ref=${VERIFY_BASE:-origin/main}
if ! base=$(git merge-base "$base_ref" HEAD 2>/dev/null); then
	cannot_tell "no merge base between $base_ref and HEAD; run \`git fetch origin main\`, or name the base in VERIFY_BASE"
fi

if ! list=$(mktemp "${TMPDIR:-/tmp}/verify-changed.XXXXXX"); then
	cannot_tell "could not make a temporary file"
fi
sorted=$list.sorted
trap 'rm -f "$list" "$sorted"' EXIT

# --no-renames lists both sides of a rename, so a file moved out of a package
# still selects the package it left.
if ! git diff --name-only -z --no-renames "$base" -- >"$list"; then
	cannot_tell "git diff against $base failed"
fi
if ! git ls-files --others --exclude-standard -z >>"$list"; then
	cannot_tell "git ls-files could not list the untracked files"
fi
# Sorted into a file of its own, with its status read, rather than through a
# process substitution whose failure would read as an empty change.
if ! LC_ALL=C sort -zu "$list" >"$sorted"; then
	cannot_tell "could not sort the changed paths"
fi

: "${GOMAXPROCS:=4}"
export GOMAXPROCS

failed=()
fail() {
	failed+=("$1")
}

# Sets, as arrays: membership is a linear scan, because a Mac's bash 3.2 has
# no associative arrays.
contains() {
	local needle=$1 item
	shift
	for item in "$@"; do
		[ "$item" = "$needle" ] && return 0
	done
	return 1
}

has_go() {
	local f
	for f in "$1"/*.go; do
		[ -e "$f" ] && return 0
	done
	return 1
}

# outside_testdata DIR prints DIR with everything from its first testdata
# component cut off.
outside_testdata() {
	local d=$1
	case "$d" in
	testdata | testdata/*)
		printf '.'
		return
		;;
	esac
	d="$d/"
	d=${d%%/testdata/*}
	printf '%s' "${d%/}"
}

# module_of DIR prints the nearest directory at or above DIR holding a go.mod.
module_of() {
	local d=$1
	while :; do
		if [ -f "$d/go.mod" ]; then
			printf '%s' "$d"
			return 0
		fi
		[ "$d" = . ] && return 1
		d=$(dirname "$d")
	done
}

# tool NAME OVERRIDE prints the binary to run: OVERRIDE when it is set, else
# NAME from PATH. It fails when neither is there.
tool() {
	local name=$1 override=$2
	if [ -n "$override" ]; then
		[ -x "$override" ] || return 1
		printf '%s' "$override"
		return 0
	fi
	command -v "$name"
}

go_files=()
pkg_dirs=()
pkg_why=()
mod_changed=()
build_only=()
workflows=()
docs_count=0
notes=()

add_pkg() {
	local dir=$1 why=$2
	if contains "$dir" ${pkg_dirs[@]+"${pkg_dirs[@]}"}; then
		return
	fi
	pkg_dirs+=("$dir")
	pkg_why+=("$why")
}

count=0
while IFS= read -r -d '' path; do
	count=$((count + 1))
	dir=$(outside_testdata "$(dirname "$path")")

	case "$path" in
	go.mod | go.sum | */go.mod | */go.sum)
		mdir=$(dirname "$path")
		if [ -f "$mdir/go.mod" ] && ! contains "$mdir" ${mod_changed[@]+"${mod_changed[@]}"}; then
			mod_changed+=("$mdir")
		fi
		continue
		;;
	*.go)
		if [ -f "$path" ]; then
			go_files+=("$path")
		fi
		if has_go "$dir"; then
			add_pkg "$dir" "$path changed"
		else
			# A package that is gone still has importers to build.
			notes+=("$dir holds no Go file any more; go build ./... covers what imported it")
			if m=$(module_of "$dir"); then
				contains "$m" ${mod_changed[@]+"${mod_changed[@]}"} ||
					contains "$m" ${build_only[@]+"${build_only[@]}"} || build_only+=("$m")
			fi
		fi
		continue
		;;
	esac

	case "$path" in
	CLAUDE.md | */CLAUDE.md | AGENTS.md | */AGENTS.md)
		# Instructions for a session, which no package reads.
		continue
		;;
	docs/*)
		docs_count=$((docs_count + 1))
		;;
	.github/workflows/*)
		[ -f "$path" ] && workflows+=("$path")
		continue
		;;
	.claude/* | Makefile)
		add_pkg scripts "$path changed, and scripts/ tests what it says"
		continue
		;;
	esac

	# The nearest directory holding Go files reads this file, if any does.
	d=$dir
	while [ "$d" != . ]; do
		if has_go "$d"; then
			add_pkg "$d" "$path changed beside it"
			break
		fi
		d=$(dirname "$d")
	done
done <"$sorted"

if [ "$count" -eq 0 ]; then
	say "nothing changed against $base_ref ($base); nothing to verify"
	exit 0
fi
say "changed paths: $count, against $base_ref ($base); Go runs with GOMAXPROCS=$GOMAXPROCS"

for note in ${notes[@]+"${notes[@]}"}; do
	say "$note"
done

if [ "${#go_files[@]}" -gt 0 ]; then
	printf 'verify: $ gofmt -s -l <%d changed Go files>\n' "${#go_files[@]}"
	if ! unformatted=$(gofmt -s -l "${go_files[@]}"); then
		fail "gofmt could not read every changed file"
	fi
	if [ -n "$unformatted" ]; then
		printf 'verify: these files need gofmt -s:\n%s\n' "$unformatted"
		fail "gofmt"
	fi
fi

# The modules to check: every one a changed package or a changed go.mod is in.
modules=()
for i in ${pkg_dirs[@]+"${!pkg_dirs[@]}"}; do
	if ! m=$(module_of "${pkg_dirs[$i]}"); then
		say "${pkg_dirs[$i]} is in no Go module; not checked"
		continue
	fi
	contains "$m" ${modules[@]+"${modules[@]}"} || modules+=("$m")
done
for m in ${mod_changed[@]+"${mod_changed[@]}"} ${build_only[@]+"${build_only[@]}"}; do
	contains "$m" ${modules[@]+"${modules[@]}"} || modules+=("$m")
done

lint=
if [ "${#modules[@]}" -gt 0 ] && ! lint=$(tool golangci-lint "${GOLANGCI_LINT:-}"); then
	say "golangci-lint is not installed (${GOLANGCI_LINT:-not on PATH}); \`make tools\` installs the pinned version"
	fail "golangci-lint (not installed)"
	lint=
fi

for m in ${modules[@]+"${modules[@]}"}; do
	label=$m
	[ "$m" = . ] && label="the root module"

	pkgs=()
	for i in ${pkg_dirs[@]+"${!pkg_dirs[@]}"}; do
		d=${pkg_dirs[$i]}
		[ "$(module_of "$d")" = "$m" ] || continue
		if [ "$d" = "$m" ]; then
			rel=.
		elif [ "$m" = . ]; then
			rel=./$d
		else
			rel=./${d#"$m"/}
		fi
		pkgs+=("$rel")
		say "$label: $rel (${pkg_why[$i]})"
	done

	if ! (
		cd "$m" || exit 1
		ok=0
		say "$label: go build ./..."
		go build ./... || ok=1
		if contains "$m" ${mod_changed[@]+"${mod_changed[@]}"}; then
			say "$label: go.mod or go.sum changed, so go mod tidy -diff"
			go mod tidy -diff || ok=1
		fi
		exit "$ok"
	); then
		fail "$label: build or tidy"
	fi

	[ "${#pkgs[@]}" -gt 0 ] || continue

	if ! (cd "$m" && say "$label: go vet ${pkgs[*]}" && go vet "${pkgs[@]}"); then
		fail "$label: go vet"
	fi
	if ! (cd "$m" && say "$label: go test -p 2 ${pkgs[*]}" && go test -p 2 "${pkgs[@]}"); then
		fail "$label: go test"
	fi
	if [ -n "$lint" ]; then
		if ! (cd "$m" && say "$label: golangci-lint run --allow-serial-runners ${pkgs[*]}" &&
			"$lint" run --allow-serial-runners "${pkgs[@]}"); then
			fail "$label: golangci-lint"
		fi
	fi
done

if [ "${#workflows[@]}" -gt 0 ]; then
	if actionlint=$(tool actionlint "${ACTIONLINT:-}"); then
		say "workflows changed: actionlint ${workflows[*]}"
		"$actionlint" "${workflows[@]}" || fail "actionlint"
	else
		say "actionlint is not installed (${ACTIONLINT:-not on PATH}); \`make tools\` installs the pinned version"
		fail "actionlint (not installed)"
	fi
	say "workflows changed: scripts/check-actions-pinned.sh"
	scripts/check-actions-pinned.sh || fail "check-actions-pinned"
fi

if [ "$docs_count" -gt 0 ]; then
	say "docs/ changed ($docs_count files); CI's Documentation job builds them with warnings as errors (\`make docs\`)"
fi

say "not run here, run by CI on every push: the race tests of every module on every supported Go, coverage, docs, the candidate release set, security; \`make verify-full\` runs the local gate"

if [ "${#failed[@]}" -gt 0 ]; then
	say "FAILED: ${failed[*]}"
	exit 1
fi
say "every check that ran passed"
