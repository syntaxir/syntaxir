#!/usr/bin/env bash
# Shared by the scripts in this directory; source it, do not run it.
#
# SXIR_AGENT_STATE is the per-machine coordination directory: one absolute path that every lane on
# the machine exports. It has no default: a default lets a lane that has not exported it run its
# gates through a second, private queue beside the shared one, and a warning would go unseen, as
# run-gate.sh sends the queue's output to the gate log. So a script that uses the state
# stops when the variable is unset or relative. A script that does not use it sets sxir_no_state=1
# before sourcing this file. Layout:
#   landing-train.md   the live queue of candidates
#   wt/<name>/         worktrees
#   runs/ slots/ (with queue/ and .seq) testslot-stalls.log benchlock/ benchlock.lock benchlock.seq benchlock-stalls.log   run queue (written by the scripts only)
#   window window.log window-expired/   the measurement window (benchwindow.sh): while it is open, the
#                      test slots and the shared jobs wait, so that a timed run has the machine alone
#   power/<tag>.<pid>.log benchlock-power.log   the power readings of the running exclusive jobs and the
#                      refusals and invalid runs of the power guard (benchlock.sh)
#   gates/<tag>.log    gate logs

die() { echo "${0##*/}: $*" >&2; exit 1; }

sxir_state=${SXIR_AGENT_STATE:-}
if [ -z "${sxir_no_state:-}" ]; then
  [ -n "$sxir_state" ] || die "SXIR_AGENT_STATE is not set: export the machine's one state directory, the same for every lane (for example \${XDG_STATE_HOME:-\$HOME/.local/state}/sxir-team)"
  case $sxir_state in /*) ;; *) die "SXIR_AGENT_STATE must be an absolute path, not $sxir_state" ;; esac
fi

# sxir_repo_root prints the top level of the checkout that holds this script's directory.
sxir_repo_root() { (cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P); }

# sxir_branch_ok BRANCH: the branch prefixes of AGENTS.md.
sxir_branch_ok() { case $1 in feat/* | fix/* | perf/* | docs/*) return 0 ;; *) return 1 ;; esac; }

# sxir_lstart PID prints the start time of the process, which tells a process from a later one
# that reuses its pid. Empty when the process is gone.
sxir_lstart() { ps -o lstart= -p "$1" 2>/dev/null | sed 's/^ *//; s/ *$//' || true; }

# sxir_shared_slots prints K, the number of benchlock.sh holders of the shared class that may run at
# once: BENCHLOCK_SHARED when set, else one per six cores, between 1 and 3. A shared job is a gate
# run at GOMAXPROCS 4 and nice 15, so K of them leave room for the test slots.
sxir_shared_slots() {
  local k=${BENCHLOCK_SHARED:-} c
  if [ -z "$k" ]; then
    c=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 6)
    k=$((c / 6)); [ "$k" -ge 1 ] || k=1; [ "$k" -le 3 ] || k=3
  fi
  case $k in '' | *[!0-9]* | 0) die "BENCHLOCK_SHARED must be a positive integer, not '$k'" ;; esac
  echo "$k"
}

# sxir_bench_live PID TAG: PID still runs benchlock.sh (of any version) with TAG. PIDs are reused, so
# a live PID alone does not make a holder or a waiter live.
sxir_bench_live() { ps -o command= -p "$1" 2>/dev/null | grep -qF "benchlock.sh $2 "; }

# sxir_run_fields RECORD prints "state start seq class" of a benchlock.sh record runs/<tag>.<pid>,
# read before its cmd=, which may hold any text. seq is the wrapper pid for records written before
# seq existed, and class is exclusive for records written before classes existed. Prints nothing
# when the record is gone.
sxir_run_fields() {
  awk -v pid="${1##*.}" 'NR == 1 {
    sub(/ cmd=.*/, ""); st = ""; t = ""; s = pid; c = "exclusive"
    for (i = 1; i <= NF; i++) {
      split($i, kv, "=")
      if (kv[1] == "state") st = kv[2]; else if (kv[1] == "start") t = kv[2]
      else if (kv[1] == "seq" && kv[2] != "") s = kv[2]; else if (kv[1] == "class") c = kv[2]
    }
    printf "%s %s %012d %s\n", st, t, s, c
  }' "$1" 2>/dev/null || true
}

