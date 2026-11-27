---
name: performance
description: Maintain SyntaxIR's optimization method and reproducible performance evidence.
---

# Performance Evidence

- Follow `docs/performance.md` and use `docs/optimizations/logs/000-template.md` for
  focused mechanism records. State operation, workload, backend, position unit,
  toolchain, configuration and hardware.
- Correctness comes before timing. Pin revisions and inputs; serialize runs and
  use paired or alternating baseline/candidate samples. Report noise and
  unmeasured paths; full snapshots describe combined performance, focused
  measurements support attribution.
- Keep focused raw logs outside Git unless deliberately versioned as fixtures.
  Preserve provenance and record evidence and measured tradeoffs in the
  record and the mechanism page. Separate plans, local results and production
  observations; avoid incomparable claims.
- Run optimization and timing work only under current owner authorization.
  Serialize benchmark runs and follow `docs/performance.md`; keep claims limited
  to measured paths and retain raw evidence outside Git unless deliberately
  versioned.
- Adding a mechanism: register it in `internal/pass/mechanisms.go` (ID, title,
  page, scope, full and partial backends) and its controls, then run
  `go generate ./internal/optcatalog`. The generator writes the page's facts
  (title, applicability, controls, state, records) and, for a new page, a stub;
  write only the prose sections, never restate a generated fact, and let
  `go test ./internal/optcatalog` reject empty sections (record 080).
- Measure time only by the timed measurement method of `docs/performance.md` (arms,
  rotated rounds, rejection, claims, GC settings, provenance, the exclusive window), which
  `go run ./cmd/timed` of syntaxir/bench applies; do not restate or vary it here. A performance-sensitive
  change lands with `-base` and `-rounds 20` or more, citing only rows whose verdict in
  `results-compare.txt` is faster or slower.
- New structural or feature work lands reasonably optimized. Profile it before landing and
  fix egregious regressions (tens of percent on any path, size blow-ups) and cheap wins in
  the same unit. File the remaining tuning, with the measured gap, for the performance
  milestone.
- "No slowdown" gates the default path, not every trade-off. When a change trades speed
  for memory or size, consider offering it as a selectable optimization profile or flag at
  zero cost on the default path, instead of dropping it.
