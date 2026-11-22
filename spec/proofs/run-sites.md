# Run Sites

Arata Furukawa · Last revised 2026-11-22

This proof covers the records of [run sites](../runtime/parsing.md#run-sites).
A reused record gives what the rest of the walk would have given, in the
environment where the walk reuses it (Lemma 1). The walks of a site evaluate
its element a number of times linear in the input and the number of walks,
once the site keeps records (Lemma 2). A walk of a site that is not hot costs
its length, and those lengths add up to the input plus a constant per walk
(Lemma 3). The walks again from one start of a run site free of the
environment with no minimum, whose element costs a constant, take its record
after a constant number of them (Lemma 7). It extends the [Skip Runs](skip-runs.md) proof, whose definitions
of outcomes, ranges, slots and walks it uses. It differs from that proof where a
run site differs from a skip-run site: the element calls rules, captures,
builds values, recovers errors, reads and binds variables, and the walks of a
site nest.

## Setting

Fix a parse, a run site $\sigma$ and a lookahead state $\lambda$. Let $n$ be
the length of the input. The element of $\sigma$ is $e$, and its minimum is
$c_{\min}$. By the conditions on run sites, $e$ contains no Operators Rule,
`#stream` or `#missing`, and no [`@error`](../language/attributes.md#error) or [`@diagnostic`](../language/attributes.md#diagnostic)
attribute. Every repetition without an
upper bound in $e$ or in a rule that $e$ calls, directly or indirectly,
`skip` expressions included, is a run site, a skip-run site or repeats a
single character. Every rule
that $e$ calls, directly or indirectly, is called without a level, is in no
left-recursion cycle, and contains no `#stream` or `#missing` and no
[`@error`](../language/attributes.md#error) or [`@diagnostic`](../language/attributes.md#diagnostic) attribute; such a rule may be an Operators Rule. No `#cut` in $e$
applies to $\sigma$. A parse with missing-token insertion keeps no records, so
assume that this parse inserts none.

**Iterations.** Let $E(r, \eta)$ be the evaluation of $e$ at position $r$ in
the environment $\eta$. It fails or matches up to some $r' \ge r$. It has a
farthest failure $f$ with expectations $X$, examines a range $[l, h]$, and
recovers a sequence of errors $R$. When it matches, it also leaves an
environment $\eta'$ and a value $v$, or no value when the iterations of
$\sigma$ build none. What $E(r, \eta)$ gives depends only on $r$, $\eta$,
$\lambda$ and the input:

- a capture of $e$ is visible to the iteration alone, so no iteration reads
  what an earlier one captured;
- no rule that $e$ calls is in a left-recursion cycle, so no growth in progress
  outside $e$ changes what a call inside it returns, and a call of such a rule
  at $r$ does not reach $\sigma$ again at $r$ before it consumes input;
- an Operators Rule that $e$ calls is called without a level, so it parses
  the operators of all its levels, and what it gives at a position depends
  only on the position, the environment, $\lambda$ and the input, as for any
  other rule;
- memoized results that $E(r, \eta)$ reads or stores change its cost, never
  what it gives ([Memoization](memoization.md)), and neither do the records
  of other sites, by Lemma 1 for them and by the
  [Skip Runs](skip-runs.md#lemma-1-exactness) proof;
- a value that $E(r, \eta)$ adds to the walk is final, since a node in a
  record is not changed by a later step
  ([Run Sites](../runtime/parsing.md#run-sites));
- a rule and its value-free twin match the same input in the same way
  ([Value-free twins](../bytecode/README.md#rule-table)), so an evaluation of
  $e$ in a walk of the twin gives what $E(r, \eta)$ gives apart from $v$:
  the same outcome, end, farthest failure and expectations, range,
  environment and errors.

**Environments.** The site is **free of the environment** when $e$ has no
predicate that reads or binds a variable, no inline action that reads one,
and calls no rule that reads one, directly or indirectly, in a predicate or
an action. (An inline action is a call of a hidden rule whose action is the
inline action, so the last condition covers it.) Then $E(r, \eta)$ gives the same for every $\eta$,
and when it matches, $\eta' = \eta$:

- a predicate or action of $e$ or of a rule that $e$ calls reads no variable,
  so its outcome and value do not depend on $\eta$;
- a call of a rule that reads no variable is memoized without the
  environment in its key ([Interaction With
  Memoization](../language/predicates.md#interaction-with-memoization)), so a
  result that it reuses was stored under any environment and is the same;
- $e$ binds no variable outside its calls, and a binding that a called rule
  makes is undone when that rule returns, so the environment after an
  iteration is the one before it.

For such a site, write $E(r)$ for $E(r, \eta)$. Every skip-run site is free of
the environment by its conditions.

For any other site, its **key** $V_\sigma$ is the set of the variables that
the predicates of $e$ read or bind and those that the rules that $e$ calls
read, directly or indirectly, in a predicate or an action, and
$B_\sigma \subseteq V_\sigma$ is the set of the variables that the predicates
of $e$ bind. Write $\eta|_V$ for the values that $\eta$ gives the variables
of $V_\sigma$. Then $E(r, \eta)$ gives the same for every two environments
with the same $\eta|_V$, and when it matches, $\eta'$ agrees with $\eta$
outside $B_\sigma$:

- a predicate of $e$ reads only variables of $V_\sigma$, and a predicate or
  action of a rule that $e$ calls only variables that the rule reads, which
  are in $V_\sigma$;
- a call of a rule is memoized with the values of the variables that the rule
  reads in its key ([Interaction With
  Memoization](../language/predicates.md#interaction-with-memoization)), so a
  result that it reuses was stored under the same values and is the same;
- a binding that a called rule makes is undone when that rule returns, so
  an iteration changes only the variables that the predicates of $e$ bind.

**The rest of a walk.** Define $\mathrm{Rest}(r, \eta)$, the end, end
environment, outcome, range, values and errors of the walk from $r$ on in
$\eta$, as in the Skip Runs proof:

- if $E(r, \eta)$ fails or matches up to $r$, then
  $\mathrm{Rest}(r, \eta)$ ends at $r$ in $\eta$, with the outcome and range
  of $E(r, \eta)$, no values, and the errors $R$ that the iteration recovered;
- if $E(r, \eta)$ matches up to $r' > r$ in $\eta'$ with value $v$, and
  $\mathrm{Rest}(r', \eta')$ ends at $\mathrm{end}'$ in $\eta''$ with outcome
  $o'$, range $\rho'$, values $V'$ and errors $R'$, then
  $\mathrm{Rest}(r, \eta)$ ends at $\mathrm{end}'$ in $\eta''$, with outcome
  $(f, X) \cdot o'$, range $[l, h] \sqcup \rho'$, values $v\,V'$ and errors
  $R\,R'$.

An iteration that fails recovers no error that survives, since its errors are
dropped with the failure. The recursion terminates because $r' > r$. For a
site free of the environment, $\mathrm{Rest}(r, \eta)$ does not depend on
$\eta$, by induction on $n - r$, and its end environment is $\eta$. Write
$\mathrm{Rest}(r)$ for it, without the end environment. For any other site,
by induction on $n - r$ in the same way, $\mathrm{Rest}(r, \eta)$ is the same
for every two environments with the same $\eta|_V$, apart from its end
environment, which agrees with $\eta$ outside $B_\sigma$ and gives the
variables of $B_\sigma$ values that depend only on $\eta|_V$: by the above,
$E(r, \eta)$ depends only on $\eta|_V$, and so does $\eta'|_V$, which agrees
with $\eta|_V$ outside $B_\sigma$ and takes the values that $E(r, \eta)$
binds on it. Call those values of the variables of $B_\sigma$ the **end
values** of $\mathrm{Rest}(r, \eta)$.

**Keys.** A record of a site that is not free of the environment is stored
under its position and the values $\eta|_V$ of its key in the environment
$\eta$ in which the walk visited that position, and holds, besides what a
record of a skip-run site holds, the end values of the rest of the walk. A
walk looks one up under its position and the values of the key in its
current environment, compared as the values of a memo key are: a record
under the same position and other values is neither found nor replaced, and
environments with equal values of the key find the same records, whatever
their other variables and whether or not they were built apart
([Run Sites](../runtime/parsing.md#run-sites)). A record of a site free of
the environment is stored and looked up under its position alone.

### Record Kinds

The records of a site are kept apart for each lookahead state, so
all the records that the walks of $(\sigma, \lambda)$ find were made by walks
of $(\sigma, \lambda)$. A rule and its value-free twin walk the same site, so
a record also notes whether the walk that made it built values. A walk that
builds values finds only a record that notes values, and treats a slot whose
record notes none as not recorded under its key; a walk that builds none
takes a record of either kind and ignores its values, which the rest of its
walk does not use. A key holds one record at a time, and a later store
under it replaces it, except that a record that notes no values never
replaces one that notes values: a walk without values that records such a
key, as in its first $c_{\min}$ iterations, leaves the record in place,
which holds everything it would store besides the values (Lemma 1). A walk
that builds values stores records that note values. So a key holds a record
without values only until a walk that builds values records it, and never
after.

## Lemma 1 (Exactness)

For a site that is not free of the environment, every record stored under
$(r, \eta|_V)$ equals $\mathrm{Rest}(r, \eta)$, end values included, which
is the same for every environment with the values $\eta|_V$ of the key
([Setting](#setting)). For a site free of the environment, every record
stored under $r$ equals $\mathrm{Rest}(r)$. A walk that reuses a record ends
where the walk that evaluated the iterations from there would end. Its
environment is then its own with the variables of $B_\sigma$ bound to the
record's end values, or its own for a site free of the environment. It leaves the enclosing result with
the same outcome and range, lists the same values after its own, recovers the
same errors after its own, and succeeds or fails as that walk would.

*Proof.* By induction on the order in which records are stored, as for
[Lemma 1](skip-runs.md#lemma-1-exactness) of the Skip Runs proof.

A recording walk visits $r_j < \dots < r_k$ while recording, in the
environments $\eta_j, \dots, \eta_k$ in which the iterations at those
positions begin. Each visit is an iteration that the parser isolates: its
expectations and examined range are set aside and then recorded into the
enclosing result. The walk notes the number of values that it has collected
and the errors that the iteration recovered. Each $\eta_{m+1}$ is the
environment that $E(r_m, \eta_m)$ left. The walk ends in one of two ways.

- At an iteration that fails or matches up to its own start. That is the last
  visit $r_k$. Its record ends at $r_k$ in $\eta_k$ with the outcome, range and
  errors of $E(r_k, \eta_k)$ and no values, which is
  $\mathrm{Rest}(r_k, \eta_k)$.
- At a position $r_{k+1}$ with a record found under its key, which notes
  values if the walk builds them ([Record Kinds](#record-kinds)). The record was
  stored earlier, under the values $\eta_{k+1}|_V$, so it is
  $\mathrm{Rest}(r_{k+1}, \eta_{k+1})$ by the induction hypothesis, end values
  included, with its values when the walk builds them. For a site free of the
  environment it is $\mathrm{Rest}(r_{k+1})$, which is the same.

The other records are stored from $r_k$ back to $r_j$. Each combines the
iteration at $r_m$ with the record just stored for $r_{m+1}$:

- the outcome and range by $\cdot$ and $\sqcup$;
- the end values of the record after it;
- the values as the suffix of the walk's list from the value of the iteration
  at $r_m$ on, whose tail is the list that the next record holds;
- the errors as those of the iteration followed by those of the next record.

That is the second case of the definition, so the record is
$\mathrm{Rest}(r_m, \eta_m)$: its end environment is that of the record
after it, which agrees with $\eta_{m+1}$, and so with $\eta_m$, outside
$B_\sigma$, and gives $B_\sigma$ the end values of that record. The walk
stores it under $(r_m, \eta_m|_V)$. For a site free of the environment, the
walk keeps no end values in the record, and the record is
$\mathrm{Rest}(r_m)$. This proves the first claim.

A walk looks up a slot only after it has matched $c_{\min}$ iterations, so the
walk that evaluates on from there succeeds, and so does the walk that reuses
the record. The evaluating walk records the outcome and range of each
iteration into the enclosing result in order, appends each value, and
recovers each error. By associativity, a reuse that does these at once leaves
the same result. A reuse in a site free of the environment keeps the walk's
environment, and a reuse in any other site binds the variables of
$B_\sigma$ to the end values of the record and keeps the others. By the
setting, that is the environment that evaluating the iterations would leave:
the end environment of $\mathrm{Rest}(r, \eta)$ agrees with $\eta$ outside
$B_\sigma$ and gives $B_\sigma$ its end values. $\square$

The walks of run sites nest. An iteration of $\sigma$ can call rules that walk
other sites, or $\sigma$ itself at a later position. Lemma 1 holds for each
site, because the records that an inner walk stores are stored before the
iteration around it ends, and the induction runs over the order of storing,
across all the walks of the parse. Whether a walk is plain or recording, and
whether the site is hot, decides only which positions get records. So neither
changes the result of a parse.

## Lemma 2 (Cost Once Hot)

A **class** of a visit of a walk to a position is the key under which the
walk looks up and stores the record of that position: for a site free of the
environment, all visits share one class; otherwise, the class is the values
of the key $V_\sigma$ in the environment of the visit, and two visits with
equal values share it. Let $P$ be the number of pairs of a position and a
class in which the walks of $(\sigma, \lambda)$ visit that position while
the site is hot. In a parse with $W$ walks of $(\sigma, \lambda)$ while the
site is hot, the element is evaluated in those walks at most $n + 1$ times at
positions whose slot is fresh, at most $2P$ times at positions whose slot is
not yet recorded under the class of the visit for the kind of the walk
([Record Kinds](#record-kinds)), or $P$ when the walks of
$(\sigma, \lambda)$ are of one kind, plus $W (c_{\min} + 1)$ times. The
walks look up at most $2P + W (c_{\min} + 1)$ slots and store at most as
many records as they evaluate iterations while recording.

*Proof.* The marks of visited positions are one bit for each position, for
every class. A mark moves a slot from fresh to visited for all classes at
once. A record moves the slot of its key to recorded. Sort the evaluations as
in [Lemma 2](skip-runs.md#lemma-2-cost) of the Skip Runs proof.

1. *A plain walk at a fresh slot.* Each position is marked at most once, so
   there are at most $n + 1$ such evaluations at positions that the walk
   marks, plus at most one per walk at the position that it does not mark.
2. *A recording walk at a slot not recorded under the class of its visit.* The
   walk stores a record under the class of each visit for every position it
   visited while recording, once it ends. While it runs, an inner walk of
   $\sigma$ can begin, but only inside an iteration at $r_m$ and at a position
   after $r_m$. A walk at $r_m$ itself
   would be a call that reaches $\sigma$ again at $r_m$ before it consumes
   input, which the conditions exclude. The inner walk ends, and stores its
   records, before the iteration at $r_m$ ends. The outer walk visits its later
   positions only after that, so where it visits a position of the inner walk
   in the class of the inner walk's visit there, it finds the inner walk's
   record. So each pair of a position and a class is evaluated this way at most
   once before it is recorded for each kind of walk, since a record that
   notes values serves both kinds and no later store takes it back to one
   without ([Record Kinds](#record-kinds)): at most $2P$ evaluations, and at
   most $P$ when the walks of $(\sigma, \lambda)$ are of one kind.
3. *A recording walk at a recorded slot.* This happens only within the first
   $c_{\min}$ iterations of the walk: at most $W c_{\min}$ evaluations.

A walk that a limit or a cancellation stops stores nothing, and no evaluation
follows it. Lookups precede evaluations of cases 2 and 3 or end a walk.
$\square$

For a site free of the environment there is one class, so $P \le n + 1$ and
the walks of $(\sigma, \lambda)$ evaluate the element at most
$3(n + 1) + W(c_{\min} + 1)$ times while hot, and at most
$2(n + 1) + W(c_{\min} + 1)$ when they are of one kind. For a site that is
not free of the environment, let $K_\sigma$ be the largest number of distinct
combinations of the values of the variables of its key among the visits
that its walks make to one position. Then $P \le K_\sigma (n + 1)$, and the
walks evaluate the element at most
$(2 K_\sigma + 1)(n + 1) + W(c_{\min} + 1)$ times while hot. $K_\sigma$ is
at most the product of the sizes of the value sets of the variables of the
key when each has finitely many values. Otherwise nothing bounds it by the
input: a binding of a new value before each walk makes a new combination at
every position that the walks visit.

## Lemma 3 (Cost Before Hot)

In a parse with $W$ walks of $(\sigma, \lambda)$ before the site turns hot,
those walks evaluate the element at most
$2(n + 1) + (c_{\mathrm{hot}} + 1) W$ times.

Here $c_{\mathrm{hot}}$ is the minimum length of
[Run Sites](../runtime/parsing.md#run-sites), a constant.

*Proof.* A walk from $s$ that ends at $t$ evaluates the element at most
$t - s + 1$ times: each iteration that matches consumes input, and the last
one fails or consumes none. The site tells its state of a walk only when
$t - s \ge c_{\mathrm{hot}}$, and keeps the furthest end $F$ of those
walks ([Run Sites](../runtime/parsing.md#run-sites)). Sort the walks that do
not turn the site hot.

- A walk with $t - s < c_{\mathrm{hot}}$ evaluates the element at most
  $c_{\mathrm{hot}}$ times.
- A walk with $t - s \ge c_{\mathrm{hot}}$ walks less than
  $c_{\mathrm{hot}}$ of the input below $F$, since otherwise it would turn
  the site hot. Above $F$ it walks the input from $\max(s, F)$ to $t$ and moves
  $F$ to $t$. These ranges above $F$ are disjoint across the walks, so they
  add up to at most $n$.

The walk that turns the site hot evaluates the element at most $n + 1$ times,
and it is the last walk before the site is hot. Later walks fall under
Lemma 2. $\square$

So the walks of a site cost, before and after it turns hot, at most a constant
times $K_\sigma (n + 1) + W$ evaluations of the element, with $K_\sigma = 1$
when it is free of the environment.

## Values

Lemma 2 counts evaluations of the element, not the cost of the list of values
that a walk returns. A walk of a site whose iterations build values places its
own values before the recorded suffix that it reaches. When that suffix
starts at the front of its buffer, the walk writes them in place. Otherwise it
copies the suffix. Walks that reach a record in the middle of another walk's
buffer, as walks from positions of different parity do in
`rule main = (r "!" / [ab])* $$; rule r = ("ab" / "b")*` on `ab` repeated
$n$ times, each copy a suffix of length $\Theta(n)$, so the copies cost
$\Theta(n^2)$. The bounds above hold for sites whose walks build no value. A
site whose walks build values is bounded only by the copies of its lists.

## Scope

The lemmas cover the engine's three backends, which keep records for run
sites, in every kind of parse they run, and the generated parsers:

- **Loaded programs.** A [compiled grammar](../runtime/compiled-grammars.md)
  marks in the [instruction](../bytecode/README.md) of each site whether it
  is free of the environment, and lists the variables of the key of every
  other site, and those that it binds, in its
  [run key table](../bytecode/README.md#run-key-table). A program loaded from
  one keys the records of a site by the values of the variables that its
  entry lists unless its instruction marks it free, and binds those that the
  entry lists as bound when it reuses a record, so it keeps the keys of the
  program it was compiled from.
- **Streams.** A [stream](../runtime/streaming.md) drops the records of the
  positions before the input it commits. A dropped record is never looked up
  again, since no walk returns before a commit, so Lemma 1 holds of the
  records that remain, and Lemmas 2 and 3 count fewer evaluations.
- **Documents.** The records of a parse last for that parse: a parse of an
  [incremental document](../runtime/documents.md) starts without them. A
  repetition that the document resumes is not walked as a run site in that
  parse, and runs as an ordinary repetition, which the lemmas do not need.
  Every other site walks as in a fresh parse.

- **Generated parsers.** Parsers generated as Go or TypeScript source keep
  the records of run sites as the engine does, with the same threshold, keys
  and records, in a batch parse, a typed parse (Go), a stream (Go), whose
  records are dropped as above, and a Document, which resumes no run, so that
  every walk in its parse walks as in a fresh parse. The lemmas hold of them
  as of the engine: they rest on what a walk records and reuses, which the
  generated runtimes do as the engine's walk operations do.

## Scan Sites

This section covers the records of [scan sites](../runtime/parsing.md#scan-sites). Lemma 4 is the
exactness of a reused record, Lemma 5 bounds what the records add to the cost of a parse, and Lemma 6
bounds the walks again from one start. Lemma 7 bounds those of a run site, and Lemma 8 what evaluating
a scan-only rule, whose sites are of either kind, again at one position costs.

**Setting.** A scan site $\sigma$ repeats a character class $C$ (or any character) without an upper
bound, with minimum $c_{\min}$, in a rule that calls no rule; it is neither a site of a skip run nor
a `#stream` repetition. The iteration of $\sigma$ is the test of $C$ and nothing else: it calls no
rule, binds and reads no variable, and has no predicate, lookahead, capture or action inside it,
whatever the rule has around $\sigma$. So an iteration reads the one character at its position and
nothing else, and no state of the parse outside the input and the position changes what it gives.
What the rule does around $\sigma$ (a capture or label of it, an action that reads its list, a
predicate, a lookahead, an attribute) runs after the walk or before it and sees only what the walk
gives, so it is evaluated as without records once the walk gives the same. Positions are in the unit of the input
(characters, or bytes when the unit is bytes), and a character of the input takes one or more units.
For a position $r$ let $\mathrm{end}(r)$ be the least position $q \ge r$ at which the input holds no
character of $C$ (the end of the input counts as such a position). The walk of $\sigma$ from $r$
matches the characters that begin at $r, \dots, \mathrm{end}(r)$, up to $q = \mathrm{end}(r)$, and
nothing else. Define $\mathrm{Rest}(r)$ as the tuple of

- the end $q = \mathrm{end}(r)$, a position,
- the number $k$ of characters matched (the iterations that succeed), which equals $q - r$ only when
  every character takes one unit,
- the examined range, which ends at the furthest position that an iteration examined: just after the
  character at $q$ (or at the end of the input), since the iteration that fails at $q$ looks at that
  character and no later one, or further when an earlier iteration read an invalid byte of a
  bytes-unit input together with the bytes that follow it. It is a function of $r$ and the input too,
  and a record holds it whole, which the iteration at $q$ alone does not give,
- the elements: one value for each of the $k$ characters, which depends on the character and its
  position alone,
- the failure of the iteration at $q$: the expectation of $C$ at $q$.

$\mathrm{Rest}(r)$ is a function of $r$ and the input alone, by the setting. A parse with missing-token
insertion keeps no records, so assume it inserts none. The record holds $q$ and $k$ as separate fields,
so nothing below derives one from the other.

**Silence.** A lookahead only silences the expectations that a failed iteration reports
(`Expect` returns when `Silent` is positive). The walk puts none of them into the record, and
$\mathrm{Rest}(r)$ does not depend on them, so a walk in a lookahead and a walk outside it that begin
at $r$ make equal records. This is why a site keeps one table of records for both (the hot state is kept
apart for each, and decides only which walks look).

## Lemma 4 (Exactness of Scan Records)

Every record stored by $\sigma$ at $s$ equals $\mathrm{Rest}(s)$ without the failure, which a walk
that takes the record derives again. A walk that takes the record at its $T$-th iteration ends where the
walk that went on from there would end, with the same outcome, farthest failure and expectations,
examined range, count and elements. No node that a record holds is changed after the record is made.

*Proof.* A walk of $\sigma$ that began at $s$ stores a record when it ends at $q$ after it matched at
least $T$ characters, and only if the input it examined, up to just after $q$, is available (the parse
has all of its input, or that range lies in the part of a stream that is loaded; the input is filled up
to each character before it is examined, so the second always holds unless the input has ended, and the
test only guards that). The iterations it ran are those of the definition, so the record holds
$q = \mathrm{end}(s)$, the count $k$ of characters it matched (not $q - s$), the range it examined (the
first $T$ iterations and the rest, whose union is the range of $\mathrm{Rest}(s)$) and the elements it
built, which are $\mathrm{Rest}(s)$'s. The availability condition makes $q$ the same on any longer prefix
of a stream: the character at $q$ is in the loaded input, so it is not in $C$ whatever follows. This
proves the first claim.

A walk that began at $s$ and has matched $T$ characters is at $s'$, the position after those
characters, and would go on to match the remaining $k - T$ characters and fail at $q$. Taking the record
moves the position to $q$, examines the recorded range and sets the count to $k$ and the elements to the
record's, which hold the first $T$ iterations' as well; they equal those the walk had built (the same
characters at the same positions). The count is the stored $k$, which is the number of characters in
$[s, q)$, so it is the count of the walk that went on, in every unit; no position is subtracted from
another. The loop then runs the iteration at $q$, which fails there with the expectation of $C$ at $q$,
or at the end of the input with that of the end, and examines that character: the outcome, farthest
failure and expectations of the walk that went on. Examined ranges only grow, so touching the recorded
range and then that of the iteration at $q$ gives the range of the walk that went on. The check of the
minimum uses the count $k$. The walk that went on and the walk that took the record leave the same
result.

It remains that a node a record holds is not changed. The elements are nodes that the repetition made
for the characters, and a rule changes a node only if the node is still fresh when it receives it as
the value of its body, to label it or to attach captures; a node that the rule that made it may still
change is final before another holder can see it, as the elements of the records of run sites are
([Run Sites](../runtime/parsing.md#run-sites)). A walk marks every element final when it stores its record, before any other
walk can reach the record, and a walk that takes a record gets elements that are final already. The
element of a scan site is never the value of a rule body in any case: an iteration has no capture and
no action, so the value that the walk gives is the list node that it makes, and what the rule makes of
it is that list, a node that holds it, or the value of an action, which reads the elements and changes
none of them; a capture of the list attaches to the list or to a node around it, never to an element. The list node is new for each walk (it is the node a rule labels) and holds the
shared elements as children only. The same holds for the list node that a walk of a scan site makes at its end: it is made after the elements
are marked, and shares nothing else. So the lists of two walks share nodes that nothing changes, and the
sharing changes no list.

Last, the sharing across [document](../runtime/documents.md) parses. The records are in the tables of
one parse and are dropped with it; no record is carried to the next parse. What persists is a tree,
whose lists may share elements. A reused tree is moved in place when an edit comes before
it, and a node moves only if its generation is below the current one and takes the current one when it
moves, so a node that two lists share moves once. Both lists cover the same range, as the walks began
at one position and ended at one, so the shift that an edit gives to the one is the shift it gives to
the other (the nodes of a reused result move together; an edit never falls inside the input that a
reused result examined). The elements are not empty (a character is at least one unit), so none is
copied as an empty node is, which would break the sharing, and the write of the moved pointer into the
shared slice of children stores the pointer already there. The same sharing of elements between lists
already happens for the nodes that rule memo entries and the records of run sites share. A parser
generated as Go source moves a reused tree of a document by copying the nodes that move into new ones,
so no node that two lists share is written in place. $\square$

Which positions have records, and the hot state of the site, decide only which walks find a record,
never what a record holds, so none changes the result of a parse. A record is looked up and made only
when no [document](../runtime/documents.md) run resumes the repetition, and the records last for one
parse.

## Lemma 5 (Cost of Scan Records)

A parse with the records of scan sites charges at most two steps more for each walk of a scan site
that reaches $T$ characters than the same parse without them, and no more for the other walks. So,
where the walks charge their tests, its steps are at most $1 + 2/(T + 1)$ times those of the parse
without them.

*Proof.* By Lemma 4 a walk that takes a record ends where it would have ended, with the same count,
elements, examined range and expectation, so the parse with records makes the same walks, at the
same positions and in the same order, as the parse without them, and differs only inside the walks
that reach $T$ characters. Without records a walk that matches $k$ characters charges one step for
each test of a character, $k + 1$ in all. (A build that charges no test of a character charges
only the steps of the records below, at most two for each walk, and the ratio is then not
claimed.) With records a walk that matches fewer than $T$ characters
is the same walk. A walk that reaches $T$ characters charges the first $T$ tests, then:

- at a site that is not hot, nothing more than the rest of its tests: noting the walk in the cold
  state charges no step, so it costs $k + 1$;
- at a hot site with a record of its start, one step for the lookup and one for the slot of the
  record, and one for the test of the character at the recorded end, which fails, so it costs
  $T + 3 \le k + 3$;
- at a hot site with no record, one step for the lookup, the rest of its tests and one step for the
  record that it makes, so it costs $k + 3$.

These are the charges that [Scan Sites](../runtime/parsing.md#scan-sites) states for every
backend that keeps records, and for parsers generated as Go or TypeScript source.

Each case costs at most $k + 3$, which is two steps above $k + 1$. A walk that reaches $T$ characters costs at least
$T + 1$ steps without records, which gives the ratio. $\square$

The bound is per walk, so it needs no count of the walks: the walks that start at different
positions of one run, which find no record, each pay at most the two steps, and the cold state,
kept apart for the walks inside lookaheads, only decides which walks look up.

## Lemma 6 (Walks Again From One Start)

Let a parse insert no missing tokens and resume no repetition of a document. For a scan site $\sigma$
and a start $s$, at most three walks of $\sigma$ from $s$ in each mode match more than $T$
characters and do not take a record, where the modes are the four pairs of whether the walk is
inside a lookahead and whether it builds a value. Every other walk of $\sigma$ from $s$ costs at most
$T + 3$ steps.

*Proof.* A walk takes a record of $s$ when its site is hot in its lookahead state and the table of
its value mode holds a record of $s$. The hot state of a pair of a site and a lookahead state only
turns on, and the record of a start, once stored, stays for the parse, since the stream drops only the
records of starts that no later walk reaches. A walk of $\sigma$ from $s$ that matches $T$ characters
or more at a site that is not hot notes itself in the cold state: the furthest end becomes at least
the end of the walk, $\mathrm{end}(s) \ge s + T$. So the next such walk from $s$ in the same
lookahead state starts below the furthest end and walks $T$ characters below it, and turns the site
hot. So at most two walks from $s$ in a mode find the site cold, and once it is hot, the first walk
from $s$ without a record stores one, which every later walk from $s$ in the value mode takes:
at most three walks in a mode take no record. A walk that takes one costs $T + 3$ steps by Lemma 5,
and a walk that matches fewer than $T$ characters costs at most $T$. $\square$

Walks from different starts each pay their own length, which this lemma does not bound.

## Lemma 7 (Run Site Walks Again From One Start)

Let a parse insert no missing tokens and resume no repetition of a document. Let $\sigma$ be a run
site free of the environment with $c_{\min} = 0$, whose element calls no rule and has no repetition
without an upper bound, and let $s$ be a start. Then at most five walks of $\sigma$ from $s$ in each
lookahead state $\lambda$ evaluate the element more than $c_{\mathrm{hot}}$ times and take no
record of $s$. Every other walk of $\sigma$ from $s$ evaluates the element at most
$c_{\mathrm{hot}}$ times, or takes the record of $s$ before its first iteration and evaluates it
not at all, and makes no suffix copy.

*Proof.* The site is free of the environment, so $E(r)$ gives the same in every environment, and
a walk of the value-free twin of the rule gives what a walk with values gives apart from the values
([Setting](#setting)). By Lemma 1, a walk that takes a record ends where the walk that evaluated the
iterations from there would end. So every walk of $(\sigma, \lambda)$ from $s$, of either kind and
with records or without, visits the positions of $\mathrm{Rest}(s)$ and ends at its end $t$. The
element calls no rule, so no walk of $\sigma$ begins inside an iteration of $\sigma$. A walk from
$s$ evaluates the element at most $t - s + 1$ times, as in Lemma 3; if $t - s < c_{\mathrm{hot}}$,
every walk from $s$ evaluates it at most $c_{\mathrm{hot}}$ times, and there is nothing more to
show. Let $t - s \ge c_{\mathrm{hot}}$.

Before the site is hot in $\lambda$: a walk from $s$ that does not turn the site hot moves the
furthest end $F$ to at least $t$ ([Run Sites](../runtime/parsing.md#run-sites)). The next walk from
$s$ in $\lambda$ starts below $F$ and walks $t - s \ge c_{\mathrm{hot}}$ of the input below it, so
it turns the site hot. At most two walks from $s$ find the site not hot.

Once the site is hot, which it stays: a walk from $s$ whose slot of $s$ is fresh starts plain and
marks $s$ visited when it moves past it, which it does since $t > s$. So at most one walk from $s$
finds the slot fresh. Every later walk from $s$ finds it visited or recorded, records from its first
iteration and, since $c_{\min} = 0$, looks up the slot of $s$ before that iteration. If the slot holds a
record for the kind of the walk ([Record Kinds](#record-kinds)), the walk takes it there and
evaluates no iteration. Otherwise it evaluates on, and when it ends it stores a record for every
position that it visited while recording, $s$ among them; a walk that a limit or a cancellation stops
stores nothing, and no step of the parse follows it. A record that notes values serves both kinds
and is never replaced by one without, and the stream drops only the records of positions that no
later walk reaches. So at most one walk of each kind finds no record for its kind: at most two.

So at most $2 + 1 + 2 = 5$ walks from $s$ in $\lambda$ take no record of $s$ and evaluate the
element more than $c_{\mathrm{hot}}$ times. A walk that takes the record of $s$ before its first
iteration has no elements of its own, so its list is the recorded suffix, shared without a copy
([Run Sites](../runtime/parsing.md#run-sites)), and no other walk from $s$ takes a record. $\square$

With $c_{\min} > 0$ a walk takes no record before it has matched $c_{\min}$ iterations, and one
whose iterations build values copies the rest of the recorded list behind its own elements, which
costs the length of that rest each time: walks again from one start of `" "+` whose values a
postfix operator part captures, in the levels of an Operators Rule that nests, cost quadratic
time.

## Lemma 8 (Scan-Only Rules)

Let a parse insert no missing tokens and resume no repetition of a document, and let $\rho$ be a
scan-only rule ([Conditions for Linear Cost](../runtime/parsing.md#conditions-for-linear-cost)) and
$p$ a position. All the evaluations of calls of $\rho$ at $p$ but a constant number cost at most a
constant each, and each of the others costs at most a constant factor of what the first evaluation
at $p$ would cost without records.

*Proof.* Each repetition without an upper bound in $\rho$ is a scan site or a run site of
Lemma 7: $\rho$ calls no rule, so neither does the element of a run site in it. Since $\rho$ calls
no rule and reads no variable, and the walks give what they give without records (Lemma 4 for scan
sites, Lemma 1 for run sites), every evaluation of $\rho$ at $p$ takes the same path through the
body and gives the same result; only the expectations differ, which a lookahead silences. An
iteration of a scan site calls nothing, and one of a run site of $\rho$ evaluates an element with
no repetition without an upper bound and no call, a constant cost. So the path makes the same walks
of sites, from the same starts, in each evaluation, a number $w$ of them bounded by the grammar (the
product of the upper bounds of the bounded repetitions around each site), and outside the walks it
is a finite expression that examines a constant amount of the input, with no scanning source other
than the sites. So an evaluation costs a constant $c$ outside its walks.

By Lemma 6, at most $12w$ walks of scan sites in all the evaluations at $p$ match more than $T$
characters and take no record, and by Lemma 7 at most $10w$ walks of run sites, five in each
lookahead state, evaluate the element more than $c_{\mathrm{hot}}$ times and take no record. So at
most $22w$ evaluations make such a walk. In every other evaluation, a walk of a scan site costs at
most $T + 3$ steps, and a walk of a run site evaluates the element at most $c_{\mathrm{hot}}$
times and, by (R3) of the [linear-cost proof](linear-cost.md), makes at most a constant number of
steps besides, with no suffix copy: the evaluation costs a constant. An evaluation that makes such a
walk is the same evaluation as the first at $p$, and costs what the parse without records pays for
it, with the excess of Lemma 5 for the walks of scan sites and, for those of run sites, at most a
constant number of steps for each iteration (R3) and the suffix copies of a walk that takes a record
of a later position, at most one element for each iteration that the record answers. $\square$

A parse with missing-token insertion keeps no records and is outside the classes. A document that
resumes a repetition resumes it without records, as Lemma 4 states; the bound is for the walks that a
parse makes. Walks from different starts each pay their own length, which these lemmas do not bound:
(R2) of the [linear-cost proof](linear-cost.md) counts them, which Lemmas 2 and 3 show for run sites.
