<!-- generated:facts -->
# G020: Lazy Backend Preparation

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: The engine's recognizer and VM setup on first use
- **An eligible subset, named under Scope, of**: closure, recursive VM, iterative VM
- **Controls**: none yet; a release needs a control on every backend the mechanism acts on
<!-- /generated:facts -->

A compiled grammar need not prepare every execution backend before its first
parse. SyntaxIR delays derived bytecode, VM metadata and the recognition graph until
an operation needs them, then caches the prepared state.

## Mechanism and Invariants

Program owns distinct `sync.Once` guards for the module, recursive VM, iterative
VM and recognizer. `Module` compiles bytecode once; `rule` prepares the selected
VM's metadata from it. `recognizer` builds a separate graph and caches either
its program or its setup error. Grammar-less programs cannot construct closure
or recognition graphs. The ordinary closure parse graph is built eagerly during
compilation.

## Scope

Engine setup for closure recognition and both VMs. Generated Go
Node/typed Go and TypeScript embed their machinery and are outside scope. This
is first-use preparation, not per-rule matching work.

## Controls and Measurements

No same-source eager-preparation control or private opt-in alternative is
maintained.

## Evidence and Implementation

There is no numbered optimization record or isolated timing/allocation
measurement. Warm parse results do not quantify avoided setup, and first-use
timing must separate preparation from steady-state matching.

Source owners: the driver's compiler side of a checked program, which builds the bytecode module once,
[`Program.recOnce, vms, prepareVM and Module`](../../../internal/runtime/engine/program.go),
the engine program's bytecode module, [`Program.Recognizer
and rule`](../../../internal/runtime/engine/program_parse.go).

The [comparison policy](../../design/records/024-optimization-mechanism-controls.md)
distinguishes same-source controls, historical changes and full checkpoints.

Related mechanisms: [M070](M070-vm-predecoded-match-data.md).

Related designs: [Recognition Lowering](../../design/implementation/recognition-lowering.md).

[Optimization Catalog](../README.md)
