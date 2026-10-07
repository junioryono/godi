#!/bin/sh
# with-check-lock.sh LOCKFILE COMMAND [ARG...]
#
# Runs COMMAND holding an exclusive lock on LOCKFILE, so two gates on one
# machine, from two sessions, worktrees or repositories, take turns instead of
# starving the machine together. A second run waits for the first and says so
# once; the exit status is COMMAND's own.
#
# An empty LOCKFILE means the lock every repository's gate shares:
# $XDG_CACHE_HOME/dev-gate.lock when XDG_CACHE_HOME is absolute, otherwise
# $HOME/.cache/dev-gate.lock. It is worked out here, in the shell, because
# make's word functions split a value holding spaces.
#
# macOS ships lockf(1) and no flock(1); Linux ships flock(1) and no lockf(1). The
# lock is a courtesy to the rest of the machine and never a gate, so a lock that
# cannot be taken (no tool, a file that cannot be created) is reported and the
# command runs without it, once.
#
# A run inside a command that already holds the same lock (a gate that runs
# another gate, directly or through another lock) runs under it, or it would
# wait for an ancestor that cannot finish until it does. The command is handed
# DEV_GATE_LOCK_HELD, one "PID:PATH" line per lock its ancestors hold, PID being
# the run that holds PATH; an entry counts only while that run is an ancestor of
# this one, so a process a gate left behind does not carry the lock with it. A
# process tree that cannot be read is a question with no answer, and the run
# goes ahead unlocked rather than risk waiting on its own ancestor.
#
# The script sets no shell option: bash rewrites an exported SHELLOPTS when one
# changes, and the command inherits it. Every expansion carries its own default
# instead of nounset, and every status the script reads, or a step that can
# fail, is taken inside an if or an or-list, so an errexit bash took from that
# SHELLOPTS cannot end it on an answer.

nl='
'

# say prints one diagnostic line. printf rather than echo, which in dash reads
# backslash sequences in a path as escapes.
say() {
	printf 'with-check-lock: %s\n' "$*" >&2 || :
}

# held_by_ancestor PATH exits 0 when a run this one is inside holds PATH, 1 when
# none does, and 2 when an entry names PATH and the process tree could not say
# whether its run is an ancestor. The list is split on newlines alone, with
# globbing off, rather than read from a here-document, whose temporary file
# can fail to be written and leave the loop silently empty.
held_by_ancestor() (
	verdict=1
	set -f
	IFS=$nl
	for entry in ${DEV_GATE_LOCK_HELD-}; do
		[ "${entry#*:}" = "$1" ] || continue
		holder=${entry%%:*}
		case "$holder" in
		'' | *[!0-9]*) continue ;;
		esac

		pid=$$
		depth=0
		while [ "$depth" -lt 128 ]; do
			if ! ppid=$(ps -o ppid= -p "$pid" 2>/dev/null); then
				verdict=2
				continue 2
			fi
			if ! ppid=$(printf '%s' "$ppid" | tr -d " \t$nl"); then
				verdict=2
				continue 2
			fi
			case "$ppid" in
			'' | *[!0-9]*)
				verdict=2
				continue 2
				;;
			esac
			# Compared before the walk stops at the root, so a holder that is a
			# container's pid 1 is still found.
			[ "$ppid" = "$holder" ] && exit 0
			[ "$ppid" -le 1 ] && continue 2
			pid=$ppid
			depth=$((depth + 1))
		done
		verdict=2
	done
	exit "$verdict"
)

if [ "$#" -lt 2 ]; then
	say "usage: with-check-lock.sh LOCKFILE COMMAND [ARG...]"
	exit 2
fi

lock=$1
shift

