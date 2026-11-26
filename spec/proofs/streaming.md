# Streaming

Arata Furukawa · Last revised 2026-11-26

This proof shows how a [stream parse](../runtime/streaming.md) relates to the
ordinary parse of the same input. A stream hands each element of its `#stream`
repetition to the caller, commits the input before the element, and then
discards that input with the memo entries, slots and records before it. The
proof shows four things. The discards drop nothing that a later step reads.
The stream delivers the elements of the ordinary parse, in order. It ends as
the ordinary parse ends without the start rule's value, its action and its
node-type conversion, with the same recovered errors: the checker keeps every
valid grammar from reading after the repetition what a stream does not keep.
Its compute units are those of that parse with the stream's store policy plus
the length of its texts. Finally, the proof states what the stream holds in
memory after each commit, and bounds it by what the attempts at the elements
of a window of 1,024 units before the commit point built.

## Setting

Fix a grammar, a start rule $S$ with a streaming repetition $\rho$, a position
unit, an input $t$ and a recording mode. Three parses are compared:

- the **stream parse** $\mathcal{S}$, a stream parse of $t$ whose reader
  splits $t$ into reads in some way;
- the **batch parse** $\mathcal{B}$, the ordinary parse of $t$ by `Parse`, in
  which `#stream` is an ordinary repetition, except that the call of $S$ at
  depth 1 builds no value: it evaluates its body, but neither the action of
  $S$ nor the conversion of its value to the node type of $S$. Every other
  call of $S$ builds its value;
- the parse $\mathcal{B}_s$, which is $\mathcal{B}$ with the store policy of a
  stream: every evaluation of a memoized rule stores its result, and nothing
  is discarded.

