#!/usr/bin/env bash
# Tests the protocol between benchlock.sh and testslot.sh in a state directory of its own:
#   1. a running slot job delays an exclusive job's command;
#   2. a queued or running exclusive job keeps a new slot job from starting;
#   3. a state stamped with a newer protocol stops testslot.sh;
#   4. a shared benchlock job does not keep slot jobs waiting;
#   5. an exclusive benchlock job inside a slot job refuses at once, a shared one runs.
# Usage: .agents/scripts/test-lock-protocol.sh   (takes about 25 seconds, uses no CPU to speak of)
set -uo pipefail
here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)
W=$(mktemp -d "${TMPDIR:-/tmp}/lockproto.XXXXXX")
pids=()
trap 'for p in "${pids[@]:-}"; do [ -z "$p" ] || kill "$p" 2>/dev/null; done; rm -rf "$W"' EXIT
export SXIR_AGENT_STATE=$W/state BENCHLOCK_POLL=1
mkdir -p "$SXIR_AGENT_STATE"
fail=0
check() { if [ "$2" = ok ]; then echo "ok   $1"; else echo "FAIL $1: $2"; fail=1; fi; }
now() { perl -MTime::HiRes=time -e 'printf "%.2f", time'; }
lt() { perl -e 'exit !($ARGV[0] < $ARGV[1])' "$1" "$2"; }

# 1 and 2: A holds a slot for 5 s; E (exclusive) is queued behind it; B (slot job) comes after E.
"$here/testslot.sh" bash -c "$(declare -f now); now > $W/a.start; sleep 5; now > $W/a.end" &
pids+=($!)
sleep 1.5
"$here/benchlock.sh" t-excl bash -c "$(declare -f now); now > $W/e.start; sleep 1; now > $W/e.end" 2> "$W/e.err" &
pids+=($!)
sleep 1.5
"$here/testslot.sh" bash -c "$(declare -f now); now > $W/b.start" 2> "$W/b.err" &
pids+=($!)
wait "${pids[@]}" 2>/dev/null
for f in a.end e.start e.end b.start; do [ -s "$W/$f" ] || check "files" "$f missing"; done
if [ -s "$W/e.start" ] && [ -s "$W/a.end" ]; then
  lt "$(cat "$W/a.end")" "$(cat "$W/e.start")" && check "the exclusive command starts after the running slot job ends" ok || check "drain" "e.start $(cat "$W/e.start") before a.end $(cat "$W/a.end")"
fi
if [ -s "$W/b.start" ] && [ -s "$W/e.end" ]; then
  lt "$(cat "$W/e.end")" "$(cat "$W/b.start")" && check "a new slot job waits for the queued exclusive job" ok || check "block" "b.start $(cat "$W/b.start") before e.end $(cat "$W/e.end")"
fi
grep -q "waiting" "$W/b.err" && check "the waiting slot job names the benchmark lock" ok || check "message" "no wait message: $(cat "$W/b.err")"
grep -q "waiting for test slot jobs" "$W/e.err" && check "the exclusive job says it waits for slot jobs" ok || check "message" "no drain message: $(cat "$W/e.err")"

# 4: a shared job does not block a slot job.
BENCHLOCK_CLASS=shared "$here/benchlock.sh" t-shared sleep 3 &
pids+=($!)
sleep 1
s=$(now)
"$here/testslot.sh" bash -c "$(declare -f now); now > $W/c.start" 2>/dev/null
lt "$(cat "$W/c.start")" "$(perl -e "print $s + 1.5")" && check "a shared benchlock job leaves the slots alone" ok || check "shared" "slot job waited for a shared job"
wait "${pids[@]}" 2>/dev/null

# 5: an exclusive job inside a slot job would wait for its own slot; it refuses at once.
t0=$(now)
"$here/testslot.sh" "$here/benchlock.sh" t-inner true 2> "$W/in.err"; rc=$?
t1=$(now)
{ [ "$rc" = 2 ] && grep -q "inside a testslot.sh job" "$W/in.err" && lt "$t1" "$(perl -e "print $t0 + 5")"; } && check "an exclusive benchlock job inside a slot job refuses at once" ok || check "self-wait" "status $rc after $(perl -e "print $t1 - $t0")s: $(cat "$W/in.err")"
BENCHLOCK_CLASS=shared "$here/testslot.sh" "$here/benchlock.sh" t-inner true 2> "$W/in2.err" && check "a shared benchlock job inside a slot job runs" ok || check "shared inner" "$(cat "$W/in2.err")"

# 3: a newer protocol stamp stops testslot.sh.
echo 99 > "$SXIR_AGENT_STATE/protocol"
"$here/testslot.sh" true 2> "$W/v.err"; rc=$?
[ "$rc" = 78 ] && check "a state of a newer protocol stops testslot.sh (status 78)" ok || check "stamp" "status $rc: $(cat "$W/v.err")"
exit "$fail"
