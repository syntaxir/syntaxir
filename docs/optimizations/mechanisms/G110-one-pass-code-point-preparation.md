<!-- generated:facts -->
# G110: One-Pass Code-Point Preparation

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Preparation of code-point input
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_input_preparation` | reference build tag | — | — | — | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [040](../logs/040-one-pass-to-prepare-code-point-input-in-generated-parsers.md) |
<!-- /generated:facts -->

Decoding, validating UTF-8 and building token-text offsets in separate passes
rereads the input. One preparation pass produces the decoded characters and
required mapping together.

## Mechanism and Invariants

Full Go input preparation emits code points, validity state and byte offsets in
one traversal. Malformed input preserves replacement semantics and the necessary
copy path. Core recognition keeps on-demand offsets when no text consumer needs
them; generated input uses its runtime's eager preparation. This does not change
the parser's selected position unit.

## Scope

Applicable CodePoints preparation before core/generated Go matching;
TypeScript has its own combined decoding/offset initialization. Bytes input
needs no code-point mapping. Shared preparation is separate from matching-path
backend cells.

## Controls and Measurements

The build tag `sxir_reference_input_preparation` prepares code-point input in three passes, as
before records 040 and 041: the code points, the validity of the UTF-8 (in TypeScript, whether the
string is well formed) and the offsets. In the engine and the Go library it selects the other body of
the preparation function; in TypeScript it replaces a marked region of the library with the
reference body, and a test fails when a marker of the region is missing or duplicated. The results
are the same, so the comparison is same-source. Lazy recognition offsets are an operation-specific
policy, not a full-parse reference.

## Evidence and Implementation

Records 040/041 historically report about 1–5% full-parse improvements on
selected Go workloads; generated CSV changes 4.6 to 4.2 ms in 040. These include
preparation cost and do not establish a pure matching improvement or current
TypeScript ratio.

Evidence records:
[040](../logs/040-one-pass-to-prepare-code-point-input-in-generated-parsers.md),
[041](../logs/041-building-the-offset-table-while-decoding-for-full-parses.md).

Source owners: [`newTextInputIn`](../../../internal/runtime/state/input.go),
[`generated setSource`](../../../pkg/runtime/libgo/lib.go), [`input
decode/offset preparation`](../../../internal/backend/codegen/typescript/lib/lib.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R080](R080-lazy-code-point-offsets.md), [A120](A120-source-token-text.md).

[Optimization Catalog](../README.md)
