<!-- generated:facts -->
# M100: Absent Expectation Filter

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Expectation sets of the Go backends
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `sxir_reference_expectation_filter` | reference build tag | — | — | — | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go | [075](../logs/075-exclude-absent-expectations-before-scanning.md) |
<!-- /generated:facts -->

An expectation already present in the active error record needs an exact
duplicate check. A fixed-size membership filter proves many new expectations
absent before scanning the ordered set.

## Mechanism and Invariants

A hash of the complete expectation ID, including its message flag, selects one
of 64 bits. An unset bit permits insertion without a scan; a set bit requires
the exact ordered check. Collisions cannot remove distinct expectations.
Farthest-position reset, isolation, memo/recovery marks and parser recycling
preserve or clear the filter consistently. Saturation retains linear duplicate
work.

## Scope

Go expectation paths in closure, both VMs, generated Node/recognition
and typed output. TypeScript is unchanged.

## Controls and Measurements

The build tag `sxir_reference_expectation_filter` builds the engine and the Go library of generated
parsers without consulting the filter: every expectation scans the current record in order, as before
the filter. The bits are still kept, so the parser state and the record marks are those of the
default build; the comparison is same-source and measures the skipped scans.

## Evidence and Implementation

Historical record 075 measures typed Python `_pydecimal.py` CodePoints from
26.723 to 23.026 ms and 12.14 to 11.82 MB, with similar scoped Bytes results.
Added parser/mark footprint and hash cost are explicit tradeoffs. Saturated
filters retain linear duplicate scans, and the unadopted TypeScript filter
showed no clear gain. These results do not establish universal constant-time or
zero-overhead expectation handling.

Evidence records: [075](../logs/075-exclude-absent-expectations-before-scanning.md).

Source owners: [`parser.expect and record
marks`](../../../internal/runtime/state/runtime.go), [`generated expectation
filter`](../../../pkg/runtime/libgo/lib.go), [`VM label/recovery
state`](../../../internal/runtime/vm/vm.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related designs: [Expectation Sets](../../design/implementation/expectation-sets.md).

[Optimization Catalog](../README.md)
