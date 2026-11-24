# Termination

Arata Furukawa · Last revised 2026-11-24

This proof shows that every parse of a grammar that passes the checks
terminates on every finite input, as
[Termination](../runtime/parsing.md#termination) states, and that the static
analyses which the checks, the runtime and the classifier rely on are sound for
the meaning that the specification gives them: nullability, the nodes that
always succeed or always fail, the rules that can match, the first terminals,
the left calls with the cycles and leaders built from them, the check of the
bindings before a left-recursive call, and the variables that a rule reads.

Each analysis is defined as a property of the nodes of a grammar, and each is
used for what it promises about a parse: a nullable node is the only kind that
can succeed without consuming input, a node that always succeeds never fails,
and the left calls of a rule are every rule that it can call before it consumes
input. The lemmas show these promises. The theorem then uses the left calls and
the leaders to bound the calls that are active at one position, and the rules
of repetitions, growths, recoveries and Operators Rules to bound the parts of
each evaluation, so that a parse is a finite tree of evaluations.

The proof is about the evaluation that the language chapters define, with the
reuse that the runtime specification allows: it holds for every backend that
evaluates as they define, in batch parses, recognition, typed parses, prefix
parses, streams and [Documents](../runtime/documents.md), with and without the
insertion of missing tokens.

## Definitions From the Specification

The proof cites the following rules, and no other rule about evaluation or the
analyses.

### Evaluation

- **(E1) Evaluation.** An expression is evaluated at a position; it succeeds
  with an end position, which is where the next expression of a sequence
  continues, or it fails, and a failure has no effect on the position, the
  captures and the variables
  ([Evaluation](../language/parser-expressions.md#evaluation)). An evaluation
  can also end the parse with an error: a run-time error of an action, an
  exceeded limit, a cancellation or a `MissingTokenLimitError`. Such an error
  is not a failure: no construct absorbs it
  ([Cancellation and Compute Limits](../runtime/parsing.md#cancellation-and-compute-limits)).
- **(E2) Combinators.** A sequence evaluates its items from left to right, each
  where the one before it ended, and fails at the first item that fails. A
  choice evaluates its alternatives from left to right and succeeds with the
  first that succeeds. An optional evaluates its body once. A lookahead
  evaluates its body at the current position and returns to it; the bindings
  made in a positive lookahead stay in effect after it, and those made in a
  negative one do not
  ([Combinators](../language/parser-expressions.md#combinators),
  [Lookahead](../language/parser-expressions.md#lookahead)).
- **(E3) Cuts.** Once an alternative passes a cut, the innermost enclosing
  choice, optional or repetition of the same rule does not try another
  alternative: a repetition or an optional whose iteration fails after a cut
  fails. A cut does not reach past a lookahead or the rule that contains it
  ([Cut](../language/parser-expressions.md#cut)), and one in the annotated
  expression or the skip of a `#recover` commits the innermost choice around
  the recovery ([`#recover`](../language/pragmas.md#recover)).
- **(E4) Repetition.** A repetition evaluates its element as often as it can,
  up to its maximum, and stops at the first iteration that fails or that
  succeeds without consuming input; that iteration counts if it succeeded, and
  the minimum is checked after stopping
  ([Repetition](../language/parser-expressions.md#repetition)).
- **(E5) Recovery.** A `#recover(skip=e)` evaluates its expression; if that
  fails, it evaluates `e` from the position where the expression started, and
  recovers only if `e` matches and consumes input; otherwise the whole fails
  ([`#recover`](../language/pragmas.md#recover)).
- **(E6) Insertion.** In a parse with missing-token insertion, a `#missing`
  literal that fails may insert its text, consuming no input. An insertion does
  not change the progress of a repetition or of a growth, and each site inserts
  at most once at a position on the current path
  ([`#missing`](../language/pragmas.md#missing)).
- **(E7) Calls.** A call evaluates the body of the rule at the current position,
  in a scope of its own: the bindings that the body makes end with the call
  ([Rule Calls](../language/parser-expressions.md#rule-calls),
  [Variables](../language/predicates.md#variables)). A `Nest` is an inlined
  call of a rule, which nests like a call, and a `Nest` of a rule occurs only
  in rules outside the component of that rule
  ([Nodes](../core-ir/nodes.md#rule-nodes),
  [Invariants](../core-ir/invariants.md)). The value-free twin of a rule
  evaluates as the rule does and has its facts
  ([Rule Facts](../core-ir/attributes.md#rule-facts)).
- **(E8) Growth.** A call of a leader $L$ at a position $p$ grows a seed. While
  $L$ is evaluated at $p$, every call of $L$ at $p$, whatever the values of
  variables, does not evaluate the body: it fails while the seed is computed,
  and afterwards succeeds with the current result. The body is evaluated again
  as long as it succeeds and ends farther than the current result; the first
  evaluation that does not ends the growth. A rule that is not a leader is
  evaluated once for a call
  ([Left Recursion](../language/parser-expressions.md#left-recursion)). Under
  insertion, later steps of a growth must advance the input (E6).
- **(E9) Operators Rules.** An Operators Rule evaluates its `skip` line, the
  operand lines as an ordered choice, the prefix operator lines at the start of
  an operand, and the infix and postfix operator lines after an operand, and
  selects among the operator parts that match. A prefix or postfix part that
  matches the empty string is not a candidate. An infix operator applied
  without consuming input ends the expression after it is applied, and the
  right operand of an operator part that consumed no input does not apply such
  a part itself until it has consumed input. An operator whose right operand
  cannot be parsed is not applied, and the expression ends before it, or, for a
  prefix operator, the operand lines are tried at the same position
  ([Parsing Rules](../language/operators-rule.md#parsing-rules)). Every
  evaluation of a line starts at a position that the expression has reached.
- **(E10) Actions.** Actions, predicates and bindings have no recursion: a
  lambda is only an argument of a built-in function, which visits a list once
  ([Termination](../runtime/parsing.md#termination),
  [Built-In Functions](../language/actions.md#built-in-functions)). They call
  no rule.
- **(E11) Reuse.** A parse may answer a call from a memo entry, a walk of a
  repetition from a record, and iterations of a Document repetition from a run
  record, instead of evaluating them
  ([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules),
  [Skip Runs](../runtime/parsing.md#skip-runs),
  [Run Sites](../runtime/parsing.md#run-sites),
  [Scan Sites](../runtime/parsing.md#scan-sites),
  [Reuse Model](../runtime/documents.md#reuse-model)). What it reuses is the
  outcome of an earlier completed evaluation of the same call or iterations at
  the same position, in the same parse or in an earlier parse of the same
  Document, shifted by the edits since where its position moved; a shift moves
  the start and the end of an outcome alike. A reuse evaluates nothing.

### Analyses

- **(N) Nullability.** Nullable is the least fixed point of the rules of
  [Nullability](../core-ir/attributes.md#nullability), and NullableIns is the
  same with a `Missing` nullable. NullableAtEnd, NullableBeforeEnd and CanMatch
  are the least fixed points of [Rule Facts](../core-ir/attributes.md#rule-facts).
- **(S) Always succeeds and always fails.** The rules of
  [Rule Facts](../core-ir/attributes.md#rule-facts) for a node that always
  succeeds, given the rules proven to succeed, and for a node that always
  fails. A rule Succeeds when it is off every left-recursion cycle and its body
  always succeeds; a loose cut is a cut outside nested choices, options,
  repetitions, lookaheads and calls.
- **(F) First terminals.** The rules of
  [First Terminals](../core-ir/attributes.md#first-terminals).
- **(LC) Left calls.** The left calls of a node are those of its call (`Call`
  or `Nest`); of a sequence's items up to the first that is not nullable; of a
  choice's alternatives up to the first that always succeeds; of a `Recover`'s
  body, and of its skip unless the body always succeeds; of a repetition's
  body unless its maximum is 0; of the bodies of `Opt`, `Look`, `Text`,
  `Drop`, `Capture`, `Label`, `Stream` and `Terminal`; and, for an
  `OperatorsRule`, of its skip line, its operand lines up to the first that
  always succeeds, its prefix operator lines, and, when some operand line is
  nullable, its infix and postfix operator lines. Under insertion, NullableIns
  takes the place of Nullable ([Rule Facts](../core-ir/attributes.md#rule-facts)).
  The last clause, on the infix and postfix lines, is the rule that this proof
  uses; [Rule Facts](../core-ir/attributes.md#rule-facts) does not yet state
  it, and the implementation does not yet follow it (see
  [What the Proof Does Not Show](#what-the-proof-does-not-show)).
- **(CY) Cycles and leaders.** A rule's cycle is its component of the graph of
  left calls when the component has several rules or the rule has a left call
  of itself. While some cycle of left calls among the rules of a component
  passes through no leader, the rule declared first among the rules on such
  cycles becomes a leader ([Rule Facts](../core-ir/attributes.md#rule-facts)).
  A parse uses the facts of its mode, without or with insertion
  ([Left Recursion](../language/parser-expressions.md#left-recursion)).
- **(B) Bindings before a left call.** A binding of a variable that the rules
  of a cycle read, directly or through the rules they call, is a compile-time
  error if it is in the body of a rule of the cycle, in the part that precedes
  a call into the cycle that may run before consuming input and may match
  without consuming input: in any alternative, repetition, optional or
  `#recover(skip=...)`, all of a positive lookahead, and the part of a negative
  lookahead before a call inside it. A binding in a called rule never counts
  ([Left Recursion](../language/parser-expressions.md#left-recursion)).
- **(V) Variables.** The Variables of a rule are the variables that the action
  expressions of the rule or of a rule it calls read
  ([Rule Facts](../core-ir/attributes.md#rule-facts)).

## Setting

Fix a grammar that passes the checks of
[Errors in a Grammar](../language/grammar-files.md#errors-in-a-grammar), a
start rule, a position unit and an input of length $n$, so that the positions
are $0, \dots, n$. The analyses are computed on the rules of the grammar as
written, and the parse evaluates the nodes of its rules with the meanings of
[Nodes](../core-ir/nodes.md), which are those of the language chapters. A
parse either inserts missing tokens or not; "nullable" below means Nullable in
a parse without insertion and NullableIns in one with it, and the left calls,
cycles and leaders are those of its mode.

An **evaluation** is one evaluation of a node at a position, from its start to
its outcome. The evaluations that it starts directly, for its parts, are its
**parts**; a call's parts are the evaluations of the rule's body, one or, for a
leader, the steps of its growth. An evaluation **completes** when it has an
outcome. An evaluation is **active** from its start until it completes, and the
active evaluations at any moment form a chain, the **stack**, each started by
the one below it. A call **makes** a call $y$ when $y$ is the call itself or a
part, or a part of a part and so on, of the evaluation of its body that is not
inside another call; an evaluation of a node makes the calls among it and its
parts, transitively, that are not inside another call.

Order the evaluations of all the parses of a Document, or of the one parse
outside a Document, by the time at which they complete. Every part of an
evaluation completes before it, and by (E11) every outcome that a reuse gives,
and by (E8) every current result that a call of a leader reads, is the outcome
of an evaluation that completed before. The lemmas that are about outcomes are
proved by induction on this order; each holds for a completed evaluation once it
holds for every evaluation that completed before. Reuse answers are covered
because their outcomes are outcomes of earlier evaluations.

The proof does not count on a limit: a parse with no nesting limit, no compute
limit and no memory limit is covered. A reader or callback that the caller
supplies is assumed to return, as
[Termination](../runtime/parsing.md#termination) excepts it.

## Lemma 1 (Positions)

Every part of an evaluation that starts at $p$ starts at a position at least
$p$, and a success ends at a position at least its start.

*Proof.* By induction on the order. A terminal advances the position or fails;
an anchor, a predicate, a binding, a cut and a lookahead end where they start
(E2). A sequence starts its first item at its start and each later one where
the one before ended, which by induction is no earlier. A choice, an optional,
a lookahead and a recovery start their parts at their start, the skip of a
recovery included (E5); a repetition starts each iteration where the one
before ended (E4). A call evaluates the body at its start (E7, E8). An
Operators Rule evaluates each line at a position that the expression has reached
(E9). A reuse ends where the reused evaluation ended, moved with its start
(E11), and an insertion consumes nothing (E6). So no part starts before $p$,
and an evaluation ends where its last part, or its own terminal, leaves it. $\square$

## Lemma 2 (Nullability)

Let an evaluation of a node $e$ at $p$ succeed and end at $p$. Then $e$ is
nullable. If the parse inserts no token, $e$ is NullableAtEnd when $p = n$ and
NullableBeforeEnd when $p < n$. If an evaluation of $e$ succeeds anywhere, $e$
is CanMatch.

*Proof.* Lemmas 2 and 3 are proved together, by induction on the order, on the
kind of $e$; each case uses the two lemmas only for evaluations that completed
before. The fixed points of (N) contain every node that their rules give from
nodes they contain.

- A nonempty literal, a class, `.`, a `ClassRun` and a `Scan` with a positive
  minimum consume input when they succeed, so they do not end at $p$. An empty
  literal, an anchor, a cut, `Succeed`, a lookahead, an optional, a predicate,
  a binding and a `Scan` with minimum 0 are nullable by (N). A `FailNode` never
  succeeds.
- A sequence that ends at $p$ succeeded in every item, and by Lemma 1 each item
  started and ended at $p$; by induction each is nullable, so the sequence is. A
  choice succeeded with one alternative that ended at $p$, which is nullable.
- A repetition that ends at $p$: by (E4) its first iteration, if it has one,
  succeeded at $p$ and ended at $p$, so the repetition stopped after it with one
  iteration. Its minimum is therefore 0, or 1 with a body that, by induction, is
  nullable, and the repetition is nullable by (N). A repetition with minimum 2
  or more never ends where it starts.
- A recovery that ends at $p$ succeeded through its expression, since a skip
  that recovers consumes input (E5); by induction the expression is nullable,
  and so is the recovery. `Text`, `Drop`, `Capture`, `Label`, `Terminal`,
  `Stream` and `Nest` end where their bodies end.
- A `Missing` that ends at $p$ either matched its body, which is then nullable,
  or inserted, which happens only in a parse with insertion, where `Missing` is
  NullableIns.
- A call ends where its outcome ends. An evaluated body, a current result read
  from a growth (E8) and a reused outcome (E11) are each the outcome of an
  earlier evaluation of the rule's body, or of a call of the rule, at $p$ or,
  shifted, at a position where it also ended at its start. By induction the
  body is nullable, and so is the call.
- An Operators Rule that ends at $p$: an applied prefix or postfix part consumes
  input, and an infix operator consumes input or, applied without consuming,
  has an empty right operand (E9). So its first operand was matched at $p$
  after a `skip` line that consumed nothing, by an operand line that succeeded
  at $p$ and ended there, which by induction is nullable; so is the rule.

At $p = n$ in a parse without insertion, the same cases give NullableAtEnd with
its own rules: `$$` succeeds there, a positive lookahead whose body succeeded
there without consuming input has a body that is NullableAtEnd by induction,
and a negative lookahead that succeeded has a body that failed there, which by
Lemma 3 is not a node that always succeeds; so the body does not certainly
succeed there, under any reading of that phrase that a node which certainly
succeeds at the end does not fail there. At $p < n$ the cases give
NullableBeforeEnd, where `$$` fails and a positive lookahead is nullable
whatever its body. With insertion a `Missing` can succeed at the end without
consuming input, which NullableAtEnd and NullableBeforeEnd do not count, so
these two facts hold for parses without insertion only.

CanMatch: a node that succeeds has parts that succeeded where its rules require
them, and a negative lookahead that succeeds has a body that failed, which by
Lemma 3 is not a node that always succeeds; a `Missing` and a `Recover` are
CanMatch by (N), and a call succeeds with an outcome of an earlier evaluation of
its rule's body. By induction each such node is CanMatch. $\square$

## Lemma 3 (Sure Success and Failure)

Let an evaluation of a node $e$ complete. If $e$ always succeeds by (S), the
evaluation succeeds; if $e$ always fails, it fails.

*Proof.* Together with Lemma 2, by induction on the order, on the kind of $e$.
Ending the parse with an error is neither (E1), so the claim is about
evaluations that have an outcome.

- An empty literal, `Succeed` and a cut succeed; a `FailNode` fails.
- A call of a rule $R$ that Succeeds: $R$ is off every left-recursion cycle, so
  it is no leader, and by (E8) the call evaluates its body once, or a reuse
  answers it with the outcome of an earlier evaluation of the same call (E11).
  The body always succeeds, so by induction that evaluation succeeded, and the
  call succeeds.
- A sequence whose items all always succeed succeeds item by item. A sequence
  with an item that always fails fails at an earlier item or at that item.
- A choice with an alternative $a_j$ that always succeeds, and no loose cut in
  an earlier alternative: each earlier alternative that fails passed no cut
  that commits this choice, since a cut that commits it is a loose cut of that
  alternative (E3); so the choice tries the next one, and if it reaches $a_j$,
  $a_j$ succeeds by induction. A choice whose alternatives all always fail
  fails.
- An optional, and a repetition with minimum 0, whose body has no loose cut:
  a failure of the body ends it with success, since only a cut that the body
  passed makes it fail (E3, E4); if the body always succeeds, no iteration
  fails. A repetition with maximum 0 and minimum 0 evaluates nothing and
  succeeds. A repetition with minimum 1 whose body always succeeds has a first
  iteration that succeeds, and no later iteration fails, so its minimum is met.
- A positive lookahead succeeds if its body succeeds and fails if it fails; a
  negative one the reverse (E2). By induction the claims for lookaheads follow
  from those of the body.
- `Capture`, `Text`, `Drop`, `Label`, `Stream` and `Terminal` succeed or fail
  with their bodies. A `Recover` whose expression always succeeds succeeds with
  it. $\square$

## Lemma 4 (Left Calls)

Let an evaluation of a node $e$ start at $p$, and let it make a call $y$ that
starts at $p$. Then the rule of $y$ is among the left calls of $e$.

So a call that a rule's body makes before the position moves from where the
body started calls one of the rule's left calls, the facts that the classifier
and the leaders are built on.

*Proof.* By induction on the structure of $e$. By Lemma 1, $y$ lies inside a part
of the evaluation that starts at $p$, and every evaluation between it and $y$
starts at $p$.

- `Call` and `Nest`: the only call that the evaluation makes is itself, and its
  rule is its left call by (LC).
- A sequence: $y$ lies inside the evaluation of an item $i_j$ that starts at
  $p$. The items before it succeeded, and by Lemma 1 each started and ended at
  $p$, so by Lemma 2 each is nullable, and (LC) includes the left calls of
  $i_j$, which by induction include the rule of $y$.
- A choice: $y$ lies inside an alternative $a_j$, which the choice tried
  because every earlier alternative failed (E2). By Lemma 3 no earlier
  alternative always succeeds, so (LC) includes the left calls of $a_j$.
- A repetition: an iteration after the first starts where an iteration that
  consumed input ended, since an iteration that consumes nothing ends the
  repetition (E4), so by Lemma 1 it starts after $p$. So $y$ lies inside the
  first iteration, and the maximum is not 0. A walk or iterations that a reuse
  answers make no call (E11).
- A recovery: $y$ lies inside its expression, or inside its skip, which the
  recovery evaluates only after the expression failed (E5), so by Lemma 3 the
  expression does not always succeed.
- `Opt`, `Look`, `Text`, `Drop`, `Capture`, `Label`, `Stream` and `Terminal`:
  $y$ lies inside the body. A terminal, an anchor, a predicate, a binding, a
  cut, `Missing`, `Scan` and `ClassRun` make no call (E10).
- An Operators Rule: the lines that the evaluation evaluates at $p$ are its
  `skip` line, its operand lines, which are an ordered choice, so that by
  Lemma 3 no line before the one tried always succeeds, its prefix operator
  lines, and its infix and postfix operator lines when they are tried at $p$.
  These are tried after an operand, and the operand ended at $p$ only if it was
  matched by an operand line that succeeded at $p$ and ended there, after a
  `skip` line that consumed nothing, since an applied prefix part consumes
  input (E9). By Lemma 2 that operand line is nullable, so (LC) includes the
  infix and postfix lines. This holds for the right operand of an operator
  part as for the first operand: its lines are the lines of the same rule, and
  it starts at $p$ only after a part that consumed nothing. A call that names a
  level evaluates the lines of the rule that the level allows, a subset. $\square$

The last case is where the infix and postfix clause of (LC) is needed. Without
it, `rule e = #operators { operand "" level a { postfix x } }` with
`rule x = e "+"` has no left call from `e` to `x`, while an evaluation of `e` at
$p$ calls `x` at $p$, which calls `e` at $p$ again.

## Lemma 5 (Leaders Cover the Cycles)

Every cycle of the graph of left calls passes through a leader.

*Proof.* A cycle of the graph lies within one of its components. The procedure
of (CY) makes a new leader in each round, among finitely many rules, so it
ends, and it ends only when no cycle of left calls among the rules of a
component passes through no leader. A rule with a left call of itself is such
a cycle, and so becomes a leader. $\square$

## Lemma 6 (Calls at One Position)

At any moment, let $C_1, \dots, C_m$ be the calls on the stack that start at
$p$, from the bottom up. Then for $i < m$ the rule of $C_{i+1}$ is a left call
of the rule of $C_i$, each leader is the rule of at most one of
$C_1, \dots, C_{m-1}$, and $m \le (|\Lambda| + 1)(|G| + 1)$, where $G$ is the
set of the rules and $\Lambda$ that of the leaders.

*Proof.* By Lemma 1 the starts do not decrease up the stack, so no call between
$C_i$ and $C_{i+1}$ starts elsewhere, and $C_{i+1}$ is a call that the
evaluation of the body of $C_i$, which starts at $p$, makes. By Lemma 4 its
rule is a left call of the body, which (LC) takes as the left calls of the rule.
A twin counts as its rule (E7).

A call $C_i$ with $i < m$ has a call above it, so it is evaluating its body. If a
call of a leader $L$ at $p$ is evaluating, every later call of $L$ at $p$ while
it is active reads the growth and evaluates nothing (E8), so it has no call
above it. A `Nest` of $L$ cannot be among the later ones: by Lemma 4 the calls
between them would give a way of left calls from $L$ to the rule $S$ that
contains the `Nest`, and the `Nest` a left call from $S$ to $L$, so $S$ would be
in the component of $L$, where (E7) puts no `Nest` of $L$. So $L$ is the rule of at most one of $C_1, \dots, C_{m-1}$.

The calls of leaders among $C_1, \dots, C_{m-1}$, at most $|\Lambda|$, split the
others into at most $|\Lambda| + 1$ runs. If two calls of one run had the same
rule, the rules of the calls from the first to the second would form a cycle of
left calls through no leader, against Lemma 5. So each run has at most $|G|$
calls, and $m - 1 \le |\Lambda| + (|\Lambda| + 1)|G|$. $\square$

## Lemma 7 (Depth)

The stack holds at most $D\,(n + 1)$ evaluations, for a constant $D$ of the
grammar.

*Proof.* Fix a position $p$ and count the evaluations on the stack that start at
$p$. The calls among them are at most $N = (|\Lambda| + 1)(|G| + 1)$ by
Lemma 6. Every other one is an evaluation of a node of the body of the nearest
call below it, and the evaluations between two calls on the stack, or above
the last, are nested parts of one body evaluation: a path of nodes of the body,
except that the evaluation of an Operators Rule holds the evaluations of its
right operands, which hold the lines again. A right operand that starts at $p$
follows an operator part that consumed nothing, and inside it no such part is
applied until it has consumed input (E9), so the right operands that start at
$p$ nest at most one deep inside another evaluation at $p$ of the same rule.
With $h$ the greatest number of nodes on a path of a rule body, the evaluations
at $p$ between two calls are at most $2(h + 1)$. They lie above at most $N + 1$
calls: those at $p$ and the one below them. So at most $N + 2(h + 1)(N + 1)$
evaluations start at $p$, and there are $n + 1$ positions. $\square$

## Lemma 8 (Finitely Many Parts)

Every evaluation at $p$ starts at most $c\,(n - p + 2)$ parts, for a constant
$c$ of the grammar, and does finitely much work besides them.

*Proof.* A sequence starts one part for each item and a choice one for each
alternative it tries; an optional, a lookahead, a `Text`, `Drop`, `Capture`,
`Label`, `Stream`, `Terminal` and `Nest` one; a recovery two (E5); a call of a
rule that is not a leader one (E7). A call of a leader starts the seed and the
steps of its growth, and each step but the last ends farther than the one
before, at a position at most $n$ (E8), so at most $n - p + 2$. A repetition
starts its iterations, and each but the last consumes input (E4), so at most
$n - p + 1$; a walk that takes a record ends there (E11). An Operators Rule
looks for an operand or an operator at a sequence of positions. At an operand
position it evaluates its `skip` line, its prefix lines and its operand lines,
the operand lines at most twice, since they are tried again only after a
prefix that is not applied (E9). At an operator position it evaluates its
`skip` line and each infix and postfix line once to select among the parts that
match. An applied prefix or postfix part consumes input, an applied infix part
consumes input or ends the expression, and an operator that is not applied ends
it (E9), so the positions increase, apart from the last, and the evaluation
starts at most a constant times $n - p + 2$ parts, its right operands
included. A call answered by a reuse or a
growth, and a walk answered by a record, start none.

A literal examines at most its length; a `Scan` and a `ClassRun` at most the
$n - p$ characters after $p$, each decoded from at most four bytes; an anchor,
a cut and an insertion a constant (E6). An action, a predicate or a binding is a
finite expression, which evaluates each node once, except the body of a lambda,
which a built-in function evaluates once for each element of a list (E10); the
values are built in finitely many steps, so by induction on the expression they
are finite, and so is the work. A reuse brings a finite outcome up to date, at
the work that [Work of a Document Parse](../runtime/documents.md#work-of-a-document-parse)
bounds in a Document. $\square$

## Theorem (Termination)

Every parse of a grammar that passes the checks, on a finite input, ends: with
its outcome, or with an error that ends it (E1).

*Proof.* The evaluations that a parse starts form a tree, in which the parts of
an evaluation are its children; the root is the evaluation of the start rule.
By Lemma 8 every evaluation has finitely many children and does finitely much
work besides them. If the parse did not end, it would do infinitely much work,
so it would start infinitely many evaluations, and the tree would be infinite.
A tree in which every node has finitely many children and which is infinite has
an infinite branch (König's lemma): evaluations $x_0, x_1, \dots$, each a part
of the one before. A part starts while the evaluation that starts it is active,
so when $x_k$ starts, $x_0, \dots, x_k$ are all on the stack, and the stack
holds more than $k$ evaluations for every $k$. This contradicts Lemma 7. The
end-of-input check, the publication of the outcome and the work of a Document
beyond its evaluations are finite steps after it. $\square$

The proof uses no limit. A [nesting limit](../runtime/parsing.md#limits), a
compute limit or a memory limit can end a parse sooner, with an error, and
Lemma 7 shows that a grammar that passes the checks needs no more than
$D\,(n + 1)$ of nesting. A parse that reads a reader or calls a callback ends
if each of those calls returns.

## Corollary (Work Under a Compute Limit)

A parse under a finite compute limit $B$ does at most $c\,(B + 1)$ work before
it ends, for a constant $c$ of the grammar and the backend, whether or not the
grammar passes the checks of the left calls.

*Proof.* Every unit of a parse is reserved before the work it covers, and the
parse is interrupted when a reservation would exceed $B$
([Checks and Interruptions](../runtime/parsing.md#checks-and-interruptions)).
Every other piece of work is at most a constant of the grammar for each event
([Compute Units](../runtime/parsing.md#compute-units)): every call is an event,
so a recursion that never consumes input is stopped by the limit like any other
work. The parse ends after at most $B + 1$ events and a constant of work for
each. A Document parse adds the work of the Document, which
[Work of a Document Parse](../runtime/documents.md#work-of-a-document-parse)
bounds separately. $\square$

## Lemma 9 (Environment of a Growth)

Let a call $C$ of a leader $L$ grow at $p$, and let a call $C'$ of $L$ at $p$
read its seed or current result. Then each variable that the rules of the cycle
of $L$ read, directly or through the rules they call, has the same value at $C'$
as at $C$.

This is what the check (B) is for: the current result was computed in the
environment of $C$, and $C'$ reads it whatever its own
([Interaction With Memoization](../language/predicates.md#interaction-with-memoization)).

*Proof.* $C'$ is above $C$ on the stack, and by Lemma 6 the calls at $p$
between them, $C = C_i, \dots, C_j = C'$, each make the next, with rules that
are left calls of the one before. With $C'$ a call of $L$, the rules of
$C_i, \dots, C_{j-1}$ lie on a cycle of left calls through $L$, so in the cycle
of $L$.

A variable has the value of its nearest binding in effect (E7). The bindings in
effect at $C'$ that were not in effect at $C$ were made while the bodies of
$C_i, \dots, C_{j-1}$ were evaluated, since a binding in a call that returned
ended with it (E7), one in a part that failed was undone (E1), and one in a
negative lookahead that ended was undone (E2). Take such a binding, made in the
body of the rule of $C_k$, before $C_{k+1}$ started. Follow the nodes of that
body from its root to the node of $C_{k+1}$. At a sequence, the items before the
one that holds the call succeeded and, by Lemma 1, ended at $p$, so by Lemma 2
they are nullable: they are the part that precedes the call and may match
without consuming input. At a choice, the alternatives before failed, and at a
recovery whose skip holds the call, the expression failed, so the bindings in
them were undone. In a repetition the call is in the first iteration (Lemma 4).
In a lookahead that holds the call, the bindings before the call are those of
its expression before it. So the binding is in the part that (B) counts for the
call $C_{k+1}$, a call into the cycle that runs before consuming input, in the
body of a rule of the cycle. (B) makes it a compile-time error if its variable
is one that the cycle reads, so it binds no such variable. $\square$

## Lemma 10 (Variables of a Rule)

Let two evaluations of a call of a rule $R$ at $p$, without reuse, start in
states that differ only in the values of variables outside the Variables of
$R$. Then they have the same outcome.

So a memo key that holds the values of the Variables of $R$, as
[Interaction With Memoization](../language/predicates.md#interaction-with-memoization)
requires, holds everything of the variables that the outcome depends on.

*Proof.* Only action expressions read variables, and those of the body of $R$
and of the rules it calls read only variables among the Variables of $R$ (V);
the Variables of a rule that $R$ calls are among them. The two evaluations take
the same steps as long as, at corresponding steps, the variables among the
Variables of $R$ have the same values. That holds at the start, and a step
keeps it: a binding binds the value of an expression that reads only such
variables, so the same value in both, and a call starts the callee with values
that agree on its own Variables. A growth at a position, and the current result
that a call reads, are the same in both, since they are computed by steps that
agree. By induction on the steps the outcomes are equal. $\square$

## Lemma 11 (First Terminals)

Let the node $e$ have a first terminal $t$ by (F), and let the character at
$p$, or the end of the input at $p = n$, not satisfy $t$. Then every evaluation
of $e$ at $p$ fails, after evaluating only the nodes on the way from $e$ to $t$
that (F) follows.

So the guard of an alternative, which skips it when the next character does not
satisfy its first terminal ([Nodes](../core-ir/nodes.md#combining-nodes)), is a
shortcut of evaluating it.

*Proof.* (F) follows the first item of a sequence, the bodies of `Capture`,
`Text`, `Drop`, `Nest` and `Terminal`, and calls of rules that are neither
leaders nor Operators Rules, and ends at a literal or a class. Each step goes to
a left call or a node of the same body, so a way that returned to a rule would
be a cycle of left calls through no leader, against Lemma 5; the way is finite.
By induction on its length, and on the order for the calls: a literal whose
first code point, in either case for a case-insensitive ASCII letter, is not
the character, and a class or the first class of a `ClassRun` that does not
contain it, fail at $p$, and so at the end of the input. A sequence whose first
item fails fails without evaluating the others (E2). `Capture`, `Text`, `Drop`
and `Terminal` fail with their bodies. A call of a rule that is not a leader
evaluates its body once (E7), which fails by induction; a `Nest` evaluates the
body of its rule, which the proof of Lemma 6 shows is not growing at $p$.
$\square$

The lemma is about evaluations. A call that a reuse answers has the outcome
that evaluating it would give, outside a parse with insertion, by the theorems
of the [Memoization](memoization.md) and [Documents](documents.md) proofs, and
so fails as well.

## Correspondence

| Analysis or rule | Where it is used | Lemma |
|:--|:--|:--|
| Nullable, NullableIns | Left calls, the check of bindings, the classifier | 2 |
| NullableAtEnd, NullableBeforeEnd, CanMatch | The classifier | 2 |
| Always succeeds, always fails, Succeeds | Left calls, the alternatives that a choice can reach | 3 |
| First terminals, guards | Skipping an alternative by the next character | 11 |
| Left calls | Cycles and leaders, the classifier | 4 |
| Cycles and leaders | The growth of left recursion | 5, 6 |
| The check of bindings before a left call | The value that a growth gives every call | 9 |
| Variables | Memo keys | 10 |

## What the Proof Does Not Show

**The left calls of Operators Rules.** The clause of (LC) on the infix and
postfix operator lines is the rule that this proof needs and uses.
[Rule Facts](../core-ir/attributes.md#rule-facts) does not state it yet, and the
implementation computes the left calls of an Operators Rule from its `skip`
line, its operand lines and its prefix operator lines only. Until both follow
it, a grammar such as the one after Lemma 4 is not grown as a left recursion:
an evaluation of `e` at a position calls itself there without end, and only
the nesting limit or the compute limit stops it (Corollary). The theorem is
proved for the left calls of (LC).

**NullableAtEnd and NullableBeforeEnd under insertion.** Lemma 2 shows these
two facts for parses without insertion only. Their rules count no insertion,
and a `Missing` can succeed without consuming input at any position of a parse
with insertion. The rules also leave "certainly succeeds there" undefined for a
negative lookahead; Lemma 2 holds under every reading in which a body that
certainly succeeds at a position does not fail there, the reading "always
succeeds" among them.

**The implementation of the analyses.** The lemmas are about the analyses as
the specification defines them; that the compiler computes those definitions
is not shown here.

**Cost.** The theorem bounds no time: as
[Termination](../runtime/parsing.md#termination) states, no bound that is a
polynomial in $n$ holds for every grammar. The
[Linear Cost](linear-cost.md) proof bounds the cost under the conditions of the
classifier.
