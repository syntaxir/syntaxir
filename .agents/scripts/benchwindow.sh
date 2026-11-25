#!/usr/bin/env bash
# Usage: benchwindow.sh open [-m MINUTES] <reason...> | close | status
# Declares the measurement window of the machine: the time in which timed runs (benchmarks, A/B
# runs, profiles) are made, with the rest of the queue paused.
#   open     opens a window for MINUTES (default 120) and prints its record. While it is open,
#            testslot.sh claims and shared benchlock.sh jobs wait (the ones running finish), and an
#            exclusive benchlock.sh job starts once the running ones have drained. Fails when a
#            window is already open.
#   close    closes the window; the paused claims and jobs go on.
#   status   prints the window and exits 0 when one is open, 1 otherwise.
# A window past its end no longer counts: the first script that sees it moves it to window-expired/
# and logs that, so a window that is not closed cannot stop the queue for good. Every open, close and
# expiry is logged to window.log. The record is $SXIR_AGENT_STATE/window, one line:
#   id=<id> owner=<owner> start=<epoch> until=<epoch> reason=<text>
# Scripts of earlier checkouts do not know the window: lanes must use the current scripts of main.
set -euo pipefail
# shellcheck source=lib.sh
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
D=$sxir_state
W=$D/window
usage() { sed -n '2,/^set /p' "$0" | sed '$d;s/^# \{0,1\}//' >&2; exit 2; }
[ $# -ge 1 ] || usage
cmd=$1; shift
mkdir -p "$D"
log() { printf '%s %s\n' "$(date +%FT%T)" "$*" >> "$D/window.log"; }
case $cmd in
  open)
    minutes=120
    if [ "${1:-}" = -m ]; then
      [ $# -ge 2 ] || usage
      minutes=$2; shift 2
    fi
    case $minutes in '' | *[!0-9]* | 0) die "-m must be a positive number of minutes, not '$minutes'" ;; esac
    [ $# -ge 1 ] || die "open needs a reason"
    if sxir_window_live; then die "a window is already open: $(cat "$W")"; fi
    now=$(date +%s)
    rec="id=$(date +%Y%m%dT%H%M%S)-$$ owner=${SXIR_WINDOW_OWNER:-${USER:-unknown}} start=$now until=$((now + minutes * 60)) reason=$*"
    tmp=$W.$$
    printf '%s\n' "$rec" > "$tmp"
    # A link fails when a window appeared meanwhile, so two openers cannot both succeed.
    if ! ln "$tmp" "$W" 2>/dev/null; then rm -f "$tmp"; die "a window is already open: $(cat "$W" 2>/dev/null || true)"; fi
    rm -f "$tmp"
    log "open $rec"
    echo "window open until $(date -r $((now + minutes * 60)) +%FT%T 2>/dev/null || date -d @$((now + minutes * 60)) +%FT%T): $rec"
    ;;
  close)
    if [ -f "$W" ]; then
      rec=$(cat "$W")
      rm -f "$W"
      log "close $rec"
      echo "window closed: $rec"
    else
      echo "no window is open"
    fi
    ;;
  status)
    if sxir_window_live; then
      echo "window: open, $(cat "$W")"
      echo "test slots held: $(sxir_slots_held)"
      exit 0
    fi
    echo "window: none"
    exit 1
    ;;
  *) usage ;;
esac
