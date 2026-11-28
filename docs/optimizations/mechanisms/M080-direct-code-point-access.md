<!-- generated:facts -->
# M080: Direct Code-Point Access

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Direct generated Go bodies over code points
- **An eligible subset, named under Scope, of**: Go Node, Go recognizer, typed Go

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `direct-reads` | setting (printer) | on | speed on, memory on, size on | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go | [152](../logs/152-the-direct-reads-setting.md) |
<!-- /generated:facts -->

Eligible generated Go bodies can read an already-decoded character directly
instead of calling the general `peek` helper for every test.

## Mechanism and Invariants

Direct classes, dot, scan loops and choice guards use `p.in[p.pos]` within the
decoded input bounds. Otherwise they call `peek`, preserving EOF and Bytes UTF-8
decoding. Bytes parsing explicitly keeps the rune input empty even with pooled
state, preventing accidental use of stale CodePoints data.

## Scope

Eligible generated Go Node and typed direct bodies. The fast read is
CodePoints-only; Bytes retains the fallback. Closure, VMs and TypeScript do not
use this Go direct-emitter access route.

## Controls and Measurements

`direct-reads` off prints every read of the next character as the library's
read (`PeekOrdinary` in an ordinary parse, `Peek` where a Document may run the
body) instead of the in-place read with that call as its fallback. Everything
else about the body stays the same, so the comparison isolates the read; the
in-place literal comparisons are `inline-literals`, a separate control. Record
152 measures the setting within each profile.

## Evidence and Implementation

Record 061 historically measures typed JSON 3.75 to 3.30 ms and XML 4.41 to 3.99
ms. Node support arrives through later direct-body lowering, so those typed
figures do not quantify Node or Bytes benefits.

Evidence records: [061](../logs/061-reading-code-points-directly-in-direct-rules.md),
[152](../logs/152-the-direct-reads-setting.md).

Source owners: [`Flow peek lowering`](../../../internal/middleend/flowlower/lower.go), [`input
preparation`](../../../pkg/runtime/libgo/lib.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M130](M130-short-literal-comparisons.md).

Related designs: [Go Node Bodies](../../design/implementation/go-node-bodies.md), [Typed Go Printing](../../design/implementation/typed-go-printing.md).

[Optimization Catalog](../README.md)