# sxir_holder_alive PID START: PID runs and started at START (ps lstart). START is empty for a holder
# recorded by its pid alone, which is then judged by the pid.
sxir_holder_alive() {
  kill -0 "$1" 2>/dev/null || return 1
  [ -z "${2:-}" ] || [ "$(sxir_lstart "$1")" = "$2" ]
}

# sxir_slot_dead DIR: the holder of the test slot DIR (slots/<i>, with slots/<i>.start) is gone. A
# claimer writes its pid right after it makes the slot, inside the claim, so a slot without a pid
# that is older than a minute was left by a claimer that was killed between the two.
sxir_slot_dead() {
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
  ! sxir_holder_alive "$p" "$st"
}

# sxir_slots_held prints the number of test slots (testslot.sh) whose holder is alive.
sxir_slots_held() {
  local s n=0
  for s in "$sxir_state"/slots/*; do
    [ -d "$s" ] || continue
    case ${s##*/} in *[!0-9]*) continue ;; esac
    sxir_slot_dead "$s" || n=$((n + 1))
  done
  echo "$n"
}

# sxir_window_field KEY FILE prints the value of KEY in a window record: one line of key=value fields,
# reason= last, whose value is free text and is not searched.
sxir_window_field() {
  awk -v k="$1" 'NR == 1 { for (i = 1; i <= NF; i++) { if ($i ~ /^reason=/) exit; if (index($i, k "=") == 1) { print substr($i, length(k) + 2); exit } } }' "$2" 2>/dev/null || true
}

# sxir_window_live: a measurement window (benchwindow.sh) is open: the record window exists and its
# until= is in the future. A window past its until= no longer counts, so a window that its owner
# forgot to close cannot stop the queue for good: the first script that sees it moves it to
# window-expired/ and logs that to window.log.
sxir_window_live() {
  local w=$sxir_state/window lim now
  [ -f "$w" ] || return 1
  lim=$(sxir_window_field until "$w")
  now=$(date +%s)
  if [ -n "$lim" ] && [ "$now" -lt "$lim" ]; then return 0; fi
  mkdir -p "$sxir_state/window-expired"
  if mv "$w" "$sxir_state/window-expired/window.$now.$$" 2>/dev/null; then
    printf '%s expired (until=%s), ignored by %s: %s\n' "$(date +%FT%T)" "${lim:-none}" "${0##*/}" \
      "$(cat "$sxir_state/window-expired/window.$now.$$")" >> "$sxir_state/window.log"
  fi
  return 1
}

# sxir_exclusive_first: an exclusive benchlock.sh job holds the lock, or is the first live job of the
# queue by its rank (start, seq) and waits only for the machine to drain. A new test slot is not
# claimed then, so that the timed job is not starved by a stream of test runs.
sxir_exclusive_first() {
  local o otag opid f ft best='' bestc='' st t s c
  o=$sxir_state/benchlock/owner
  if [ -f "$o" ]; then
    read -r otag opid _ < "$o" 2>/dev/null || true
    if [ -n "${opid:-}" ] && sxir_bench_live "$opid" "$otag"; then return 0; fi
  fi
  for f in "$sxir_state"/runs/*; do
    [ -f "$f" ] || continue
    ft=${f##*/}
    sxir_bench_live "${ft##*.}" "${ft%.*}" || continue
    read -r st t s c <<< "$(sxir_run_fields "$f")"
    [ "$st" = queued ] || continue
    if [ -z "$best" ] || [[ "$t $s" < "$best" ]]; then best="$t $s"; bestc=$c; fi
  done
  [ "$bestc" = exclusive ]
}

