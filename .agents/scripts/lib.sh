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
