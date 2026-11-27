# Design

How SyntaxIR is built, and the records of the decisions that shaped it. Read these pages to find your way in the
compiler and the runtime; read the [specification](../../spec/README.md) for what an implementation must do.

The pages describe the ideas and invariants that the code does not show on its own. The package comments describe the
code as it is today, and the code is the source of truth for details. A record explains why a decision was taken and may
describe work that is proposed or only partly implemented; its status says which: Proposed, Accepted, Partially
Implemented, Implemented or Superseded.

| Page | Contents |
|:--|:--|
| [Architecture](architecture.md) | The overview: what SyntaxIR provides, the execution model, Core IR and Code IR with the pipeline between them, the backends, the tools and the command line, the package layout and its placement principles, the values, documents and streams, and the invariants that cut across packages |
| [Core IR and Code IR](intermediate-representations.md) | Why there are two internal representations, how Code IR is divided into the Flow, Action and Exec dialects, and how optimization works on them, with examples of rewrites |
| [The Pass Pipeline](pass-pipeline.md) | How the passes are organized: declared passes, the pass model and the analysis manager, the pipeline and its lowerings, the Flow passes, the dialect-independent passes of Code IR, the registry of named optimizations, the `speed`, `memory` and `size` profiles, and verification |
| [Package Layering](layering.md) | The runtime side and the compiler side of the module, the rules on imports between their packages, and the import-graph test that enforces them |
| [The Public API](public-api.md) | The packages under `pkg/` that users import: the grammar source and AST, the compiler entry points, the tool packages, the bytecode package and the runtime and its subpackages |
| [Code Generation for Host Languages](host-code-generation.md) | How a generated parser is made in a host language: the package of each host language, the path from the Exec stage to printed source, the passes that the host languages share and the passes of Go and TypeScript, and the runtime library |
| [Observability](observability.md) | The event and profile model shared by the backends, and what observation costs |
| [Cost Analysis](cost-analysis.md) | How `sxir lint` reaches its complexity classification: the normal view of a grammar, the classifier, the exclusion analysis and the run records of the engine |
| [Implementation Designs](implementation/) | The representations and algorithms the rest of the implementation is built on, and the techniques of the compiler and the tools that change no parser: the Node layout, the memo and its policies, Document and stream storage, value-free twins, recognition, and the printing of generated parsers |
| [Design Records](records/) | One record per decision, with its alternatives, validation and status; [record 072](records/072-release-package-layout.md) is the decision on the release package layout |
