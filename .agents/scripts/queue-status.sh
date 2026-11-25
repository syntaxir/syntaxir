#!/usr/bin/env bash
# Usage: queue-status.sh
# Prints the run queue: the lock (free, its exclusive owner, or its shared holders out of K), every
# record of runs/ in rank order (start, seq) with its class, the stall log, a STALE line for each
# holder or record whose benchlock.sh is gone, and a STUCK line when jobs are queued but none runs.
# Only records of live wrappers count as queued or running. A record without class= (an earlier
# benchlock.sh) is exclusive. Earlier versions cannot take back the slot of a dead shared holder,
# so with only their waiters queued the queue stays stuck until a current benchlock.sh runs.
# The test slots follow: the slots held, and the testslot.sh waiters in rank order (slots/queue,
# first served first; a record whose waiter is gone is shown as gone and is not a failure, the next
# claim removes it), and testslot-stalls.log, which counts as stalls do.
# Exit status 1 when stuck, when anything is stale or when the stall log is not empty, so that a
# poll can act on it.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
D=$sxir_state
echo "state: $D"
L=$D/benchlock
stale=()
if [ -f "$L/owner" ]; then
  read -r otag opid _ < "$L/owner" || true
  echo "lock: exclusive, owner ${otag:-} ${opid:-}"
  if [ -n "${opid:-}" ] && ! sxir_bench_live "$opid" "$otag"; then
    stale+=("exclusive holder $otag $opid is gone; it holds benchlock/")
  fi
elif [ -d "$L/shared" ]; then
  holders=()
  for e in "$L/shared"/*; do
    [ -f "$e" ] || continue
    read -r htag hpid _ < "$e" || true
    holders+=("${htag:-} ${hpid:-}")
    if [ -n "${hpid:-}" ] && ! sxir_bench_live "$hpid" "$htag"; then
      stale+=("shared holder $htag $hpid is gone; its slot ${e##*/} holds benchlock/")
    fi
  done
  list=$(printf '%s, ' ${holders[@]+"${holders[@]}"}); list=${list%, }
  echo "lock: shared, ${#holders[@]} of $(sxir_shared_slots) slots: $list"
elif [ -d "$L" ]; then
  echo "lock: held, no owner yet: $(find "$L" -mindepth 1 -maxdepth 1 2>/dev/null | sed 's|.*/||' | tr '\n' ' ')"
else
  echo "lock: free"
fi
for f in "$D"/runs/*; do
  [ -f "$f" ] || continue
  read -r _ start seq class <<< "$(sxir_run_fields "$f")" || continue
  printf '%s %s %s\t%s\n' "$start" "$seq" "$class" "$(cut -c1-200 "$f" 2>/dev/null || true)"
done | sort
q=0; r=0
for f in "$D"/runs/*; do
  [ -f "$f" ] || continue
  read -r st _ <<< "$(sxir_run_fields "$f")" || continue
  ft=${f##*/}
  if ! sxir_bench_live "${ft##*.}" "${ft%.*}"; then
    stale+=("$st record $ft: its benchlock.sh is gone")
    continue
  fi
  case $st in queued) q=$((q + 1)) ;; running) r=$((r + 1)) ;; esac
done
held=0
for e in "$D"/slots/[0-9]*; do [ -d "$e" ] && held=$((held + 1)); done
echo "test slots: $held held"
for f in "$D"/slots/queue/[0-9]*; do
  [ -f "$f" ] || continue
  read -r wpid wstart < "$f" || true
  if [ -n "${wpid:-}" ] && kill -0 "$wpid" 2>/dev/null && [ "$(sxir_lstart "$wpid")" = "${wstart:-}" ]; then w=waiting; else w=gone; fi
  echo "  slot waiter ${f##*/} pid ${wpid:-}: $w"
done
rc=0
if [ -s "$D/testslot-stalls.log" ]; then echo "TESTSLOT STALLS (a queue bug, report it):"; cat "$D/testslot-stalls.log"; rc=1; fi
if [ -s "$D/benchlock-stalls.log" ]; then echo "STALLS (a queue bug, report it):"; cat "$D/benchlock-stalls.log"; rc=1; fi
for m in ${stale[@]+"${stale[@]}"}; do echo "STALE: $m"; rc=1; done
if [ "${#stale[@]}" -gt 0 ]; then echo "(any current benchlock.sh run takes back what is stale)"; fi
echo "queued: $q running: $r"
if [ "$q" -gt 0 ] && [ "$r" -eq 0 ]; then echo "STUCK: jobs are queued and none runs"; rc=1; fi
exit "$rc"
