<!-- generated:facts -->
# G250: Library of the Used Features

- **Classification**: optimization ([design record 080](../../design/records/080-optimization-management.md))
- **Path scope**: Generated parsers with an embedded library
- **An eligible subset, named under Scope, of**: Go Node, Go recognizer, typed Go, TypeScript

| Control | Kind | Default | Profiles | State | Backends | Record |
|:--|:--|:--|:--|:--|:--|:--|
| `used-features` | setting (printer) | on | speed on, memory on, size on | adopted (speed, memory, size) | Go Node, Go recognizer, typed Go, TypeScript | [144](../logs/144-library-of-the-used-features.md) |
<!-- /generated:facts -->

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

## Scope

Generated Go and TypeScript parsers that embed their library (`-lib embed`). A parser that
imports the library, and `-lib only`, carry the whole library; the engine backends have no generated
library.

## Controls and Measurements

With the setting off, the same generator embeds every feature; what a parse returns and charges is the same, so
the comparison measures source and binary size only.

## Evidence and Implementation

Record [144](../logs/144-library-of-the-used-features.md) decides the state on the generated size.

Source owners: the `common` package of the code generators (the feature analysis) and the Go printer's
package (the embedded Go library).

[Optimization Catalog](../README.md)
