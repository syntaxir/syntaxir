---
name: documentation
description: Maintain ZeroParse user documentation, contracts, design records and development guidance.
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
  applicability and evidence. `docs/benchmarks.md` is generated.
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
- The backlog writer (a light sidekick) owns documentation context and backlog issue management
  after handoff. The primary owns site code, other implementation, tests and Git.
  The primary must review documentation changes and verify saved results.
