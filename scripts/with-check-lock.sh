#!/bin/sh
# with-check-lock.sh LOCKFILE COMMAND [ARG...]
#
# Runs COMMAND holding an exclusive lock on LOCKFILE, so two `make verify` runs
# on one machine, from two sessions, two worktrees or two repositories sharing the
# lock file, take turns instead of starving the machine together. A second run
# waits for the first and says so once; the exit status is COMMAND's own.
#
# macOS ships lockf(1) and no flock(1); Linux ships flock(1) and no lockf(1). The
# lock is a courtesy to the rest of the machine and never a gate, so a lock that
# cannot be taken (no tool, a file that cannot be created) is reported and the
# command runs without it.
set -u

if [ "$#" -lt 2 ]; then
	echo "usage: with-check-lock.sh LOCKFILE COMMAND [ARG...]" >&2
	exit 2
fi

lock=$1
shift

# The lock's directory is made here, quoted, so a path holding spaces is one
# path, and a directory that cannot be made still ends in the command running,
# unlocked and saying so.
dir=$(dirname -- "$lock")
if ! mkdir -p -- "$dir" 2>/dev/null; then
	echo "with-check-lock: could not create $dir, so this run is not serialised with others" >&2
	exec "$@"
fi

# The probe runs `true` under a lock it cannot wait for, so its status is the
# lock tool's own answer and never the command's: 0 free, 75 held, anything
# else a lock that could not be taken at all.
if command -v lockf >/dev/null 2>&1; then
	lockf -k -t 0 "$lock" true 2>/dev/null
	probe=$?
	held="lockf -k"
elif command -v flock >/dev/null 2>&1; then
	flock -n -E 75 "$lock" true 2>/dev/null
	probe=$?
	# -o closes the locked descriptor before the command runs, so a process the
	# command leaves behind cannot go on holding the lock after it exits.
	held="flock -o"
else
	echo "with-check-lock: neither lockf nor flock is installed, so this run is not serialised with others" >&2
	exec "$@"
fi

case "$probe" in
0) ;;
75) echo "with-check-lock: another run holds $lock; waiting for it to finish" >&2 ;;
*)
	echo "with-check-lock: could not take $lock (status $probe), so this run is not serialised with others" >&2
	exec "$@"
	;;
esac

# $held is one of the two literal spellings above; it is split on purpose.
# shellcheck disable=SC2086
exec $held "$lock" "$@"
