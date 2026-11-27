<!-- generated:facts -->
# A040: Action Operand Scratch

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Operands of actions
- **Every parser of**: closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript
- **Controls**: none yet; a release needs a control on every backend the mechanism acts on
<!-- /generated:facts -->

Actions and predicates need evaluation contexts, operand storage and
constructor field lists. Reusing invocation-local scratch avoids allocating
those containers for every expression.

## Mechanism and Invariants

The parser keeps an evaluation context, the register files of action programs,
a one-element action buffer and operator locals. An action program takes its
registers above the current top of each file and gives them back, cleared,
when it returns, so nested evaluations reuse the same storage. Built-ins and
constructors cannot retain registers; published values receive separate
ownership. Cleanup on failed predicates must drop any gathered children and
construction tracking.

## Scope

Reusable action plumbing in closure, both VMs and generated Go
Node/typed Go. TypeScript reuses a per-parser action context and evaluation
storage where its runtime permits; it does not pool a parser across separate
calls. Direct typed code can eliminate some context use entirely.

## Controls and Measurements

No common same-source scratch-off control isolates all action buffers.
Whole-input parser pooling and concat fusion are separate mechanisms with
different lifetimes.

## Evidence and Implementation

Records 010/014/015/027/028 measure successive changes. For example, VM JSON
allocation counts fall from 292k to 147k in 014, and closure Operators Rule
action plumbing from about 40.6k to 0.6k in 027. These are historical
backend/workload-specific comparisons, not one combined or current effect
estimate.

Evidence records: [014](../logs/014-one-operand-stack-for-vm-expression-code.md),
[015](../logs/015-allocation-free-action-plumbing-in-all-backends.md),
[027](../logs/027-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend.md),
[028](../logs/028-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers.md).

Source owners: [`evalCtx`](../../../internal/runtime/state/eval.go), [`engine action programs`](../../../internal/runtime/state/),
[`action registers and operator
locals`](../../../internal/runtime/state/runtime.go), [`generated
ActCtx`](../../../pkg/runtime/libgo/lib.go), [`typed evaluation
scratch`](../../../pkg/runtime/libgo/typed.go), [`Parser action
context`](../../../internal/backend/codegen/typescript/lib/lib.ts).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A190](A190-evaluation-context-reuse.md), [A150](A150-vm-position-chunks.md).

[Optimization Catalog](../README.md)
