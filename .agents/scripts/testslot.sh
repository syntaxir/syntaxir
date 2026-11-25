#!/usr/bin/env bash
# Usage: testslot.sh <cmd...>
# Counting semaphore (TESTSLOT_N slots, default 4) for targeted `go test` runs. Each slot is a
# directory slots/<i> holding a `pid` file; a slot whose holder is dead is reclaimed. The holder
# is identified by its pid and its start time (slots/<i>.start), so a reused pid is not taken
# for the holder. An old holder, which writes only the pid, is judged by the pid alone.
#
# Claiming is serialized between these scripts by an flock on slots/.lock, which the kernel
# drops when its holder dies, so reclaiming a dead holder's slot or a dead mutex is one step
# that no second waiter can interleave with. The earlier zsh holders serialize on the directory
# slots/.mutex; a claimer also takes that directory, writing its pid and start time to
# .mutex/owner. A .mutex of a dead owner, or without owner and older than a minute (an old
# holder killed inside it), is removed.
#
# Admission is first come, first served, ranked as benchlock.sh ranks its waiters: a waiter takes a
# ticket seq from slots/.seq under flock and, in the same step, writes its record
# slots/queue/<seq, 12 digits>.<pid> ("<pid> <start time>"), so the records sort in arrival order.
# Under the claim flock a waiter claims a slot only when the lowest record of a live waiter is its
# own; records of dead waiters (pid gone, or start time differing) are removed on the way. As a
# backstop, a waiter that sees a free slot for TESTSLOT_STALL seconds (default 300) while it is not
# first logs that to testslot-stalls.log and claims regardless, so the queue always progresses.
# Compatibility: holders and waiters of earlier versions do not queue. A holder is respected as
# before, and a live earlier waiter that polls just as a slot frees may still take it ahead of the
# queue (it is not ranked; only waiters of this version are served in order). Earlier versions
# ignore slots/queue and slots/.seq, and read slots/<i> only.
#
# The command runs in a process group of its own (job control), started before the script can
# be signalled again: TERM, INT or HUP, even one that arrives while the command starts, signals
# the group, waits for it, frees the slot and exits 143. The command runs as a background job,
# so its standard input is /dev/null, not the terminal: a queued command must not read it.
# State: $SXIR_AGENT_STATE/slots, testslot-stalls.log.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
D=$sxir_state/slots
n=${TESTSLOT_N:-4}
[ $# -gt 0 ] || { echo "usage: testslot.sh <cmd...>" >&2; exit 2; }
command -v perl >/dev/null 2>&1 || { echo "testslot.sh: perl is required for the claim lock and is not installed" >&2; exit 127; }
Q=$D/queue
stall=${TESTSLOT_STALL:-300}
mkdir -p "$D" "$Q"
mine=
rec=
child=
term=
me="$$ $(sxir_lstart $$)"
# ticket takes the next seq and writes the waiter's record in one step under the flock on slots/.seq,
# so a later ticket never exists before the earlier record does. It prints the record's path.
ticket() {
  local name
  name=$(perl -e 'use Fcntl qw(:flock); my ($sf, $qd, $pid, $rec) = @ARGV; open(my $f, "+>>", $sf) or die "seq: $!\n"; flock($f, LOCK_EX) or die; seek($f, 0, 0); my $n = <$f> // 0; $n = int($n) + 1; truncate($f, 0); print $f "$n\n"; my $name = sprintf("%012d.%d", $n, $pid); open(my $r, ">", "$qd/.new.$name") or die "record: $!\n"; print $r "$rec\n"; close $r; rename("$qd/.new.$name", "$qd/$name") or die "record: $!\n"; close $f; print $name' "$D/.seq" "$Q" "$$" "$me") || { echo "testslot.sh: cannot allocate a ticket" >&2; exit 125; }
  rec=$Q/$name
}
# shellcheck disable=SC2329  # runs from the traps
release() {
  [ -z "$rec" ] || rm -f "$rec"
  if [ -n "$mine" ]; then
    rm -f "$mine/pid" "$mine.start"; rmdir "$mine" 2>/dev/null || true; mine=
  fi
}
trap release EXIT
# shellcheck disable=SC2329  # runs from the trap
on_term() {
  term=1
  if [ -n "$child" ]; then kill -TERM -- "-$child" 2>/dev/null || true; fi
}
trap on_term TERM INT HUP

# holder_alive PID START: PID is the holder. START is empty for an old holder.
holder_alive() {
  kill -0 "$1" 2>/dev/null || return 1
  [ -z "${2:-}" ] || [ "$(sxir_lstart "$1")" = "$2" ]
}
# slot_dead DIR: the holder of the slot is gone. A claimer writes its pid right after it makes
# the slot, inside the claim, so a slot without a pid that is older than a minute was left by a
# claimer that was killed between the two.
slot_dead() {
  local p st rec
  p=$(cat "$1/pid" 2>/dev/null || true)
  if [ -z "$p" ]; then
    [ -n "$(find "$1" -maxdepth 0 -mmin +1 2>/dev/null)" ]
    return
  fi
  st=
  rec=$(cat "$1.start" 2>/dev/null || true)
  # The start file counts only when it was written by that pid.
  if [ "${rec%% *}" = "$p" ] && [ "$rec" != "$p" ]; then st=${rec#* }; fi
  ! holder_alive "$p" "$st"
}
# mutex_stale: under the flock, the .mutex directory has no live owner.
mutex_stale() {
  local rec
  rec=$(cat "$D/.mutex/owner" 2>/dev/null || true)
  if [ -n "$rec" ]; then
    if holder_alive "${rec%% *}" "${rec#* }"; then return 1; else return 0; fi
  fi
  [ -n "$(find "$D/.mutex" -maxdepth 0 -mmin +1 2>/dev/null)" ]
}
# first_waiter: under the claim flock, this job's record is the lowest of the live waiters. Records
# of dead waiters are removed. A record that has gone is written again under its name.
first_waiter() {
  local f p st
  [ -f "$rec" ] || echo "$me" > "$rec"
  for f in "$Q"/*; do
    [ -f "$f" ] || continue
    read -r p st < "$f" || true
    if [ -n "${p:-}" ] && holder_alive "$p" "${st:-}"; then
      [ "$f" = "$rec" ]
      return
    fi
    rm -f "$f"
  done
  return 1
}
# slot_free: under the claim flock, some slot is unheld.
slot_free() {
  local i=1
  while [ "$i" -le "$n" ]; do
    if [ ! -d "$D/$i" ] || slot_dead "$D/$i"; then return 0; fi
    i=$((i + 1))
  done
  return 1
}
# try_flock returns 1 when the lock is held by another claimer, and stops the script when it cannot
# be asked for at all (the perl status 2), which a retry would not mend.
try_flock() {
  local rc=0
  perl -e 'use Fcntl ":flock"; open(my $f, ">&=", 9) or exit 2; flock($f, LOCK_EX | LOCK_NB) or exit 1' || rc=$?
  [ "$rc" -le 1 ] || { echo "testslot.sh: cannot take the claim lock (perl status $rc)" >&2; exit 127; }
  return "$rc"
}

# claim_once takes a slot when one is free; it returns 1 when it must be retried.
claim_once() {
  exec 9>> "$D/.lock"
  try_flock || { exec 9>&-; return 1; }
  # Ranking applies to the waiters of this version: a waiter that is not first leaves a free slot
  # to the first, and counts how long it has seen one free (see the backstop in the header).
  if [ "$free" -lt "$stall" ] && ! first_waiter; then
    if slot_free; then free=$((free + 1)); else free=0; fi
    exec 9>&-
    return 1
  fi
  if [ -d "$D/.mutex" ] && mutex_stale; then rm -f "$D/.mutex/owner"; rmdir "$D/.mutex" 2>/dev/null || true; fi
  if mkdir "$D/.mutex" 2>/dev/null; then
    echo "$me" > "$D/.mutex/owner"
    local i=1 s
    while [ "$i" -le "$n" ]; do
      s=$D/$i
      if [ -d "$s" ] && slot_dead "$s"; then rm -f "$s/pid" "$s.start"; rmdir "$s" 2>/dev/null || true; fi
      if mkdir "$s" 2>/dev/null; then echo $$ > "$s/pid"; echo "$me" > "$s.start"; mine=$s; break; fi
      i=$((i + 1))
    done
    rm -f "$D/.mutex/owner"; rmdir "$D/.mutex" 2>/dev/null || true
  fi
  if [ -n "$mine" ]; then
    if [ "$free" -ge "$stall" ]; then
      printf '%s stall: slot free %ss, %s took it ahead of the queue: %s\n' "$(date +%FT%T)" "$free" "$$" \
        "$(cat "$Q"/* 2>/dev/null | tr '\n' '|')" >> "$sxir_state/testslot-stalls.log"
    fi
    rm -f "$rec"; rec=
  fi
  exec 9>&-
  [ -n "$mine" ]
}
free=0
ticket
until claim_once; do
  [ -z "$term" ] || exit 143
  sleep 1
  [ -z "$term" ] || exit 143
done
# Job control gives the command a process group of its own as it is forked, so a signal sent to
# the group from here on cannot miss it. A signal that came earlier is replayed below.
set -m
"$@" < /dev/null &
child=$!
set +m
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
child=
[ -z "$term" ] || exit 143
exit "$rc"
