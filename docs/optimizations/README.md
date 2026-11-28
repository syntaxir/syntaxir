# Optimization Catalog

This catalog describes optimization mechanisms, their backend scope and
measured evidence. The mechanism index below assigns stable IDs to current
mechanisms and maps each one to its applicable backends and control state.
Numbered records preserve implementation details, tradeoffs and
reproduction commands for individual changes; commits provide chronology. The
[performance document](../performance.md) keeps the measurement method and
latest full-suite analysis.

## Mechanism Index

Each stable ID uses a category letter and a three-digit number: `E` (value elision), `M` (matching and calls), `R` (memo and Document reuse), `A` (allocation and retention), and `G` (generation and compilation). Initial allocations use steps of ten, leaving gaps for related mechanisms. New mechanisms may use those unused numbers; existing IDs are never renumbered or reused. IDs are independent of numbered evidence records and backlog IDs.

The registry of optimizations in the compiler is the source of this index ([design record 080](../design/records/080-optimization-management.md)). The tables between the generated markers are written by `go generate ./internal/optcatalog` from the registry, the profiles and the records, and a test fails when they differ. Each mechanism is classified either as an optimization, which has a control and is listed under Optimization Mechanisms, or as an [implementation design](../design/implementation/README.md), which has no control and is described in the design pages; [Former Mechanism IDs](#former-mechanism-ids) maps the IDs that left the index.

<!-- generated:counts -->
Unclassified mechanisms: 0. Experiment mechanisms, off by default: 1. Optimization mechanisms without a control: 30. Optimization mechanisms with backends that no control reaches: 3. All are 0 at a release.
<!-- /generated:counts -->

### Optimization Mechanisms

A backend cell is `✓` where the mechanism acts on every parser of the backend, `Partial` where it acts on the eligible subset that its page names, `Experiment` where an unadopted candidate, off by default, would act when its switch turns it on, and `—` where it does not act. The controls of a mechanism list only backends that it acts on, and at a release they list every one of them; until then, the counts above include the mechanisms that act on a backend that no control reaches yet, and the settings and the reference controls below show the backends of each control. The closure backend builds from the Core IR, the recursive and the iterative VM from the same bytecode, and the Go Node, Go recognizer, typed Go and TypeScript backends are the generated parsers.

<!-- generated:index -->
| ID | Mechanism | Path scope | closure | recursive VM | iterative VM | Go Node | Go recognizer | typed Go | TypeScript | Controls |
|:--|:--|:--|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:--|
| [A020](mechanisms/A020-node-and-list-slabs.md) | Node and List Slabs | Nodes and child lists of the engine and of generated Go Node parsers | ✓ | ✓ | ✓ | ✓ | — | — | — | none yet |
| [A030](mechanisms/A030-parser-frame-reuse.md) | Iterative VM Frame Reuse | Frames of the iterative VM | — | — | ✓ | — | — | — | — | none yet |
| [A040](mechanisms/A040-action-operand-scratch.md) | Action Operand Scratch | Operands of actions | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | none yet |
| [A060](mechanisms/A060-direct-typed-output-construction.md) | Direct Typed Output Construction | Typed output of the rules the typed plan prints directly | — | — | — | — | — | Partial | — | none yet |
| [A070](mechanisms/A070-typed-output-bootstrap.md) | Typed Output Bootstrap | Typed Go output | — | — | — | — | — | ✓ | — | `typed-output-bootstrap` |
| [A080](mechanisms/A080-generated-node-capture-scratch.md) | Generated Node Capture Scratch | Capture frames of generated Go Node parsers and recognizers | — | — | — | ✓ | Partial | — | — | `sxir_reference_node_frames` (reference) |
| [A100](mechanisms/A100-document-vm-value-stack.md) | Document VM Value Stack | Document parses of the VMs | — | ✓ | ✓ | — | — | — | — | `sxir_reference_document_vm_values` (reference) |
| [A120](mechanisms/A120-source-token-text.md) | Source Token Text | Token text, subject to the input representation | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | none yet |
| [A140](mechanisms/A140-construction-tracking-release.md) | Construction Tracking Release | Construction of values | ✓ | ✓ | ✓ | ✓ | — | ✓ | ✓ | none yet |
| [A150](mechanisms/A150-vm-position-chunks.md) | VM Position Chunks | Positions of the VMs | — | ✓ | ✓ | — | — | — | — | none yet |
| [A160](mechanisms/A160-whole-input-scratch-pooling.md) | Whole-Input Scratch Pooling | Scratch of a whole-input parse in Go | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | `sxir_reference_scratch_pools` (reference) |
| [A170](mechanisms/A170-operators-attempt-scratch.md) | Operators Rule Attempt Scratch | Operators Rule attempts in Go | Partial | Partial | Partial | Partial | Partial | Partial | — | none yet |
| [A180](mechanisms/A180-typed-frame-reuse.md) | Typed Frame Reuse | Capture frames of typed Go | — | — | — | — | — | ✓ | — | none yet |
| [A190](mechanisms/A190-evaluation-context-reuse.md) | Evaluation Context Reuse | Evaluation of actions | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | none yet |
| [A200](mechanisms/A200-stream-capture-frame-reuse.md) | Stream Capture Frame Reuse | Streams of the engine | ✓ | ✓ | ✓ | — | — | — | — | `sxir_reference_stream_frames` (reference) |
| [A210](mechanisms/A210-bounded-stream-text-snapshots.md) | Bounded Stream Text Snapshots | Streams of the engine | ✓ | ✓ | ✓ | — | — | — | — | `sxir_reference_stream_text` (reference) |
| [A220](mechanisms/A220-stream-prefix-isolation.md) | Stream Prefix Isolation | Streams of the engine | ✓ | ✓ | ✓ | — | — | — | — | `sxir_reference_stream_prefix` (reference) |
| [A230](mechanisms/A230-stream-undo-cleanup.md) | Stream Undo Cleanup | Streams of the engine | ✓ | ✓ | ✓ | — | — | — | — | `sxir_reference_stream_undo` (reference) |
| [A240](mechanisms/A240-stream-vm-stack-cleanup.md) | Stream VM Stack Cleanup | Streams of the VMs | — | ✓ | ✓ | — | — | — | — | `sxir_reference_stream_stack` (reference) |
| [A250](mechanisms/A250-typed-object-slabs.md) | Typed Object Slabs | Typed Go output | — | — | — | — | — | ✓ | — | `typed-object-slabs` |
| [A260](mechanisms/A260-typed-list-slabs.md) | Typed List Slabs | Typed Go output | — | — | — | — | — | ✓ | — | `typed-list-slabs` |
| [A270](mechanisms/A270-shared-empty-capture-frame.md) | Shared Empty Capture Frame | Scopes without captures | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [A280](mechanisms/A280-typed-chunk-growth.md) | Typed Chunk Growth | Typed Go output | — | — | — | — | — | ✓ | — | `typed-chunk-growth` |
| [E010](mechanisms/E010-value-free-rule-bodies.md) | Value-Free Rule Bodies | Rule bodies whose values no reader needs | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [E030](mechanisms/E030-operators-line-value-elision.md) | Operators Rule Line Value Elision | Operators Rule lines whose values no reader needs | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [E050](mechanisms/E050-projected-repetition-values.md) | Projected Repetition Values | Repetitions whose readers need one projection of the elements | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [E060](mechanisms/E060-concat-input-fusion.md) | Concat Input Fusion | Eligible inputs of concat | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [G010](mechanisms/G010-generated-helper-sharing.md) | Generated Helper Sharing | Generated parsers with exactly equal bodies | — | — | — | Partial | Partial | Partial | Partial | `method-sharing` |
| [G020](mechanisms/G020-lazy-backend-preparation.md) | Lazy Backend Preparation | The engine's recognizer and VM setup on first use | Partial | Partial | Partial | — | — | — | — | none yet |
| [G070](mechanisms/G070-literal-text-predicates.md) | Literal Text Predicates | Predicates that compare a capture's text with literals, without limit controls | — | — | — | Partial | Partial | Partial | Partial | `text-dispatch` |
| [G110](mechanisms/G110-one-pass-code-point-preparation.md) | One-Pass Code-Point Preparation | Preparation of code-point input | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [G130](mechanisms/G130-generated-literal-sharing.md) | Generated Literal Sharing | Generated parsers with equal literals; TypeScript as an opt-in | — | — | — | Partial | Partial | Partial | Partial | `literal-sharing`, `ts-literal-sharing` |
| [G220](mechanisms/G220-outlined-typescript-nodes.md) | Outlined TypeScript Nodes | Large and repeated nodes of TypeScript Node and recognizer tables | — | — | — | — | — | — | Partial | `outline` |
| [G240](mechanisms/G240-typescript-exec-forms.md) | Compact TypeScript Exec Forms | Expectation merges and tested calls in TypeScript | — | — | — | — | — | — | Partial | `ts-merged-records`, `ts-tested-inits` |
| [G250](mechanisms/G250-used-library-features.md) | Library of the Used Features | Generated parsers with an embedded library | — | — | — | Partial | Partial | Partial | Partial | `used-features` |
| [M010](mechanisms/M010-first-character-dispatch.md) | First-Character Dispatch | Choices whose alternatives have known first characters | Partial | Partial | Partial | Partial | Partial | Partial | Partial | `dispatch`, `guards`, `guarded-classes` |
| [M020](mechanisms/M020-character-class-tests.md) | Character-Class Tests | Character classes | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | none yet |
| [M030](mechanisms/M030-literal-matching.md) | Literal Matching | Literals over loaded input | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | none yet |
| [M040](mechanisms/M040-character-scan-loops.md) | Character Scan Loops | Value-free repetitions of a class or of any character | Partial | Partial | Partial | Partial | Partial | Partial | Partial | `scans` |
| [M050](mechanisms/M050-plain-rule-calls.md) | Plain Rule Calls | Calls of rules that are neither captured nor memoized | Partial | Partial | Partial | Partial | Partial | Partial | Partial | none yet |
| [M060](mechanisms/M060-left-recursion-growth.md) | Left-Recursion Growth State | Left-recursion seed growth in Go | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | — | none yet |
| [M070](mechanisms/M070-vm-predecoded-match-data.md) | Predecoded VM Match Data | Literals and classes of VM programs | — | ✓ | ✓ | — | — | — | — | none yet |
| [M080](mechanisms/M080-direct-code-point-access.md) | Direct Code-Point Access | Direct generated Go bodies over code points | — | — | — | Partial | Partial | Partial | — | none yet |
| [M090](mechanisms/M090-diagnostic-line-index.md) | Diagnostic Line Index | Lines and columns of diagnostics | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | none yet |
| [M100](mechanisms/M100-absent-expectation-filter.md) | Absent Expectation Filter | Expectation sets of the Go backends | Partial | Partial | Partial | Partial | Partial | Partial | — | none yet |
| [M110](mechanisms/M110-compact-vm-backtracking-state.md) | Compact VM Backtracking State | Backtracking state of the VMs | — | ✓ | ✓ | — | — | — | — | none yet |
| [M120](mechanisms/M120-unboxed-text-comparisons.md) | Unboxed Text Comparisons | Comparisons of text | Partial | Partial | Partial | Partial | Partial | Partial | Partial | `field-compare` |
| [M130](mechanisms/M130-short-literal-comparisons.md) | Short Literal Comparisons | Short literals of generated Go | — | — | — | Partial | Partial | Partial | — | `inline-literals` |
| [M140](mechanisms/M140-terminal-programs.md) | Terminal Programs | Lean typed Go; value-free choices of at least 16 literals and classes | — | — | — | — | — | Partial | — | `terminals` |
| [M150](mechanisms/M150-class-runs.md) | Class Runs | Lean typed Go; two or more consecutive classes | — | — | — | — | — | Partial | — | `class-runs` |
| [M160](mechanisms/M160-class-tables.md) | Class Tables | Lean typed Go; classes of at least the configured number of ranges | — | — | — | — | — | Partial | — | `class-tables` |
| [R030](mechanisms/R030-sparse-seen-position-storage.md) | Sparse Seen-Position Storage | Seen positions of deferred memoization | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | `sparse-memo`, `sxir_reference_sparse_memo` (reference) |
| [R060](mechanisms/R060-chunked-document-input.md) | Chunked Document Input | Document text | ✓ | ✓ | ✓ | Partial | — | Partial | — | `sxir_reference_document_pieces` (reference) |
| [R070](mechanisms/R070-document-memo-edits.md) | Document Memo Edits | Document memo edits of the engine | ✓ | ✓ | ✓ | — | — | — | — | none yet |
| [R080](mechanisms/R080-lazy-code-point-offsets.md) | Lazy Code-Point Offsets | Code-point offsets of recognition and Documents | ✓ | ✓ | ✓ | — | — | — | — | none yet |
| [R090](mechanisms/R090-moving-document-trees.md) | Moving Document Trees | Values that a Document reuses | ✓ | ✓ | ✓ | ✓ | — | Partial | — | none yet |
| [R130](mechanisms/R130-document-snapshot-record-sharing.md) | Document Snapshot Record Sharing | Document snapshots of the engine | ✓ | ✓ | ✓ | — | — | — | — | `sxir_snapshot_sharing_reference` (reference) |
| [R140](mechanisms/R140-share-document-snapshot-child-runs.md) | Document Snapshot Child Run Sharing | Child runs of Document snapshots | ✓ | ✓ | ✓ | — | — | — | — | `sxir_snapshot_child_chunks_reference` (reference) |
| [R150](mechanisms/R150-cache-snapshot-child-run-descriptors.md) | Cached Snapshot Child Descriptors | Records of Document snapshots | ✓ | ✓ | ✓ | — | — | — | — | none yet |
| [R160](mechanisms/R160-relative-snapshot-position-overlays.md) | Relative Snapshot Position Overlays | Document snapshots, when the tag is on | Experiment | Experiment | Experiment | — | — | — | — | experiment (off by default): `sxir_snapshot_position_overlays` turns it on |
<!-- /generated:index -->

### Settings

The settings are the same-source controls of the optimizations: the flags of `-optimize`, `-opt` and `-no-opt` of `sxir gen` and of the commands that build a parser for the engine, and of `compiler.Optimize` (the [code-generation guide](../guide/code-generation.md#optimizations) describes each; [Optimization Settings](../../spec/runtime/optimization-settings.md) is their contract). A flag is adopted when `speed` or another profile turns it on, an opt-in otherwise, or an experiment; the record decides the state. The timings of the settings are kept with the timed results in [syntaxir/bench](https://github.com/syntaxir/bench), not in this repository: its [settings table](https://github.com/syntaxir/bench/blob/main/SETTINGS.md) shows each flag's timing in the measurement window that last covered it ([Per-Setting Timing](../performance.md#per-setting-timing)). The deterministic effects of each setting are the `setting` metrics of `bench/metrics` ([Deterministic Metrics](../performance.md#deterministic-metrics)).

<!-- generated:settings -->
| Setting | Mechanism | Kind | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|
| `dispatch` | M010 | pass | adopted (speed, memory, size) | recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [141](logs/141-dispatch-of-kept-guards.md) |
| `guards` | M010 | pass | adopted (speed, memory, size) | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `outline` | G220 | pass | adopted (speed, memory, size) | TypeScript | [146](logs/146-outlined-typescript-nodes.md) |
| `scans` | M040 | pass | adopted (speed, memory, size) | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go, TypeScript | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `field-compare` | M120 | action pass | adopted (speed, memory, size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `text-dispatch` | G070 | action pass | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go, TypeScript | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `class-runs` | M150 | flow pass | adopted (speed, memory, size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `class-tables` | M160 | flow pass | adopted (size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `guarded-classes` | M010 | flow pass | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `inline-literals` | M130 | flow pass | adopted (speed, memory) | Go Node, Go recognizer, typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `terminals` | M140 | flow pass | adopted (speed, memory, size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `ts-merged-records` | G240 | exec pass | adopted (speed, memory, size) | TypeScript | [143](logs/143-compact-typescript-exec-forms.md) |
| `ts-tested-inits` | G240 | exec pass | adopted (speed, memory, size) | TypeScript | [143](logs/143-compact-typescript-exec-forms.md) |
| `literal-sharing` | G130 | printer | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `method-sharing` | G010 | printer | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go, TypeScript | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `ts-literal-sharing` | G130 | printer | opt-in | TypeScript | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `used-features` | G250 | printer | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go, TypeScript | [144](logs/144-library-of-the-used-features.md) |
| `sparse-memo` | R030 | storage | adopted (speed, memory, size) | TypeScript | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `typed-chunk-growth` | A280 | storage | adopted (speed, memory, size) | typed Go | [139](logs/139-geometric-typed-value-chunks.md) |
| `typed-list-slabs` | A260 | storage | adopted (speed, size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `typed-object-slabs` | A250 | storage | adopted (speed, size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
| `typed-output-bootstrap` | A070 | storage | adopted (speed, memory, size) | typed Go | [136](logs/136-measured-effect-of-the-generation-flags.md) |
<!-- /generated:settings -->

### Reference Controls

A reference control is a build tag that builds the parser without the mechanism, for a mechanism that has no zero-cost setting; it compares two builds instead of two settings.

<!-- generated:reference-controls -->
| Build tag | Mechanism | Backends | Record |
|:--|:--|:--|:--|
| `sxir_reference_node_frames` | A080 | Go Node, Go recognizer | [092](logs/092-reuse-generated-node-capture-scratch.md) |
| `sxir_reference_document_vm_values` | A100 | recursive VM, iterative VM | [086](logs/086-reuse-document-vm-values.md) |
| `sxir_reference_scratch_pools` | A160 | closure, recursive VM, iterative VM, Go Node, Go recognizer, typed Go | [057](logs/057-pooling-the-scratch-memory-of-whole-input-parses.md) |
| `sxir_reference_stream_frames` | A200 | closure, recursive VM, iterative VM | [085](logs/085-reuse-stream-capture-frames.md) |
| `sxir_reference_stream_text` | A210 | closure, recursive VM, iterative VM | [088](logs/088-share-bounded-stream-text-snapshots.md) |
| `sxir_reference_stream_prefix` | A220 | closure, recursive VM, iterative VM | [089](logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md) |
| `sxir_reference_stream_undo` | A230 | closure, recursive VM, iterative VM | [089](logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md) |
| `sxir_reference_stream_stack` | A240 | recursive VM, iterative VM | [089](logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md) |
| `sxir_reference_sparse_memo` | R030 | closure, recursive VM, iterative VM | [087](logs/087-sparse-deferred-memo-seen-pages.md) |
| `sxir_reference_document_pieces` | R060 | closure, recursive VM, iterative VM | [093](logs/093-chunked-document-input.md) |
| `sxir_snapshot_sharing_reference` | R130 | closure, recursive VM, iterative VM | [097](logs/097-exact-document-snapshot-record-sharing.md) |
| `sxir_snapshot_child_chunks_reference` | R140 | closure, recursive VM, iterative VM | [099](logs/099-share-document-snapshot-child-runs.md) |
<!-- /generated:reference-controls -->

Records [025](logs/025-discarding-separators-in-the-example-grammars.md) and [055](logs/055-tracing-hook-on-the-call-path.md) remain in the evidence archive but are not optimization mechanisms: the first covers grammar-author output choices and the second measures tracing-instrumentation cost.

### Former Mechanism IDs

An ID that is no longer a mechanism stays reserved and is never reused. An implementation design or a retired mechanism is carried by the design page that describes it, in a marker that the generator reads, and a rejected attempt by the record that rejected it.

<!-- generated:former -->
| ID | Now | Reason |
|:--|:--|:--|
| A010 | [Node Layout](../design/implementation/node-layout.md) | Every backend that builds Nodes reads this layout, and it is the public representation of a Node; a parser without it would need a different Node |
| A050 | [Bounded Stream Storage](../design/implementation/stream-storage.md) | The stream contract bounds what a stream keeps by its window: it discards the input and memo entries before what it delivered, and its Memory count bounds the window, not the length of the input |
| A090 | [094. Generation-Only Typed-Output Liveness Cleanup](logs/094-generated-typed-output-liveness.md) | Rejected attempt; no code remains |
| A110 | [Expectation Sets](../design/implementation/expectation-sets.md) | The memo entries, recovery state and diagnostics of every backend store expectations as these IDs and stack windows; a parser without them would need another representation of expectations |
| E020 | [Value-Free Twins](../design/implementation/value-free-twins.md) | The specification makes a twin a rule of its own, with its own memo entries and its own call unit; compute units and memo identities depend on it |
| E040 | [Recognition Lowering](../design/implementation/recognition-lowering.md) | The specification makes recognition build values only where a predicate can read them, with the same units at every setting; it is an operation of the contract, not a form that can be switched off |
| G030 | [Sample Constructor Dependencies](../design/implementation/sample-constructor-dependencies.md) | A technique of the sample generator, off the generation path: no generated parser depends on it, so its output is the same with and without it |
| G040 | [Attribute Normalization](../design/implementation/attribute-normalization.md) | A compiler technique whose generated output is byte-identical with and without it, checked over the corpus with the eager form forced on |
| G050 | [Limit-Control Forms of Generated Go](../design/implementation/limit-control-forms.md) | Not a separable choice: the two forms are the user option -no-limits, and the specification states that a parser generated without limit controls counts no units |
| G060 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | The only route by which typed Go rules are printed; a rule that the plan leaves out is a generation error, so no maintained form exists without it |
| G080 | [Capture Types](../design/implementation/capture-types.md) | Retired: the join of types removes equal types as part of its definition, so no separable technique is left |
| G085 | [Capture Types](../design/implementation/capture-types.md) | Retired: the language has no positional items, so nothing is left to skip |
| G090 | [Variable Read Components](../design/implementation/variable-read-components.md) | A compiler technique whose result, the sorted set of variables each rule reads, is the same as that of the per-rule search it replaced; that search no longer exists, so there is no other code path to compare |
| G100 | [Go Node Bodies](../design/implementation/go-node-bodies.md) | The only route by which Node and recognizer bodies of generated Go are printed; a body that neither form prints is a generation error |
| G120 | [LSP Position Checkpoints](../design/implementation/lsp-position-checkpoints.md) | A technique of the language server, off the generation path: no generated parser depends on it, so its output is the same with and without it |
| G125 | [Lint Prefix Bounds](../design/implementation/lint-prefix-bounds.md) | A technique of the linter, off the generation path: no generated parser depends on it, so its output is the same with and without it |
| G140 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: local cuts of flat rules |
| G150 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: framed bodies of left-recursion leaders |
| G160 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: framed Operators Rule lines |
| G170 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: cuts in framed bodies |
| G180 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: the capture layouts of framed bodies |
| G190 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: framed bodies of rules that recover |
| G200 | [Typed Go Printing](../design/implementation/typed-go-printing.md) | Part of the only typed Go printing route: the capture layouts of flat rules |
| G210 | [TypeScript Printing](../design/implementation/typescript-printing.md) | The only route by which TypeScript rules are printed; a rule that Flow IR does not print fails generation, and there is no second printer |
| R010 | [Memo Chains](../design/implementation/memo-chains.md) | The memo structure that the complexity bounds rely on; every backend's memo, its keys and the Document reuse are built on it |
| R020 | [Deferred Memoization](../design/implementation/deferred-memoization.md) | The specification fixes exactly which first evaluations leave no result, the same on every backend, because the compute units depend on which calls the memo answers |
| R040 | [Memo Entry Retirement](../design/implementation/memo-entry-retirement.md) | The stream contract bounds a stream's memory by its window, which needs the storage of dropped entries returned, and Documents report retired entries in their statistics |
| R050 | [Variable Environments](../design/implementation/variable-environments.md) | The compute units bound every other piece of work by a constant of the grammar for each event; an environment that kept every binding would make a read walk the bindings of a repetition |
| R100 | [Document Repetition Records](../design/implementation/document-repetition-records.md) | The Document contract makes a resumed element no event, neither performed nor counted, and reports resumed elements in its statistics |
| R110 | [Document Repetition Records](../design/implementation/document-repetition-records.md) | Part of the repetition records that a Document keeps for its whole text between parses, which the Document contract requires |
| R120 | [Transient Rule Memo Policy](../design/implementation/transient-rule-memo-policy.md) | The specification fixes which rules an ordinary parse does not memoize, and the compute units depend on which calls the memo answers |
| R170 | [Left-Recursion Member Memoization](../design/implementation/left-recursion-member-memo.md) | The specification defines how the memo answers and stores the calls of the rules of a left-recursion cycle in the context of growths, on every backend and in Documents |
| R180 | [Skip-Run Records](../design/implementation/skip-run-records.md) | The specification defines skip-run sites, their records and the units of their walks, the same on every backend; a walk that takes a record neither performs nor counts what it skips |
<!-- /generated:former -->

## Optimization History

## Records 1–25
- [001. Value-free rule bodies, value-free twins, transient rules (0b2c903)](logs/001-value-free-rule-bodies-value-free-twins-transient-rules-0b2c903.md)
- [002. Frame pooling in the iterative VM (8fab402)](logs/002-frame-pooling-in-the-iterative-vm-8fab402.md)
- [003. Node fields as a slice (54a5774)](logs/003-node-fields-as-a-slice-54a5774.md)
- [004. Expected sets as interned IDs on a shared stack (11a6ce3)](logs/004-expected-sets-as-interned-ids-on-a-shared-stack-11a6ce3.md)
- [005. Memo table with per-position chains (cf77e4f)](logs/005-memo-table-with-per-position-chains-cf77e4f.md)
- [006. Slab allocation for nodes, child lists and frames (5d03bf2)](logs/006-slab-allocation-for-nodes-child-lists-and-frames-5d03bf2.md)
- [007. Recognition mode (3f7e9bf)](logs/007-recognition-mode-3f7e9bf.md)
- [008. Fast path for unmemoized calls, `SCAN`, nesting limit (d2db87d)](logs/008-fast-path-for-unmemoized-calls-scan-nesting-limit-d2db87d.md)
- [009. Value-free Operators Rule lines; discarding trivia in example grammars (936857c)](logs/009-value-free-operators-rule-lines-discarding-trivia-in-example-grammars-936857c.md)
- [010. Zero-copy token text; reusable evaluation context](logs/010-zero-copy-token-text-reusable-evaluation-context.md)
- [011. Generated parsers catch up with changes 4–10](logs/011-generated-parsers-catch-up-with-changes-410.md)
- [012. Memoizing rules that read variables](logs/012-memoizing-rules-that-read-variables.md)
- [013. Allocation-free Operators Rule attempts and pooled Operators Rule frames in the engine and VMs](logs/013-allocation-free-operators-rule-attempts-and-pooled-operators-rule-frames-in-the-engine-and-vms.md)
- [014. One operand stack for VM expression code](logs/014-one-operand-stack-for-vm-expression-code.md)
- [015. Allocation-free action plumbing in all backends](logs/015-allocation-free-action-plumbing-in-all-backends.md)
- [016. Start positions and lambdas from chunks in the VMs](logs/016-start-positions-and-lambdas-from-chunks-in-the-vms.md)
- [017. ASCII bitmaps for character classes in the VMs](logs/017-ascii-bitmaps-for-character-classes-in-the-vms.md)
- [018. Memoizing from the second call at a position](logs/018-memoizing-from-the-second-call-at-a-position.md)
- [019. A plain call path for unmemoized rules without captures](logs/019-a-plain-call-path-for-unmemoized-rules-without-captures.md)
- [020. Splicing the memo table on Document edits](logs/020-splicing-the-memo-table-on-document-edits.md)
- [021. Changes 18 and 19 in the generated parsers](logs/021-changes-18-and-19-in-the-generated-parsers.md)
- [022. Bounded memory and less copying in stream parsing](logs/022-bounded-memory-and-less-copying-in-stream-parsing.md)
- [023. Splicing the text of a Document in place](logs/023-splicing-the-text-of-a-document-in-place.md)
- [024. The plain call path in the iterative VM](logs/024-the-plain-call-path-in-the-iterative-vm.md)
- [025. Discarding separators in the example grammars](logs/025-discarding-separators-in-the-example-grammars.md)
## Records 26–50
- [026. Lambda calls without allocation in the closure backend](logs/026-lambda-calls-without-allocation-in-the-closure-backend.md)
- [027. Struct and call operands on the expression stack in the closure backend](logs/027-struct-and-call-operands-on-the-expression-stack-in-the-closure-backend.md)
- [028. Field lists and list built-ins without allocation in the generated parsers](logs/028-field-lists-and-list-built-ins-without-allocation-in-the-generated-parsers.md)
- [029. Shifting reused subtrees into the parser's chunks](logs/029-shifting-reused-subtrees-into-the-parsers-chunks.md)
- [030. Building the code-point offset table on demand](logs/030-building-the-code-point-offset-table-on-demand.md)
- [031. Specialized character-class tests in the closure backend](logs/031-specialized-character-class-tests-in-the-closure-backend.md)
- [032. Matching literals against loaded input in one loop](logs/032-matching-literals-against-loaded-input-in-one-loop.md)
- [033. Smaller VM entries](logs/033-smaller-vm-entries.md)
- [034. One frame per plain call in the iterative VM](logs/034-one-frame-per-plain-call-in-the-iterative-vm.md)
- [035. Left-recursion growth without allocation](logs/035-left-recursion-growth-without-allocation.md)
- [036. Comparing texts without boxing them](logs/036-comparing-texts-without-boxing-them.md)
- [037. No intermediate lists in `concat`](logs/037-no-intermediate-lists-in-concat.md)
- [038. Calling plain rules directly from closure call sites](logs/038-calling-plain-rules-directly-from-closure-call-sites.md)
- [039. A line table for error positions](logs/039-a-line-table-for-error-positions.md)
- [040. One pass to prepare code-point input in generated parsers](logs/040-one-pass-to-prepare-code-point-input-in-generated-parsers.md)
- [041. Building the offset table while decoding, for full parses](logs/041-building-the-offset-table-while-decoding-for-full-parses.md)
- [042. Generated calls of plain rules](logs/042-generated-calls-of-plain-rules.md)
- [043. Direct calls of plain rules in the VMs](logs/043-direct-calls-of-plain-rules-in-the-vms.md)
- [044. Changes 36 and 37 in the VMs (instruction set 2)](logs/044-changes-36-and-37-in-the-vms-instruction-set-2.md)
- [045. Moving reused trees in place after document edits](logs/045-moving-reused-trees-in-place-after-document-edits.md)
- [046. Direct calls of plain rules in generated parsers](logs/046-direct-calls-of-plain-rules-in-generated-parsers.md)
- [047. Recognition in generated parsers](logs/047-recognition-in-generated-parsers.md)
- [048. A typed runtime for ParseValue](logs/048-a-typed-runtime-for-parsevalue.md)
- [049. First-character dispatch in generated choices](logs/049-first-character-dispatch-in-generated-choices.md)
- [050. First-character dispatch in the closure backend](logs/050-first-character-dispatch-in-the-closure-backend.md)
## Records 51–98
- [051. First-character dispatch in the VMs (instruction set 3)](logs/051-first-character-dispatch-in-the-vms-instruction-set-3.md)
- [052. Projected repetitions in generated `Parse`](logs/052-projected-repetitions-in-generated-parse.md)
- [053. Projected repetitions in the engine and the VMs](logs/053-projected-repetitions-in-the-engine-and-the-vms.md)
- [054. Resuming long repetitions in a `Document`](logs/054-resuming-long-repetitions-in-a-document.md)
- [055. Tracing hook on the call path](logs/055-tracing-hook-on-the-call-path.md)
- [056. Applying a `Document` edit to memo entries when they are looked up](logs/056-applying-a-document-edit-to-memo-entries-when-they-are-looked-up.md)
- [057. Pooling the scratch memory of whole-input parses](logs/057-pooling-the-scratch-memory-of-whole-input-parses.md)
- [058. Pooling the scratch memory of generated `Parse` and `Recognize`](logs/058-pooling-the-scratch-memory-of-generated-parse-and-recognize.md)
- [059. Resuming long repetitions in the VMs](logs/059-resuming-long-repetitions-in-the-vms.md)
- [060. Direct rules in the typed runtime](logs/060-direct-rules-in-the-typed-runtime.md)
- [061. Reading code points directly in direct rules](logs/061-reading-code-points-directly-in-direct-rules.md)
- [062. Comparing short literals in place in direct rules](logs/062-comparing-short-literals-in-place-in-direct-rules.md)
- [063. A smaller `Node`](logs/063-a-smaller-node.md)
- [064. A smaller `Node` in generated parsers](logs/064-a-smaller-node-in-generated-parsers.md)
- [065. Position conversion in the language server](logs/065-position-conversion-in-the-language-server.md)
- [066. No memo key allocated for variables where none is defined](logs/066-no-memo-key-allocated-for-variables-where-none-is-defined.md)
- [067. Retire memo entries when their lookup slots are removed](logs/067-retire-memo-entries-when-their-lookup-slots-are-removed.md)
- [068. Release construction tracking when an action returns nil](logs/068-release-construction-tracking-when-an-action-returns-nil.md)
- [069. Bound persistent variable binding histories](logs/069-bound-persistent-variable-binding-histories.md)
- [070. Keep repetition records across unchanged Document parses](logs/070-keep-repetition-records-across-unchanged-document-parses.md)
- [071. Dependency propagation for sample constructor analysis](logs/071-dependency-propagation-for-sample-constructor-analysis.md)
- [072. Reuse structurally equal duplicate capture types](logs/072-reuse-structurally-equal-duplicate-capture-types.md)
- [073. Dependency-component propagation of variable reads](logs/073-dependency-component-propagation-of-variable-reads.md)
- [074. Smaller first chunks for generated typed values](logs/074-smaller-first-chunks-for-generated-typed-values.md)
- [075. Exclude absent expectations before scanning](logs/075-exclude-absent-expectations-before-scanning.md)
- [076. Inline value-free plain generated Go rules](logs/076-inline-value-free-plain-generated-go-rules.md)
- [077. Inline value-building and memoized Node expressions in generated Go parsers](logs/077-inline-node-expressions-in-generated-go-parsers.md)
- [078. Local cuts in typed direct rules](logs/078-local-cuts-in-typed-direct-rules.md)
- [079. Inline eligible typed left-recursion bodies](logs/079-inline-eligible-typed-left-recursion-bodies.md)
- [080. Inline eligible typed Operators Rule line matchers](logs/080-inline-eligible-typed-operators-rule-lines.md)
- [081. Scoped cuts in unfinished typed Go bodies](logs/081-scoped-cuts-in-unfinished-typed-go-bodies.md)
- [082. Match typed direct bodies to general frame layouts](logs/082-match-typed-direct-frame-layouts.md)
- [083. Inline eligible typed recovery bodies](logs/083-inline-typed-recovery-bodies.md)
- [084. Complete typed Go-local capture layouts](logs/084-complete-typed-local-capture-layouts.md)
- [085. Reuse stream capture frames at element commit](logs/085-reuse-stream-capture-frames.md)
- [086. Reuse the Document VM value stack](logs/086-reuse-document-vm-values.md)
- [087. Store deferred-memo seen positions sparsely](logs/087-sparse-deferred-memo-seen-pages.md)
- [088. Share bounded stream text snapshots](logs/088-share-bounded-stream-text-snapshots.md)
- [089. Isolate stream prefixes and batch scratch cleanup](logs/089-isolate-stream-prefixes-and-batch-scratch-cleanup.md)
- [090. Share identical generated helpers and literal tables](logs/090-share-identical-generated-helpers.md)
- [091. Specialize pure literal-text disjunctions](logs/091-specialize-pure-literal-text-disjunctions.md)
- [092. Reuse generated Node capture scratch](logs/092-reuse-generated-node-capture-scratch.md)
- [093. Chunked input storage for Document edits](logs/093-chunked-document-input.md)
- [094. Generation-only typed-output liveness cleanup](logs/094-generated-typed-output-liveness.md)
- [095. Copy-on-change attribute normalization](logs/095-attribute-copy-on-change.md)
- [096. Specialize generated Go budget-control methods](logs/096-specialize-generated-go-method-helpers.md)
- [097. Exact Document Snapshot record sharing](logs/097-exact-document-snapshot-record-sharing.md)
- [098. Bootstrap generated typed output allocations](logs/098-bootstrap-generated-typed-output-allocations.md)

## Records 99 Onward

- [099. Share Document snapshot child runs](logs/099-share-document-snapshot-child-runs.md)
- [100. Relative Snapshot Position Overlays](logs/100-relative-snapshot-position-overlays.md)
- [101. Skip positional items for action-free rules](logs/101-skip-positional-items-for-action-free-rules.md)
- [102. Read text struct fields without a struct lookup](logs/102-read-text-struct-fields-without-a-struct-lookup.md)
- [103. Inline the common budget charge](logs/103-inline-the-common-budget-spend.md)
- [104. Smaller generated Go parsers](logs/104-smaller-generated-go-parsers.md)
- [105. Character class tables in generated Go parsers](logs/105-character-class-tables.md)
- [106. Dropping what only non-start rule entries reach](logs/106-start-rule-reachability.md)
- [107. Shared shapers and a compact rule table](logs/107-shared-shapers-and-compact-rule-table.md)
- [108. Prologue and rollback helpers](logs/108-prologue-and-rollback-helpers.md)
- [109. One body for the invocation and the plain call](logs/109-one-body-for-invoke-and-plain-call.md)
- [110. No recovered-error rollback without recovery](logs/110-no-recovered-rollback-without-recovery.md)
- [111. Terminal programs in generated Go parsers](logs/111-terminal-programs.md)
- [112. Guarded first classes in generated Go parsers](logs/112-guarded-first-classes.md)
- [113. The action's rule name is set, not restored](logs/113-action-rule-name-set-once.md)
- [114. No comments on unexported generated code](logs/114-no-comments-on-unexported-generated-code.md)
- [115. Grouped variable declarations](logs/115-grouped-variable-declarations.md)
- [116. A short alias for the runtime import](logs/116-short-runtime-import-alias.md)
- [117. Straight-line actions and no unread variables in generated parsers](logs/117-straight-line-generated-actions.md)
- [118. TypeScript operator nodes are built directly](logs/118-typescript-operator-nodes-built-directly.md)
- [119. Case-insensitive literals in generated parsers](logs/119-case-insensitive-literals.md)
- [120. Unreachable rule bodies in generated parsers](logs/120-unreachable-rule-bodies.md)
- [121. Inactive budget-control checks in the generated Go runtime](logs/121-inactive-budget-control-checks.md)
- [122. Generated TypeScript printed from Flow IR](logs/122-typescript-from-flow-ir.md)
- [123. Smaller boilerplate in generated Go](logs/123-smaller-generated-go-boilerplate.md)
- [124. Coarse budget charges in the engine](logs/124-coarse-engine-budget-charges.md)
- [125. The cost of the default budget](logs/125-default-budget-cost.md)
- [126. Unreachable rule bodies in generated TypeScript](logs/126-unreachable-rule-bodies-typescript.md)
- [127. The depth limit counts recursion](logs/127-depth-counted-by-recursion.md)
- [128. Indexed variable environments](logs/128-indexed-variable-environments.md)
- [129. Expected-token sets are recorded on request](logs/129-opt-in-expectations.md)
- [130. Memoizing the members of left-recursion cycles](logs/130-memoizing-members-of-left-recursion-cycles.md)
- [131. Length of a node from its range](logs/131-length-of-a-node-from-its-range.md)
- [132. Records of the walks of recovery skips](logs/132-records-of-recovery-skip-walks.md)
- [133. Run records of restarted repetitions](logs/133-run-records-of-restarted-repetitions.md)
- [134. Records of single-character scans](logs/134-records-of-single-character-scans.md)
- [135. Shared recovered-error lists](logs/135-shared-recovered-error-lists.md)
- [136. Measured effect of the generation flags](logs/136-measured-effect-of-the-generation-flags.md)
- [137. Scan records on every target](logs/137-scan-records-on-every-target.md)
- [138. Typed-only output of generated TypeScript](logs/138-typed-only-output-of-generated-typescript.md)
- [139. Geometric typed value chunks](logs/139-geometric-typed-value-chunks.md)
- [140. Compute units counted down as fuel](logs/140-compute-units-counted-down-as-fuel.md)
- [141. Dispatch of the kept guards](logs/141-dispatch-of-kept-guards.md)
- [142. Canonicalization without flags](logs/142-canonicalization-without-flags.md)
- [143. Compact TypeScript Exec forms](logs/143-compact-typescript-exec-forms.md)
- [144. Library of the used features](logs/144-library-of-the-used-features.md)
- [145. TypeScript spend runs removed](logs/145-typescript-spend-runs-removed.md)
- [146. Outlined TypeScript nodes](logs/146-outlined-typescript-nodes.md)
- [147. Decoded text of generated Go Documents](logs/147-decoded-text-of-generated-go-documents.md)
- [148. Moving reused trees of generated Go Documents](logs/148-moving-reused-trees-of-generated-go-documents.md)
- [149. Kept child stack of generated Go Documents](logs/149-kept-child-stack-of-generated-go-documents.md)
- [150. Compact memo entries of generated Go](logs/150-compact-memo-entries-of-generated-go.md)
- [151. Documents without value-less lexical entries](logs/151-documents-without-value-less-lexical-entries.md)

## Rejected Attempts

The records with status Rejected, with their result and the condition under which they are worth measuring again.

<!-- generated:rejected -->
| Record | Mechanisms | Result | Revisit when |
|:--|:--|:--|:--|
| [094. Generation-Only Typed-Output Liveness Cleanup](logs/094-generated-typed-output-liveness.md) | A090 | Less retained heap on the direct typed path, but material small-input latency and allocation costs, and no reduction of retained heap or peak RSS on the conversion path | A revised design meets the cost limits of the default path while it keeps a meaningful benefit across direct and conversion results, returned matches, both position units, and ordinary and discard-heavy inputs |
| [140. Compute Units Counted Down as Fuel](logs/140-compute-units-counted-down-as-fuel.md) | none | No measurable change in time, allocations or bytes on any workload, for a library protocol change and two removed `Parser` fields | A later mechanism leaves count sites that are both hot and many, or a workload with heavy counting shows more than 5% overhead of the limits |
| [151. Documents Without Value-less Lexical Entries](logs/151-documents-without-value-less-lexical-entries.md) | none | A Minilang Document retains 31.7% less, but its edits take 1.1 to 5 times as long on every backend: each reparse matches again the blanks before every statement of the program, where it looked their results up before | A Document reparse no longer visits each element of a long repetition whose elements it reuses, or a narrower class of rules keeps the memory saving with no more units per edit than the memo |
<!-- /generated:rejected -->

## Experiments That Did Not Pay Off

| Experiment | Result | Decision |
|:--|:--|:--|
| Delay the variable-name hint until a second name occurs in the current environment | A snapshot with a different single name can be saved before the hint exists; restoring it later produces a false negative and retains duplicate bindings | Rejected for correctness. Record names from the first assignment, including branches that are later abandoned |
| Go arenas (`GOEXPERIMENT=arenas`) for per-parse scratch memory (memo entries, memo slots, expectation chunks), freed at the end of `Parse` | No measurable change in time or allocation: memo entries were already slab-allocated, and the tree must stay on the heap because it is returned | Not adopted. Requires an experimental build flag for no gain |
| ASCII bitmap for character classes in the closure engine (instead of scanning the ranges) | No measurable change when measured against the unmodified code in alternating runs (JSON, CSV); classes in practice have one to three ranges, which scan as fast as a bitmap lookup | Not adopted then; change 31 specializes small classes instead, which does pay |
| Freeing capture frames when their rule invocation returns (LIFO arenas for frames and their slots, marked in `invokeBegin` and freed in `invokeEnd`) | −16 to −23% bytes per parse, but 1–6% slower on every workload and backend (recognition included), even with a fast path that skips freeing when nothing was allocated: the per-call bookkeeping and the clearing of freed slots cost more than the garbage collector saved | Not adopted |
| Storing all-ASCII input in code points as bytes (positions are the same in both units), with `len` of strings still counting code points | −9% bytes per parse on ASCII versions of the JSON, CSV and XML inputs (no rune array or offset table), but 0–4% slower: matching bytes checks for multi-byte characters at every step, which indexing code points does not | Not adopted |
| Iterative VM: holding body frames by value in the VM stack (a tagged entry instead of a pooled frame behind an interface) | 5–19% slower: each entry is about 150 bytes, and copying and clearing it on every push and pop costs more than the interface call and the pool it saves | Not adopted |
| Iterative VM: calling body frames directly (a type assertion before the interface call) and returning them to the pool without the type switch | Within ±4% (noise) | Not adopted |
| Estimating a smaller `Node` (120 → about 88 bytes with `int32` positions and interned type and rule names) by the opposite change: 32 bytes of padding | Closure: JSON +1%, XML +4%, Arith_OperatorsRule +1%, Minilang and the long `Document` within noise, about +12% bytes; so shrinking would gain a few percent at most | Adopted later as change 63, accepting the API change for the memory it saves |
| Inlining small plain rules into direct typed rules (after change 62; a body without calls of up to 16 expressions, also tried transitively) | `ParseValue` JSON −1% to −7% depending on the run, the other benchmarks within ±3% | Not adopted: no consistent gain for more generated code |
| Setting the fields of the reused action context in place in direct typed rules, instead of copying a whole `tctx` | `ParseValue` JSON −5%, Outline +5%, the others within ±2% | Not adopted (noise) |
| Memoizing every rule (classic packrat) | 2–3× slower than the transient policy on all workloads; memo entries were never reused for leaf and single-reference rules | Replaced by the transient policy (change 1) |
| Counting compute units down as fuel to the next check, so that a count site loads one field and tests its sign ([record 140](logs/140-compute-units-counted-down-as-fuel.md)) | `Parse` and `ParseValue` time within ±1% of the base (`4ad68de00` against its parent `361b3fbe5`, 10 rounds, A/A accepted) on JSON, XML and both arithmetic grammars; allocations and bytes unchanged; generated parsers 0.5–0.9% smaller | Rejected: no gain for a library protocol change and two removed `Parser` fields |

## Grammar Authoring Guidelines for Performance

- Inside a captured expression, discard parts the action does not need with `-x` (typically whitespace and
  punctuation): `rest:(-ws -"," -ws v:value)*`. The engine cannot drop them automatically, because a captured value
  exposes its full CST.
- A predicate that reads a capture makes the capture's rules build values even in recognition mode, and with them
  every rule they call: in `alpha-parsers/cue`, one predicate reading a capture that contained an expression made nearly
  the whole grammar build values in `Recognize`. Keep captures that predicates read small (a name, a token).
- Prefer named node types (`rule name Name = (...) #atomic` with `type Name = node[string]`) or `(...) #atomic` for
  tokens: their bodies are matched without building values.
- Write actions that use captures rather than `$n` where possible; a body whose action does not use `$n` is matched
  without building its CST.
