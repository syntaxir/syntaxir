#!/usr/bin/env bash
# Usage: [BENCHLOCK_CLASS=shared] benchlock.sh <owner-tag> <command...>
# Runs the command while holding the heavy-run lock in one of two classes:
#   exclusive (the default)  alone on the machine.
#   shared                   in one of K slots, beside other shared jobs. K is BENCHLOCK_SHARED,
#                            default one per six cores, between 1 and 3 (see lib.sh).
# The rule: every heavy run that measures no time is shared (gates, test suites, generation, the
# alpha-parsers check); only timed runs (benchmarks, A/B runs, profiles) are exclusive.
# An exclusive job starts only when no holder of either class runs. The queue is first come, first
# served across both classes: a queued exclusive job blocks the shared jobs queued after it, so a
# stream of shared jobs cannot starve it. A holder whose PID is dead is taken over.
# The command runs in a process group of its own, recorded in runs/<tag>.<pid> while it runs, so
# benchstop.sh can stop exactly the groups this script started and nothing else.
#
# The command runs in a process group of its own (job control), created as the command is forked,
# so TERM, INT or HUP to this script, even one that arrives while the command starts, signals
# the group. The command's standard input is /dev/null, set explicitly: job control does not redirect it.
# State: $SXIR_AGENT_STATE (see lib.sh). The layout is the one of the earlier versions of this script
# (the zsh one and the bash one without classes), so jobs queued by any of them are ranked together:
#   benchlock/owner         "<tag> <pid>" of the exclusive holder; the directory is the lock.
#   benchlock/shared/<tag>.<pid>   one file per shared holder. Earlier versions see benchlock/
#                           without an owner while it is shared, so they wait and never take it over.
#   runs/<tag>.<pid>        the record: state= seq= class= tag= wrapper= ... cmd=. A record without
#                           class= (written by an earlier version) is exclusive.
# Admission, reclaiming dead holders and the last shared holder's removal of benchlock/ are
# serialized between these scripts by an flock on benchlock.lock, which the kernel drops when its
# holder dies. Earlier versions do not take it; they only make benchlock/ with mkdir, which is
# atomic, and remove it only when its owner is dead.
# Earlier versions cannot take back the slot of a dead shared holder: until a waiter of this
# version comes, they wait, and queue-status.sh reports the queue as stuck.
#
# A measurement window (benchwindow.sh) and the test slots (testslot.sh): an exclusive job starts only
# when no test slot has a live holder, and while one is first in the queue or holds the lock, new
# test claims wait. While a window is open, shared jobs are not admitted (those already running
# finish), so that the timed runs of the window have the machine alone. An exclusive holder runs
# its command with SXIR_BENCHLOCK set to "<tag> <pid>", as benchlock/owner records it, so that the
# command can check that it holds the lock (the timed harness of syntaxir/bench does).
#
# Deadlock freedom: every waiter is ranked by the key (start, seq). seq is a counter handed out
# under flock, so no two waiters share a key and exactly one live waiter is first; all waiters
# rank with the same function. As a backstop, a waiter that sees the lock free while it waits for
# BENCHLOCK_STALL seconds (default 300) logs the stall to benchlock-stalls.log and takes the lock,
# so the queue always makes progress even if the ranking were wrong.
# shellcheck disable=SC2329  # the admission functions run through with_flock
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
[ $# -ge 2 ] || { echo "usage: [BENCHLOCK_CLASS=shared] benchlock.sh <owner-tag> <command...>" >&2; exit 2; }
D=$sxir_state
L=$D/benchlock
S=$L/shared
R=$D/runs
stall=${BENCHLOCK_STALL:-300}
poll=${BENCHLOCK_POLL:-20}
class=${BENCHLOCK_CLASS:-exclusive}
case $class in exclusive | shared) ;; *) echo "benchlock.sh: BENCHLOCK_CLASS must be exclusive or shared, not '$class'" >&2; exit 2 ;; esac
k=$(sxir_shared_slots)
tag=$1; shift
command -v perl >/dev/null 2>&1 || { echo "benchlock.sh: perl is required for the ticket and is not installed" >&2; exit 127; }
mkdir -p "$R"
seq=$(perl -e 'use Fcntl qw(:flock); open(my $f, "+>>", $ARGV[0]) or die "seq: $!\n"; flock($f, LOCK_EX) or die; seek($f, 0, 0); my $n = <$f> // 0; $n = int($n) + 1; truncate($f, 0); print $f "$n\n"; close $f; print $n' "$D/benchlock.seq") || { echo "benchlock: cannot allocate a ticket" >&2; exit 125; }
rec=$R/$tag.$$
recline="state=queued seq=$seq class=$class tag=$tag wrapper=$$ cwd=$PWD start=$(date +%FT%T) cmd=$*"
printf '%s\n' "$recline" > "$rec"
# A wrapper is live only if its PID still runs this script with its own tag: PIDs are reused, and a
# record whose PID now belongs to another process would otherwise block the queue forever.
# Records of wrappers that are no longer live, queued or running, are moved to runs-stale/.
live() { sxir_bench_live "$@"; }
# scan sets ahead to the number of live queued records ranked before this one by the key
# (start, seq), and ahead_x to those of them that are not shared. The start= may be edited by
# the integrator for train priority.
scan() {
  local f w ft st t s c mine
  read -r _ t s _ <<< "$(sxir_run_fields "$rec")"
  mine="$t $s"
  ahead=0; ahead_x=0
  for f in "$R"/*; do
    [ -f "$f" ] && [ "$f" != "$rec" ] || continue
    w=${f##*.}; ft=${f##*/}; ft=${ft%.*}
    live "$w" "$ft" || { mkdir -p "$D/runs-stale"; mv -f "$f" "$D/runs-stale/" 2>/dev/null || true; continue; }
    read -r st t s c <<< "$(sxir_run_fields "$f")"
    [ "$st" = queued ] || continue
    if [[ "$t $s" < "$mine" ]]; then
      ahead=$((ahead + 1))
      [ "$c" = shared ] || ahead_x=$((ahead_x + 1))
    fi
  done
}
# reclaim, under the flock, frees the lock of dead holders: an exclusive owner, shared holders
# (benchlock/ goes when the last one does), and a benchlock/ with neither, older than a minute,
# left by an exclusive holder killed between making it and writing its owner.
reclaim() {
  local pid otag e t p
  [ -d "$L" ] || return 0
  if [ -f "$L/owner" ]; then
    pid=$(awk '{print $2}' "$L/owner" 2>/dev/null || true)
    otag=$(awk '{print $1}' "$L/owner" 2>/dev/null || true)
    if [ -n "$pid" ] && ! live "$pid" "$otag"; then rm -f "$L/owner"; rmdir "$L" 2>/dev/null || true; fi
  elif [ -d "$S" ]; then
    for e in "$S"/*; do
      [ -f "$e" ] || continue
      read -r t p _ < "$e" || true
      if [ -n "${p:-}" ] && ! live "$p" "$t"; then rm -f "$e"; fi
    done
    if rmdir "$S" 2>/dev/null; then rmdir "$L" 2>/dev/null || true; fi
  elif [ -n "$(find "$L" -maxdepth 0 -mmin +1 2>/dev/null)" ]; then
    rmdir "$L" 2>/dev/null || true
  fi
}
# with_flock CMD... runs CMD while holding the flock on benchlock.lock.
with_flock() {
  local rc=0
  exec 9>> "$D/benchlock.lock"
  perl -e 'use Fcntl ":flock"; open(my $f, ">&=", 9) or exit 2; flock($f, LOCK_EX) or exit 2' \
    || { exec 9>&-; echo "benchlock.sh: cannot take benchlock.lock" >&2; exit 125; }
  "$@" || rc=$?
  exec 9>&-
  return "$rc"
}
# admit takes the lock in this job's class when the queue lets it; it returns 1 when it must be
# retried. An exclusive job must be first and find benchlock/ absent. A shared job must have no
# exclusive job queued before it, and a slot that the shared jobs queued before it leave free.
# After BENCHLOCK_STALL seconds of a free lock, either takes a free lock regardless of rank.
admit() {
  local stalled=0 h
  [ "$free" -lt "$stall" ] || stalled=1
  [ -f "$rec" ] || printf '%s\n' "$recline" > "$rec"
  reclaim
  scan
  if [ "$class" = exclusive ]; then
    [ "$(sxir_slots_held)" = 0 ] || return 1
    { [ "$ahead" = 0 ] || [ "$stalled" = 1 ]; } && mkdir "$L" 2>/dev/null || return 1
    echo "$tag $$" > "$L/owner"
    return 0
  fi
  ! sxir_window_live || return 1
  if [ ! -d "$L" ]; then
    { { [ "$ahead_x" = 0 ] && [ "$ahead" -lt "$k" ]; } || [ "$stalled" = 1 ]; } || return 1
    mkdir "$L" 2>/dev/null || return 1
    mkdir "$S" || { rmdir "$L"; return 1; }
  elif [ -d "$S" ] && [ ! -f "$L/owner" ]; then
    h=$(find "$S" -mindepth 1 -maxdepth 1 -type f | wc -l)
    [ "$ahead_x" = 0 ] && [ $((ahead + h)) -lt "$k" ] || return 1
  else
    return 1
  fi
  echo "$tag $$ $(sxir_lstart $$)" > "$S/$tag.$$"
}
# release frees this job's hold; the last shared holder removes benchlock/. A stray file in
# benchlock/ keeps it from going: release reports that on stderr and in benchlock-stalls.log, which
# queue-status.sh shows, and returns 1. It never changes the command's exit status.
release() {
  if [ "$class" = exclusive ]; then
    [ "$(cat "$L/owner" 2>/dev/null || true)" = "$tag $$" ] || return 0
    rm -f "$L/owner"
  else
    rm -f "$S/$tag.$$"
    rmdir "$S" 2>/dev/null || return 0
  fi
  rmdir "$L" 2>/dev/null && return 0
  local msg
  msg="$(date +%FT%T) stray: $tag.$$ ($class) could not remove benchlock/, which holds: $(find "$L" -mindepth 1 -maxdepth 1 2>/dev/null | sed 's|.*/||' | tr '\n' ' ')"
  echo "benchlock.sh: $msg" >&2
  printf '%s\n' "$msg" >> "$D/benchlock-stalls.log"
  return 1
}
free=0
# The lock counts as free for the stall backstop only when nothing legitimate holds this job back:
# not the lock, not the test slots an exclusive job waits for (its own, or the first exclusive job's
# ahead of a shared one), not a window a shared job waits for.
until with_flock admit; do
  if [ -d "$L" ] || { [ "$(sxir_slots_held)" != 0 ] && { [ "$class" = exclusive ] || sxir_exclusive_first; }; } \
    || { [ "$class" = shared ] && sxir_window_live; }; then
    free=0
  else
    free=$((free + poll))
  fi
  sleep "$poll"
