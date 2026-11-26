---
name: documentation
description: Maintain SyntaxIR user documentation, contracts, design records and development guidance.
---

# Documentation

- User docs: `docs/tutorial/`, `docs/guide/`, and `docs/cookbook/` explain
  learning paths, features and runnable task recipes.
- Contracts: `spec/` holds separate specifications (`language/`, `runtime/`, `core-ir/`,
  `bytecode/`) for normative grammar, parser, IR and bytecode behavior.
  Keep tutorials and development history out of specifications.
- Current design: `docs/design/` explains the stable base design (stages,
  representations, invariants, reasons). The code is the source of truth: do not
  copy field lists, file inventories or per-function detail; point to the
  package instead. Draw structure as Mermaid when it has more than a few nodes.
- Decisions: `docs/design/records/` records problem, decision, alternatives and
  validation; distinguish proposed, accepted and implemented work.
- Development: `docs/development.md` and `docs/site.md` cover contribution and
  site workflows. `docs/performance.md` and `docs/optimizations/` hold method,
  applicability and evidence. The site renders `docs/benchmarks.md` from the
  syntaxir/bench release that `site/benchmarks.json` pins; quoted figures come from `site/figures.json`.
- Design pages (`docs/design/`) describe only the target architecture; migration plans live in the issue tracker.
  They contain no code: no code listings, signatures or code blocks. Public API names are allowed, as the style guide's
  Code References rule states; internal code is named only at the level of packages (no internal functions, types,
  fields, files or lines). They describe language-specific parts in terms of the host language, the language of the
  generated parser, naming Go and TypeScript only as current instances.
  Design pages describe what the target has; they do not deny what the target lacks (no "there is no layer",
  "nothing else", "not a utility"). Keep the negative statements that are contracts the target enforces, such as
  "the runtime never links the compiler".
- Style: follow `docs/documentation-style.md` for page kinds, link directions,
  titles and headings, voice and terms, openings, and examples. The site tests
  enforce link directions and heading capitalization.
- Describe only the current product. Before the first release, breaking changes are
  routine, so reader docs never explain history: no former names, no "earlier versions",
  no old spellings. Each implementation change updates the docs it affects in the same
  change. Write a spec early and keep it current, rather than waiting for the code to
  settle, since proofs and tests depend on it.
- Present the precise types: `node[T]` is the node type for any `T`, and `node[_]` is
  only a fallback for a statically unknown value.
- Public positioning is a universal parser platform: any target language, optimization,
  engine and usage, with the best speed in each. Never frame the project around one host
  language. Pair each universal claim with its current status. Narrowing it needs the
  owner.
- Keep `AGENTS.md` concise. Follow existing indexes and templates, remove
  authoring prompts from finished pages, verify examples and links, and keep
  published docs independent of chats and issue numbers.
- Roles follow `AGENTS.md`. In a multi-lane session, documentation work is a member lane of
  the `team-workflow` skill and is reviewed and integrated like any other lane; the backlog
  writer handles only the backlog and Issue #1. Verify saved results before integrating.
