# Typed Go Printing

<!-- former-mechanism G060: The only route by which typed Go rules are printed; a rule that the plan leaves out is a generation error, so no maintained form exists without it -->
<!-- former-mechanism G140: Part of the only typed Go printing route: local cuts of flat rules -->
<!-- former-mechanism G150: Part of the only typed Go printing route: framed bodies of left-recursion leaders -->
<!-- former-mechanism G160: Part of the only typed Go printing route: framed Operators Rule lines -->
<!-- former-mechanism G170: Part of the only typed Go printing route: cuts in framed bodies -->
<!-- former-mechanism G180: Part of the only typed Go printing route: the capture layouts of framed bodies -->
<!-- former-mechanism G190: Part of the only typed Go printing route: framed bodies of rules that recover -->
<!-- former-mechanism G200: Part of the only typed Go printing route: the capture layouts of flat rules -->

A typed Go rule matches and runs its action in the generated code itself, with Go locals for its captures, instead of
dispatching each expression and passing captures through frames of the runtime.

## Design and Invariants

The typed rules are built as Core IR, planned for the typed table and lowered with the rest of the table, as one module,
by the pipeline of the compilation, and printed from Exec IR. There are two forms of a rule:

- **Flat rules.** A rule that the plan accepts as local is a flat function. Its captures are Go locals and its failures
  are labels. Backtracking saves and restores captures, input, environment and recovered errors; a repetition clears
  the locals of its element at each iteration. Actions read the locals directly, and constructors receive the range of
  the rule. Cuts keep their lexical scope of choice and repetition; marks restore input, environment, captures and
  recovered errors, and a cut inside a lookahead disappears when the lookahead resets.
- **Framed bodies.** The bodies of left-recursion leaders and of rules that recover, and the lines of Operators Rules,
  are framed functions, which the library calls under the frames it owns. The runtime keeps the invocation, growth,
  memoization, precedence, longest-match selection and the finishing of actions; the framed body inlines the matching.
  Each body takes its cut state from the caller and hands it back on both returns; choice, option and repetition keep
  separate scopes, and a repetition restores the full mark before a failing cut drops the values it collected. Bodies
  that recover use full rollback marks, isolate and keep expectations, and keep failed skips, zero progress and the
  rollback of captures and environment exact.

**Capture layouts.** The layout of a rule's captures comes from the capture metadata of its Node rule; the layouts of
independent Operators Rule scopes and of owned or projected repetitions are collected with the same build mode,
predicate visibility, projection and order as the rule's. Syntax that the plan shows dead contributes captures that
are known to be nil, so a dead capture still reads nil without a frame of the runtime. A required slot can widen a
frame. Locals are declared only where reads, marks or values need them.

The printer follows reachability: a statement at an unreachable point is left out and its text is not formed, so it
declares none of the package variables it would name. A variable that no printed statement reads is not declared, and
its assignments are left out.

## Scope

Generated typed Go output. Generated Node parsers have [their own bodies](go-node-bodies.md), and TypeScript is
[printed from Flow IR](typescript-printing.md); the closure engine and the VMs do not use these bodies.

## Why It Is a Design

This is the only route by which typed Go rules are printed: a rule that the plan leaves out is reported as an error,
and no table has another route. A parser without it would need another printer, not a slower form of this one, so
there is nothing a control could switch off. Typed Go output is compared with the engine over the corpus.

## Evidence

The records measured each part against the general route of its time, which no longer exists:

- Record 060: flat rules; JSON typed parsing from 6.44 to 3.86 ms and XML from 6.96 to 4.34 ms, with fewer frame
  allocations on the measured grammars.
- Record 078: local cuts; for 512 rows, median ratios of 0.562 (code points) and 0.712 (bytes), with the same
  allocation counts.
- Record 079: framed left-recursion leaders; large action and capture fixtures at 0.893 (code points) and 0.943
  (bytes), with growth fixed.
- Record 080: framed Operators Rule lines; large fixtures at 0.891 (code points) and 0.940 (bytes), with the same
  allocations; small action cases overlap parity.
- Record 081: cuts in framed bodies; large fixtures at about 0.889–0.943 of the reference time; the small Operators Rule
  case on bytes is 0.4% slower at its median.
- Record 082: capture layouts of framed bodies; large inputs at 0.904–0.945; a widened frame can cost memory.
- Record 083: framed bodies that recover; accepted Operators Rule recovery at 0.891 (code points) and 0.911 (bytes),
  other shapes mixed; generated source and generation allocations grow slightly.
- Record 084: complete capture layouts of flat rules; twelve accepted and rejected medians at about 0.711–0.829, with
  the same allocations.

These results concern their fixtures and the routes of their time; they do not establish a broad speedup today.

Evidence records: [060](../../optimizations/060-direct-rules-in-the-typed-runtime.md),
[078](../../optimizations/078-local-cuts-in-typed-direct-rules.md),
[079](../../optimizations/079-inline-eligible-typed-left-recursion-bodies.md),
[080](../../optimizations/080-inline-eligible-typed-operators-rule-lines.md),
[081](../../optimizations/081-scoped-cuts-in-unfinished-typed-go-bodies.md),
[082](../../optimizations/082-match-typed-direct-frame-layouts.md),
[083](../../optimizations/083-inline-typed-recovery-bodies.md),
[084](../../optimizations/084-complete-typed-local-capture-layouts.md).

Packages: `internal/middleend/flowplan` and `internal/middleend/flowlower` (the typed plan and its lowering),
`internal/backend/codegen/golang/print` (the printer) and `pkg/runtime/libgo` (the frames the library owns).

Related: [limit-control forms](limit-control-forms.md), [Go Node bodies](go-node-bodies.md).
