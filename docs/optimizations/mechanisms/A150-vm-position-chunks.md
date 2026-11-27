<!-- generated:facts -->
# A150: VM Position Chunks

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Positions of the VMs
- **Every parser of**: recursive VM, iterative VM
- **Controls**: none yet; a release needs a control on every backend the mechanism acts on
<!-- /generated:facts -->

Putting an integer position in a generic operand can allocate a boxed value.
VM-owned chunks amortize the cells. Action code evaluates lambdas in place, so the
VMs make no lambda objects to allocate.

## Mechanism and Invariants

`PUSHPOS` stores a pointer to an integer cell taken from a chunk; sequence, atomic
and repetition consumers dereference it. These cells stay valid for their
evaluation lifetime; the change does not alter logical positions or public
results.

## Scope

Recursive and iterative VM execution. Closure and generated
Go/TypeScript use their own expression representations.

## Controls and Measurements

No maintained same-source per-box allocation reference exists.

## Evidence and Implementation

Record 016 reports VM JSON allocation counts about 32k to 1.9k, with allocated
bytes and time unchanged within noise. This is fewer objects for GC, not an
established throughput improvement.

Evidence records:
[016](../logs/016-start-positions-and-lambdas-from-chunks-in-the-vms.md).

Source owners: [`position cells`](../../../internal/runtime/vm/vm.go),
[`PUSHPOS evaluation`](../../../internal/runtime/vm/vm.go), [`iterative value
consumers`](../../../internal/runtime/vm/ivm.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [A040](A040-action-operand-scratch.md).

[Optimization Catalog](../README.md)
