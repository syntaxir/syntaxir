<!-- generated:facts -->
# E010: Value-Free Rule Bodies

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Rule bodies whose values no reader needs
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript
- **Controls**: none yet; a release needs a control on every backend the mechanism acts on
<!-- /generated:facts -->

A rule can consume input without retaining the intermediate tree used to match
its body. This removes Node and child-list construction when an action needs
named captures but no positional body values.

## Mechanism and Invariants

`leanBody` selects value-free lowering for an action that does not read
positional body values (`$n`).
Streaming bodies keep their general route. Captures still build the values their
actions need.

```sxir
type B = { B node[string] }
rule ws = [ ]+
rule b = [a-z]
rule a = ws b:b ws => B{B: $b}
```

`a` needs `$b`, but no positional body value. Both `ws` calls still match and
consume whitespace; their unused Node values are omitted. Capture `b` still
builds the value stored in `B.B`. If `ws` has its own action, ordinary parsing
still evaluates it: an unused call result does not authorize skipping the
callee's action. CST twins, Operators Rule line elision and recognition are
separately analyzed mechanisms.

## Scope

Eligible bodies in closure, both VMs, generated Go Node,
typed Go and TypeScript. It does not mean that every value inside a lean body is
discarded.

## Controls and Measurements

No body-elision-only same-source control is maintained across all backends.
Comparing recognition with a valued parse changes the requested operation and
cannot isolate this mechanism.

## Evidence and Implementation

Record 001 reports historical JSON from 442 to 144 ms and 183 to 76 MB for the
combined body/twin/transient-rule change. That result cannot be attributed
wholly to body elision; current isolated all-backend effects are unmeasured.

Evidence records:
[001](../logs/001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md).

Source owners: the lean-body test and the walk of rule bodies in the front end's check package,
[`body value
lowering`](../../../internal/backend/codegen/golang/print/generator.go), [`direct body build
mode`](../../../internal/backend/codegen/golang/print/rule_shapes.go), [`TypeScript body build
mode`](../../../internal/backend/codegen/typescript/print/generator.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [E030](E030-operators-line-value-elision.md).

Related designs: [Value-Free Twins](../../design/implementation/value-free-twins.md), [Recognition Lowering](../../design/implementation/recognition-lowering.md), [Transient Rule Memo Policy](../../design/implementation/transient-rule-memo-policy.md).

[Optimization Catalog](../README.md)