done
if [ "$free" -ge "$stall" ]; then
  printf '%s stall: lock free %ss, %s.%s (seq %s, %s) took it; queue: %s\n' "$(date +%FT%T)" "$free" "$tag" "$$" "$seq" "$class" \
    "$(cat "$R"/* | cut -c1-80 | tr '\n' '|')" >> "$D/benchlock-stalls.log"
fi
term=
child=
# shellcheck disable=SC2329  # runs from the trap
on_term() {
  term=1
  if [ -n "$child" ]; then kill -TERM -- "-$child" 2>/dev/null || true; fi
}
trap on_term TERM INT HUP
if [ "$class" = exclusive ]; then export SXIR_BENCHLOCK="$tag $$"; else unset SXIR_BENCHLOCK; fi
set -m
"$@" < /dev/null &
child=$!
set +m
printf '%s\n' "state=running seq=$seq class=$class tag=$tag wrapper=$$ pgid=$child cwd=$PWD start=$(date +%FT%T) cmd=$*" > "$rec"
if [ -n "$term" ]; then kill -TERM -- "-$child" 2>/dev/null || true; fi
# child_gone: the command's leader has exited (a zombie counts: it only waits to be reaped).
child_gone() {
  local st
  kill -0 "$1" 2>/dev/null || return 0
  st=$(ps -o stat= -p "$1" 2>/dev/null | tr -d ' ') || true
  [ "${st#Z}" != "$st" ]
}
# A TERM can reach the command's group while the forked shell still runs this script's handler,
# before it has reset its traps: the signal is then lost. So once TERM was received, repeat it
# until the group leader is gone, and kill the group if it ignores TERM for five seconds.
rc=0; reaped=0; tries=0
until child_gone "$child"; do
  if [ -n "$term" ]; then
    kill -TERM -- "-$child" 2>/dev/null || true
    tries=$((tries + 1))
    if [ "$tries" -gt 50 ]; then kill -KILL -- "-$child" 2>/dev/null || true; fi
    sleep 0.1 || true
  else
    wait "$child" && rc=0 || rc=$?
    if [ -z "$term" ]; then reaped=1; fi
  fi
done
if [ "$reaped" = 0 ]; then wait "$child" && rc=0 || rc=$?; fi
rm -f "$rec"
with_flock release || true
exit "$rc"
