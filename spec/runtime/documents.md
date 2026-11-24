# Incremental Documents

A `Document` holds a text that changes over time, applies edits to it and parses
the current text. Reparsing after an edit reuses results of the previous parse
that the edit did not affect. Reuse never changes the outcome: a parse of a
document is the same as a parse of the current text from scratch. This chapter
defines the document API, its versions and snapshots, change notifications and
reuse-history controls.

## Scope and Terminology

A **document** is a `Document`, created by `Parser.NewDocument`. The **text** is
the current content of the document. A **version** is the opaque token returned
by `Document.Version`. A **snapshot** is an immutable capture of one revision's
text, parse outcome and value graph. A **batch** is a set of replacements that
apply simultaneously to one base revision.

Positions and edit ranges use the document's position unit, which is fixed when
the document is created; see [Positions](parsing.md#positions).

## Contract

### Construction and Ownership

| Operation | Behavior |
|:--|:--|
| `p.NewDocument(text, opts...)` | Returns a document holding `text`. `WithUnit` fixes the position unit (also the unit of edits) and `WithExpectations` fixes whether syntax errors [record expectations](parsing.md#recording-expectations) (a later `Parse` or `Snapshot` that asks for the other mode is an error); `WithContext` and `WithLimits` set defaults for initialization and later parses. It fails for an unknown start rule and when initialization is interrupted. Nothing is parsed |
| `doc.Text()` | The current text. In `Bytes` it holds the exact bytes of the initial text and the replacements; in `CodePoints` each invalid UTF-8 byte of them is replaced with U+FFFD |
| `doc.Unit()` | The fixed position unit |
| `doc.Version()` | A comparable opaque token identifying the current text revision. A new document starts at sequence 1. Parsing does not advance the version. The token does not retain the document, the text or parse caches |

A `Document` has one owner and is not safe for concurrent use; a `Parser` is safe
for concurrent use. A document is always parsed as a whole: `#stream` has no
effect on it and it is not a stream parse. `WithMissingTokens` fixes the
missing-token limit of every parse ([Missing Tokens](#missing-tokens)).

For `Bytes`, construction can charge no budget, because a large immutable
input need not be scanned or copied. For `CodePoints`, initialization decodes and
normalizes the input, and the inspected input is charged.

### Edits

`doc.Edit(start, end, text)` replaces `[start, end)` with `text`, in the
document's unit. `start == end` inserts and an empty `text` deletes. It returns
an error if the range is outside the text and, in `Bytes`, if `start` or `end`
lies inside a character. In `CodePoints`, invalid UTF-8 in `text` is
normalized, one replacement character per invalid byte. It does not parse. A successful `Edit` advances the
version once, including an edit that changes nothing. A failed edit changes
nothing.

`doc.EditBatch(base, edits...)` applies replacements simultaneously to the text
identified by the token `base`, a `DocumentVersion` that `doc.Version()` returned. Each
edit is an `input.TextEdit`, which replaces `[Start, End)` with `Text`.

- Every range uses coordinates in the original base text and the document's unit.
- Starts MUST be strictly increasing and ranges MUST NOT overlap. A repeated
  start, including several insertions at one position, is rejected.
- A token from another document or from an older revision is rejected, even for
  an empty batch.
- An empty valid batch changes neither text nor version. A successful nonempty
  batch advances the version once and does not parse. Version sequence overflow
  is rejected before any change.
- The complete batch is validated and prepared before it is published. A
  rejected batch leaves the text, version, statistics, reusable state and
  previously returned nodes unchanged.
- In `Bytes`, endpoints are checked against character boundaries of the base
  text. Replacements can create or remove UTF-8 sequences at their seams; the
  next parse decodes them. In `CodePoints`, invalid UTF-8 in replacement text is
  normalized, one replacement character per invalid byte.
- After a successful batch the next parse equals a fresh parse of the resulting
  text. Committing a batch clears reusable parse caches; it does not promise the
  reuse that the same replacements applied as separate `Edit` calls could give.

In `Bytes`, only the interior of a valid UTF-8 character is excluded as an edit
boundary. Each invalid byte is a separate replacement character, so text can be
replaced at it, or inserted before or after it. An edit can join an incomplete
prefix and continuation bytes into a valid character; the next parse
invalidates results that depended on the earlier decoding, including a prefix
completed by an insertion at the end of the text. In `CodePoints`, invalid bytes
have already been replaced with U+FFFD, and editing the normalized text does not
reassemble the original bytes: an invalid byte is one replacement character
whether it stands alone or belongs to an incomplete or ill-formed sequence, so a
replacement never joins with the text around it.

`Edit` and `EditBatch` are outside parse compute limits.

### Parsing

`doc.Parse(opts...)` parses the current text using the construction defaults
overridden by the options of the call; `WithLimits` overrides only the nonzero
fields it sets ([Default Limits](parsing.md#default-limits)). Context, limits and
tracing MAY vary per call; the unit, backend, start rule, non-recognition mode
and missing-token limit MUST NOT, and an option that tries to change them is an
error. Counters reset on
each call. Results and errors are those of `Parser.Parse`, including recovered
`SyntaxErrors`.

`Parse` publishes recovered parser-produced syntax diagnostics as caller-owned
values: it copies each `SyntaxError` and its `Expected` and `Messages` slices, so
a caller's mutation cannot change memoized errors or a later parse. Ordinary parse
failures are newly constructed; caller-supplied errors such as `Context.Err` keep
their identity. This does not detach the mutable `Node` graph, which MAY share
nodes with the document, and nodes that an earlier parse returned MAY be moved in
place by a later one. A later parse moves only the nodes it reuses, so even when it
completes a tree from an earlier parse can be left partly moved, and when it is
interrupted while it moves them no later parse moves them again. The positions in a
tree from an earlier parse are unspecified once a parse after an edit has started; to
keep a tree as it was, take a `Clone` before that parse.
Typed values returned by a generated parser's
`Document.ParseValue` are read-only and shared with the document and earlier results;
its generated `CloneValue` returns an independent copy (see
[Generated Parsers](generated-parsers.md)). Parsing and diagnostic publication use
the same invocation controls. If publication is interrupted, no result is returned and reusable
execution state is cleared for retry.

Under a finite [memory limit](parsing.md#memory-limit), the count of a `Parse`
starts at the bytes the document retains from earlier parses: its memo table,
its repetition records and edits, and the stacks it keeps. If they alone exceed
`Memory`, the document drops them before parsing, as `ResetHistory` does, and the
parse runs from scratch. Retained bytes within `Memory` are kept, so a parse
that reuses them can exceed the limit where a parse from scratch would not; that
interruption clears them, and the next parse starts without them. The retained
bytes are counted as the memory limit counts storage, not as `EstimatedBytes`
estimates it. A parse of a generated Go document, whether it builds nodes
or typed values, counts everything the document keeps for both forms, once: the
memo table and repetition records of node parses, an estimate of the slots and
entries of the typed memo, and the edits; a generated TypeScript document counts
its memo slots and entries, its repetition records and its edits.

### Statistics

| Operation | Behavior |
|:--|:--|
| `doc.Stats()` | `ParseStats` of the last parse. `Evaluated` counts rule bodies run, `MemoHits` and `ResumedElements` count the two kinds of reuse, and `Reused` is their sum |
| `doc.CacheStats()` | Point-in-time counts of the retained reuse cache, read by the owner of the document |
| `doc.ResetHistory()` | Drops memo, repetition and reusable parser history and keeps the text, the version, the construction options and the last statistics. The next parse starts fresh. No threshold applies; the caller chooses when to reset. Nodes returned earlier stop moving through reuse; movement from parses before the reset is not undone |

`CacheStats` has the fields `LiveMemoEntries` (memo entries linked for reuse),
`RetiredMemoEntries` (retired entries retained for reuse), `RepetitionElements`
(elements in resumable repetition runs) and `EstimatedBytes` (approximate
cache-owned backing storage). All four are zero immediately after
`ResetHistory`. `EstimatedBytes` excludes the text, caller-held result trees,
shared strings, shared expectation, error and environment payloads, map buckets
and allocator overhead; it is neither process heap use nor a limit.

### Snapshots

`doc.Snapshot(opts...)` captures the current text revision, parse outcome and
value graph in one immutable `Snapshot`. It resolves invocation overrides once
over the construction defaults, with the same variable and fixed options as
`Parse`. Parsing and freezing use the same cancellation context and memory
limit. Copying the text, freezing nodes and fields, copying diagnostics and
comparing the output with the previous one are
[work of the Document](#work-of-a-document-parse) that a parse from scratch does
not do: no `Compute` limit counts it, and the polls for cancellation and the
memory limit count its steps, so the compute limit stops only the parse. A
snapshot MAY share immutable records with the previous one of the same document;
whether it does is not observable except in memory, and the work it reserves for
sharing is the same whether or not the previous snapshot is still alive: it is a
function of the document and of the new graph alone.

A normally returned parse outcome produces a snapshot, including an ordinary
syntax error and a recovered error. For an ordinary syntax failure the snapshot
keeps the text and version, has a zero root as `Document.Parse` does, and exposes
a defensive copy of the `SyntaxError`. A recovered outcome keeps its root and
`SyntaxErrors`. A parse that nests deeper than the depth limit is a normally returned
outcome too: the snapshot keeps a copy of its `LimitError` of kind `LimitDepth`.
Cancellation or an exhausted memory limit during parsing or freezing, or an
exhausted compute limit during parsing, returns a nil snapshot and a fatal interruption error, which grammar
choice, predicates and recovery cannot absorb. No partial graph, text or
diagnostics are published. If execution began, reusable execution state is
cleared before retry; completed input tables MAY remain and partial tables are
discarded. An error before execution keeps existing caches and statistics.
Cleanup is not charged. A trace callback panic propagates unchanged after
cleanup. Snapshot accessors and `Materialize` run after capture and do not
inherit its context or compute limits.

| Accessor | Meaning |
|:--|:--|
| `snapshot.Root()` | The captured root, a `SnapshotNode` |
| `snapshot.Text()` | The exact text of the revision |
| `snapshot.Version()` | The revision token |
| `snapshot.Error()` | The diagnostic of that revision |
| `snapshot.Materialize()` | A new independent mutable `*Node` graph. Each call creates a separate graph |

`SnapshotNode` is a read-only handle. Its zero value represents a nil node.
`Kind`, `Len` and `Value` answer as `Node.Kind`, `Node.Len` and `Node.Value` do
for the node that was frozen ([Node Model](nodes.md#node-model)), and each takes
constant time: `Kind` is what the node stands for, `Len` is `ChildCount` for a
tuple or list and 0 for any other node, and `Value` is the value of a `KindValue`
node or the string of a `KindString` node and nil for any other. On the zero
value `Len` is 0 and `Value` is nil, and `Kind` panics, as `Node.Kind` does not
accept a nil node.
`ChildCount` and `Child` address the elements of a tuple or list node, and `ChildCount` includes nil slots. `Field(name)` returns a value and a
presence flag that distinguishes an absent field from a present nil value; a typed
nil `*Node` field becomes a zero handle and an untyped nil stays nil. `Fields()`
returns an owned slice of `SnapshotField` values, each a `Name` and a `Value`, in original order. Handles and snapshots are immutable and
safe for concurrent reads while the document has one editing and parsing owner.

Freezing preserves shared node identities, cycles, nil child slots and nil versus
allocated-empty collections. It accepts field values of dynamic type `nil`, `int`,
`string`, `bool` and `*Node`, including a typed nil `*Node`; a boxed value
may have type `nil`, `int`, `string` or `bool`. Any other dynamic
type returns no snapshot and a `*SnapshotCaptureError` that contains the
deterministic path and the `reflect.Type`, without retaining the unsupported
value; the error also carries a defensive copy of the ordinary parse error, if one
existed, and preserves `errors.Is` and `errors.As` behavior for it. `Materialize`
preserves identities, cycles, nil slots and nil versus allocated-empty
collections.

### Interruption

A cancellation or option error detected before execution touches reusable state
preserves valid caches and the last statistics. Once execution starts, an
interruption clears memo and repetition state before the next attempt, because
entries can depend on unfinished left-recursion work and a result that was being
moved in place can be left partly moved. Fully completed input
tables MAY be retained; incomplete tables are never published. The text and
options stay available for retry or editing. A retry with fresh controls MUST
produce the result of parsing the current text from scratch. Syntax errors and
recovered errors are completed outcomes and keep their reusable caches.

### Missing Tokens

With a positive `WithMissingTokens(n)` at construction, every parse of a
Document inserts missing tokens for [`#missing`](../language/pragmas.md#missing)
as a parse of its text from scratch with that limit does: each `Parse` and
`Snapshot` has the whole limit `n`, and attempts of earlier parses do not count.
No result that depends on insertions reaches a later parse. A parse with
insertion memoizes no rule that contains `#missing`, directly or through the
rules it calls ([Evaluations of Rules](parsing.md#evaluations-of-rules)), and
records no run during which an iteration reached a `#missing` literal outside a
lookahead or restored an insertion ([Reuse Model](#reuse-model)); so a result
that a later parse reuses has neither inserted a token nor taken an attempt. A
parse that exhausts the limit returns a `*MissingTokenLimitError` and no result,
and clears the memo and the runs as an [interruption](#interruption) does;
`Snapshot` returns a nil snapshot, so the snapshot published before stays the
latest. A grammar that uses `#missing` parses with strict literals when the
limit is zero.

### Reuse

Reuse never changes a result: every parse that completes returns what a parse
of the current text from scratch returns, run with no compute, memory or
nesting limit. The limits bound the work of the Document parse itself, not that
of the parse from scratch: a result that a parse reuses counts fewer units than
its evaluation, and a call that a reuse answers does not nest, so a Document
parse can complete where a parse of its text from scratch is stopped by the
compute limit or the [nesting limit](parsing.md#limits). Its tree can then nest
deeper than the nesting limit, so code that recurses over the tree must not rely
on that limit to bound its own depth. Reuse does change the
[compute units](parsing.md#compute-units) of a parse. A later parse counts
only the events that it performs: a result that it reuses counts one call, and
its key if the rule is memoized, and a result that it evaluates again counts
the events of its evaluation. Which results a parse reuses is therefore
normative, and the same on every backend:

- the results that a parse stores, under the rules of
  [Evaluations of Rules](parsing.md#evaluations-of-rules) and the transient
  rules that only a Document memoizes ([Reuse Model](#reuse-model));
- the examined range that each entry records, as [Reuse Model](#reuse-model)
  defines it;
- what each edit does to an entry: keep it, shift it or drop it
  ([Reuse Model](#reuse-model));
- the entries that a lookup treats as absent: one that lags behind by more
  than 128 edits, and every entry after the edit that finds 65,536 edits in the
  log or after a batch ([Edits](#edits));
- the results that never reach a later parse: those that read the growing
  result of a left recursion, and every result of a parse that does not
  complete;
- that a store never narrows the calls that the entry it finds answers;
- the run records: which runs a parse records, with at least 16 iterations,
  and which recorded iterations a repetition resumes, at most once for each
  record in a parse ([Reuse Model](#reuse-model)).

These decisions are the whole of the reuse policy. They depend on the text, the
edits and the parses since the Document was created, never on the backend,
the storage or the order in which a backend does its work, so a parse of the
same Document after the same operations counts the same total on every
backend. How a backend carries them out is free and counts nothing
([Work of a Document Parse](#work-of-a-document-parse)): it may apply an edit
to an entry at once or when a lookup finds it, move a reused value or copy it,
index a chain or walk it, and unlink an entry that a lookup treats as absent
whenever it meets it. Reuse is observable through the statistics and the
compute units, not through results.

A lookup that finds a memo entry
applies the edits since the entry was stored or last found, one unit of work
for each edit it applies, which no `Compute` limit counts
([Work of a Document Parse](#work-of-a-document-parse)). An entry that lags behind by more than 128
edits is treated as absent and the call evaluates again, so a lookup applies at most 128 edits and the
[linear cost proof](../proofs/linear-cost.md#setting) bounds their work by that
constant. An edit applies itself at once only to the entries at the positions it replaced
([Reuse Model](#reuse-model)), so it costs time in proportion to them and to its distance from the
previous edit, not to the memo. The number 128 is where reuse stops paying when the edits are
spread over the whole text; it is one constant shared by every backend, and every backend counts
the lag of each entry from the parse that stored it or last found it.

A result that read the growing result of a left recursion in progress when it
started depends on that growth, not only on the text. A parse keeps it only
while the growth at its position lasts (see
[Evaluations of Rules](parsing.md#evaluations-of-rules)), so a later parse
evaluates it again, even when the text is unchanged. A result that a later
parse reuses read no such growing result.

### Reuse Model

The engine and the generated runtimes reuse the results of a Document parse in
the following way. Wherever a rule below keeps a result, evaluating again would
give the same result; wherever it does not, the parse evaluates again.
[Reuse](#reuse) states the guarantee that results do not change.

**Slots.** Slot i of a text of length n is the character at position i
when i < n, and the end of the text when i = n. A terminal, a character
class, `.`, an anchor or the decoding of a character **examines** a slot when
its result depends on what the slot holds:

- matching a character examines its slot, and rejecting one examines it too;
- testing for the end of the input or of a line at i examines slot i;
- testing for the beginning of a line (`^`) or of the input (`^^`) at i
  examines slot i − 1, or at i = 0 the absence of one, written slot -1;
- decoding the character at position i in bytes examines the bytes from i up
  to the first that completes it or makes it invalid, or slot n when the text
  ends inside a valid prefix of a character, and no byte before i.

In bytes, a slot is a byte, and only what decodes a character examines more than
one slot at a time: a character class, `.`, and the `U+FFFD` of a literal, which
matches one invalid byte. Any other character of a literal is compared byte by
byte with its UTF-8 encoding, so a literal that differs at a byte examines no byte
after it, and testing for the end of a line examines the one byte at i. Skipping
an alternative or a call by the character at i is a shortcut of evaluating it and
examines what the evaluation would.

**Examined range.** The examined range of an evaluation is the interval
[l, h) from the least to one past the greatest slot that it, or a call
within it, examined; an evaluation at p that examined no slot has the empty
range [p, p). For an evaluation at p, l ≤ p ≤ h. Every failure that an
evaluation records is at a slot that it examined. The range is sound: every
slot whose content can change the outcome of the evaluation lies in it, so
an edit that changes only slots outside it leaves that outcome as it was.
Every Document records l and h exactly, except that a call at p that reuses
a result kept in a left-recursion context records p − 1 as examined when that
result examined a slot before p, instead of the slots it examined there: those
slots lie inside the growth that made the result, which the call examines too.

**Position-free evaluations.** An evaluation is **position-free** when no
action or predicate in it, or in a call within it, reads a position (the
`.Start` and `.End` members), and its outcome has no recovered errors, whose
lines and columns depend on positions. **Shifting** the outcome of a
position-free evaluation by δ adds δ to its end, to the ranges
of its nodes and to its farthest failure.

**Entries under edits.** At each edit that replaces [a, b) and changes the
length of the text by δ, a memo entry with recorded range [l, h) is:

- kept unchanged if h ≤ a;
- shifted by δ, together with its position and its range, if l ≥ b
  and its evaluation was position-free;
- dropped otherwise.

The values of `Parse` are nodes and basic values, which every backend moves.
`Document.ParseValue`, which only a generated Go parser has, keeps a typed memo
whose values it cannot move unless they are basic values; it drops an entry
whose value holds any other value where these rules would shift it.

Edits are applied to an entry in order, either all at once or when the entry is
next looked up. Every backend applies them to an entry when a lookup finds it, and
at once to the entries at the positions a to b, both included. An entry found to be
dropped may stay where it is, where no lookup returns it, until a store under
its key replaces it, a later edit whose positions include its position drops
it, a walk of its chain finds that it lags behind by more than 128 edits
([Work of a Document Parse](#work-of-a-document-parse)), or the whole memo is
dropped. A batch is one edit for the text and drops
every entry and run ([Edits](#edits)).

**Results that are stored.** A parse stores entries under the rules of
[Evaluations of Rules](parsing.md#evaluations-of-rules), except that it also
memoizes the rules that the first exception there leaves unmemoized in an
ordinary parse (the transient rules); since a leader is memoized in every
case, no leader is memoized only by a Document. The key of a transient rule
holds, for each string variable that the rule reads, the assignment that bound
its value: each assignment of a string that changes the variable is one of its
own over the life of the Document, and backtracking restores the assignments of
the variables with their values. A lookup of a transient rule finds an entry
only where each such variable holds the value of the same assignment, or the
initial value of the variable on both sides, at a constant cost for each
variable; an equal string that another assignment bound, in the same parse or
an earlier one, is a miss, and the rule is evaluated again. A result that read the
growing result of a left recursion is dropped within the parse and never
reaches a later one. A parse that does not complete, because it is
[interrupted](#interruption) or aborted by a run-time error of an action, the
nesting limit or another fatal error, drops the memo entries and the runs, and
publishes no partial table.

**Runs.** A **run record** of a repetition at p holds, for each iteration
that matched, its start, its end, its examined range, its value, and its
farthest failure with its expected items. A Document records only runs of
at least 16 iterations.

- *Recording.* A run is recorded for a repetition that matched outside a
  lookahead and a `skip`, whose element has no predicate, calls no rule that
  reads variables, and writes no capture to an enclosing scope. No recovered
  error remains at the end of the run. During the run no call read the
  current result of a growth, directly or through an entry stored by a call
  that did, even of a growth that started and ended within the run, nor
  reused a result that read one; a run during which such a read or reuse
  happened is not recorded. In a parse with missing-token insertion, no
  iteration reached a `#missing` literal outside a lookahead or restored an
  insertion ([Missing Tokens](#missing-tokens)).
- *Resuming.* A repetition resumes a record only outside a lookahead and a
  `skip`, in one of two ways. **Whole**: the record of the same repetition at
  the same position, stored earlier in the same parse, or by an earlier parse
  of the same text with no edit since; it reuses every recorded iteration.
  **Across one edit**: the record of the previous parse when exactly one edit
  of [a, b) by δ came since, stored at p when p ≤ a, or at p − δ when
  p ≥ b + δ; at a position between them no record is resumed. It reuses the recorded iterations from the first on
  while their examined range ends at or before a, whatever δ is. It
  then evaluates iterations, possibly none. When the current position is
  s + δ for the start s of a recorded iteration j such that every
  recorded iteration from j on examined only slots at or after b, it reuses
  those iterations shifted by δ, which requires δ = 0 or
  position-free iterations. After the reused iterations it goes on as a
  repetition does.

### Work of a Document Parse

A Document parse, on every backend, does work that a parse of the same text
from scratch does not: it applies pending edits, walks and indexes the chains of
memo entries that earlier parses left, moves or copies the values it reuses,
works through the records of runs, grows the memo table, copies diagnostics and
decodes the characters of its text. A `Snapshot`, and a `Parse` with
[change notifications](#change-notifications), also capture and compare the
output, as [Snapshots](#snapshots) describes. None of this work is an event of the parse
([Cost](parsing.md#cost)): no `Compute` limit, the default or an explicit one,
counts it, and it does not change the units that a parse reports, so a parse
reaches the same decision on the same limit whichever way a backend does it:
moving a value, copying it, applying an edit at once or when a lookup finds the
entry. What a parse counts is its calls, their keys and the other events of the
table in [Cost](parsing.md#cost); a result that it reuses counts one call and
its key, whatever the work to bring it up to date.

This section bounds that work instead, in units of work: the steps that an
implementation makes and that the polls for cancellation and the memory limit
count, as they count the steps of any parse. Each bound is an upper bound: an
operation may do less. The bounds are what keeps the time of a Document parse
proportional to what the parse counts, plus the bound of each operation, even
though no limit counts the work. Several of them still grow with what the
Document retains; [Bound of a Document Parse](#bound-of-a-document-parse) states
the bound that the whole work of a parse is to meet.

The work is a function of the logical state, as for every parse: the same
Document after the same operations does the same work for its next parse.

A **chain** is the sequence of memo entries at one position, live or dropped
([Reuse Model](#reuse-model)), in the order a lookup passes them. An entry
**lags** when it lags behind by more than 128 edits, the entry that a lookup
treats as absent ([Reuse](#reuse)).

**Edits.** An edit does no work that a parse counts. A Document keeps a log of the edits since
its memo was last dropped. The edit that finds 65,536 edits in the log drops the
memo entries and the run records and empties the log instead of adding to it, so
a parse finds at most 65,536 pending edits.

**Lookups.** A lookup counts nothing for finding an entry: the call of the rule
paid for the lookup of its key ([Cost](parsing.md#cost)), whatever the lookup
compares. The work of a lookup stays bounded by that payment:

- it passes the entries of the chain, compares the keys of the entries of the
  same rule (a lookup of a rule that only a Document memoizes compares a string
  variable by the assignment that bound its value, so another assignment of an
  equal string is a miss), and removes each lagging entry that it meets from
  the chain without comparing it; each entry is removed once, after the counted
  call that stored it;
- it compares at most 32 entries that do not lag; on a chain that holds more, a
  lookup of a rule that a parse from scratch memoizes too then removes every
  lagging entry of the chain, builds the index of the position if it has none,
  hashes its own key and probes the index, comparing its key with at most as
  many entries as the chain holds. A lookup of a rule that only a Document
  memoizes stops after the 32 entries and misses;
- building an index hashes the key of each entry of the chain. Only an edit and
  the removal of a lagging entry drop the index of a position, so a parse builds
  the index of a position at most once;
- when the entry it finds does not account for every edit of the log, it
  applies the pending edits to the entry, one unit of work for each, at most 128
  ([Reuse](#reuse)), since an entry that lags is never found. This is the only
  work of a lookup beyond the bound above, and no limit counts it.

**Stores.** A store does only bounded work: it walks and indexes the chain as a lookup
does to find the entry it replaces, and links the new entry. When the entry it
replaces lies beyond the first 32, it walks the chain up to it; when it lies
among them and the position has an index, it hashes the key it replaces. A store of a rule that only a Document memoizes walks at most
32 entries, as its lookup does, and when none of them is the entry it replaces,
it links the new entry in front of the chain and replaces none. A store never
narrows the calls that the entry it finds answers
([Evaluations of Rules](parsing.md#evaluations-of-rules)): when that entry was
computed outside a lookahead and the new one inside one, it keeps that entry and
links nothing. In the reference implementation it first
applies to that entry the edits that it does not account for, as a lookup does;
an entry that cannot take them is replaced, and so is an entry whose value
an edit would shift but the memo cannot move ([Reuse Model](#reuse-model)). A store at a position
beyond the slots of the memo table grows them, which counts nothing, as in any
parse ([Cost](parsing.md#cost)).

**Moves.** A backend either moves the value of an entry that an edit shifted
or copies it; the bound below is the engine's, and a copy of the same value is
bounded by the same number of units, one for each node and one for each child
and field it copies. A lookup that finds an entry whose value an edit shifted
([Reuse Model](#reuse-model)) moves the value, once: the entry then accounts for
every pending edit. A move makes one pass over the value. It does one unit of work
for each node it visits and one for each child and field of it. It does not
visit a node that starts where it ends (an **empty** node) in place: it copies it
for the move, with the empty nodes below it, and does the copy as a visit and
at most one more unit of work to forget it at the end; a reference to a node that the
move has already copied costs one unit and copies nothing. A node that does not start where it ends is moved in place, and a
move does not visit a node that this parse built or has already moved, so such a
node moves at most once in a parse. Each node of the value accounts for the
edits up to the parse that last moved or built it. For the nodes that account
for the same number of edits, the move replays the edits that they do not
account for once, at one unit for each, and does at most one unit to forget
them at the end. A move therefore does at most two units for each node it visits,
counting an empty node once for each reference by which it reaches it, and for
each child and field of those nodes, plus one unit for each edit it replays.

**Runs.** The operations of a repetition that may resume a record
([Reuse Model](#reuse-model)) merge the expected items of an iteration into
those of the farthest failure. The operations of runs reserve no unit on any
backend, and a resumed element is no event: its work is bounded in steps, as
follows, but no limit counts it. A **merge** takes one step for each item, and,
when the Document records expected items (`WithExpectations`), at most one more
step for each item that the record of the farthest failure holds when it adds
the item: it may look for the item among them. That record holds each item at
most once, so a merge does at most one plus the number of distinct expected
items of the grammar for each item it merges. The operations take:

- starting the run, and starting the evaluation of an iteration: nothing;
- each attempt to reuse the next recorded iteration: one step, and when it
  reuses one, the merge of its expected items, and, for an iteration after the
  edit, the move of its value;
- ending an evaluated iteration: the merge of its expected items, and, when it
  matched, at most two steps for each expected item that it records (it
  compares them with the last set of expected items that the parse kept and
  copies them unless the two are the same) and, when the number of new
  iterations that it then holds is a power of two, the number that it held
  before;
- looking for a recorded iteration after the edit, before an iteration after the
  edit is evaluated: one step for each recorded iteration that it passes.
  Scanning recorded iterations charges nothing; each recorded iteration is
  scanned at most once per parse, by the search for the iteration that starts
  where the next one would, and at most once more by the check that it and the
  iterations after it examined only input after the edit, since the search
  resumes where it last stopped and an iteration that fails the check bars every
  later look up to it; when it passes them all the run reuses them;
- keeping the values of the iterations that it reuses: one step for each, and
  growing the stack of values as any parse does;
- recording the run when it ends: one step for each iteration, and nothing for a
  run too short to record.

A reuse, and each recorded iteration that the look for an iteration after the
edit passes, counts toward the interval at which the parse observes its context
and its memory limit ([Checks and
Interruptions](parsing.md#checks-and-interruptions)), as a unit does,
without charging it.

A parse resumes a record of the previous parse at most once: resuming it removes
it from the records that the parse can resume.

This work of growing storage is a function of lengths and of the operations of the parse: the capacity that
earlier parses left in storage that the Document keeps for its next parse
changes no work.

**Decoding.** A parse in `Bytes` positions that decodes a character of the text
of a Document does, besides the unit of a parse from scratch, one unit of work for
each further byte that it reads for the decoding: at most three more units for
each examination of the input.

### Bound of a Document Parse

A parse of a Document after edits is to cost at most a constant factor more
than a parse of its text from scratch, however often the Document has been
edited and whatever it keeps from earlier parses. This section states that
bound, a finer bound in terms of what a parse evaluates again, and the
conditions under which they hold. The items marked (not implemented) are
targets that the implementation does not yet meet; the end of this section
lists where it exceeds them.

Let P be a parse of a Document, t the text that it parses and n the length of
t. Let U₀ be the total ([Compute Units](parsing.md#compute-units)) of a parse of
t from scratch with the grammar, the start rule, the position unit, the
expectation mode and the missing-token limit of the Document, run with no
compute, memory or nesting limit; it is the same on every backend. Let U be the
total of P. The **work** of P is its compute units together with the units of
work of [Work of a Document Parse](#work-of-a-document-parse) that it does from
the start of `Parse` to its outcome, the publication of its diagnostics
included. Let:

- J be the length of the text that P copies to make constant the texts that
  cross the pieces of its text ([Cost](parsing.md#cost)): at most a constant
  times n, and 0 when P takes no such text;
- the **moved size** M be the sum of one plus the number of children and
  fields of each distinct node that P moves or copies to bring a reused value
  up to date ([Work of a Document Parse](#work-of-a-document-parse)), an empty
  node counted once however often it is copied;
- R be the number of recorded iterations that P reuses when it resumes runs
  ([Reuse Model](#reuse-model));
- D be the number of recovered errors that P publishes, with their expected
  items.

Then, for a constant c:

1. **Units.** U is at most U₀. P counts the events that it performs: a result
   that it reuses counts its call, and its key if an ordinary parse memoizes
   the rule, which the parse from scratch counts at that call too, and a
   result that it evaluates again counts its evaluation ([Reuse](#reuse)).
   This is argued and checked by the tests on random edits, not proved: which
   calls a parse evaluates also depends on the state of that parse, the memo
   entries that it stores, the evaluations of a key that it keeps and the
   records of its run sites with the walks that make a site keep them
   ([Run Sites](parsing.md#run-sites)), and that state differs between P and
   the parse from scratch.
2. **Work.** The work of P is at most c · (U₀ + J) (not implemented).
3. **Work after edits.** The work of P is at most c · (U + M + R + D + J)
   (not implemented). R is at most U₀ and D at most c · U₀, since the parse
   from scratch evaluates every iteration that P reuses and lists the same
   errors, and M is at most c · U₀ (not implemented). What P evaluates again
   follows from the edits and the examined ranges
   ([Reuse Model](#reuse-model)): P evaluates a call again only where no kept
   entry answers it, and an edit of [a, b) keeps an entry only when its
   examined range ends at or before a, or starts at or after b and its
   evaluation is position-free. So after one edit P evaluates again, among
   others, the calls whose examined ranges overlap the replaced range or, for
   an insertion, cover the slots on both sides of it, the calls at positions
   inside the replaced range, the calls after the edit whose results read
   positions or hold recovered errors, which are dropped even when the edit
   does not change the length of the text, the calls that no earlier parse
   made, and those whose entries lag.

Item 3 bounds the work by U, M, R, D and J; a reparse after a local edit is
fast only if these stay small, and the Reuse Model and the model of values
decide that, not this section. Today they need not: a length-changing edit
moves every later value that P reuses (M); a resumed repetition takes each
reused iteration and builds the list of its values again with every element
(R); the dropped entries after an edit are evaluated again (U); and a text
that crosses the pieces of an edited text is copied, so that a parse after
each edit that takes such a text, the text of the whole result for one, copies
up to the whole text (J).

The constant c depends only on the grammar and the backend. It does not depend
on n, on the edits since the previous parse or on the length of the edit log,
or on anything that the Document retains from earlier parses: its memo entries,
live, dropped or lagging, and the chains that hold them, its run records, the
nodes and values that these reach, the slots of its memo table, the capacity
of the storage that it keeps and the texts of earlier revisions.

The bounds are to hold on every backend that provides a Document (the three
backends of the engine, the `Parse` and `ParseValue` of a generated Go parser
and the `parse` and `parseValue` of a generated TypeScript parser), in either
position unit, with or without the recording of expectations and with
missing-token insertion. They are to hold for a parse that completes, for one
that ends in a syntax error or a run-time error of an action, and for one that
a cancellation or a limit interrupts, counting the work up to the interruption
and the clearing of its state. A parse after a batch, after `ResetHistory` or
after the memory limit drops what the Document retains is a parse from scratch
with the work of the Document added.

Two kinds of work lie outside the bounds. The capture of a `Snapshot` and the
comparison of change notifications follow the outcome and are proportional to
the text and to the graph that they capture, which includes the values that the
parse reused ([Snapshots](#snapshots)). An edit does its own work outside every
parse: in proportion to its text, to the entries at the positions that it
replaces and to its distance from the previous edit ([Reuse](#reuse)), and,
once the text is held in pieces, a walk over the pieces and, when their number
passes a bound, a copy of the whole text into one piece. A backend meets the
bounds without moving work of a parse into the edits beyond that.

Until the bounds are implemented, these bounds of
[Work of a Document Parse](#work-of-a-document-parse) exceed item 2, since they
grow with what the Document retains:

- the removal of lagging entries, the indexes of chains and their probes, and
  the walk of a store to the entry it replaces, which grow with the entries
  that earlier texts left at a position;
- the moves, which replay for each group of nodes the edits that it does not
  account for, up to the length of the log, copy an empty node for each
  reference to it, and can visit more distinct nodes than the parse from
  scratch builds, since values that different parses built share fewer nodes;
- the search in a recorded run of the previous parse, which passes the
  recorded iterations of that parse;
- the growth of the slots of the memo table, which grows with the slots that
  deletions leave;
- the generated runtimes, which copy a reused value at each reuse instead of
  moving it once in a parse.

The engine also clears, at the end of each Document parse, the whole capacity
of its stack of repetition values, which grows with the longest stack of an
earlier parse, against the rule above that the capacity left by earlier parses
changes no work.

## Change Notifications

Notifications are off by default. `doc.EnableChanges(limit)` turns them on for
one document before the edits and parses to be observed. The positive `limit`
bounds the number of queued events; it does not bound the bytes of replacement
text or the size of the comparison snapshot. Enabling twice is rejected.
`doc.DrainChanges()` transfers owned records out and clears the queue. Tracking
retains one comparison snapshot. A record is a `DocumentChange`: its `Kind` (an
`input.DocumentChangeKind`), the versions `From` and `To`, the position `Unit`, the
`Edits` of an edit event as `input.TextEdit` values, `FullRefresh`, the conservative
`Invalidated` and `Reevaluation` of an edit, and the `Output` comparison of an outcome
(an `input.OutputChange`).

There are three kinds of event.

| Kind | Meaning |
|:--|:--|
| `ChangeEdit` | An accepted edit or atomic batch. `From` is the coordinate base and `To` the resulting source revision. Batch edits remain simultaneous replacements in the base coordinates and original order. Failed operations and empty batches emit nothing. Identity edits still advance the version and emit an event |
| `ChangeOutcome` | A `Parse` or `Snapshot` outcome. `From` identifies the preceding comparable capture and `To` the current revision. It MAY cover several previously delivered edits and does not advance an edit-replay cursor, so source revisions and displayed-output revisions are tracked separately |
| `ChangeFullRefresh` | Replaces history that overflowed the limit. Consumers rebuild from the latest state; the marker cannot reconstruct dropped edits or intermediate comparisons. A gap in edit revisions also requires a rebuild |

Edit events mark the whole output conservatively invalidated, and their
re-evaluation is reported as `unknown`, because invalidation and memo statistics
cannot establish execution ranges. Notifications never promise minimal tree
differences.

The first capture reports `OutputMayHaveChanged`. Later complete captures report
`OutputUnchanged` or a conservative `OutputChanged`, comparing immutable node
metadata, positions, string text, elements, scalar fields, syntax diagnostics
and graph sharing. Alias topology and nil versus empty collection differences
count as changes; this is a refresh policy and not language value equality.
Unknown non-syntax diagnostic types count as changed. Source text and parse
statistics are not part of output equality.

With tracking enabled, both `Parse` and `Snapshot` capture and compare under the
invocation's context and memory limit; the capture and the comparison are
[work of the Document](#work-of-a-document-parse), which no `Compute` limit counts.
A cancellation, a memory limit, or a compute limit during the parse returns nil and emits `OutputUnavailable`, which clears comparison
continuity: the next successful capture reports `OutputMayHaveChanged`. Completed
syntax failures stay comparable. Unsupported captures also report unavailable.

Snapshots that consumers retain stay immutable. `Parse` still returns its mutable
`Node`, which later parses of the document can change; a `Snapshot` never changes.
The `To` of a `ChangeEdit` is the document version after the edit, and the `To` of a
`ChangeOutcome` is the document version when the `Parse` or `Snapshot` ran. The
version of the latest outcome can therefore be older than the version of the latest
edit: edits that follow an outcome are not part of it.

## Validation and Errors

| Condition | Result |
|:--|:--|
| `NewDocument` with `Recognize`, an unavailable backend or an unknown start rule | Error; no document |
| A negative `WithMissingTokens` limit in `NewDocument` | Error; no document |
| `Edit` range outside the text, or inside a character in `Bytes` | Error; text and version unchanged |
| `EditBatch` with a foreign or stale version, unordered or overlapping edits, repeated starts or invalid ranges | The whole batch is rejected |
| Parse option that changes unit, backend, start rule, mode or missing-token limit | Error before execution; caches and statistics kept |
| Missing-token limit exhausted during `Parse` or `Snapshot` | `*MissingTokenLimitError`, no result and a nil snapshot; execution state cleared ([Missing Tokens](#missing-tokens)) |
| Cancellation, or a compute or memory limit, during `Parse` | Error and no result; execution state cleared |
| Cancellation or a memory limit during `Snapshot`, or a compute limit during its parse | Nil snapshot and a fatal error |
| Unsupported field value type while freezing | Nil snapshot and `*SnapshotCaptureError` |
| `EnableChanges` twice | Error |
| Stream diagnostic options (`WithDiagnosticCallback`, `WithDiagnosticLimit`) | Rejected |

## Compatibility and Support

All three engine backends and both position units support documents. Generated Go
parsers provide an opt-in `Document` with `NewDocument`, `Text`, `Edit`, `Parse`,
`Stats` and `Node.Clone`, and, with typed values, `Document.ParseValue`; they have
neither `EditBatch`, `Version`, `Unit`, `Snapshot`, `CacheStats`, `ResetHistory`
nor change notifications. `Parse` reuses memoized results and resumes runs as
the engine does, and their `Stats` counts both; `ParseValue` keeps a typed memo of
its own, which resumes no runs.
Generated TypeScript parsers provide an opt-in `Document` with `createDocument`,
`text`, `bytes`, `edit`, `parse`, `stats` and, with typed values, `parseValue`.
`text` and `bytes` return the current text as `doc.Text()` defines it, in
`CodePoints` with each invalid UTF-8 byte replaced with U+FFFD. It
reuses the memoized results of earlier parses and resumes runs as the generated Go
`Parse` does, in `parse` and in `parseValue`, which converts the result of such a
parse, and its `stats` counts both. See
[Generated Parsers](generated-parsers.md#incremental-documents).

## Conformance

The behavior is established by the document tests of the repository, which
compare random edits with fresh parses, and by the edit fuzzer of
`sxir edit-fuzz`, which compares every reparse with a fresh parse.
