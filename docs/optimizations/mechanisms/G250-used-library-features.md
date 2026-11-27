# G250: Library of the Used Features

A generated parser that embeds its library carries every feature of it unless something leaves the
unused ones out: the growth of left-recursion seeds, recovery, Operators Rules, streams, Documents and
the limit controls. Leaving out what the grammar and the options rule out makes the source and the
binary smaller.

## Mechanism and Invariants

The feature analysis of the code generators' `common` package decides once per generation which
features a parser can use, from the generation options and from the rules its entry points reach; every
target reads the same set. The analysis over-approximates: a feature that a parse could use is always
in the set. With the flag on, the embedded library keeps the code of those features only, and the
declarations that the parser names. With the build tag of the feature witness, a parser panics when a
parse uses a feature outside its set, which the generated-parser tests build with.

## Scope and State

Adopted for generated Go and TypeScript parsers that embed their library (`-lib embed`). A parser that
imports the library, and `-lib only`, carry the whole library; the engine backends have no generated
library.

## Reference Control

`-no-opt used-features` generates the parser with every feature from the same generator; what a parse
returns and charges is the same.

## Evidence and Implementation

Record [144](../144-library-of-the-used-features.md) decides the state on the generated size.

Source owners: the `common` package of the code generators (the feature analysis) and the Go printer's
package (the embedded Go library).

[Optimization Catalog](../README.md)
