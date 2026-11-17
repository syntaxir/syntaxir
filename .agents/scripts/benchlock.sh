#!/usr/bin/env bash
# Usage: benchlock.sh <owner-tag> <command...>
# Runs the command while holding the shared heavy-run lock; a lock whose owner PID is dead is taken over.
# The command runs in a process group of its own, recorded in runs/<tag>.<pid> while it runs, so
# benchstop.sh can stop exactly the groups this script started and nothing else.
#
# The command runs in a process group of its own (job control), created as the command is forked,
# so TERM, INT or HUP to this script, even one that arrives while the command starts, signals
# the group. The command runs as a background job: its standard input is /dev/null.
# State: $ZP_AGENT_STATE (see lib.sh). The layout is the one of the earlier
# zsh version of this script, so jobs queued by either version are ranked together.
#
# Deadlock freedom: every waiter is ranked by the key (start, seq). seq is a counter handed out
# under flock, so no two waiters share a key and exactly one live waiter is first; all waiters
# rank with the same function. As a backstop, a waiter that sees the lock free while it waits for
# BENCHLOCK_STALL seconds (default 300) logs the stall to benchlock-stalls.log and takes the lock,
# so the queue always makes progress even if the ranking were wrong.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
[ $# -ge 2 ] || { echo "usage: benchlock.sh <owner-tag> <command...>" >&2; exit 2; }
D=$zp_state
L=$D/benchlock
R=$D/runs
stall=${BENCHLOCK_STALL:-300}
tag=$1; shift
zp_queue_note
mkdir -p "$R"
seq=$(perl -e 'use Fcntl qw(:flock); open(my $f, "+>>", $ARGV[0]) or die "seq: $!\n"; flock($f, LOCK_EX) or die; seek($f, 0, 0); my $n = <$f> // 0; $n = int($n) + 1; truncate($f, 0); print $f "$n\n"; close $f; print $n' "$D/benchlock.seq") || { echo "benchlock: cannot allocate a ticket" >&2; exit 125; }
rec=$R/$tag.$$
recline="state=queued seq=$seq tag=$tag wrapper=$$ cwd=$PWD start=$(date +%FT%T) cmd=$*"
printf '%s\n' "$recline" > "$rec"
# A wrapper is live only if its PID still runs this script with its own tag: PIDs are reused, and a
# record whose PID now belongs to another process would otherwise block the queue forever.
# Records of wrappers that are no longer live are moved to runs-stale/.
live() { ps -o command= -p "$1" 2>/dev/null | grep -qF "benchlock.sh $2 "; }
# The rank key of a record: its start= (the integrator may edit it for train priority) and then its
# seq= (records written before seq existed use their wrapper PID). Unique per waiter.
key() {
  local t s
  t=$(grep -o 'start=[^ ]*' "$1" || true); t=${t#start=}
  s=$(grep -o 'seq=[0-9]*' "$1" || true); s=${s#seq=}
  [ -n "$s" ] || s=${1##*.}
  printf '%s %012d' "$t" "$s"
}
# First come, first served: only the first live queued wrapper by key may take a free lock.
first() {
  local f best='' bk='' k w ft
  for f in "$R"/*; do
    [ -f "$f" ] || continue
    grep -q '^state=queued' "$f" || continue
    if [ "$f" != "$rec" ]; then
      w=${f##*.}; ft=${f##*/}; ft=${ft%.*}
      live "$w" "$ft" || { mkdir -p "$D/runs-stale"; mv -f "$f" "$D/runs-stale/" 2>/dev/null || true; continue; }
    fi
    k=$(key "$f")
    if [ -z "$best" ] || [[ "$k" < "$bk" ]]; then best=$f; bk=$k; fi
  done
  [ "$best" = "$rec" ]
}
free=0
until { first || [ "$free" -ge "$stall" ]; } && mkdir "$L" 2>/dev/null; do
  [ -f "$rec" ] || printf '%s\n' "$recline" > "$rec"
  if [ -d "$L" ]; then
    free=0
    pid=$(awk '{print $2}' "$L/owner" 2>/dev/null || true)
    otag=$(awk '{print $1}' "$L/owner" 2>/dev/null || true)
    if [ -n "$pid" ] && ! live "$pid" "$otag"; then rm -f "$L/owner"; rmdir "$L" 2>/dev/null || true; continue; fi
  else
    free=$((free + 20))
  fi
  sleep "${BENCHLOCK_POLL:-20}"
done
if [ "$free" -ge "$stall" ]; then
  printf '%s stall: lock free %ss, %s.%s (seq %s) took it; queue: %s\n' "$(date +%FT%T)" "$free" "$tag" "$$" "$seq" \
    "$(cat "$R"/* | cut -c1-80 | tr '\n' '|')" >> "$D/benchlock-stalls.log"
fi
echo "$tag $$" > "$L/owner"
term=
child=
# shellcheck disable=SC2329  # runs from the trap
on_term() {
  term=1
  if [ -n "$child" ]; then kill -TERM -- "-$child" 2>/dev/null || true; fi
}
trap on_term TERM INT HUP
set -m
"$@" &
child=$!
set +m
printf '%s\n' "state=running seq=$seq tag=$tag wrapper=$$ pgid=$child cwd=$PWD start=$(date +%FT%T) cmd=$*" > "$rec"
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
if [ "$(cat "$L/owner" 2>/dev/null || true)" = "$tag $$" ]; then rm -f "$L/owner"; rmdir "$L"; fi
exit "$rc"
