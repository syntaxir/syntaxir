<!-- generated:facts -->
# A170: Operators Rule Attempt Scratch

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Operators Rule attempts in Go
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go
- **Controls**: none yet; a release needs a control on every backend the mechanism acts on
<!-- /generated:facts -->

Trying every operator candidate should not allocate a fresh attempt object and
capture-save slice for each try.

## Mechanism and Invariants

`longest` keeps the best attempt by value and uses a parser-owned buffer to save
captures across reset. The winner still follows longest match and
declaration-order ties. Iterative execution passes the attempt through dedicated
parser/frame state instead of enlarging every generic step result. Scratch
cannot alias the winning published value or erase live captures.

## Scope

Go Operators Rule routes in closure, both VMs and generated Node/typed
Go. TypeScript uses its own JavaScript candidate objects; Go by-value attempt
allocation is not applied there.

## Controls and Measurements

No independent maintained same-source per-candidate allocation control exists.

## Evidence and Implementation

Records 011/013 measure lower Minilang/Operators allocation counts. Record 013
also records a rejected generic-result layout that slowed the iterative VM about
20%. The adopted route's historical effects do not imply every Operators Rule
allocation is eliminated.

Evidence records: [011](../logs/011-generated-parsers-catch-up-with-changes-410.md),
[013](../logs/013-allocation-free-operators-rule-attempts-and-pooled-operators-rule-frames-in-the-engine-and-vms.md).

Source owners: [`parser.longest and saved
captures`](../../../internal/runtime/state/runtime.go), [`Operators Rule attempt
state`](../../../internal/runtime/vm/ivm.go), [`generated
longest`](../../../pkg/runtime/libgo/lib.go), [`typed Operators Rule
attempts`](../../../pkg/runtime/libgo/typed.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E030](E030-operators-line-value-elision.md), [A030](A030-parser-frame-reuse.md).

Related designs: [Typed Go Printing](../../design/implementation/typed-go-printing.md).

[Optimization Catalog](../README.md)