The outermost call of $\mathcal{B}$ is that of $S$ at depth 1, and its action
and conversion would be the last steps of the parse. So $\mathcal{B}$ takes
the steps of `Parse` up to the end of the body of $S$, and ends as `Parse`
ends whenever that action and conversion succeed, apart from the value and
their compute units. $\mathcal{B}$ and $\mathcal{B}_s$ are both instances of
the memoized parse $\mathcal{M}$ of the
[memoization proof](memoization.md#setting), whose storing is free under its
conditions, with the outermost call built as above: no evaluation reads the
value or the memo entry of that call, since building its value is the last
step of the parse, and $S$ is on no left-recursion cycle (T), so no call of
$S$ at position $0$ occurs inside it. Outcomes, keys and the
reference parse $\mathcal{R}$, with the same outermost call, are as defined
there.

A **commit** at $c$ follows the delivery of an element that ends at $c$. The
**commit point** at a step of $\mathcal{S}$ is the position of the last commit
before it, or $0$ if there is none. Write $c_1 \le c_2 \le \dots$ for the
positions of the commits.

**Scope.** The proof covers a stream parse that runs to its end. No limit
stops it (nesting, compute, input or memory), no cancellation stops it, and
neither the callback nor the reader returns an error or panics.
[Interruptions](interruptions.md#lemma-4-streams-keep-what-they-delivered)
shows that an interrupted stream delivers a prefix of the elements of the
stream that is not interrupted. Like the memoization proof, it covers parses
without insertion of [`#missing`](../language/pragmas.md#missing) tokens; *What
the Proof Does Not Show* says what holds with insertion. It takes no
[diagnostic limit](../runtime/streaming.md#recovered-errors), so the stream
keeps all of its recovered errors to the end of the parse and returns them.

**Rules of the specification.** The proof cites the following rules, named as
it uses them.

- **(T) Placement.** `#stream` annotates a repetition that is the body of a
  rule, an item of the body's top-level sequence, also of a sequence grouped
  in such an item, as in `("h" (a* #stream)) $$`, or the expression that such
  a body or item captures. Only groups, captures and the attributes
  `@error`, `@diagnostic` and `#ignorecase` may stand on the way. Only the
  start rule streams, at call depth 1. A rule with `#stream` is on no
  left-recursion cycle, and `#recover` does not wrap the repetition
  ([Grammar](../runtime/streaming.md#grammar)). None of the expressions that
  may stand on the way continues after its expression fails, as a choice, an
  option, a repetition, a lookahead, `#recover` or `#missing` would: a
  sequence fails when an item fails, a group, a capture or such an attribute
  fails with its expression, and a call of the start rule at depth 1 that
  fails ends the parse.
- **(H) Handed elements.** Handed elements are not included in the
  repetition's returned list
  ([`#stream`](../language/pragmas.md#stream)). Each element is delivered when
  it matches, before the next one is tried, and is final
  ([Delivery and Finality](../runtime/streaming.md#delivery-and-finality)).
  The bounds of `#repeat`, the minimum and the rule for an empty element are
  those of an ordinary repetition
  ([Grammar](../runtime/streaming.md#grammar), rows *Finite bounds* and
  *Empty elements*; [`#stream`](../language/pragmas.md#stream), whose
  zero-width iteration stops ordinary repetitions too).
- **(R) Reads after the repetition.** The predicates of $S$ that run after
  $\rho$ read no capture that holds $\rho$, and take no `text` whose argument
  reads a capture made before $\rho$ that does not hold a string node; a
  `text` of a lambda parameter counts as a text of each capture that its
  statement reads. The inline actions after $\rho$ see only their own
  captures ([Grammar](../runtime/streaming.md#grammar), row *Reads after the
  repetition*). A grammar that breaks the rule is rejected.
- **(A) Start value.** A stream parse builds no value of $S$ at its top call:
  it evaluates neither the action of $S$ nor the conversion of its value, and
  counts none of their compute units
  ([Entry Points](../runtime/streaming.md#entry-points),
  [Compute Units](../runtime/parsing.md#compute-units)).
- **(X) Texts.** In a stream, `text` of a node other than a string node whose
  range begins at or after the commit point is the input of the range; one
  whose range begins before the commit point ends the parse with an error,
  which only a compiled grammar that no compiler made can take. A string node
  holds its text.
  Positions count from the start of the whole input
  ([Positions and Text](../runtime/streaming.md#positions-and-text)).
- **(L) Looking back.** An evaluation at $p$ examines no slot before $p - 1$.
  Only `^` examines $p - 1$, and the stream keeps the unit before the commit
  point for it. No construct of the language looks further back
  ([Positions and Text](../runtime/streaming.md#positions-and-text)).
- **(P) Discards.** After a commit at $c$, a stream discards the input before
  $c - 1$. It drops memo entries at positions before $c$ where the parse makes
  no later call ([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules)).
  It drops the slots of skip runs and the records of run sites and scan sites
  at positions before $c$ ([Skip Runs](../runtime/parsing.md#skip-runs),
  [Run Sites](../runtime/parsing.md#run-sites),
  [Scan Sites](../runtime/parsing.md#scan-sites)). It drops the memo entries,
  slots and records at the first commit at least $W = 1024$ units past the
  commit at which it last dropped them, or past position $0$, and at no other
  commit ([Memory](../runtime/streaming.md#memory)). Only a stream with a
  diagnostic limit forgets the entries whose recovered errors it has
  delivered ([Compute Units](../runtime/parsing.md#compute-units)); without
  one it keeps them to the end
  ([Recovered errors](../runtime/streaming.md#recovered-errors)). With the
  release of the delivered element (H), this lists everything a commit drops:
  the farthest failure, its expected items, the open evaluations and the
  values they hold stay.
- **(W) No walk records $\rho$.** $\rho$ is no scan site
  ([Scan Sites](../runtime/parsing.md#scan-sites)) and no recorded site
  ([Conditions for Linear Cost](../runtime/parsing.md#conditions-for-linear-cost)),
  so its walk keeps no record. The expression of a run site, and every rule
  it calls, contains no `#stream`
  ([Run Sites](../runtime/parsing.md#run-sites)), so no recorded walk
  contains $\rho$.
- **(V) Scope.** Inside a repetition element, only captures from that
  iteration are visible
  ([Predicate Actions](../language/predicates.md)). A rule's captures are
  visible only inside the rule. Variables hold `int`, `string` or `bool`
  values ([Variables](../language/predicates.md#variables)).
- **(U) Units.** The compute units of a parse are the units of its events
  ([Compute Units](../runtime/parsing.md#compute-units)). These depend on the
  store policy of the memo, and a stream stores the result of every
  evaluation. A text in a stream is an event of one unit for each position
  unit of its result. Outside a stream, `text` counts nothing. Reading the
  input of a stream and delivering its elements count nothing.

## Lemma 1 (Open Evaluations at a Commit)

At each commit at $c$, the evaluations that are open are the call of $S$ at
depth 1, the expressions on the path from its body to $\rho$, and $\rho$. No
growth is open, and the lookahead state is $0$. After the commit, every
evaluation begins at a position of at least $c$ and examines no slot before
$c - 1$.

*Proof.* $\rho$ streams only in the call of $S$ at depth 1, and by (T) the path
from the body of $S$ to $\rho$ consists of sequences, groups, captures and
the attributes `@error`, `@diagnostic` and `#ignorecase`. The element
delivered at the commit is the value of an iteration that has matched and
completed (H), so no evaluation inside $\rho$ is open. No lookahead is on
the path, so the lookahead state is $0$. The call of $S$ is the outermost
call. $S$ is on no left-recursion cycle (T), so no growth is open.

After the commit, $\rho$ tries its next iteration at $c$, or ends at $c$. Each
evaluation begins at the position its parent has reached. An open evaluation
that has begun at $p$ has reached a position of at least $p$. When a part of
it fails, it goes back only to a position that it has itself reached. The
open evaluations above $\rho$ never go back to a position before $c$: they
take the end of $\rho$ as the start of the next item, and by (T) none of them
continues after a failure. A failure on the path fails the call of $S$ and
ends the parse. So every evaluation that begins after the commit begins at a
position of at least $c$, by induction on the order in which evaluations
begin. By (L), an evaluation at $p \ge c$ examines no slot before
$p - 1 \ge c - 1$. $\square$

## Lemma 2 (Discards Drop Nothing That Is Read)

After a commit at $c$, no step of $\mathcal{S}$ reads input before $c - 1$,
looks up a memo entry at a position before $c$, or looks up a slot or a record
at a position before $c$. So $\mathcal{S}$ takes the steps that it would
take without the discards of (P), and it drops memo entries only where the
parse makes no later call, as (P) requires.

*Proof.* An evaluation reads input at the slots it examines, and by Lemma 1
every slot examined after the commit is at least $c - 1$. The only other
step that reads input is `text` of a node other than a string node, which in
a stream reads the input of a range that begins at or after $c$, or ends the
parse without reading any (X), so it reads nothing before $c$; a string node
holds its text and reads no input. Lemma 4 shows that no step of a grammar
that compiles ends the parse that way.

A call looks up the memo at its own position, and a walk of a skip run, a run
site or a scan site looks up its slots and records at the starts of its
iterations. By Lemma 1, both are positions of at least $c$. No growth is open
(Lemma 1), so no result kept under (D) of the memoization proof is left to
answer a call: what it keeps is dropped when the first growth at its position
ends. No walk records $\rho$ (W), so no record holds an element that the
commit released. Within the scope, (P) forgets no recovered error. $\square$

**With a diagnostic limit.** The entries that a stream with a limit forgets
are read only as the start of the errors that the parse returns. The
recovered errors recorded within an open evaluation are taken as the list of
an entry only when that evaluation stores its result. By Lemma 1, the open
evaluations at the commit are the call of $S$, the path and $\rho$; of these
only the call of $S$ could store a result, at the end of the parse, where no
later call reads it, and $\rho$ records nothing (W). Which tail the limit
keeps is not shown (*What the Proof Does Not Show*).

## Lemma 3 (Ranges Around a Commit)

Let $c$ be the commit point at a step of $\mathcal{S}$ that takes `text` of a
node $x$ other than a string node. If the step is inside an iteration of
$\rho$, the range of $x$ begins at or after $c$, and the text is the text that
$\mathcal{B}_s$ takes. A step before $\rho$ has $c = 0$. After $\rho$ ends,
the range of $x$ either begins at or after $c$, or ends at or before $c$.

*Proof.* Inside an iteration, the step reads the captures of the iteration
(V), the values of the rules it calls, and variables, whose values are
scalars (V) and hold no node. The iteration begins at the commit point $c$,
or at the start of $\rho$ before the first commit, where $c = 0$. Its
captures and the results of its calls are values of evaluations that begin
at or after the start of the iteration (Lemma 1). A result that a memo entry
holds is the value of an evaluation that began at the position of the entry,
which is the position of the call. A string, tuple or list node or a
recovered value made by an evaluation has a range inside the input that
evaluation matched or skipped. The fields of such a value are values of the
same kind. Lists and structs that actions build have no range that `text`
takes ([`text`](../language/actions.md#text)). So $x$ begins at or after $c$,
and by (X) its text is the input of its whole range, as in $\mathcal{B}_s$.

Before the first commit, $c = 0$. After $\rho$ ends, the nodes that the
start rule and the rules it calls read fall into three groups. Captures
made before $\rho$ end at or before the start of $\rho$. A capture that holds
$\rho$ has the range of $\rho$. Nodes made after $\rho$
begin at or after its end. Let $e$ be the end of $\rho$. An iteration that
fails consumes nothing, so $e$ is the end of the last element if $\rho$
delivered one, and then $c = e$. If $\rho$ delivered none, then $c = 0$.
Ranges that end at or before the start of $\rho$, or that end at $e$, end at
or before $c$ in the first case and begin at or after $c = 0$ in the second.
Ranges that begin at or after $e$ begin at or after $c$ in both cases.
$\square$

So after at least one element, a text of a node made before or by $\rho$
whose range is not empty at $c$ begins before $c$ and, by (X), would end
$\mathcal{S}$ with an error where $\mathcal{B}_s$ goes on; (R) keeps every
valid grammar from taking it (Lemma 4).

## Lemma 4 (No Text Begins Before the Commit Point)

No step of $\mathcal{S}$ reads the list of $\rho$. Every `text` that
$\mathcal{S}$ takes at a step is of a range that begins at or after the
commit point, and is the text that $\mathcal{B}_s$ takes at the same step,
provided the two parses have taken the same steps before it.

*Proof.* The list of $\rho$ is visible only through a capture that holds
$\rho$, which is made when $\rho$ ends (V). Inside an iteration the capture
does not exist yet, and an iteration sees only its own captures (V). After
$\rho$ ends, the steps of $\mathcal{S}$ are those of the items of the path
after $\rho$, of the expressions on the path that end after it, and of the
calls, inline actions and predicates within them. The predicates of $S$ read
no capture that holds $\rho$ (R). The inline actions and the rules called see
only their own captures (R, V). The action of $S$ at the top call, which could
read it, is not evaluated (A). Variables hold scalars (V), not the list.

A text taken before the first commit has $c = 0$ and is whole by (X). A text
taken inside an iteration is the text of $\mathcal{B}_s$ by Lemma 3. After
$\rho$ ends at $e$, the commit point is $c \le e$ (Lemma 3). A text there is
taken by a predicate of $S$, by an inline action, or within a called rule. An
inline action and a called rule take texts of their own captures and of the
values of their calls, all made by evaluations that begin at or after $e$
(Lemma 3), so their ranges begin at or after $c$ and the texts are whole (X).
A predicate of $S$ takes a text of an expression that reads, by (R), only
captures made after $\rho$, which begin at or after $e$; captures made before
$\rho$ that hold a string node, whose text they hold (X); variables, which
hold scalars; and lambda parameters, which count as the captures that the
statement reads, so they range over values of the same captures. Every such
text begins at or after $c$, so by (X) it is the input of its whole range,
the text of $\mathcal{B}_s$, and no step ends the parse with the error of
(X). $\square$

For example, (R) rejects `h:("h" "h") (("a" ";")* #stream)
{ len(text($h)) == 2 } $$`: after an element, the range of `$h` begins before
the commit point, so a stream would end with an error where the batch parse
succeeds. A variable
bound before the repetition carries such a text across it.

## Lemma 5 (Stream Steps Follow the Parse With Its Policy)

$\mathcal{S}$ and $\mathcal{B}_s$ take the same steps in the same states,
except that the list of $\rho$ in $\mathcal{S}$ holds no element. The
iterations of $\rho$ have the same outcomes, and the elements that
$\mathcal{S}$ delivers are the elements of the list of $\rho$ in
$\mathcal{B}_s$, in order.

*Proof.* By induction on the steps. A step reads the input, the memo, the
slots and records, the state of the evaluation, the recovered errors, the
farthest failure with its expected items, and the values that it can see.

- **Input.** $\mathcal{S}$ demands input until the slot is read or the input
  ends. Each slot it reads holds what the same slot of $t$ holds, whatever the
  reads. The discarded input is not read (Lemma 2), and every text is the
  text of $\mathcal{B}_s$ (Lemma 4).
- **Memo, slots and records.** Both parses store under the same policy, and
  the discards change no lookup (Lemma 2). So both look up and find the same
  entries.
- **Farthest failure and expected items.** A commit drops none of them (P).
- **Recovered errors.** Within the scope, a commit forgets none (P).
- **Values.** The two states differ only in the list of $\rho$ (H). No step
  reads it (Lemma 4).
- **The top call.** Neither parse evaluates the action of $S$ or converts its
  value at the call at depth 1 (A and the setting).

The number of iterations, the minimum and maximum of `#repeat`, and the rule
for an empty element are the same in both parses (H). An element is the value
of an iteration that matched, so the elements are those of the list in
$\mathcal{B}_s$. $\square$

## Theorem (Stream Parses Follow Batch Parses)

Let $\mathcal{S}$ run to its end, within the scope of the setting.

1. **Elements.** The elements that $\mathcal{S}$ delivers are, in order, the
   values of the iterations of $\rho$ that match in $\mathcal{B}$, equal as
   values. If $\mathcal{B}$ ends with a run-time error inside an iteration,
   $\mathcal{S}$ ends with it after delivering the elements before that
   iteration.
2. **Outcome.** $\mathcal{S}$ ends as $\mathcal{B}$ ends. It succeeds if and
   only if $\mathcal{B}$ succeeds, with the recovered errors of $\mathcal{B}$
   in the same order. Otherwise it fails with the syntax error of
   $\mathcal{B}$ (its position, expected items and messages) or the run-time
   error of $\mathcal{B}$. Neither returns a value.
3. **Units.** The compute units of $\mathcal{S}$ are those of $\mathcal{B}_s$
   plus the total length of the results of the texts it takes. The units, the
   elements and the outcome of $\mathcal{S}$ do not depend on how the reader
   splits $t$.

*Proof.* By Lemma 5, $\mathcal{S}$ takes the steps of $\mathcal{B}_s$ to the
end and delivers the iterations of $\rho$ in $\mathcal{B}_s$. It therefore
has the outcome of $\mathcal{B}_s$, and the recovered errors are the same
sequence.

$\mathcal{B}_s$ and $\mathcal{B}$ are instances of $\mathcal{M}$ with an
empty table. By the
[memoization theorem](memoization.md#theorem-memoization-never-changes-the-result),
both return the recovered errors and syntax error of $\mathcal{R}$, or abort
with its run-time error. Its invariant (I1) holds for every evaluation,
including the body of $S$ at the top call, whose outcome without its value is
the outcome of that call, and each iteration of $\rho$, whose
state is the same in all three parses by induction. So the iterations have
the outcomes of $\mathcal{R}$ in both parses, and parts 1 and 2 follow. The
values agree as in [Consequences](memoization.md#consequences).

For part 3, the two parses take the same steps (Lemma 5), so they perform the
same events of (U). The only exception is `text`, which costs the length of
its result in $\mathcal{S}$ and nothing in $\mathcal{B}_s$; the results are
the same texts (Lemma 4). The steps of $\mathcal{S}$ read only the slots of
$t$, whatever the reads, and its commits and discards depend only on its
steps. So nothing in it depends on how the reader splits $t$. $\square$

**The batch parse and `Parse`.** $\mathcal{B}$ is `Parse` without the action
and conversion of the top call of $S$. When the start rule has neither, or
they succeed, the stream ends as `Parse` ends, without its value. When they
fail, `Parse` ends with their run-time error while the stream does not. Only
the action can fail in a valid grammar. Without an action, the value of the
body is the list node of $\rho$ or the tuple node of the body's sequence, by
(T): groups and the attributes allowed on the way create no value, a capture
has the value of its operand, and a sequence is a tuple node even of one
element, with the elements of a sequence grouped in it
([Values and the Concrete Syntax Tree](../language/parser-expressions.md#values-and-the-concrete-syntax-tree)).
The checker rejects a declared node type to which such a value cannot be
converted ([Types and Errors](../language/actions.md#types-and-errors)). In
the grammar

```sxir
@start
rule main = ("(" main ")")? xs:(([a-z] ";")* #stream) => 10 / (len($xs) - 2)
```

`Parse` of `a;a;` and of `(a;)a;a;` fails with a division by zero, and the
stream succeeds and delivers two elements. The call of `main` inside the
parentheses is not at depth 1 and builds its value, so on `(a;a;)a;` both
fail.

**Units of the batch parse.** The units of $\mathcal{S}$ are those of
$\mathcal{B}_s$, not of $\mathcal{B}$: an ordinary parse leaves no result for
the first evaluation of a memoized rule at a position, which a later call
evaluates again, while a stream stores every result
([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules)). Neither
counts the units of the action of $S$ at the top call, which `Parse` counts.
The default compute limit of a stream also differs from that of a batch parse
([Default Limits](../runtime/parsing.md#default-limits)). So a compute limit
can stop one of the two parses and not the other.

## Lemma 6 (What a Stream Holds)

After a commit at $c$, let $b$ be the position of the commit at which the
stream last dropped memo entries, slots and records, or $0$ if it has not
dropped any. Then $c - W < b \le c$, and the stream holds the following, and
what they reach:

- the input from $c - 1$ to the furthest position demanded;
- the memo entries, slots and records at positions at or after $b$;
- the open evaluations of Lemma 1;
- what the start rule built before $\rho$;
- the values of variables and the keys of the memo;
- the recovered errors.

The list of $\rho$ holds no element. Apart from what the memo entries and
records at or after $b$ reach, the stream holds nothing that an element it
delivered holds. A memo entry, slot or record at a position $p$, and what it
reaches apart from recovered errors and the keys of the memo, was built by
evaluations that began at or after $p$.

*Proof.* The bound on $b$ is by induction on the commits. Before the first
commit, $b = 0$. At a commit at $c$, either $c - b \ge W$ for the $b$ of the
commit before, and the stream drops at $c$, so that $b = c$, or $c - b < W$
and $b$ stays (P). Commits do not go back, so in both cases $c - W < b \le c$.

(P) discards the input before $c - 1$ and the memo entries, slots and records
before $b$. By Lemma 1, the open evaluations are the call of $S$, the path and
$\rho$. Their values are the captures that the start rule made before $\rho$,
and the list of $\rho$, which holds no element (H). An evaluation that begins
later begins at or after $c$ (Lemma 1). An element was delivered and then
dropped from every structure of the parse but the memo entries and records
that hold it. Variables hold scalars (V), and the keys hold the values of
variables.

A memo entry at $p$ holds the outcome of an evaluation that began at $p$, and
a slot or a record at $p$ holds what a walk that began at $p$ found. The value
of the outcome was built by that evaluation, by the evaluations inside it, and
by the evaluations whose outcomes these took from the memo or a record, which
began where the call or the walk that took them began. Each of these begins at
a position that its parent has reached, which is at or after $p$. None sees a
capture of the rules that called it (V), so what the entry reaches, apart from
the recovered errors and the values of variables, these evaluations built.
$\square$

## Theorem (Memory of a Stream)

Call the part of $\mathcal{S}$ that evaluates the body of $S$ up to $\rho$
an **attempt** that starts at $0$, and the trial of each iteration of $\rho$,
whether it matches or not, an attempt that starts at the position where the
iteration begins. Let the **reach** $\lambda$ be the largest distance from
the start of an attempt so far to a slot that it examines or a position at
which an evaluation inside it begins.

After each commit at $c$, every memo entry, slot and record that $\mathcal{S}$
holds, with what it reaches apart from recovered errors and the keys of the
memo, was built by an attempt that starts after $c - W - \lambda$, and the
input it holds is at most $\lambda + 2$ units. At most $W + \lambda + 1$
attempts start after $c - W - \lambda$ and at or before $c$. So the storage
that $\mathcal{S}$ holds after the commit is at most a constant times

$$
I_0 + E + (\lambda + 2) + (W + \lambda + 1) \cdot C,
$$

where $I_0$ is the storage of what the start rule built before $\rho$, of the
values of variables and of the keys of the memo, $E$ that of the recovered
errors, and $C$ the largest cost of an attempt. Apart from $E$, the bound does
not grow with the number of elements delivered: it depends on the largest
attempt and the reach.

*Proof.* By Lemma 6, a memo entry, slot or record that the stream holds is at
a position $p \ge b > c - W$, and what it reaches was built by evaluations
that began at or after $p$. Each of these began inside an attempt, which
started at a position $s$ with $p - s \le \lambda$, so
$s \ge p - \lambda > c - W - \lambda$. The attempt has begun before the
commit, so $s \le c$. The input held runs from $c - 1$ to the furthest slot
demanded, which an attempt that starts at or before $c$ examines, so it is at
most $c + \lambda$.

An iteration of $\rho$ begins where the one before ended, and an iteration
that matches the empty string is delivered once and ends the repetition
(H), so no iteration follows it. So the iterations begin at distinct
positions, and at most $W + \lambda$ of them start in the interval, which
holds $W + \lambda$ positions, besides the attempt that starts at $0$.

The memory a parse allocates is at most proportional to its cost
([Time and Memory](../runtime/parsing.md#time-and-memory)): each memo entry,
node and value is built by cost that is counted. So what the attempts in the
interval built, and with it what the stream holds of them, is at most a
constant times $(W + \lambda + 1) \cdot C$. The open evaluations hold what
the start rule built before $\rho$ and the empty list of $\rho$ (Lemma 6).
$\square$

The window of $W$ units is the one that
[Memory](../runtime/streaming.md#memory) states: the stream holds no memo
entry, slot or record of a position $W$ or more units before the commit
point. The bound is that of the specification: the memory held depends on the
size of the elements, through $C$ and $\lambda$, and not on the number of
elements delivered, with the recovered errors as the stated exception.

## What the Proof Does Not Show

- **Missing tokens.** Lemmas 1 to 5 do not use the memoization theorem, and
  hold with insertion of `#missing` tokens: $\mathcal{S}$ inserts what
  $\mathcal{B}_s$ inserts, under the same limit for the whole parse. That
  $\mathcal{B}_s$ inserts what $\mathcal{B}$ does rests on memoization with
  insertion, which the [memoization proof](memoization.md#setting) leaves out.
- **Diagnostics.** The order in which the diagnostic callback receives the
  recovered errors among the elements, and the tail that a diagnostic limit
  keeps, are not shown.
- **Lines and columns.** That the line and column of a syntax error, which a
  stream computes from a running count over the discarded input
  ([Positions and Text](../runtime/streaming.md#positions-and-text)), are those
  of the batch parse is not shown; the theorem covers the position.
- **Typed delivery.** A generated Go parser with typed values delivers
  typed elements through `ParseStreamValue` and `IterateValue`
  ([Streaming](../runtime/generated-parsers.md#streaming)). That each typed
  element is the conversion of the delivered Node, and that the typed stream
  ends as the untyped one, is the conformance of the generator.
- **Backends.** That each backend and each generated parser, including the
  push stream of a generated TypeScript parser, follows the rules cited is
  its conformance.
- **Exact memory counts.** Lemma 6 and the memory theorem state what a stream holds,
  not the counts of the [memory limit](../runtime/parsing.md#memory-limit),
  which depend on the backend. That the count follows the same window is the
  conformance of each backend.
