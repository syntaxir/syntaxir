#!/usr/bin/env bash
# Usage: [BENCHLOCK_CLASS=shared] benchlock.sh <owner-tag> <command...>
# Runs the command while holding the heavy-run lock in one of two classes:
#   exclusive (the default)  alone on the machine.
#   shared                   in one of K slots, beside other shared jobs. K is BENCHLOCK_SHARED,
#                            default one per six cores, between 1 and 3 (see lib.sh).
# The rule: every heavy run that measures no time is shared (gates, test suites, generation, the
# alpha-parsers check); only timed runs (benchmarks, A/B runs, profiles) are exclusive.
# An exclusive job starts only when no holder of either class runs, and no test slot (testslot.sh) has
# a live holder; inside a test slot job (SXIR_IN_TESTSLOT) it would wait for its own slot, so it
# refuses at once with status 2. The queue is first come, first
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
# The power guard: a timed (exclusive) job runs on AC power with at least BENCHLOCK_MIN_CHARGE percent
# of charge (default 50, on AC too; see sxir_power in lib.sh), for the whole run. It checks the power
# before it queues and again once admitted, and refuses to start on battery or under the floor: it
# says why on stderr, logs it to benchlock-power.log and exits 75. While the command runs it reads
# the power every BENCHLOCK_POWER_POLL seconds (default 30) into power/<tag>.<pid>.log, whose path
# and first reading it passes to the command in SXIR_POWER_LOG and SXIR_POWER_START. After the
# command it reads the power once more: when any reading was on battery or under the floor, the run
# is invalid: it says so on stderr and in benchlock-power.log, keeps the readings, and exits 75
# whatever the command's own status, which the message gives. On a Mac, a reading whose probe
# failed or named neither AC nor battery is refused like battery. A machine with no probe (not a
# Mac, probe=none) runs unchecked: benchlock-power.log notes it, and its readings are kept. Shared
# jobs measure no time and are not checked.
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
if [ "$class" = exclusive ] && [ -n "${SXIR_IN_TESTSLOT:-}" ]; then
  echo "benchlock.sh: an exclusive run cannot start inside a testslot.sh job: it would wait for its own test slot to drain. Run it outside the slot; a shared run (BENCHLOCK_CLASS=shared) may run here." >&2
  exit 2
fi
k=$(sxir_shared_slots)
tag=$1; shift
P=$D/power
plog=$D/benchlock-power.log
ppoll=${BENCHLOCK_POWER_POLL:-30}
power_now=
# power_ok WHEN reads the power for an exclusive job and sets power_now; when the reading does not
# allow a timed run it says why, logs it and returns 1. A machine with no probe is logged and allowed.
power_ok() {
  local why
  power_now=$(sxir_power)
  why=$(sxir_power_refusal "$power_now" "$floor")
  if [ -n "$why" ]; then
    echo "benchlock.sh: $tag not started: a timed job needs AC power and at least $floor% charge (BENCHLOCK_MIN_CHARGE), and the machine is $why ($power_now)" >&2
    printf '%s refused (%s): %s.%s is %s: %s\n' "$(date +%FT%T)" "$1" "$tag" "$$" "$why" "$power_now" >> "$plog"
    return 1
  fi
  case $power_now in
    source=unknown*) printf '%s unknown (%s): %s.%s runs without a power check: %s\n' "$(date +%FT%T)" "$1" "$tag" "$$" "$power_now" >> "$plog" ;;
  esac
}
command -v perl >/dev/null 2>&1 || { echo "benchlock.sh: perl is required for the ticket and is not installed" >&2; exit 127; }
if [ "$class" = exclusive ]; then
  floor=$(sxir_power_floor)
  case $ppoll in '' | *[!0-9]* | 0) echo "benchlock.sh: BENCHLOCK_POWER_POLL must be a positive number of seconds, not '$ppoll'" >&2; exit 2 ;; esac
  power_ok queued || exit 75
fi
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
# The power can change while the job waits: check it again before the command starts.
if [ "$class" = exclusive ] && ! power_ok admitted; then
  rm -f "$rec"
  with_flock release || true
  exit 75
fi
term=
child=
sampler=
# stop_sampler ends the power sampler and the sleep it waits in.
stop_sampler() {
  [ -n "$sampler" ] || return 0
  pkill -TERM -P "$sampler" 2>/dev/null || true
  kill -TERM "$sampler" 2>/dev/null || true
  wait "$sampler" 2>/dev/null || true
  sampler=
}
trap stop_sampler EXIT
# shellcheck disable=SC2329  # runs from the trap
on_term() {
  term=1
  if [ -n "$child" ]; then kill -TERM -- "-$child" 2>/dev/null || true; fi
}
trap on_term TERM INT HUP
if [ "$class" = exclusive ]; then
  export SXIR_BENCHLOCK="$tag $$"
  mkdir -p "$P"
  powerlog=$P/$tag.$$.log
  printf '%s %s\n' "$(date +%FT%T)" "$power_now" > "$powerlog"
  export SXIR_POWER_LOG=$powerlog SXIR_POWER_START=$power_now
  # The sampler writes nothing to this script's output, so that a caller reading it is not held open.
  # kill -0 $$ ends the sampler when the wrapper dies without running its EXIT trap (SIGKILL).
  ( while kill -0 $$ 2>/dev/null && sleep "$ppoll"; do printf '%s %s\n' "$(date +%FT%T)" "$(sxir_power)" >> "$powerlog"; done ) < /dev/null > /dev/null 2>&1 &
  sampler=$!
else
  unset SXIR_BENCHLOCK SXIR_POWER_LOG SXIR_POWER_START
fi
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
if [ "$class" = exclusive ]; then
  stop_sampler
  printf '%s %s\n' "$(date +%FT%T)" "$(sxir_power)" >> "$powerlog"
  bad='' unchecked=''
  while read -r when reading; do
    why=$(sxir_power_refusal "$reading" "$floor")
    if [ -n "$why" ]; then bad="$why at $when"; break; fi
    case $reading in source=unknown*) unchecked=${unchecked:-$when} ;; esac
  done < "$powerlog"
  if [ -n "$bad" ]; then
    echo "benchlock.sh: $tag ran on power that does not allow timing ($bad): its timings are invalid; the command exited $rc; readings in $powerlog" >&2
    printf '%s invalid: %s.%s ran %s; the command exited %s; readings in %s\n' "$(date +%FT%T)" "$tag" "$$" "$bad" "$rc" "$powerlog" >> "$plog"
    exit 75
  fi
  if [ -n "$unchecked" ]; then
    # No probe on this machine: the run stays allowed, and its readings stay as the trace.
    printf '%s unchecked: %s.%s ran without a power check from %s; readings in %s\n' "$(date +%FT%T)" "$tag" "$$" "$unchecked" "$powerlog" >> "$plog"
  else
    rm -f "$powerlog"
  fi
fi
exit "$rc"
