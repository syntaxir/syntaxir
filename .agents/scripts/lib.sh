#!/usr/bin/env bash
# Shared by the scripts in this directory; source it, do not run it.
#
# ZP_AGENT_STATE is the per-machine coordination directory. Default:
# ${XDG_STATE_HOME:-$HOME/.local/state}/zp-team. Layout:
#   landing-train.md   the live queue of candidates
#   wt/<name>/         worktrees
#   runs/ slots/ benchlock/ benchlock.lock benchlock.seq benchlock-stalls.log   run queue (written by the scripts only)
#   gates/<tag>.log    gate logs

# zp_state_defaulted is set when the caller did not choose a directory.
zp_state_defaulted=
[ -n "${ZP_AGENT_STATE:-}" ] || zp_state_defaulted=1
zp_state=${ZP_AGENT_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/zp-team}
export ZP_AGENT_STATE=$zp_state

die() { echo "${0##*/}: $*" >&2; exit 1; }

# zp_repo_root prints the top level of the checkout that holds this script's directory.
zp_repo_root() { (cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P); }

# zp_branch_ok BRANCH: the branch prefixes of AGENTS.md.
zp_branch_ok() { case $1 in feat/* | fix/* | perf/* | docs/*) return 0 ;; *) return 1 ;; esac; }

# zp_queue_note warns on stderr when the queue directory is the default and does not exist yet:
# lanes that mean to share the machine's queue must all export the same ZP_AGENT_STATE.
zp_queue_note() {
  if [ -n "$zp_state_defaulted" ] && [ ! -d "$zp_state" ]; then
    echo "${0##*/}: creating the queue directory $zp_state (set ZP_AGENT_STATE so that every lane shares one queue)" >&2
  fi
}

# zp_lstart PID prints the start time of the process, which tells a process from a later one
# that reuses its pid. Empty when the process is gone.
zp_lstart() { ps -o lstart= -p "$1" 2>/dev/null | sed 's/^ *//; s/ *$//' || true; }

# zp_shared_slots prints K, the number of benchlock.sh holders of the shared class that may run at
# once: BENCHLOCK_SHARED when set, else one per six cores, between 1 and 3. A shared job is a gate
# run at GOMAXPROCS 4 and nice 15, so K of them leave room for the test slots.
zp_shared_slots() {
  local k=${BENCHLOCK_SHARED:-} c
  if [ -z "$k" ]; then
    c=$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 6)
    k=$((c / 6)); [ "$k" -ge 1 ] || k=1; [ "$k" -le 3 ] || k=3
  fi
  case $k in '' | *[!0-9]* | 0) die "BENCHLOCK_SHARED must be a positive integer, not '$k'" ;; esac
  echo "$k"
}

# zp_run_fields RECORD prints "state start seq class" of a benchlock.sh record runs/<tag>.<pid>,
# read before its cmd=, which may hold any text. seq is the wrapper pid for records written before
# seq existed, and class is exclusive for records written before classes existed. Prints nothing
# when the record is gone.
zp_run_fields() {
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
