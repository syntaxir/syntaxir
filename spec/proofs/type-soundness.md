# Type Soundness

Arata Furukawa · Last revised 2026-11-24

This proof shows that the types of the [Type System](../language/types.md) and
the rules of [Type Checking](../language/type-checking.md) are sound: in a
grammar that the checker accepts, every value that a rule produces in a parse
is a value of the rule's type, every capture, variable and struct field holds a
value of its type, and every Action Expression that completes gives a value of
its static type. An action stops with a run-time error only at the places that
the specification leaves to run time, which the proof lists.

The proof first gives the types a meaning, a relation between values and types
that follows the check a cast makes when it runs. It then shows that
assignability, joins, zero values and conversions respect that relation, and
that each Rule Expression and each Action Expression produces what its type
says, by induction on the evaluation of a parse.

The proof is about the evaluation that the language chapters define, with the
reuse that the runtime specification allows. It holds for every backend that
evaluates as they define, in batch parses, recognition, typed parses, prefix
parses, streams and [Documents](../runtime/documents.md), with and without the
insertion of missing tokens.

## Definitions From the Specification

The proof cites the following rules, and no other rule about types or values.

### Values

- **(M1) Values.** A value is a node, a struct, a tuple value, a list value, an
  `int`, a `string`, a `bool` or `nil`. A node is a string node, a tuple node, a
  list node or an error value; a string node holds its text, a tuple node the
  tuple of its elements and a list node the list of its elements, each element
  a value or `nil`. An error value is a range that `#recover` skipped, or a
  `missing` token that `#missing` inserted, with its `expected` and `message`
  fields; it has no value. A node of a named node type carries the name of the
  type. A struct carries its struct type and the fields that its constructor
  set. A function is not a value
  ([Node Model](../runtime/nodes.md#node-model),
  [Built-In Types](../language/types.md#built-in-types),
  [Field Access](../language/actions.md#field-access)).
- **(M2) Values of Rule Expressions.** A literal, a character class, `.` and
  `a #atomic` produce a string node. A sequence produces a tuple node of the
  values of its elements that have a value, in order; the elements of a group
  or of a call of a hidden rule without an action are elements of the
  enclosing sequence, and a recovered expression is one element. A choice
  produces the value of the alternative that matched, or `nil` when that
  alternative has no value and another has one. A repetition produces a list
  node of the values of its iterations, and an iteration without a value is
  the unit `()`. An optional produces the value of its operand or `nil`. A
  capture produces the value of its operand, or the unit `()` with the range
  that its operand matched when the operand has no value. A rule call produces the value of
  the rule, and a rule whose body has no value produces the unit `()`, the
  empty tuple node, with the range that it matched. Anchors, the cut, a discard, `_`, `_|_`, a lookahead and a
  predicate have no value
  ([Values and the Concrete Syntax Tree](../language/parser-expressions.md#values-and-the-concrete-syntax-tree),
  [Hidden Rules](../language/actions.md#hidden-rules)).
- **(M3) Recovery and insertion.** A `#recover` whose expression fails and
  whose skip consumes input produces an error value in place of the value of
  the expression; an expression without a value stays without one, apart
  from an expression of type `_|_`, which never matches: its recovery
  produces an error value, as in `("a" _|_) #recover(skip="z")`. A `#missing`
  literal that inserts produces a `missing` value
  ([`#recover`](../language/pragmas.md#recover),
  [`#missing`](../language/pragmas.md#missing)).
- **(M4) Captures.** A capture records the value of its operand under its name
  in its scope, a rule body or one iteration of a repetition. A capture that
  did not match is `nil`, and a capture made in a match that backtracking
  undid does not survive. A capture inside a positive lookahead is kept beside
  the tuple of its scope. Captures are not allowed inside `#atomic`, `-`, `!`
  and the skip of a recovery
  ([Capture](../language/parser-expressions.md#capture)).
- **(M5) Rule values.** A rule without an action produces the value of its
  body; one with a Value Action produces the result of the action, which runs
  after the body succeeds. A rule whose declared type is exactly a named node
  type `N` converts its value to `N`; an inline action is a call of a hidden
  rule whose action it is
  ([Actions](../language/actions.md),
  [Inline Value Actions](../language/actions.md#inline-value-actions),
  [Named Node Types](../language/types.md#named-node-types)).
- **(M6) Operators Rules.** An Operators Rule produces the value of an operand
  line, or of an operator applied to the values of its operands: the result of
  its action, or without one a tuple node `(lhs, op, rhs)`, `(lhs, op)` or
  `(op, rhs)`. A rule declared with exactly a named node type `N` and without a
  whole-rule action converts the value of each operator to `N`. A whole-rule
  action runs once per call on the folded value `$value`
  ([Operators Rule](../language/operators-rule.md#value-actions),
  [Whole-Rule Action](../language/operators-rule.md#whole-rule-action),
  [Named Node Types](../language/operators-rule.md#named-node-types)).
- **(M7) Actions.** The operators and built-in functions compute their results
  as [Operators and Functions](../language/actions.md#operators-and-functions)
  defines. A field of a struct that the constructor left out reads as the zero
  value of its declared type. `.Value` of an error value is `nil`. A list
  function treats `nil` and an error value as an empty list. A run-time error
  of a Value Action ends the parse; one of a Predicate Action makes the
  predicate fail ([Types and Errors](../language/actions.md#types-and-errors)).
- **(M8) Variables.** Every parse starts with each variable bound to its initial
  value; a binding binds the value of its expression, and a binding whose
  expression raises an error makes its predicate fail and binds nothing.
  Bindings end with the call that made them and are undone by backtracking
  ([Variables](../language/predicates.md#variables)).
- **(M9) Reuse.** A parse may answer a call from a memo entry, iterations from a
  record and, in a Document, results of earlier parses. What it reuses is the
  outcome of an earlier completed evaluation of the same call or iterations, in
  the same parse or an earlier parse of the same Document
  ([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules),
  [Reuse Model](../runtime/documents.md#reuse-model)), and by the
  [Memoization](memoization.md) and [Documents](documents.md) theorems it is
  the outcome that evaluating again would give. A stream hands elements of its
  repetition to the caller and leaves them out of the returned list
  ([`#stream`](../language/pragmas.md#stream)).

### Types

- **(T1) Types.** The types are `bool`, `int`, `string`, `node[T]`, `error`,
  `missing`, `_`, `_|_`, tuple types `(T1, ..., Tn)`, list types `[]T`,
  optional types `*T`, joins `T | U`, meets `T & U`, struct types and named node
  types `type N = node[U]`. An alias stands for its right-hand side and never
  refers to itself. A join is normalized: nested joins are flattened, duplicate
  members removed, and a join with an optional member is optional. A node type
  that the checker infers also records the types of the captures of the node
  ([Type System](../language/types.md),
  [Rule Expression Types](../language/type-checking.md#rule-expression-types)).
- **(T2) Admission.** A cast `x.(T)` checks the value of `x` against `T`:
  `int`, `string` and `bool` admit values of that kind and `nil` admits `nil`;
  `*T` admits `nil` and what `T` admits; a join admits what a member admits;
  a struct type admits structs of that type; `node[T]` admits a string, tuple
  or list node, named or not, whose value `T` admits, and no error value; a
  named node type `N` admits a node whose name is `N`; a tuple type admits a
  tuple value of as many elements, each admitted by its type; `[]T` admits a
  list value whose every element `T` admits; `error` admits error values and
  `missing` values; `_` admits every value and `_|_` none
  ([Values and Constructors](../language/actions.md#values-and-constructors),
  [Casts](../language/actions.md#casts),
  [Top, Bottom, Join and Meet](../language/types.md#top-bottom-join-and-meet)).
- **(T3) Assignability.** The rules of
  [Assignability](../language/types.md#assignability), including those for
  named node types, and that the meet `T & U` is assignable to `T` and to `U`.
- **(T4) Zero values.** The table of
  [Zero Values](../language/types.md#zero-values), and that a constructor of a
  struct must set every field whose type has no zero value.
- **(T5) Rule Expression types.** The table of
  [Rule Expression Types](../language/type-checking.md#rule-expression-types),
  with [`_|_`](../language/pragmas.md#recover) for an expression that never
  matches. A choice some of whose alternatives have no value is optional, as
  its value is `nil` when such an alternative matched (M2). A recovered
  expression has the join of its type and `error`, and a `#missing` literal
  the type `node[string] | missing`.
- **(T6) Capture types.** The rules of
  [Types of Captures](../language/type-checking.md#types-of-captures): a
  capture has the type of its operand, and `*T` when it is made only in some
  alternatives of a choice, inside an optional expression, or inside a
  recovered expression; a name captured more than once has the join of its
  captures, optional unless one of them is always made.
- **(T7) Action Expression types.** The table of
  [Action Expression Types](../language/type-checking.md#action-expression-types),
  the operand types of [Operator Precedence and
  Types](../language/actions.md#operator-precedence-and-types), the argument
  rules of [Built-In Functions](../language/actions.md#built-in-functions), and
  the rules of [Field Access](../language/actions.md#field-access): a field
  access is rejected on a receiver whose static type may be `nil`, a member of
  a join without the field is rejected unless it is a recovered value, which
  makes the result optional, and the result is otherwise the join of the types
  of the field in the members.
- **(T8) Checks.** A grammar compiles only if the checker reports none of the
  [Checked Errors](../language/type-checking.md#checked-errors): in
  particular, each rule value, constructor field, typed-list element and
  binding has a static type assignable to its target, or is converted to a
  named node type, and a value of unknown type `_` is assignable only to `_`.
- **(T9) Inference.** A rule without a declared type has the type of its body,
  or of its action. Rules are inferred after the rules they call; a recursive
  group is iterated until no type changes, and a type that keeps growing is
  widened to `_`, the type that is not known, for the rest of inference. The
  accumulator of `foldl` and `foldr` is widened to the join of the initial
  value and the result of the function until the result is assignable to it,
  or the call is an error
  ([Type Inference](../language/type-checking.md#type-inference)).
- **(T10) Operators Rule types.** With a declared type `T` and no whole-rule
  action, the values of all lines are assignable to `T` and `$lhs` and `$rhs`
  have type `T`; without a declared type, the rule has the join of the types
  of its lines; with a whole-rule action, `$lhs`, `$rhs` and `$value` have the
  folded type, the least fixed point of that join with `$lhs` and `$rhs` of
  that type. When it keeps growing, `$lhs` and `$rhs` are widened to
  `node[_]` when the type of every line, with `$lhs` and `$rhs` of type
  `node[_]`, is assignable to `node[_]`, and to `_` otherwise, and `$value`
  has the join of the lines so typed. The result of the action is assignable
  to the declared type ([Types](../language/operators-rule.md#types)).
- **(T11) Conversion.** A conversion to `N`, by `N{x}`, by a rule declared
  `N` or by an Operators Rule declared `N`, is accepted only when every
  alternative of the static type of the value is a node, named or not, whose
  underlying type is assignable to the underlying type `node[U]` of `N`, and it
  gives the node the name `N` and keeps its value
  ([Named Node Types](../language/types.md#named-node-types)).
- **(T12) Variables.** A variable has the type `int`, `string` or `bool` of its
  declaration, its initial value has that type, and a binding is accepted only
  if the static type of its value is assignable to it; a read has that type
  ([Variables](../language/predicates.md#variables)).

## Typing of Values

The proof gives each type a set of values. The relation $v : T$, read "$v$ has
type $T$", is defined by induction on $v$, and for a given $v$ on the size of
$T$ with aliases unfolded. A named node type or a struct type unfolds only
inside a node or a struct, whose values are smaller, so the definition is well
founded also for recursive types such as `type Tree = node[[]Tree]`.

- $v$ : `_` for every $v$; no $v$ has type `_|_`.
- $v$ : `int`, `string` or `bool` if $v$ is a value of that kind; `nil` : `nil`.
- $v$ : `*T` if $v$ is `nil` or $v$ : $T$.
- $v$ : $T \mid U$ if $v$ : $T$ or $v$ : $U$; $v$ : $T$ `&` $U$ if both hold.
- $v$ : `node[T]` if $v$ is a string, tuple or list node, with or without a
  name, whose value has type $T$; when the type records captures, each capture
  $y$ of $v$ that the type records is `nil` or holds a value of its recorded
  type.
- $v$ : `N`, for a named node type `N = node[U]`, if $v$ is a node named `N`
  and $v$ : `node[U]`.
- $v$ : `S`, for a struct type `S`, if $v$ is a struct of type `S` and every
  field that its constructor set holds a value of the field's declared type.
- $v$ : $(T_1, \dots, T_n)$ if $v$ is a tuple value of $n$ elements and each
  element $v_i$ : $T_i$.
- $v$ : `[]T` if $v$ is a list value and each element has type $T$.
- $v$ : `error` if $v$ is an error value, including a `missing` value;
  $v$ : `missing` if $v$ is a `missing` value.

A value is **well formed** if every node named `N` within it has type `N`, and
every struct of type `S` within it has type `S`. Within a well-formed value,
the name of a node and the type of a struct say everything that the relation
asks of them.

## Lemma 1 (Admission)

For every type $T$ other than a meet and every well-formed value $v$, the
check of a cast to $T$ (T2) succeeds on $v$ if and only if $v$ : $T$.

**Proof.** By induction on $v$ and $T$ as in the definition. For `_`, `_|_`,
the scalars, `nil`, optional types, joins, tuple types, list types, `error` and
`missing`, the clauses of (T2) and of the relation are the same, with the
induction hypothesis for the parts. For `node[T]`, (T2) asks that the value of
the node be admitted by $T$, which by the hypothesis is that it has type $T$; a
type written in a cast records no captures. For a named node type `N`, (T2)
checks only the name; a node named `N` within a well-formed value has type `N`,
and conversely $v$ : `N` asks that $v$ be named `N`. For a struct type `S`,
(T2) checks only the struct type, and a struct of type `S` within a
well-formed value has type `S`. (T2) does not list meets: a meet resolves to
a type by the rules of (T3), which is assignable to both of its operands, so a
value that the check of the resolved type admits has both types by Lemma 2. ∎

## Lemma 2 (Assignability)

If $V$ is assignable to $T$ (T3) and $v$ : $V$, then $v$ : $T$.

**Proof.** By induction on the derivation of assignability, and inside it on
$v$. Same type: immediate. $T$ = `_`: every value has type `_`. $V$ = `_|_`: no
value has type `_|_`. $V$ a join: $v$ has the type of one member, which is
assignable to $T$. $T$ a join: $v$ : $V$ and $V$ is assignable to a member, so
$v$ has the type of that member and so of the join. $T$ = `*U`: $v$ is `nil`,
which has type `*U`; or $V$ = `*W` with $W$ assignable to $U$, and $v$ is `nil`
or $v$ : $W$, hence $v$ : $U$; or $V$ is assignable to $U$. $T$ = `node[_]`:
$v$ is a string, tuple or list node, and its value has type `_`. $T$ =
`node[U]` and $V$ = `node[W]` with $W$ assignable to $U$: the value of $v$ has
type $W$, hence $U$ by the hypothesis on the smaller value; the target records
no captures, or the same captures, since it is a type written in a grammar or
built by a join (T1). $V$ = `N` with underlying `node[W]` and $T$ = `node[U]`
with $W$ assignable to $U$: $v$ : `N` gives $v$ : `node[W]`, and the case
before applies; an unnamed `node[U]` admits nodes whatever their names. The
named node rules that deny assignability add no case. $T$ = `[]U` and
$V$ = `[]W`: each element. Tuple types: each element, with the same number of
elements. $T$ = `error` and $V$ = `missing`: a `missing` value is an error
value. A meet $V$ = $A$ `&` $B$ is assignable to $A$ and to $B$, and $v$ : $V$
gives both. ∎

**Joins.** $v$ : $T$ gives $v$ : $T \mid U$, and the normalization of a join
(T1) keeps the relation: flattening and removing duplicates change no member
set, and $v$ : $A \mid$ `*B` if and only if $v$ : `*(A | B)`, as both hold
exactly when $v$ is `nil`, $v$ : $A$ or $v$ : $B$. So the order of the members
of a join, which inference does not count as a change (T9), is not a
difference of meaning.

## Lemma 3 (Zero Values)

Every type $T$ that has a zero value (T4) has $z_T$ : $T$, and $z_T$ is well
formed.

**Proof.** `0`, `""` and `false` have their types; the empty list has every
list type, including `[]_|_`; `nil` has every optional type and `_`. The tuple
of the zero values of its elements has the tuple type by the claim for each
element, by induction on the type. A struct type `S` has a zero value only if
every field type has one, without a path back to `S` through fields and tuple
elements, so the induction on that path is finite; the zero value is the struct
of type `S` with no field set, which has type `S` vacuously, and a read of a
field gives the zero value of the field type, which has that type by
induction. ∎

## Lemma 4 (Conversion)

If $x$ has a static type $X$ that a conversion to `N = node[U]` accepts (T11)
and $x$ : $X$, the converted value has type `N` and is well formed when $x$ is.

**Proof.** $x$ has the type of an alternative of $X$, which (T11) requires to
be a node type, named or not, with an underlying type `node[W]` assignable to
`node[U]`. So $x$ is a node whose value has type $W$, hence $U$ by Lemma 2,
and $x$ is not `nil` or an error value, which (T11) rejects with the scalars,
`_` and `node[_]`. The conversion keeps the value and sets the name to `N`, so
the result has type `N`, and the values within it are those of $x$. ∎

## Setting

The proof is by induction on the order in which evaluations complete, across
all parses of a Document: an evaluation completes after the evaluations of its
parts, and a reused result after the evaluation that it is the outcome of. The hypothesis for an evaluation is the theorem for every evaluation
that completed before it, which includes every result that it reuses (M9).
A left call that a growth answers reads the growing result, the outcome of an
earlier step of that growth, which completed before the step that reads it
([Growths of Left Recursion](../runtime/parsing.md#growths-of-left-recursion)),
so the hypothesis covers it as it covers a reused result.
The theorem is about a grammar that the checker accepts (T8), with the types
that the checker gives: the declared type of each rule and capture, the
inferred type of a rule without one (T9), and the type of each Action
Expression (T7).

A **typing environment** of an action, predicate or line is its captures with
their types (T6), the variables with their declared types (T12), the
parameters of the lambdas around it with the types that the checker gives
them, and in an Operators Rule `$lhs`, `$rhs`, `$op` and `$value` with their
types (T10). A run-time environment **matches** it if each name holds a
well-formed value of its type.

## Lemma 5 (Rule Expressions)

Let a Rule Expression $e$ of a rule succeed in an evaluation whose callees
satisfy the theorem. Then the value that $e$ contributes, if it has one, is
well formed and has the type of $e$ (T5), and each capture made in $e$ that
survives holds a well-formed value of the type of its operand.

**Proof.** By induction on $e$, with the value of (M2).

- **Terminals.** A literal, a character class, `.` and `a #atomic` produce a
  string node, of type `node[string]`.
- **Sequence.** The tuple node holds the values of the elements with a value,
  in order, which have their types by induction; the elements of a group or a
  hidden rule without an action are listed in the tuple type as they are in
  the tuple (T5, M2). An element of type `_|_` never matches (below), so a
  sequence with one never succeeds.
- **Choice.** The value is that of the alternative that matched, of its type
  and so of the join, or `nil` when that alternative has no value, which the
  optional type of such a choice admits (T5).
- **Repetition.** Each iteration has the type of the element, or is the unit
  `()` of type `node[()]` when the element has no value; the list node has the
  list type. A stream leaves out elements, and a shorter list has the same
  list type (M9).
- **Optional.** The value of the operand, or `nil`, of type `*A`.
- **Capture.** The value of the operand, of its type; the capture holds it
  (M4).
- **Recovery.** The value of the expression, of its type, or an error value,
  of type `error`; the join has both (M3, T5). An expression without a value
  has none either way. An expression of type `_|_` never succeeds (below), so
  a recovery of it that succeeds gives an error value, of type `error`, which
  is the join of `_|_` and `error`.
- **Insertion.** The string node of the literal or a `missing` value, both of
  type `node[string] | missing`.
- **Calls.** The value of the rule, of the rule's type by the hypothesis; an
  inline action is a call of its hidden rule (M5).
- **No value in a value position.** An expression without a value in a
  value position, the operand of a capture or the body of a rule, gives the
  unit `()`, a tuple node of no elements, which has type `node[()]`, the type
  of an expression without elements (T5). So `rule e = _` and
  `rule f = -"x"` produce `()`, and a call of either contributes that node as
  an element (M2); the captures `x:-"a"`, `x:^^`, `x:&"a"` and `x:_` read as
  `()` and contribute it as an element as well.
- **Never matching.** An expression of type `_|_` never succeeds: `_|_` fails,
  a sequence fails at such an element, a choice of such alternatives fails at
  each, `a+` fails at its first iteration, a capture or an `#atomic` matches
  only where its operand does, and a rule whose body has the type `_|_` would
  need a success of that body, which by induction on the order of completion
  has none, a growth of it included, which starts from a seed that fails.

Values within the value of $e$ are values of terminals, of callees and of
recoveries, all well formed. ∎

## Lemma 6 (Captures Read by an Action)

When the action or predicate of a scope runs, each capture $y$ of the scope
holds `nil` or a well-formed value of the type of its operand, and holds `nil`
only if its type (T6) is optional.

**Proof.** Lemma 5 gives the type of each capture that was made and survives.
A capture holds `nil` when it was not made in the match that succeeded (M4). In
a scope that succeeded, a capture outside choices, optional expressions and
recovered expressions is made: every element of
a sequence that succeeded matched, a capture matched where its operand did, a
positive lookahead that succeeded matched its body, and the scope of a
repetition iteration is the iteration. So a capture that is not made is in some
alternative of a choice, in an optional expression or in an expression whose
recovery skipped, and its type is `*T` (T6). A name captured more than once
holds the value of the last capture that matched; its type is the join of
theirs, and `nil` is in it unless one of the captures is always made. ∎

The clause of (T6) on recovered expressions is needed: without it,
`(x:"a" "b") #recover(skip="z") => $x.Start` passes the check of field access
on a receiver that may be `nil` (T7) and, on the input `z`, reads `.Start` of
`nil`.

## Lemma 7 (Variables)

In every parse, every variable holds a value of its declared type.

**Proof.** Lemmas 7 and 8 are proved together, by the induction of the
Setting. A parse starts with the initial values, which have the declared
types (T12). A binding binds the value of its expression only if the expression
completes (M8); its static type is assignable to the declared type (T12), and
by Lemma 8 for the expression, which runs in an environment that matches by
induction, the value has that type by Lemma 2. Scopes and backtracking only
restore earlier bindings. ∎

## Lemma 8 (Action Expressions)

Let an Action Expression $x$ of static type $X$ (T7) be evaluated in a
run-time environment that matches its typing environment. Then $x$ either
raises a run-time action error of the kinds listed in
[Run-Time Errors](#run-time-errors) below, or it gives a well-formed value $v$
with $v$ : $X$.

**Proof.** By induction on the evaluation of $x$. Each case assumes that the
operands gave values of their static types, as the induction hypothesis gives
whenever they did not raise an error.

- **Literals and references.** Integer, string and boolean literals and `nil`
  have their types. `$label` has the type of the capture by Lemma 6, a
  variable that of its declaration by Lemma 7, a lambda parameter, `$lhs`,
  `$rhs`, `$op` and `$value` the types of the environment.
- **Operators.** (T7) accepts `||`, `&&` and `!` on `bool`, arithmetic on two
  `int`, `+` on two `string` and ordering on two `int` or two `string`
  operands, and rejects `_` and other operands. So the operands have those
  kinds, and the result is the `int`, `string` or `bool` of the table. `/` and
  `%` raise an error on a zero divisor. An equality has a `bool` result; it
  raises an error only for operands whose static types leave the comparison
  open, a type that is not known or one that may be `nil`.
- **Members of nodes.** `x.Start`, `x.End`, `len(x)` give an `int` and
  `text(x)` a `string`; (T7) accepts them on string, tuple and list nodes,
  named node types and error values, `len` and `text` also on strings and
  `nil`, and `len` on list values, which are the kinds on which (M7) defines
  them. `x.Value`
  of a node of type `node[T]`, or of `N` over it, is the value of the node, of
  type $T$ by the relation; on an error value it is `nil`, and a join with a
  recovered member gives an optional type, which admits `nil`.
- **Field access.** The receiver's static type admits no `nil` (T7), so the
  receiver is a struct, a node or an error value. For a struct type, every
  member of the receiver's type declares the field, and the receiver is a
  struct of one of those types; a set field holds a value of its declared
  type, and an unset one reads the zero value, of that type by Lemma 3. The
  result has the type of the field in that member, which is in the join. A
  recovered member reads `nil`, which the optional result admits. The fields
  `expected` and `message` are read on a receiver of type `missing`, which is
  a `missing` value by the relation, and both hold strings (M1), of their type
  `string`. A receiver
  whose type is not known statically is not accepted by the checker for a
  field access; a capture read `x.y` of a grammar in JSON or a compiled
  grammar has the looser check of [Field Access](../language/actions.md#field-access)
  and may raise an error at run time.
- **Capture access.** `x.$y` on a node: every node type that $x$ may have
  records the capture $y$, and $x$ has one of them, so the capture holds `nil`
  or a value of its recorded type; the result is the join, optional when a
  capture may not be made.
- **Indexing.** $x$ is a list, a tuple or a node whose value is one, and the
  index an `int` (T7). The element of a list has the element type; a constant
  index into a tuple selects that element's type, and another index gives the
  type that (T7) gives the elements.
  An index outside the list or tuple raises an error.
- **Cast.** $x$`.(T)` gives $v$ when the check of $T$ succeeds on it, and then
  $v$ : $T$ by Lemma 1, since $v$ is well formed; otherwise it raises an error.
- **Constructors.** `S{F: e, ...}` sets fields whose values have static types
  assignable to the declared field types (T8), so by Lemma 2 each set field
  has its type, and every field without a zero value is set (T4): the struct
  has type `S` and is well formed. `[]T{...}` has elements of static types
  assignable to $T$, so the list has type `[]T`. A tuple literal has the tuple
  of the element types. `N{x}` has type `N` by Lemma 4.
- **List functions.** `list` and `concat` give lists of their arguments or
  their elements, each of a type in the join of (T7); `nil` and an error value
  contribute no element (M7). `map` gives the results of the function, of its
  result type by induction, each call in an environment that binds the
  parameter to an element of the element type. `filter` keeps elements of the
  input and has its element type; a result of the function that is not a
  `bool` raises an error. `foldl` and `foldr`: the accumulator type $A$ of
  (T9) admits the initial value, and the result of the function, called with
  an accumulator of type $A$ and an element, is assignable to $A$; by
  induction over the list, every accumulator has type $A$, and so does the
  result.
- **Lambdas.** A function is not a value and appears only as an argument of
  `foldl`, `foldr`, `map` and `filter` (T7), whose cases above treat it.

The values that the cases build hold only values of their operands, literals,
zero values and conversions, so they are well formed. ∎

## Lemma 9 (Inferred and Declared Types)

For every rule $r$, if the body or the action of $r$ gives a value of the type
that the checker computes for it from the types of the rules that $r$ calls,
then the value of $r$ has the type $T_r$ of $r$.

**Proof.** If $r$ declares $T_r$, the type of its value is assignable to $T_r$
or its value is converted to $T_r$ (T8, T11), and Lemma 2 or Lemma 4 gives
the claim. If $r$ declares none, $T_r$ is the result of inference (T9). An
acyclic rule is inferred after its callees, from their final types, so $T_r$
is the type of its body or action. For a recursive group, the iteration stops
only when no type changes, so each $T_r$ is the type that the body or action
of $r$ has under the final types of the group; a rule widened to `_` keeps `_`,
which every value has, and the others are computed again from it before the
iteration stops. A change in the order of the members of a join is not a change
of meaning (Lemma 2). ∎

## Lemma 10 (Operators Rules)

Every value that an Operators Rule produces has the rule's type, and `$lhs`,
`$rhs` and `$value` hold values of their types.

**Proof.** By induction on the operators that the call applies, in the order
they are applied. With a declared type $T$ and no whole-rule action, the value
of each operand line is assignable to $T$ (T10), so it has type $T$ by
Lemma 5, Lemma 8 and Lemma 2; an operator's `$lhs` and `$rhs` are values of
earlier lines or operators of the call, of type $T$ by induction, so the
environment of its action matches, and its result, or the tuple node of its
operands and operator part, is assignable to $T$. If $T$ is exactly `N`, each
operator value is converted to `N`, which Lemma 4 makes of type `N`, and each
operand line is assignable to `N` (M6). Without a declared type, the same
argument holds with the join of the lines, which every line is assignable to.
With a whole-rule action, let $W$ be the type of `$lhs` and `$rhs` (T10). The
type of every line, computed with `$lhs` and `$rhs` of type $W$, is assignable
to $W$: when $W$ is the folded type, a fixed point of the join of those
types, each is assignable to the join (T3); when $W$ is `node[_]`, the
widening requires it (T10); and every value has type `_` (T2). So, by
induction on the operators that the call applies, `$lhs` and `$rhs` hold
values of type $W$: they are values of earlier operand lines, whose types
do not depend on `$lhs` and `$rhs`, or of earlier operators, whose
environments matched by induction, so each such value has the type of its
line by Lemma 5 and Lemma 8, and so type $W$ by Lemma 2. An error value is a
node but not a value of `node[_]` (T2), so a widening to `node[_]` under the
weaker condition that every line produces a node would not keep this: a
line that recovers has a type with `error`, which is not assignable to
`node[_]`, and widens $W$ to `_`. `$value` is the folded value, the value of
a line, of the type of that line and so of the join of the lines (Lemma 2).
The result of the action is assignable to the declared type, or is the
rule's type when there is none. ∎

## Theorem (Type Soundness)

Let a grammar compile, which includes that the checker accepts it (T8). In
every parse of it, on every backend that evaluates as the language chapters
define:

1. every successful evaluation of a rule $r$ produces a well-formed value of
   its type $T_r$;
2. every capture, variable and struct field holds a value of its type, and
   every action and predicate runs in an environment that matches its typing
   environment;
3. every Action Expression that completes gives a well-formed value of its
   static type, and every run-time action error is one of those of
   [Run-Time Errors](#run-time-errors).

**Proof.** By induction on the order in which evaluations complete (Setting).
Take an evaluation of a call of $r$ that succeeds. If it reuses a result, the
result is the outcome of an earlier evaluation (M9), which satisfies the
theorem by the hypothesis. Otherwise its callees completed before it and
satisfy the theorem. For an ordinary rule, Lemma 5 gives the value of the body
and the captures; when the body succeeds, the action of $r$ runs with captures
of their types by Lemma 6, variables by Lemma 7 and the values of the rule's
scope, so its environment matches and Lemma 8 gives its result. Lemma 9 gives
$T_r$ for the value of $r$. For an Operators Rule, Lemma 10 gives it. The
predicates of the evaluation run in matching environments by the same lemmas,
and a predicate that raises an error fails (M7). Part 3 is Lemma 8. ∎

**Corollary (typed results).** The value that a parse returns for its start
rule $s$ has type $T_s$, and a typed parse that builds the typed form of that
value meets only values of the types that $T_s$ describes. A cast
`x.(T)` and a value of type `_` are the only places where a value of an
unknown type meets a type, and both are checked by Lemma 1 where the value is
used.

## Run-Time Errors

A Value Action or a Predicate Action of a grammar that compiles raises a
run-time error only in these cases. Each is a place where the specification
leaves a check to run time, and Lemma 8 shows that no other case can arise.

| Place | Error | Static condition |
|:--|:--|:--|
| A cast `x.(T)` | The value is not a `T` | Any cast that the checker accepts, which is when some value can have both types |
| `/` and `%` | Division by zero | Any divisor |
| `xs[i]` | The index is outside the list or tuple, or a dynamically typed index is not an `int` | Any index |
| `==` and `!=` | Two values that cannot be compared, such as two nodes or a node and a string | An operand whose type is not known, or may be `nil` |
| `filter(xs, f)` | The function's result is not a `bool` | A result whose type is not known; the checker of the reference implementation is stricter and rejects a result of any type other than `bool`, so the error does not arise there |
| `x.Name`, `x.$y` | A struct that lacks the field, a struct read for a capture, or `nil` | A receiver whose type is not known, as in a capture read `x.y` of a grammar in JSON or a compiled grammar |

The value of a rule of unknown type `_` is itself not an error: it is a value
of every type that `_` stands for, and a cast is required where it is used
with a more precise type (T8).

## Correspondence

| Claim | Proved by |
|:--|:--|
| Assignability, joins and meets keep the values of their types | Lemma 2 |
| A cast checks exactly the meaning of its type | Lemma 1 |
| A struct field always reads as a value of its declared type | Lemma 3, Lemma 8 |
| A named node type holds only nodes whose value has its underlying type | Lemma 4 |
| Each Rule Expression produces a value of its type | Lemma 5 |
| A capture that may not be made has an optional type | Lemma 6 |
| A variable always holds a value of its declared type | Lemma 7 |
| An Action Expression gives a value of its type or one of the listed run-time errors | Lemma 8 |
| Inferred types, including the widenings, are sound | Lemma 9 |
| Operators Rules produce values of their types | Lemma 10 |

## What the Proof Does Not Show

- **Captures in recovered expressions.** The checker does not yet type a
  capture inside a recovered expression as optional, which (T6) requires, so
  the grammar `(x:"a" "b") #recover(skip="z") => $x.Start` compiles and raises
  a run-time error on the input `z`. The theorem holds of the checker that
  follows (T6).
- **The checker as implemented.** The proof shows that the rules of the
  specification are sound. It does not show that the checker computes the
  types those rules give; the inference of recursive types relies on
  iteration until no type changes, whose termination the widenings ensure.
- **Typed APIs.** The corollary shows that the values of a parse have the
  types of their rules. That the types of the generated Go and TypeScript
  typed APIs represent those types, so that building the typed form never
  fails, is a property of the [generated parsers](../runtime/generated-parsers.md)
  that this proof does not cover.
- **Grammars that are not checked.** [Default values](../language/actions.md#default-values)
  drop the actions and the declared types, and a compiled grammar that is
  loaded is not checked again; the proof covers a compiled grammar as the
  checker accepted it when it was compiled.