if [ -z "$lock" ]; then
	case "${XDG_CACHE_HOME-}" in
	/*) lock=$XDG_CACHE_HOME/dev-gate.lock ;;
	*)
		if [ -z "${HOME-}" ]; then
			say "neither an absolute XDG_CACHE_HOME nor HOME is set, so this run is not serialised with others"
			exec "$@"
		fi
		lock=$HOME/.cache/dev-gate.lock
		;;
	esac
fi

# A relative path names a lock in this directory, so it is made absolute before
# it is compared with or handed to anything that may run elsewhere. A path with
# a newline in it cannot be one line of DEV_GATE_LOCK_HELD.
case "$lock" in
/*) ;;
*)
	if ! here=$(pwd -P); then
		say "could not read this directory, so this run is not serialised with others"
		exec "$@"
	fi
	lock=$here/$lock
	;;
esac
case "$lock" in
*"$nl"*)
	say "the lock path holds a newline, so this run is not serialised with others"
	exec "$@"
	;;
esac

if held_by_ancestor "$lock"; then
	ancestry=0
else
	ancestry=$?
fi
case "$ancestry" in
0)
	say "$lock is held by the run this one is inside; running under it"
	exec "$@"
	;;
1) ;;
*)
	say "could not tell whether a run this one is inside holds $lock, so this run is not serialised with others"
	exec "$@"
	;;
esac

# The lock's directory is made here, quoted, so a path holding spaces is one
# path, and a directory that cannot be made still ends in the command running,
# unlocked and saying so. Something other than a file where the lock goes (a
# FIFO, whose open waits for a writer) is not taken either.
if ! dir=$(dirname -- "$lock") || ! mkdir -p -- "$dir" 2>/dev/null; then
	say "could not create ${dir:-the directory of $lock}, so this run is not serialised with others"
	exec "$@"
fi
if [ -e "$lock" ] && [ ! -f "$lock" ]; then
	say "$lock is not a regular file, so this run is not serialised with others"
	exec "$@"
fi

# The probe runs `true` under a lock it cannot wait for, so its status is the
# lock tool's own answer and never the command's: 0 free, 75 held, anything
# else a lock that could not be taken at all.
if command -v lockf >/dev/null 2>&1; then
	if lockf -k -t 0 "$lock" true 2>/dev/null; then probe=0; else probe=$?; fi
	held="lockf -k"
elif command -v flock >/dev/null 2>&1; then
	if flock -n -E 75 "$lock" true 2>/dev/null; then probe=0; else probe=$?; fi
	# -o closes the locked descriptor before the command runs, so a process the
	# command leaves behind cannot go on holding the lock after it exits.
	held="flock -o"
else
	say "neither lockf nor flock is installed, so this run is not serialised with others"
	exec "$@"
fi

case "$probe" in
0) ;;
75) say "another run holds $lock; waiting for it to finish" ;;
*)
	say "could not take $lock (status $probe), so this run is not serialised with others"
	exec "$@"
	;;
esac

# The lock is taken a second time, now waiting, and that take can fail too (the
# file's directory replaced since the probe, say). The command's first act under
# the lock is to remove a marker, and the command runs only once that removal
# succeeded, so a failure that leaves the marker never ran the command and the
# command runs unlocked; any other status is the command's own. The marker lives
# in a directory made for this run alone, apart from the lock's, so nothing
# another run does to either can stand in for it. The write is in a subshell
# because a failed redirection on `:` ends a dash script outright.
if ! private=$(mktemp -d "${TMPDIR:-/tmp}/with-check-lock.XXXXXX" 2>/dev/null); then
	say "could not make a private directory for this run, so it is not serialised with others"
	exec "$@"
fi
started=$private/started
if ! (: > "$started") 2>/dev/null; then
	rm -rf -- "$private" || :
	say "could not write in $private, so this run is not serialised with others"
	exec "$@"
fi

# Only the locked command is told this run holds the lock; the unlocked fallback
# below inherits what this run was given and nothing more.
holding="${DEV_GATE_LOCK_HELD-}${DEV_GATE_LOCK_HELD:+$nl}$$:$lock"

# The inner shell runs the command in a child it waits for rather than exec'ing
# it, so a command ended by a signal reports the shell's 128+n (macOS lockf
# reports any signalled child as 70); the child execs it, so a program is never
# swapped for a shell builtin of the same name. The exit after the subshell
# keeps the child a child: dash runs a subshell that is the last command of
# `sh -c` in place, which puts the command straight back under lockf (measured
# 2026-10-07, dash as sh: 70 without it, 143 with it). $held is one of the two literal spellings above, split
# on purpose; the inner script's $1 and $@ are its own, so they stay in single
# quotes.
# shellcheck disable=SC2086,SC2016
if DEV_GATE_LOCK_HELD=$holding $held "$lock" sh -c 'rm -f -- "$1" || exit; shift; (exec "$@"); exit' with-check-lock "$started" "$@"; then
	status=0
else
	status=$?
fi

if [ "$status" -ne 0 ] && [ -e "$started" ]; then
	rm -rf -- "$private" || :
	say "could not take $lock (status $status), so this run is not serialised with others"
	exec "$@"
fi

rm -rf -- "$private" || :
exit "$status"
