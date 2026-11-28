<!-- generated:facts -->
# M030: Literal Matching

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Literals over loaded input in the engine
- **Every parser of**: closure, recursive VM, iterative VM

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_literals` | reference build tag | — | — | — | closure, recursive VM, iterative VM | [032](../logs/032-matching-literals-against-loaded-input-in-one-loop.md) |
<!-- /generated:facts -->

Comparing a fully loaded literal in one loop avoids repeated input-loading
checks and examined-position bookkeeping for each character.

## Mechanism and Invariants

`matchLiteral` compares against loaded input, then updates position and examined
range. A mismatch still leaves position after the matched prefix and examines
the differing character. Lazy stream input keeps its reader-aware fallback. VM
literal arrays are prepared once; generated literal matchers use their runtime's
input representation. Short in-place Go comparisons and unboxed action text
comparison have independent eligibility. Case-insensitive literals have no in-place or
whole-input path: generated runtimes match them rune by rune through `Peek`
(`MatchLiteralFold`), and the VMs use the folded string instruction `STRF`. See record
119.

## Scope

Loaded-input matching in the closure and both VMs, which share one parser for loaded and lazily read
input. The literal matchers of generated Go and TypeScript parsers over loaded input are plain loops
over their runtime's input representation by construction, with no per-character bookkeeping to
spare; their stream paths are separate matchers. The one-shot byte comparison of generated Go is a
short-literal comparison, not this mechanism.

## Controls and Measurements

The build tag `sxir_reference_literals` builds the engine without the loaded-input loop: every
literal is matched one character at a time through the path of lazily read input, which records the
examined range and fills the input for each character, as before record 032. The results are the
same, so the comparison is same-source and measures the bookkeeping.

## Evidence and Implementation

Historical record 032 measures 1.5–7% faster sampled closure/recursive VM
workloads except unchanged CSV VM timing. It does not establish current
TypeScript, stream or all-literal benefits.

Evidence records:
[032](../logs/032-matching-literals-against-loaded-input-in-one-loop.md).

Source owners: [`parser.matchLiteral`](../../../internal/runtime/state/input.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M070](M070-vm-predecoded-match-data.md), [M120](M120-unboxed-text-comparisons.md), [M130](M130-short-literal-comparisons.md).

[Optimization Catalog](../README.md)
