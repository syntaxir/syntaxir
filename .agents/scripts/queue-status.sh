#!/usr/bin/env bash
# Usage: queue-status.sh
# Prints the run queue: the lock (free, its exclusive owner, or its shared holders out of K), every
# record of runs/ in rank order (start, seq) with its class, the stall log, and a STUCK line when
# jobs are queued but none runs. A record without class= (an earlier benchlock.sh) is exclusive.
# Exit status 1 when stuck or when the stall log is not empty, so that a poll can act on it.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
D=$zp_state
echo "state: $D"
L=$D/benchlock
if [ -f "$L/owner" ]; then
  echo "lock: exclusive, owner $(cat "$L/owner" 2>/dev/null || true)"
elif [ -d "$L/shared" ]; then
  echo "lock: shared, $(find "$L/shared" -mindepth 1 -maxdepth 1 -type f | wc -l | tr -d ' ') of $(zp_shared_slots) slots: $(cat "$L/shared"/* 2>/dev/null | awk '{print $1, $2}' | paste -sd, - | sed 's/,/, /g')"
elif [ -d "$L" ]; then
  echo "lock: held, no owner yet"
else
  echo "lock: free"
fi
for f in "$D"/runs/*; do
  [ -f "$f" ] || continue
  read -r _ start seq class <<< "$(zp_run_fields "$f")" || continue
  printf '%s %s %s\t%s\n' "$start" "$seq" "$class" "$(cut -c1-200 "$f" 2>/dev/null || true)"
done | sort
q=0; r=0
for f in "$D"/runs/*; do
  [ -f "$f" ] || continue
  if grep -q '^state=queued' "$f"; then q=$((q + 1)); fi
  if grep -q '^state=running' "$f"; then r=$((r + 1)); fi
done
rc=0
if [ -s "$D/benchlock-stalls.log" ]; then echo "STALLS (a queue bug, report it):"; cat "$D/benchlock-stalls.log"; rc=1; fi
echo "queued: $q running: $r"
if [ "$q" -gt 0 ] && [ "$r" -eq 0 ]; then echo "STUCK: jobs are queued and none runs"; rc=1; fi
exit "$rc"
