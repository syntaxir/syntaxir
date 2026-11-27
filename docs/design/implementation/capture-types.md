# Capture Types

<!-- former-mechanism G080: Retired: the join of types removes equal types as part of its definition, so no separable technique is left -->
<!-- former-mechanism G085: Retired: the language has no positional items, so nothing is left to skip -->

The type checker infers the type of each capture from its occurrences. Two techniques once made that cheaper for
repeated captures and for rules without actions; neither is a separable technique any longer.

## Joins of Equal Types

A capture label that occurs several times has the [join](../../../spec/language/types.md#join-types) of the types of its
occurrences. The join flattens nested joins and removes duplicates: two types are the same if they are of the same kind
and print the same text. Removing equal types is part of what a join is, not a choice beside it, so it has no control.
Whether a capture is available stays the logical or of its occurrences; equal value types never erase a conditional
availability.

Record 072 measured the reuse of equal capture types when it was a separate step: complete type checking of 1,024
recursive duplicate captures went from 423.966 to 61.126 ms and from 1.188 GB to 32.16 MB.

## Positional Items

Actions once read the items of a sequence by position, so a rule with an action typed its visible top-level items a
second time, and a rule without one skipped that pass. The language no longer has positional items: `$` followed by
digits is a token that the parser rejects ([Capture References](../../../spec/language/lexical.md#capture-references)),
and actions read captures by name. Nothing is left to skip.

Record 101 measured the skip: 32,768 record captures went from 18.57 to 9.74 ms and from 131,243 to 103 allocations.

## Evidence

Evidence records: [072](../../optimizations/072-reuse-structurally-equal-duplicate-capture-types.md),
[101](../../optimizations/101-skip-positional-items-for-action-free-rules.md).

Packages: `internal/frontend/types` (types and their join) and `internal/frontend/check` (the type checker).
