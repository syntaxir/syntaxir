# Typed APIs

Arata Furukawa · Last revised 2026-11-27

This proof shows that the typed APIs of the [generated parsers](../runtime/generated-parsers.md#typed-values)
represent the types of the grammar: every type that a typed parse meets has a
row of the Go table and of the TypeScript table, which gives it a type that the
generated code declares, and converting a value to that type ends, never fails,
and, for a value of the type, keeps what the value holds. With the
[Type Soundness](type-soundness.md) theorem, whose corollary says that the
value of a parse has the type of its start rule, building the typed value of a
parse never fails and keeps its content.

The proof is about the conversion that the specification defines: the value
of the start rule converted by its type. A Go parser may build typed values
directly instead; that this gives the same values is not proved here. It is
checked by the [conformance tests](../runtime/generated-parsers.md#conformance)
of the generated parsers, which compare the direct route with the conversion
on grammars that convert types of every row of the Go table.

## Definitions From the Specification

The proof cites the following rules, the definitions of
[Type Soundness](type-soundness.md) (the values (M1), the reads of fields that a
constructor did not set (M7), the types (T1) and the
relation $v : T$ of [Typing of Values](type-soundness.md#typing-of-values)),
and its theorem and corollary.

- **(R1) Rows of Go.** The table of [Typed Values](../runtime/generated-parsers.md#typed-values)
  gives each grammar type a Go type: `int`, `string`, `bool`; `*T` for a struct
  type `T` with its fields and a `Span`; `*TNode[T']` for `node[T]`, with
  `*TNode[string]` for a string node and `*TNode[any]` for `node[_]`; `*N` for a
  named node type `N = node[T]`, a struct that embeds `TNode[T']`; `TupleN` or
  `Unit` for a tuple; `*Error`, `*Missing` and `ErrorValue` for `error` and
  `missing`; an interface for a join of node types that an alias declares or
  that is written in place with a struct or named node type member; a slice
  for `[]T`; `*int`, `*string`, `*bool` or a pointer to a tuple for `*T` where
  the Go type of `T` is a scalar or a tuple, and else the type of `T`; for a rule whose type is not declared the Go type of its value; `any`
  for `_`, for the joins that the table gives no interface, and for a value that
  closes a cycle of rules; and for an alias `A ~ T` the Go type of `T`. A join
  without an interface whose members all have one Go type has that type and
  converts as its first member.
- **(R2) Rows of TypeScript.** The TypeScript table gives each grammar type a
  TypeScript type by the same cases: an interface with `type`, `start`, `end`
  and the fields for a struct, `TNode<T'>` or the interface of its kind for
  `node[T]`, an interface `N` with `"N"` as its `type` for a named node type,
  a readonly array for a tuple and for `[]T`, a nullable target for `*T`,
  `number | bigint`, `string` and `boolean` for the scalars, the complete target
  for an alias, `RecoveredError`, `Missing` and `ErrorValue` for `error` and
  `missing`, the union of the members for a join of node types, and
  `TypedValue` for the other joins and `_`, except that a join whose members
  all convert alike has the type of its members.
- **(R3) Conversion.** `ParseValue` and `parseValue` return the value of the
  start rule converted by its type, and the errors of the parse; a parse
  without a value returns the zero value of the Go type, or a null value in
  TypeScript. A value of a join converts to the member that its node type
  names, or, for a node of an unnamed node type or of a named node type that is
  no member, to the unnamed node member of its kind; `node[_]` takes every
  other node. `any` and `TypedValue` hold the value converted by its own kind:
  a struct by its struct type, a node by its named node type or its kind, a
  list or tuple element by element, a scalar as it is.
- **(R4) Absent values.** A missing element of a tuple is `nil`. A field that a
  constructor did not set holds the zero value of its Go type; in TypeScript an
  absent scalar field is `""`, `false` or `0`, and another absent value is
  null. After `#recover`, an error value in a list of a join type stays in the
  list as `*Error`, and an inserted token as `*Missing`; where a struct type is
  expected it becomes `nil`.
- **(R5) Names.** A grammar type keeps its name; the names that the types
  package or `types.ts` declares itself, and a join that needs a generated
  name that is taken, are generation errors. A field whose name collides with
  the span or a method gets a trailing underscore in Go and reads back under its
  grammar name. A join of the same members in any order is one interface.
- **(R6) Integers in TypeScript.** An `int` is a `number` when it is a safe
  integer and a `bigint` otherwise
  ([Generated TypeScript API](../runtime/generated-parsers.md#generated-typescript-api)).

## The Types of a Typed Parse

The **types of a typed parse** of a start rule $s$ are the smallest set that
holds the type of $s$ and, with a type, the types within it: the declared types
of the fields of a struct type, the element of a list, the elements of a tuple,
the element of an optional type, the value type of a node type and of a named
node type, the members of a join, the target of an alias, and, for a rule
whose type is not declared, the type of its value with the rules that it names
replaced by their own value types. A rule named while its own value type is
being replaced closes a cycle and stands for `_` there (R1).

A type of the set is in the normal form of (T1): joins are flattened and free
of duplicates and optional members, and meets are resolved to the types that
[Top, Bottom, Join and Meet](../language/types.md#top-bottom-join-and-meet)
gives them, so no meet remains.

**The set is finite.** A grammar has finitely many declared types and rules,
and each step above gives a part of a written type, a declared type, or the
value type of a rule with its rule names replaced. Replacing rule names
expands each rule at most once on any path, since a rule named again on the
path closes a cycle and stands for `_`, so each replaced type is finite and so
is the set.

## Lemma 1 (Every Type Has a Row)

Each type of a typed parse has exactly one row in the Go table and one in the
TypeScript table, and the type that the row gives is declared by the generated
code or by its library, or generation fails with an error that names the type.

**Proof.** By cases on the normal form of the type $T$ (T1).

- `int`, `string`, `bool`: the scalar rows.
- A struct type: the struct row; the Go type and the TypeScript interface are
  declared with the fields, whose types are types of the parse, and the names
  are kept or rejected by (R5).
- `node[T]`, not named: the node row, with `node[_]` (value `_` or `unknown`)
  as `*TNode[any]` and `TNode<unknown>`. A node type that the checker infers
  and that records its captures, or whose elements are not known, maps by its
  value as the node row and the row of rules without a declared type say.
- A named node type: its row, declared once per type.
- A tuple: its row; `Unit` for `()`.
- `error`, `missing`: their rows, chosen by whether the grammar has `#missing`.
- A join: if its members are node types, error and missing, and an alias
  declares it, or it is written in place with a struct or named node type
  member and no two unnamed node members of one kind, the join rows give it an
  interface or union, one per set of members (R5); otherwise, if its members
  have one Go type, it has that type, and else the row of `_` and of the other
  joins gives `any` and `TypedValue`.
- `[]T` and `*T`: their rows, for every $T$.
- A rule whose type is not declared: the type of its value, which is a type of
  the parse, maps by its own row; a rule that closes a cycle stands for `_`.
- `_`, `unknown`, `_|_` and `nil`: the row of `_`, `any` and `TypedValue`. No
  value has type `_|_`, and `nil` is a value of `_`.
- An alias of a type other than a join of node types: its row, the type of its
  target.

The cases are exclusive by the form of $T$, so exactly one row applies. Where a
row needs a generated name, (R5) rejects the grammars whose names collide, so
in a generated package every name the rows use is declared. ∎

## Lemma 2 (Conversion Ends and Never Fails)

For every type $T$ of a typed parse and every value $v$, including `nil`, an
error value and a value that does not have type $T$, the conversion of $v$ by
$T$ ends and gives a value of the Go type and of the TypeScript type of $T$.

**Proof.** The conversion is defined by cases on $T$ (R1 to R4), and each case
gives a value of its target type for every $v$:

- a scalar converts a value of its kind to itself and any other value to the
  zero value of the Go type, `0`, `""` or `false` (R4);
- a struct, a node, a named node type, `error` and `missing` convert a value of
  their kind to the typed form and any other value to `nil` or null (R4);
- a join with an interface converts a node to the member chosen by (R3), and a
  value that no member takes to `nil` or null, which the interface and the
  union admit; a join of members of one Go type converts as its first member;
- a list converts each element by the element type, and a value that is no
  list to `nil` or null; a tuple converts each element by its type, and a
  missing element or a value that is no tuple to the zero element; `*T`
  converts `nil` to `nil` or null and another value by $T$;
- `any` and `TypedValue` take every value, converted by its own kind (R3).

Each step either ends or converts a smaller value, an element, a field or the
value of a node, or converts the same value by a type of lower rank, where the
rank of $T$ for a given value is: 0 for a type that converts the value
without converting it by another type; for `*T`, a join, an alias and a rule
whose type is not declared, one more than the rank of the type it converts by
(the element, the chosen member, the target, the value type); and for `any`,
one more than the rank of the type of the value's own kind. Optional members
are flattened and joins have no join members (T1), an alias never refers to
itself (T1), a rule's value type names no rule that is still being replaced,
and the own type of a value is no `any`; so the rank is finite. Values are
finite, so the conversion ends. No case raises an error. A value that several
parts of a parse share converts once per type it is converted by. ∎

## Lemma 3 (One Go Type, One Conversion)

Let $A$ and $B$ be types of a typed parse with one Go type, and $v$ a
well-formed value with $v : B$. The conversion of $v$ by $A$ gives the typed
value that its conversion by $B$ gives. In TypeScript a join takes the type of
its members only when they convert by one rule (R2), so there the members of
such a join convert every value alike by definition.

**Proof.** Call a type *transparent* when its row gives it the Go type of
another type and converts by that type: a rule whose type is not declared (its
value type), an alias (its target), `*T` for a $T$ whose Go type is no scalar
and no tuple ($T$, which converts `nil` to `nil` as `*T` does, R4), and a join without
an interface whose members have one Go type (its first member). Each converts
by a type of lower rank (Lemma 2). Every other type is a *head*, and its Go
type is given by its own row (R1). By induction on $v$, and for a given $v$ on
the sum of the ranks of $A$ and $B$:

- If $A$ is transparent, it converts as a type $A'$ of the same Go type and of
  lower rank, and the hypothesis for $A'$ and $B$ applies. If $B$ is a
  transparent join, $v : B_i$ for a member $B_i$; the conversion by $B$ is that
  by $B_1$, which by the hypothesis for $B_1$ and $B_i$ is that by $B_i$, and
  the hypothesis for $A$ and $B_i$ applies. If $B$ is another transparent type,
  $v$ has the type $B'$ it converts by, and the hypothesis for $A$ and $B'$
  applies.
- Otherwise both are heads with one Go type, and the rows of (R1) give one Go
  type to two heads only in these cases:
  - a named Go type: `*S` for the struct type `S`, `*N` for the named node type
    `N`, `*Error` or `ErrorValue` for `error`, `*Missing` for `missing`, and the
    interface of a join of node types. The name belongs to one grammar type,
    since the names that collide are errors (R5) and a join of the same members
    in any order is one interface, so $A$ and $B$ are one type;
  - `int`, `string`, `bool`, and `*int`, `*string`, `*bool`: only the scalar,
    and only the option of the scalar, have them;
  - `TupleN[...]` or `Unit`, and a pointer to it: tuples of $n$ elements whose
    elements have pairwise one Go type, or options of such tuples. A tuple
    converts element by element, and $v$ is `nil`, which both convert alike,
    or a tuple whose elements have the types of those of $B$; the hypothesis
    applies to each element;
  - `[]G`: lists whose elements have the Go type `G`; a list converts element
    by element, `nil` to `nil`, and the hypothesis applies to each element;
  - `*TNode[G]`: unnamed node types whose values have the Go type `G`, which
    differ at most in the captures that an inferred node type records, and,
    for `*TNode[any]`, `node[_]` and the node types that record their captures
    and not their elements. Each converts a node to a typed node of its range
    whose value is the text of a string node or the conversion of the node's
    value by its value type, and by the hypothesis the value types convert it
    alike. A value type whose Go type is `any`, and `node[_]`, convert the
    value by its own kind (R3);
  - `any`: the types of the row of `_` and the joins without an interface or
    one Go type, which all hold the value converted by its own kind (R3).

In each case the conversions by $A$ and by $B$ coincide. ∎

## Lemma 4 (Conversion Keeps a Value of Its Type)

Let $v$ be a well-formed value with $v : T$, for a type $T$ of a typed parse.
Its conversion by $T$ represents $v$:

1. a scalar is itself, and in TypeScript an `int` is a `number` or a `bigint`
   of the same value (R6);
2. a struct is the typed struct of the same type and range, each field set by
   the constructor is the conversion of its value by the field's declared type,
   and each field that it did not set holds the zero value of its Go type, or
   in TypeScript `""`, `false`, `0` or null (R4);
3. a node of type `node[U]` is a typed node of the same range whose value is
   the conversion of the node's value by $U$, its text for a string node; a node
   of a named node type `N` is the typed `N` of the same range and value;
4. a tuple is the tuple of the conversions of its elements, a list the list of
   the conversions of its elements, empty for the empty list and `nil` or null
   only for `nil`; `nil` is `nil` or null where the type is `*T`;
5. an error value is `*Error` or `RecoveredError` of the same range and
   message, a `missing` value `*Missing` or `Missing` of the same range,
   expected token and message;
6. a value of a join is the conversion by the member of the join that it has;
7. a value of `_` is held by `any` and `TypedValue`, converted by its own kind
   as in 1 to 6.

**Proof.** By induction on $v$, and for a given $v$ on the rank of $T$ (Lemma
2), following the definition of $v : T$.

- For a scalar, $v$ is a value of that kind, which the scalar row keeps.
- For a struct type `S`, $v$ is a struct of type `S` whose set fields hold
  values of their declared types; the struct row converts each field by its
  type, and the induction hypothesis applies to each value. A field that is not
  set reads as the zero value of its type (M7), and the typed field holds the
  zero value of its Go type (R4); that is the only place where the two differ,
  and the field reads back as unset.
- For `node[U]`, $v$ is a string, tuple or list node whose value has type $U$;
  the node row keeps the range and converts the value, by the hypothesis. For a
  named node type `N = node[U]`, $v$ is a node named `N` with $v$ : `node[U]`,
  and the row of `N` keeps the range and converts the value by $U$.
- For a tuple and a list, each element has its type, and the hypothesis applies
  to it. The empty list converts to the empty slice or array, not to `nil`
  (R1, R2).
- For `*T`, $v$ is `nil`, which converts to `nil` or null, or $v : T$, which
  converts by $T$, of lower rank.
- For `error` and `missing`, $v$ is an error value or a `missing` value, which
  the rows keep with their fields.
- For a join $T_1 \mid \dots \mid T_n$ with an interface or a union, $v : T_i$
  for some $i$. If $T_i$ is a struct type or a named node type, $v$ is a struct
  or a node of that name, which (R3) converts by $T_i$. If $T_i$ is an unnamed
  node type of a kind $k$, $v$ is a node of kind $k$; if its name is a member
  $T_j$ of the join, then $v : T_j$, since $v$ is well formed, and (R3)
  converts it by $T_j$; otherwise (R3) converts it by the unnamed node member of
  kind $k$, which is $T_i$, since the join has an interface only if no two of
  its unnamed node members share a kind (Lemma 1), or, for an open member,
  by `node[_]`. If $T_i$ is `error` or `missing`, $v$ is an error value, which
  the interface and the union hold. In each case the member has a lower rank
  for $v$, and the hypothesis applies.
- For a join without an interface whose members have one Go type, the
  conversion is that of the first member $T_1$, while $v : T_i$. By Lemma 3,
  converting $v$ by $T_1$ gives what converting it by $T_i$ gives, and the
  hypothesis applies to $T_i$, of lower rank. In TypeScript the members
  convert alike by definition (R2).
- For a join without an interface otherwise, `_` and `unknown`, the value is held by
  `any` and `TypedValue`, converted by its own kind (R3): a struct by its type
  and a node by its named node type, whose types are types of the grammar and
  which $v$ has since it is well formed, and a tuple, a list or a scalar as
  above. By the hypothesis each part is represented.
- For an alias, the conversion is that of its target, and for a rule whose
  type is not declared, that of its value type, of lower rank. ∎

## Theorem (Typed APIs)

Let a grammar compile with typed output for Go or TypeScript. For every parse
of its start rule $s$ with a value $v$:

1. the Go type and the TypeScript type of $T_s$ are declared by the generated
   code, and so is the type of every part of the typed value;
2. converting $v$ ends without an error and gives a value of those types;
3. the typed value represents $v$ as Lemma 4 says: the same structs, nodes,
   ranges, texts, elements, scalars and recovered values, with absent fields
   and elements as (R4) gives them.

**Proof.** By the [Type Soundness](type-soundness.md) theorem and its corollary,
$v$ is a well-formed value of type $T_s$. Lemma 1 gives part 1 for $T_s$ and
every type of the parse, Lemma 2 part 2, and Lemma 4, with Lemma 3, part 3. A parse without a
value returns the zero value or null, with the errors of the parse (R3). ∎

## Correspondence

| Claim | Proved by |
|:--|:--|
| Every type that a typed parse meets has exactly one row of each table, and its typed form is declared | Lemma 1 |
| Converting any value by any such type ends and does not fail | Lemma 2 |
| Two types with one Go type convert a value of either alike | Lemma 3 |
| Converting a value of its type keeps its content, up to the absent values of (R4) | Lemma 4 |
| The typed value of a parse is total and faithful | Theorem |

## What the Proof Does Not Show

- **The direct route.** A Go parser may build typed values directly instead of
  converting the tree. The specification says that the result is the same;
  this proof does not show it. The conformance tests compare the two routes,
  with the values of the engine as the reference, on grammars that convert
  types of every row of the Go table, and a broken direct route makes them
  fail.
- **The generators as implemented.** The proof shows that the rules of the
  tables give a total and faithful conversion. That the generated code follows
  them is checked by the conformance tests, which compare the typed values of
  the generated Go and TypeScript parsers with the values of the engine as the
  tables map them, in both position units, on grammars that convert types of
  every row of each table.
- **Other typed entry points.** `ParseRuleValue`, the typed values of
  [Documents](../runtime/documents.md) and the elements of a
  [stream](../runtime/streaming.md) convert by the type of the rule or of the
  element as `ParseValue` does, so Lemmas 1 to 4 apply to them; their
  conformance is not part of this proof.
