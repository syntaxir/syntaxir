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
# The command runs in a process group of its own (job control), started before the script can
# be signalled again: TERM, INT or HUP, even one that arrives while the command starts, signals
# the group, waits for it, frees the slot and exits 143. The command runs as a background job,
# so its standard input is /dev/null, not the terminal: a queued command must not read it.
# State: $ZP_AGENT_STATE/slots.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
D=$zp_state/slots
n=${TESTSLOT_N:-4}
[ $# -gt 0 ] || { echo "usage: testslot.sh <cmd...>" >&2; exit 2; }
command -v perl >/dev/null 2>&1 || { echo "testslot.sh: perl is required for the claim lock and is not installed" >&2; exit 127; }
mkdir -p "$D"
mine=
child=
term=
me="$$ $(zp_lstart $$)"
# shellcheck disable=SC2329  # runs from the traps
release() {
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
  [ -z "${2:-}" ] || [ "$(zp_lstart "$1")" = "$2" ]
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
  exec 9>&-
  [ -n "$mine" ]
}
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
