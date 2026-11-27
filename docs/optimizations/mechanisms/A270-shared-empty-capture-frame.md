<!-- generated:facts -->
# A270: Shared Empty Capture Frame

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Scopes without captures
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript
- **Controls**: none yet; a release needs a control on every backend the mechanism acts on
<!-- /generated:facts -->

A scope with no capture slots needs no fresh frame storage. One immutable empty
frame can represent it.

## Mechanism and Invariants

The frame is shared only for zero-slot scopes and is never written. Captured
scopes keep their own slot arrays and rollback. Plain calls can leave a caller's
frame current, while generic zero-slot invocations use the empty owner. Complete
layout collectors must count required dead/nil slots before deciding a scope is
empty.

## Scope

Capture-free scope representation in closure, both VMs, generated Go
Node/typed Go and TypeScript.

## Controls and Measurements

No shared-empty-frame-only comparator is maintained. Plain-call or layout
controls affect a larger route.

## Evidence and Implementation

Record 001 lists the empty-frame mechanism among combined changes; 019 measures
a broader capture-free call path. Neither provides a current isolated
empty-frame effect.

Evidence records:
[001](../logs/001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md),
[019](../logs/019-a-plain-call-path-for-unmemoized-rules-without-captures.md).

Source owners: [`emptyFrame` and
`invokePlain`](../../../internal/runtime/state/runtime.go), [`newFrame`](../../../internal/runtime/state/alloc.go), [`generated empty
frame`](../../../pkg/runtime/libgo/lib.go),
[`Parser.newFrame`](../../../internal/backend/codegen/typescript/lib/lib.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M050](M050-plain-rule-calls.md), [A180](A180-typed-frame-reuse.md).

[Optimization Catalog](../README.md)
