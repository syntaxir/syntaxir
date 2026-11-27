# Sample Constructor Dependencies

<!-- former-mechanism G030: A technique of the sample generator, off the generation path: no generated parser depends on it, so its output is the same with and without it -->

Building samples need not sweep every rule whenever one estimate of a derivation changes. Reverse dependencies and a
bounded worklist propagate only the changes that can affect callers.

## Design and Invariants

The analysis builds contiguous reverse edges, removes repeated calls, seeds a worklist in explicit depth-first
postorder, and evaluates callers again when minimal heights, byte lengths or coverage sets change. Callees without
cycles run first; cycles converge to the same fixed point. The numbering of targets and the exclusions for negative
lookahead and recovery are unchanged. This changes the work of the analysis, not the matching of the grammar.

## Scope

The analysis of `sample.New`, which needs a grammar AST. No parser runs it: neither the closure engine and the VMs nor
the generated parsers.

## Why It Is a Design

It is a technique of a tool, and the generation path does not reach the sample package, so every generated parser is
the same with and without it. The [classification criteria](../records/080-optimization-management.md) make such a
technique a design of its tool.

## Evidence

Record 071 times the analysis after parsing the grammar. A caller-first alias chain of depth 2,048 goes from 234.498 to
0.9909 ms, but bytes per operation grow 8.6–13.0% across the chain cases for the dependency and worklist storage.
Leaf-first ranges overlap parity. These are trade-offs of scaling and setup of the analysis, not parsing throughput.

Evidence records: [071](../../optimizations/logs/071-dependency-propagation-for-sample-constructor-analysis.md).

Packages: `pkg/sample`.

Related: [variable read components](variable-read-components.md).
