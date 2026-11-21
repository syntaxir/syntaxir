#!/usr/bin/env bash
# Shared by the scripts in this directory; source it, do not run it.
#
# SXIR_AGENT_STATE is the per-machine coordination directory. Default:
# ${XDG_STATE_HOME:-$HOME/.local/state}/sxir-team. Layout:
#   landing-train.md   the live queue of candidates
#   wt/<name>/         worktrees
#   runs/ slots/ benchlock/ benchlock.seq benchlock-stalls.log   run queue (written by the scripts only)
#   gates/<tag>.log    gate logs

# sxir_state_defaulted is set when the caller did not choose a directory.
sxir_state_defaulted=
[ -n "${SXIR_AGENT_STATE:-}" ] || sxir_state_defaulted=1
sxir_state=${SXIR_AGENT_STATE:-${XDG_STATE_HOME:-$HOME/.local/state}/sxir-team}
export SXIR_AGENT_STATE=$sxir_state

die() { echo "${0##*/}: $*" >&2; exit 1; }

# sxir_repo_root prints the top level of the checkout that holds this script's directory.
sxir_repo_root() { (cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd -P); }

# sxir_branch_ok BRANCH: the branch prefixes of AGENTS.md.
sxir_branch_ok() { case $1 in feat/* | fix/* | perf/* | docs/*) return 0 ;; *) return 1 ;; esac; }

# sxir_queue_note warns on stderr when the queue directory is the default and does not exist yet:
# lanes that mean to share the machine's queue must all export the same SXIR_AGENT_STATE.
sxir_queue_note() {
  if [ -n "$sxir_state_defaulted" ] && [ ! -d "$sxir_state" ]; then
    echo "${0##*/}: creating the queue directory $sxir_state (set SXIR_AGENT_STATE so that every lane shares one queue)" >&2
  fi
}
