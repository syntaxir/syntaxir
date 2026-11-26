# Machine Load and the Run Queue

Many lanes share one machine, and the CPU is the bottleneck. Unthrottled parallel test runs
once drove the load to 180 on 16 cores and slowed every gate. Timed measurements also need a
quiet machine. So runs fall into four classes.

| Run | How |
|:--|:--|
| Docs, spec, site and skill checks (`cd site && go test -count=1 ./...`, the site build and check), `go vet`, deterministic count or charge probes that measure no time | Directly, throttled: `nice -n 15 env GOMAXPROCS=4 ...`. Never queue them. |
| Targeted package tests while developing | Throttled through the test slots: `.agents/scripts/testslot.sh nice -n 15 env GOMAXPROCS=4 go test -p 2 -run '<tests>' -short <pkgs>` |
| Whole packages of the large test suites (engine, codegen and complexity tests), the root suite, `-tags sxir_long`, `go generate ./internal/backend/... ./internal/runtime/state/sharedsrc ./bench ./internal/frontend/parser ./spec/language/...`, `hack/test-alpha-parsers.sh`, `hack/test-reference-tags.sh`, the engine and full gates, and any other heavy run that measures no time | Only through the queue, in its shared class: `BENCHLOCK_CLASS=shared .agents/scripts/benchlock.sh <tag> zsh -f -c 'cd <worktree> && ...'` (`run-gate.sh` and `alpha-check.sh` do this) |
| Benchmarks, A/B runs, profiles and any other timed run | Only in a measurement window that the leader declares with the owner, through the queue, exclusive (the default): `.agents/scripts/benchlock.sh <tag> zsh -f -c 'cd <worktree> && ...'` |

Heavy test runs, including the root `go test ./...`, go only through the queue. Docs and site
checks and count probes run directly, throttled.

## The queue

- `.agents/scripts/benchlock.sh <tag> <command...>` waits for the heavy-run lock and then runs
  the command in its own process group. Its record is `runs/<tag>.<pid>`; waiters of both
  classes are served in (`start=`, `seq=`) order. An exclusive job (the default) runs alone. A
  shared job (`BENCHLOCK_CLASS=shared`) runs beside up to K-1 other shared jobs and never beside
  an exclusive one; K is `BENCHLOCK_SHARED`, default one per six cores between 1 and 3. A queued
  exclusive job holds back the shared jobs queued after it, so it is not starved. The rule:
  every heavy run that measures no time is shared; only timed runs (benchmarks, A/B runs,
  profiles) are exclusive. A
  line in `benchlock-stalls.log` is a queue bug: report it.
- `.agents/scripts/benchwindow.sh open [-m MINUTES] <reason>` declares a measurement window
  (default 120 minutes); `close` ends it and `status` shows it. While it is open, new test
  slot claims and shared jobs wait, and the running ones finish. An exclusive job starts only
  when no test slot is held, and while it holds the lock or is first in the queue, new test
  claims wait too. A window past its end stops counting by itself (logged in `window.log`).
  Never run `testslot.sh` inside a benchlock.sh job or the other way round: the job would
  wait for itself. While an exclusive job is first or holds the lock, the stall backstop
  cannot fire either, so an exclusive benchlock.sh run started from inside a test slot
  deadlocks for good. A `testslot.sh` copy older than the window ignores windows: claim
  slots only through the current script of main.
- Timed runs need mains power. An exclusive job refuses to start, with exit 75 and the
  reason, when the machine is on battery or its charge is under `BENCHLOCK_MIN_CHARGE`
  (default 50, on AC too: charging a low battery under load heats the machine). It checks
  when it queues and again when admitted, reads the power every 30 seconds while it runs
  (`power/<tag>.<pid>.log`, passed to the command as `SXIR_POWER_LOG`), and exits 75 after
  the command when any reading failed: its timings are invalid, whatever the command
  returned. Refusals and invalid runs go to `benchlock-power.log`. Shared jobs are not
  checked. On a Mac, a failed `pmset` or a reading naming neither AC nor battery is refused
  like battery. A machine with no probe (not a Mac) runs unchecked: the log notes it and
  keeps the readings. Plug the machine in before a window; never lower the floor to get a
  run through. `SXIR_POWER_CMD` replaces the probe only together with `SXIR_POWER_TEST=1`;
  both are for the script tests, never for real runs.
- `.agents/scripts/queue-status.sh` shows the window, the queue and the power, and exits 1
  when it is stuck (jobs queued, none running), a stall was logged, or an exclusive job is
  queued or running on power that does not allow timing. Inspect at once when it does.
- `.agents/scripts/run-gate.sh LEVEL...` picks the run class of a `hack/gate.sh` level for you.
- Use `zsh -f` for queued commands. A login shell profile can reset PATH and drop `node`.
  The TypeScript parity tests then fail with "Node is required", which is the environment,
  not the change.
- Stop your own job with `.agents/scripts/benchstop.sh <tag>`. It signals only the recorded group. Never kill
  a process you did not start. Before any manual kill, confirm in the same command that the
  PID is still yours (`ps -o command= -p <pid>`), because PIDs are reused.
- Priority: landing gates and checks that unblock another lane go before measurement
  probes and exploratory runs. The leader may move a probe's record later by editing its
  `start=`; do not move it back. The integrator sets the `start=` of a train record ahead of
  every non-train job; only train records are moved forward. Keep probe jobs few and
  batched.
- Never idle while your job waits. Continue the next independent step of your lane in a
  separate worktree, never in the worktree a queued or running job tests. If your lane has no
  independent step, tell the leader in one line.

## Measurements

- Follow the [performance skill](../../performance/SKILL.md) for the A/A landing rule, the
  run records and the measurement method.

## Caches and disk

Tests that build generated programs make a unique program per test, so their build cache
entries are never reused. A shared Go build cache once grew past 1 TB and filled the disk.
Every test package that builds generated programs calls `codegentest.SetGoCache` in its
`TestMain`. If the disk fills, find the producer. Then clean the caches through the queue:
build and test caches may always be deleted.
