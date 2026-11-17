#!/usr/bin/env bash
# Usage: testslot.sh <cmd...>
# Counting semaphore (TESTSLOT_N slots, default 4) for targeted `go test` runs. Each slot is a
# directory slots/<i> holding a `pid` file; a slot whose holder PID is dead is reclaimed.
# Claiming runs under a short global mutex (slots/.mutex, holding the claimer's pid) so that
# reclaiming a dead holder's slot cannot race with a new holder taking it. A mutex whose pid is
# dead, or that has no pid and is older than a minute (a holder of the earlier zsh version),
# is reclaimed. Holders release without the mutex (pid file first).
# The command runs in a process group of its own; TERM, INT or HUP to this script signals the
# group, waits for it, and frees the slot.
# State: $ZP_AGENT_STATE/slots.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
D=$zp_state/slots
n=${TESTSLOT_N:-4}
[ $# -gt 0 ] || { echo "usage: testslot.sh <cmd...>" >&2; exit 2; }
zp_queue_note
mkdir -p "$D"
mine=
locked=
child=
# shellcheck disable=SC2329  # runs from the traps
release() {
  if [ -n "$mine" ]; then rm -f "$mine/pid"; rmdir "$mine" 2>/dev/null || true; mine=; fi
  if [ -n "$locked" ]; then rm -f "$D/.mutex/pid"; rmdir "$D/.mutex" 2>/dev/null || true; locked=; fi
}
# shellcheck disable=SC2329  # runs from the traps
stop() {
  if [ -n "$child" ]; then kill -TERM -- "-$child" 2>/dev/null || true; wait "$child" 2>/dev/null || true; fi
  release
  exit 143
}
trap stop TERM INT HUP
trap release EXIT
while :; do
  if mkdir "$D/.mutex" 2>/dev/null; then
    locked=1
    echo $$ > "$D/.mutex/pid"
    i=1
    while [ "$i" -le "$n" ]; do
      s=$D/$i
      if mkdir "$s" 2>/dev/null; then echo $$ > "$s/pid"; mine=$s; break; fi
      p=$(cat "$s/pid" 2>/dev/null || true)
      if [ -n "$p" ] && ! kill -0 "$p" 2>/dev/null; then
        rm -f "$s/pid"; rmdir "$s" 2>/dev/null || true
        if mkdir "$s" 2>/dev/null; then echo $$ > "$s/pid"; mine=$s; break; fi
      fi
      i=$((i + 1))
    done
    rm -f "$D/.mutex/pid"; rmdir "$D/.mutex"; locked=
    [ -n "$mine" ] && break
  else
    mp=$(cat "$D/.mutex/pid" 2>/dev/null || true)
    if [ -n "$mp" ]; then
      if ! kill -0 "$mp" 2>/dev/null; then rm -f "$D/.mutex/pid"; rmdir "$D/.mutex" 2>/dev/null || true; continue; fi
    elif [ -d "$D/.mutex" ] && [ -n "$(find "$D/.mutex" -maxdepth 0 -mmin +1 2>/dev/null)" ]; then
      rmdir "$D/.mutex" 2>/dev/null || true; continue
    fi
  fi
  sleep 1
done
perl -e 'setpgrp(0,0); exec @ARGV or die "exec: $!\n"' -- "$@" &
child=$!
rc=0
wait "$child" || rc=$?
while kill -0 "$child" 2>/dev/null; do wait "$child" || rc=$?; done
child=
exit "$rc"
