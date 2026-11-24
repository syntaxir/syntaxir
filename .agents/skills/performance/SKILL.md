---
name: performance
description: Maintain SyntaxIR's optimization method and reproducible performance evidence.
---

# Performance Evidence

- Follow `docs/performance.md` and use `docs/optimizations/000-template.md` for
  focused mechanism records. State operation, workload, backend, position unit,
  toolchain, configuration and hardware.
- Correctness comes before timing. Pin revisions and inputs; serialize runs and
  use paired or alternating baseline/candidate samples. Report noise and
  unmeasured paths; full snapshots describe combined performance, focused
  measurements support attribution.
- Keep focused raw logs outside Git unless deliberately versioned as fixtures.
  Preserve provenance and update the optimization index with applicability,
  evidence and measured tradeoffs. Separate plans, local results and production
  observations; avoid incomparable claims.
- Run optimization and timing work only under current owner authorization.
  Serialize benchmark runs and follow `docs/performance.md`; keep claims limited
  to measured paths and retain raw evidence outside Git unless deliberately
  versioned.
- Landing rule for a performance-sensitive change: at least 20 interleaved rounds against
  an A/A copy, with the geomean within the A/A spread. Record `uptime` with each run and
  discard runs whose A/A spread exceeds 1.5%. `go run ./bench/gate` does this: it takes
  the benchmark lock, runs the base, candidate and base again per round, and prints the
  verdicts; `docs/performance.md` (Benchmark Gates) describes the rule. An exclusive
  benchlock job also holds back new `testslot.sh` jobs and waits for the running ones, so
  every `go test`, `go vet` and `go build` of more than a few seconds goes through
  `testslot.sh` (the gate's own builds do). Exclusive jobs are rare and short, so slot jobs may wait on them and a steady stream of them could starve the slots: accepted. An exclusive benchlock job refuses to start inside a slot job. A gate whose load is above `-max-load` at either end is invalid.
- New structural or feature work lands reasonably optimized. Profile it before landing and
  fix egregious regressions (tens of percent on any path, size blow-ups) and cheap wins in
  the same unit. File the remaining tuning, with the measured gap, for the performance
  milestone.
- "No slowdown" gates the default path, not every trade-off. When a change trades speed
  for memory or size, consider offering it as a selectable optimization profile or flag at
  zero cost on the default path, instead of dropping it.
