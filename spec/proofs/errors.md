# Errors

Arata Furukawa · Last revised 2026-11-25

This proof shows that the errors of a parse are a function of the grammar, the
start rule, the position unit, the input and whether the parse records
expectations: the position of a syntax error, its expected items and the
messages of [`@error`](../language/attributes.md#error), the errors that
[`#recover`](../language/pragmas.md#recover) recovers, and the run-time error of
an action that ends a parse. They do not depend on memoization, on the records
of run sites and skip runs, on the reuse of a [Document](../runtime/documents.md),
on the alternatives that a parser skips by the next character, or on the
backend that runs the parse. A parse that skips the actions whose values
nothing reads reports the same errors apart from the run-time errors of those
actions (*Recognition and default values*).

## Setting

Fix a grammar, a start rule, a position unit, an input $t$ and a recording
mode, which records [expectations](../runtime/parsing.md#recording-expectations)
or not. The proof covers the parses that no limit stops, outside the
missing-token mode: [Interruptions](interruptions.md) shows what a parse that a
compute limit, a nesting limit or a cancellation stops reports, and the
[memoization proof](memoization.md#setting), on which this proof rests, is
stated for the parses without insertion of `#missing` tokens.

**Outcomes.** The evaluations of expressions and calls have the outcomes
$\omega = (s, q, v, \varepsilon, o)$ of the [memoization
proof](memoization.md#setting), or abort with a run-time error $E$ of an
action. In $o = (f, X)$, $X$ is a sequence of items without duplicates, in the
order in which the parse first records them at $f$, combined by the operation
$\cdot$ of [Skip Runs](skip-runs.md#setting). An item is an expected item, a
literal, a character class, `any character`, an anchor or a name of
[`@diagnostic`](../language/attributes.md#diagnostic), or the message of an
`@error` that applies at $f$
([Syntax Errors](../runtime/parsing.md#syntax-errors)). A parse that does not
record expectations records no item, and every $X$ of it is empty; it tracks
$f$ all the same.

**The error part of a result.** A parse that fails reports the syntax error at
the farthest failure $f$ of its outcome, with its line and column, with
`Expected`, the expected items of $X$ ordered as
[Syntax Errors](../runtime/parsing.md#syntax-errors) states, and with
`Messages`, the messages of $X$ in the order of $X$. A parse that succeeds
reports the errors $\varepsilon$ that its result keeps, each with its
position, its expected items and messages, and the range of the error value.
A parse that aborts reports $E$. Each of these is read from the outcome of the
evaluation of the start rule at the start of the input, the end of input that
[Whole input](../runtime/parsing.md#whole-input) expects after it included.
So two parses whose outcomes of the start rule are equal, or that abort with
the same run-time error, report the same errors.

**The reference parse.** Let $\mathcal{R}$ be the parse that evaluates every
call, every iteration of every walk and every alternative that it tries, by
the rules of [Parsing](../runtime/parsing.md) and of the language, in the
recording mode fixed above. This is the parse $\mathcal{R}$ of the
[memoization proof](memoization.md#setting).

## Lemma 1 (Reference Parse)

The outcome of every evaluation that $\mathcal{R}$ makes is a function of
the expression, the position, the state in which it is evaluated (the
variables, the captures, the lookahead state and the open growths with their
current results), the grammar, the input and the recording mode. So the errors
that $\mathcal{R}$ reports are a function of the grammar, the start rule, the
position unit, the input and the recording mode.

*Proof.* By induction on the order in which the evaluations complete. Each
rule of evaluation that the specification gives determines the outcome of an
expression from the outcomes of the evaluations that it makes, in an order
that it fixes, and from the input: a terminal compares characters in the
position unit; a sequence evaluates its elements in order, a choice its
alternatives in order up to the first that matches, and a repetition its
iterations up to the first that fails or consumes nothing
([Combinators](../language/parser-expressions.md#combinators)); a lookahead
and a cut follow their rules, and a `#recover` its steps
([`#recover`](../language/pragmas.md#recover)). An Operators Rule tries its
parts in the order that [Parsing
Rules](../language/operators-rule.md#parsing-rules) fixes and selects the
longest match, the first declared among equals. A call evaluates the body of
its rule, and a call of a leader grows the seed, whose result is unique
([Left-Recursion Growth](left-recursion.md)). An action computes its value
from the values that it reads with the operators and built-in functions,
which are functions of their operands, and raises its run-time errors for
given operands ([Types and Errors](../language/actions.md#types-and-errors));
no operation reads which node is which, by (A3) of the [memoization
proof](memoization.md#assumptions). The record $o$ is built by $\cdot$ from
the records of the failed terminals, of the `@error` and `@diagnostic` that
apply, and of the evaluations made, in the order of evaluation, and nothing
is recorded inside a lookahead or the `skip` of an Operators Rule (A1). The
order of the evaluations is the one that these rules fix, so the order of
$X$ is too. The errors are read from the outcome of the start rule (Setting).
$\square$

## Lemma 2 (Reuse)

A parse that answers calls from its memo, that reuses the records of run
sites or skip runs, or that is a parse of a Document after any sequence of
edits, reports the errors that $\mathcal{R}$ reports.

*Proof.* The [memoization
theorem](memoization.md#theorem-memoization-never-changes-the-result) shows
that a parse that answers calls from a table of sound entries, an empty one
for an ordinary parse, returns the value, the recovered errors and the syntax
error of $\mathcal{R}$, or aborts with the same run-time error. Its outcomes
carry $o$ with the order of $X$, so the order of the messages is the same as
well. Its proof treats each walk that reuses a record as the walk that
evaluates its iterations, by [Lemma 1 of Run
Sites](run-sites.md#lemma-1-exactness) and [Lemma 1 of Skip
Runs](skip-runs.md#lemma-1-exactness), which show that a reused record leaves
the enclosing result with the outcome of those iterations. The
[Documents theorem](documents.md#theorem-document-parses-equal-fresh-parses)
shows that a parse of a Document returns what a parse of its current text from
scratch returns. $\square$

## Lemma 3 (Skipped Alternatives)

*A parse that skips alternatives of choices by the next character, where
[Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules) allows it,
reports the errors of a parse that evaluates them.*

*Proof.* Let a choice at a position $p$ skip an alternative $a$. By the rule
that allows the skip, the evaluation of $a$ would fail at $p$: it would
consume no input, enter no repetition and make no call other than those on
the way to a terminal $x$ that cannot match at $p$, each of which would fail
there, and it would record only the expectation of $x$ at $p$. None of the
expressions on the way, the first item of a sequence, a capture, a discard, an
`#atomic` and a call, is a `#recover`, so it would recover nothing and leave
$\varepsilon$ empty, and a failure has no value and no effect on variables or
captures ([Evaluation](../language/parser-expressions.md#evaluation)). So its
outcome would be a failure at $p$ with $o = (p, [x])$, and the choice would go
on to the next alternative in the same state.

The skip leaves the same state and records the expectation of $x$ at $p$,
except where recording it could not change the expectations that the parse
reports, which leaves $c \cdot (p, [x])$ and $c$ with the same reported
errors in each case. Inside a lookahead or the `skip` of an Operators Rule
recording is the identity (A1). When $p$ is before the farthest failure $f$ of
the record $c$, $c \cdot (p, [x]) = c$. In a parse that does not record
expectations, the item would be dropped, and the farthest position is
tracked either way ([Recording
Expectations](../runtime/parsing.md#recording-expectations)): the skip moves
$f$ to $p$ when $p$ is past it, as the evaluation would. So each skip leaves
the outcome of its choice, and by induction on the order of completion every
outcome, as the evaluation of the alternative would. Where the parse also
answers calls from a memo, the skip leaves the memo as the evaluation would
leave it under limit controls and in a Document, and elsewhere a memo entry
that the skip does not store is one that the memoization theorem allows a
parse not to store ([Consequences](memoization.md#consequences), *Order of
storing*). $\square$

## Theorem (Errors Do Not Depend on How a Parse Is Run)

Every parse of the grammar from the start rule on the input, in a recording
mode, that no limit stops and that makes no insertion of `#missing` tokens,
reports the errors that $\mathcal{R}$ reports, whichever calls it answers
from its memo, whichever records it reuses, whichever Document it parses and
whichever alternatives it skips by the next character. These errors are a
function of the grammar, the start rule, the position unit, the input and the
recording mode.

*Proof.* Lemmas 2 and 3 compose: the induction of the memoization theorem
holds with the skips of Lemma 3, which leave every outcome as the evaluation
would, and Lemma 1 gives the function. $\square$

**Backends.** Each backend of the engine and each generated parser implements
the rules of the specification that Lemma 1 cites, with the memo, the records,
the Documents and the skips that Lemmas 2 and 3 allow. The proof cites the
specification only, so that a backend follows those rules is its conformance
to the specification, which the tests of each backend check against the
others, and not a statement of this proof. Given it, the theorem holds of
every backend, and two backends report the same errors for a parse that no
limit stops. The error that a limit reports, and the point where it stops a
parse, are those of [Interruptions](interruptions.md).

**Recording mode.** A parse that does not record expectations reports the same
position, the same line and column, and succeeds or fails exactly when the
parse that records them does: recording changes $X$ and nothing that an
evaluation reads, since no operation reads the expectations, and the
farthest failure is tracked in both. Its recovered errors have the same
positions and ranges.

**Recognition and default values.** A [recognition](../runtime/parsing.md)
and a parse with [default values](../language/actions.md#default-values) do
not run the actions whose values nothing reads, so they raise no run-time
error of those actions
([Whole-Rule Action](../language/operators-rule.md#whole-rule-action)). They
keep the actions of the rules called inside the captures that predicates
read, so every predicate decides as in a parse that runs every action. When
the parse that runs every action ends with no run-time error, the
recognition and the parse with default values therefore make the same
evaluations with the same outcomes apart from the values, and report the same
syntax error and the same positions, ranges, expected items and messages of
the recovered errors. A [stream parse](../runtime/streaming.md) is not
treated here.
