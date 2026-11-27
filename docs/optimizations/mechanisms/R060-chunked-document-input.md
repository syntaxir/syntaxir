<!-- generated:facts -->
# R060: Chunked Document Input

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Document text
- **Every parser of**: closure, recursive VM, iterative VM
- **An eligible subset, named under Scope, of**: Go Node, typed Go

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_document_pieces` | reference build tag | — | — | — | closure, recursive VM, iterative VM | [093](../logs/093-chunked-document-input.md) |

No control reaches it yet on: Go Node, typed Go.
<!-- /generated:facts -->

A small edit to a large Document should not copy the entire unchanged text
suffix. Immutable chunks and indexed piece ranges replace full-buffer splicing
above a size threshold.

## Mechanism and Invariants

Documents below 4,096 logical positions stay flat. Growing across the threshold
promotes once to piece storage; later shrinking does not demote. Edits
split/replace ranges, and more than 512 pieces triggers compaction while keeping
piece storage. Byte boundaries, malformed UTF-8 and code-point positions
preserve the input contract. Ordinary batch and stream input stay contiguous.

## Scope

Eligible Document storage in closure and both VMs. Generated Go
Documents (`-document`, Node and typed) keep the decoded code points as one
array that each edit splices, since generated code indexes them directly, and
keep the token text in chunks of about 4,096 code points, with sampled byte
offsets for non-ASCII chunks; an edit rebuilds only the chunks it touches.
Generated TypeScript Documents decode and copy the whole text at each edit.

## Controls and Measurements

The build tag selects flat same-source input. It isolates storage while retaining surrounding Document logic. The generated Go
form has no control: decoding the text at each parse is not a maintained form
of the Document contract (record 147).

## Evidence and Implementation

Record 093 measures CodePoints edit-plus-reparse about 19–22% faster, and long
CSV about 25–30% faster, with lower allocation volume. Small edit-only cases can
cost more, supporting the threshold; batch controls show 0.76–3.31% cost. These
results are workload-specific. Design 051 defines ownership and boundary
behavior.

Evidence records: [023](../logs/023-splicing-the-text-of-a-document-in-place.md),
[093](../logs/093-chunked-document-input.md),
[147](../logs/147-decoded-text-of-generated-go-documents.md).

Source owners: [`useDocumentPieces`](../../../internal/runtime/state/input_pieces.go),
[`piece input`](../../../internal/runtime/state/input_pieces.go), [`storage
contract`](../../design/records/051-chunked-document-input.md), and the libgo package for
generated Go Documents.

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [R070](R070-document-memo-edits.md), [R080](R080-lazy-code-point-offsets.md).

[Optimization Catalog](../README.md)