# sxir_power prints the power state of the machine as one line "source=ac|battery|unknown charge=N|-
# probe=P". On macOS (the timing host) it reads pmset -g batt (probe=pmset): a machine without a battery
# reports charge=-, and an output that names neither AC nor battery is source=unknown; when pmset fails
# the probe is probe=failed. On any other system there is no probe: source=unknown probe=none.
# The tests replace the probe: SXIR_POWER_CMD is a command whose output is read as that of pmset
# (probe=test), or "none" for a system without a probe; it counts only with SXIR_POWER_TEST=1 beside
# it, so that a stray SXIR_POWER_CMD in an environment cannot replace the real probe.
sxir_power() {
  local out src=unknown charge probe
  if [ -n "${SXIR_POWER_CMD:-}" ] && [ "${SXIR_POWER_TEST:-}" = 1 ]; then
    if [ "$SXIR_POWER_CMD" = none ]; then echo "source=unknown charge=- probe=none"; return 0; fi
    probe='test'
    out=$(bash -c "$SXIR_POWER_CMD" 2>/dev/null) || probe=failed
  elif [ "$(uname -s)" = Darwin ]; then
    probe=pmset
    out=$(pmset -g batt 2>/dev/null) || probe=failed
  else
    echo "source=unknown charge=- probe=none"
    return 0
  fi
  if [ "$probe" = failed ]; then echo "source=unknown charge=- probe=failed"; return 0; fi
  case $out in
    *"drawing from 'AC Power'"*) src=ac ;;
    *"drawing from 'Battery Power'"*) src=battery ;;
  esac
  charge=$(printf '%s\n' "$out" | sed -n 's/.*[[:space:]]\([0-9][0-9]*\)%;.*/\1/p' | head -n 1)
  echo "source=$src charge=${charge:--} probe=$probe"
}

# sxir_power_floor prints the least charge in percent at which a timed job runs: BENCHLOCK_MIN_CHARGE,
# default 50. It applies on AC power too: a machine that charges a low battery under load runs hot.
sxir_power_floor() {
  local f=${BENCHLOCK_MIN_CHARGE:-50}
  case $f in '' | *[!0-9]*) die "BENCHLOCK_MIN_CHARGE must be a percentage from 0 to 100, not '$f'" ;; esac
  [ "$f" -le 100 ] || die "BENCHLOCK_MIN_CHARGE must be a percentage from 0 to 100, not '$f'"
  echo "$f"
}

# sxir_power_refusal READING FLOOR prints why a reading of sxir_power does not allow a timed run (on
# battery, a charge under FLOOR, or a power that a probe could not tell), or nothing when it does. Only a
# system without a probe (probe=none, not a Mac) runs with an unknown source.
sxir_power_refusal() {
  local src charge probe
  src=$(printf '%s\n' "$1" | sed -n 's/.*source=\([a-z]*\).*/\1/p')
  charge=$(printf '%s\n' "$1" | sed -n 's/.*charge=\([0-9-]*\).*/\1/p')
  probe=$(printf '%s\n' "$1" | sed -n 's/.*probe=\([a-z]*\).*/\1/p')
  if [ "$src" != ac ] && [ "$src" != battery ] && [ "$probe" != none ]; then
    if [ "$probe" = failed ]; then echo "on unknown power: the power probe failed"; else echo "on unknown power: the probe named neither AC nor battery"; fi
    return 0
  fi
  if [ "$src" = battery ]; then
    if [ -n "$charge" ] && [ "$charge" != - ]; then echo "on battery ($charge%)"; else echo "on battery"; fi
    return 0
  fi
  case $charge in '' | - | *[!0-9]*) return 0 ;; esac
  if [ "$charge" -lt "$2" ]; then echo "at $charge% charge, under the floor of $2%"; fi
}
