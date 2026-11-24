# Operations

This chapter lists the operations on the state of a parse, by the state component they belong to; [Values](values.md)
lists those on the values of action expressions. Each entry gives the declaration on one line, as the [text form](printing.md#catalog-entries)
writes it, the components and registers the operation reads and writes, its cost bound (see
[Costs](../code-ir/module.md#costs)) and its contract, with its precondition and its laws where it has them. The cost of an operation that counts no compute unit (a free operation of the conformance suites) is its work bound, not a charge: it charges nothing, and the compositions count the events around it.

## Memo Table

The operations of the component `memo`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op memo.lookup(rule int, pos int, level int, key Values, docOnly bool) (Entry, bool) noreturn` | reads memo | `0 + 1*pending_edits` | Returns the entry stored under the key, if one is there and not replaced, and true; otherwise false and an entry that must not be used. Two keys are equal when their rules, positions and levels are equal and the host's value equality holds for their values. A lookup with docOnly set, for a rule that only a Document memoizes, finds an entry only if each string of the key holds the value of the same assignment as the entry's (env.key), or the initial value of the variable on both sides, not merely an equal value, which it decides at a constant cost for each variable, and finds none beyond the first entries of a long chain: a miss is always sound, since the rule is then evaluated. Law (stable read): two lookups with no write of memo between them return the same result. |
| `op memo.store(rule int, pos int, level int, key Values, ok bool, end int, silent bool, value Value, errs Errs, docOnly bool) Entry` | reads memo; writes memo | `0` | Stores a complete entry with the outcome ok, the end, whether it was computed in a silent context, its value and its recovered errors under the key, replacing the entry stored under the same key, and returns it. The store never narrows the calls that the entry under the key answers: when a lookup would return that entry and it was computed outside a silent context while the new one is computed inside one, the store keeps it, stores nothing and returns it; otherwise the new entry starts with the present set of the entry it replaces if a lookup would return that one (memo.setPresent), and with none if not. Entries under other keys are unchanged. With docOnly set, the key of a stored entry matches only the key of a lookup with the same values (see memo.lookup), so the store replaces the entry of the same values only. A target that keeps records beside the entries (memo.examine) drops those of the entry that the store replaces. Requires: end is at least pos. The handle of the entry that the store replaces is not used after it: an implementation may reuse it for a later entry. |
| `op memo.prune(pos int) [stream] noreturn` | reads memo; writes memo | `0 + 1*pruned + 3*covered` | Removes the entries stored at positions before pos and raises the floor of the memo to pos. Below the floor the memo keeps nothing: a lookup there finds no entry, and a store there returns its entry without keeping it. The removed entries are no longer valid; entries at or after pos are unchanged. Requires: pos is at least the floor (memo.floor). The handles of the removed entries are not used after the prune: an implementation may reuse them for later entries. |
| `op memo.floor() int [stream]` | reads memo | `0` | Returns the floor of the memo: 0 at the start of a parse, then the position of the last memo.prune. |
| `op memo.examine(e Entry, before bool, end int) [document]` | reads memo; writes memo | `0` | Records with entry e the input that the call which computed it examined, for the reuse of e after an edit (memo.splice): from the position of e, or from the unit before it if before (a call reads at most that unit before its position, to tell a line start), to end, exclusive, and for the report of a call that reuses e in an observed parse (memo.examined). The record replaces those that memo.examine and memo.pin made before. Requires: end is at or after the position of e. |
| `op memo.examined(e Entry) int [document]` | reads memo | `0` | Returns the end of the input that memo.examine recorded with entry e, or 0 if none was recorded. |
| `op memo.before(e Entry) bool [document]` | reads memo | `0` | Reports whether the input that memo.examine recorded with entry e starts before its position; false if none was recorded. |
| `op memo.pin(e Entry) [document]` | reads memo; writes memo | `0` | Marks entry e as depending on positions, not only on the input it examined: an edit before it removes it instead of moving it (memo.splice). |
| `op memo.splice(start int, end int, delta int) [document]` | reads memo; writes memo | `0` | Applies an edit of the input that replaces [start, end) with input delta units longer. An entry whose recorded examined input ends at or before start stays where it is. An entry whose recorded examined input starts at or after end, that is not pinned and has no recovered errors, moves by delta: it is found at its position plus delta, and its end and its examined input move with it, and its value moves when it is reused (doc.move). Every other entry is removed. The handles of all entries from before the edit are no longer valid: a target may copy the entries it keeps. What a target records with an entry besides these moves with it as that record says (memo.setExpected). The operation costs nothing: a target may apply the edit to an entry when it next finds the entry instead of at once, so that memo.lookup does that work, one step for each edit applied. Lookups then see what they would have seen had the edit applied at once. Requires: 0 <= start <= end, and start <= end + delta. |
| `op memo.ok(e Entry) bool` | reads memo | `0` | Returns whether the call of entry e succeeded. |
| `op memo.end(e Entry) int` | reads memo | `0` | Returns the end of the match of entry e; for a failure, the position of the call. |
| `op memo.silent(e Entry) bool` | reads memo | `0` | Returns whether entry e was computed in a silent context. |
| `op memo.value(e Entry) Value` | reads memo | `0` | Returns the value of entry e. |
| `op memo.errs(e Entry) Errs` | reads memo | `0` | Returns the recovered errors of entry e. |
| `op memo.far(e Entry) int` | reads memo | `0` | Returns the farthest position of the expectations of entry e. Requires: memo.setExpected recorded expectations with e. |
| `op memo.expected(e Entry) ExpKept` | reads memo | `0` | Returns the expectations of entry e. Requires: memo.setExpected recorded expectations with e. |
| `op memo.setExpected(e Entry, far int, k ExpKept)` | writes memo | `0` | Records the expectations k at far of the call that computed entry e. When memo.splice moves e, far moves with it, by the same amount as its end. |
| `op memo.setPresent(e Entry, c Ctx)` | reads growth; writes memo | `0` | Adds to the present set of the entry e that of the context state c, for ctx.allows: the entry answers the calls in contexts whose leaders are all in the union, since a result stored in the memo read no growing result and is the same in each of them. Requires: the present set of c fits in a word of bits, which a call whose result ctx.pop sends to the memo meets. |
| `host op doc.move(e Entry) bool [document] noreturn` | reads memo, host; writes memo | `0 + 2*moved + 1*move_edits` | Makes the value of entry e valid at the position of e, after memo.splice moved e: moves the positions in the value by as much as the edits moved e since the value was made. A target that may not change a value that the parse returned moves a copy; one whose contract says that a reuse moves the nodes of earlier results moves them in place, where each node records how many edits its positions account for. Returns false, changing nothing, when the target cannot move the value (a value of the host that is not made of nodes and of basic values); the call that finds e then evaluates the rule again. Returns true for an entry that no edit moved. The cost has two parts. Each pass over the value costs, for each distinct node it visits, one step and one for each child and field. A target that copies checks the value and then copies it, in two passes, and a refusal costs no more than the check of the nodes it reached. A target that moves in place makes one pass, costs a second step for each reference to an empty node, which it copies, and for each copy it forgets at the end, and replays the edits since the generation of a node that another result moved, once for each generation. |
| `host op memo.seed(e Entry) bool [document]` | reads memo | `0` | Reports whether entry e is the seed of a growth, which no edit keeps. It guards memoAdvance: a seed lives within one parse, when no edit is pending, so the guard is not reached in a parse; only a case of conformance, which stores seeds without a parse, reaches it. |
| `host op memo.from(e Entry) int [document]` | reads memo | `0` | Returns the start of the input that entry e examined, as of the edits it accounts for. |
| `host op memo.pinned(e Entry) bool [document]` | reads memo | `0` | Reports whether a call pinned entry e (memo.pin). |
| `host op memo.shifted(e Entry) bool [document]` | reads memo | `0` | Reports whether an edit moved entry e since its value was last made valid (doc.move). |
| `host op memo.applied(e Entry) int [document]` | reads memo | `0` | Returns the number of edits that entry e accounts for. |
| `host op memo.advance(e Entry, from int, examined int, shift int, shifted bool, applied int) [document]` | writes memo | `0` | Records that entry e accounts for the first applied edits: its examined input is [from, examined), its position, its end and the far position of its expectations move by shift, the shift of its value grows by shift, and shifted tells whether its value has to move. |
| `host op edit.count(log EditLog) int [document]` | reads memo | `0` | Returns the number of edits of the log. |
| `host op edit.start(log EditLog, i int) int [document]` | reads memo | `0` | Returns the start of the input that edit i replaced. Requires: 0 <= i < edit.count(log). |
| `host op edit.end(log EditLog, i int) int [document]` | reads memo | `0` | Returns the end of the input that edit i replaced. Requires: 0 <= i < edit.count(log). |
| `host op edit.delta(log EditLog, i int) int [document]` | reads memo | `0` | Returns how many units longer the replacement of edit i is than the input it replaced. Requires: 0 <= i < edit.count(log). |

## Deferred Memoization

The operations of the component `seen`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op seen.first(rule int, pos int) bool` | reads seen; writes seen | `0` | Records a call of the rule at pos and reports whether its memoization is deferred: true when the rule is not eager and has not been called at pos before. Each rule counts its calls and its repeated calls, the calls at a position where it was called before. A call that finds the rule eager returns false and records nothing. Otherwise the call count grows by one, and when it reaches 2^31 both counts are halved (rounding down); a repeated call then grows the repeat count, and when the repeats exceed the calls divided by 16 (rounding down), the rule becomes eager for the rest of the parse. The policy decides how many rule bodies run, which the statistics report, so every target follows it exactly. The record costs nothing, and deferring memoization changes no unit that a call charges or counts. Requires: 0 <= rule < the number of rules the component was created with; pos >= 0. |
| `op seen.defers() bool` | reads seen | `0` | Reports whether the parse defers the memoization of a rule at a position to the rule's second call there (seen.first): a parse of a whole input or of a prefix does, and a stream parse and a Document parse memoize on the first call. |

## Budget

The operations of the component `budget`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op budget.charge(n u64) [budget] noreturn` | reads budget; writes budget | `0 + 1*n` | Charges n units, as n charges of one unit do. When the limit is not zero and the units used plus n exceed it, the parse ends with a limit interruption at the first unit that does not fit, which reports the limit as the units used and one unit requested; otherwise n is added to them. Charges are never rolled back. A target may also observe cancellation here, at intervals of its choice, and end the parse with a cancellation interruption. Law (charge merge): charge(a); charge(b) may merge into charge(a+b), and a charge may move past a call, only when no call between them writes `observe` or `window`, calls back into the caller, or commits; what an interruption exposes is then the same (see [Checks and Interruptions](../runtime/parsing.md#checks-and-interruptions)). |
| `op budget.chargeKey(r Rule) [budget] noreturn` | reads budget, env, rules; writes budget | `0 + 1*vars + 1*key_bytes` | Charges the lookup of the memo key of a call of the rule, as budget.charge does: one unit for each variable that the rule reads and one for each byte of the UTF-8 encoding of each string value that such a variable holds. A call of a rule that a parse from scratch memoizes pays it whether or not it looks the key up, so that deferring memoization changes no unit; a rule that reads no variable charges nothing. |
| `host op budget.poll() noreturn` | reads budget; writes budget | `0` | Counts a step of work that charges no unit toward the interval at which the parse observes cancellation and its memory limit, and observes them at the end of each interval: with budget controls where budget.charge of one unit would, and with logical compute units, which such work does not advance, after each interval of as many of these steps. It charges and counts nothing, so it never ends the parse with a limit interruption of Compute; it may end it with a cancellation or memory interruption. Only builds with budget controls or logical units (Budget, Units) call it. |
| `op budget.used() u64 [budget]` | reads budget | `0` | Returns the units charged so far: by budget.charge and inside other operations. |
| `op budget.freeBegin(on bool) u64 [budget, document]` | reads budget; writes budget | `0` | Opens a region of Document work when on is true, and returns the mark that budget.freeEnd takes. The units that the region charges are work that only a Document parse does: no Compute limit, the default or an explicit one, counts them, and they are no event of the parse. They still advance the polls for cancellation and memory. The region must end before any work that a parse from scratch counts starts, and a region does not nest. When on is false, it opens no region and charges nothing. |
| `op budget.freeEnd(mark u64) [budget, document] noreturn` | reads budget; writes budget | `0` | Ends the region that budget.freeBegin opened with mark, if it opened one. It charges nothing. |
| `op units.count(n u64) [units]` | reads budget; writes budget | `0` | Counts n logical compute units: adds n to the units counted so far, without comparing them with the limit. A count never runs ahead of the events it counts. |
| `op units.check() [units] noreturn` | reads budget; writes budget | `0` | Checks the units counted so far. When the limit is not zero and they exceed it, the parse ends with a limit interruption that reports the limit and the units counted, and no units requested. The check also observes cancellation, and then the memory count, once at least 256 units have been counted since they were last observed, and ends the parse with their interruption first. The parse adds the units of the events performed so far before it checks, so that the count compared is exactly their sum. |
| `op units.key(r Rule) [units] noreturn` | reads budget, env, rules; writes budget | `0` | Counts the key of a call of the rule, a sized event: one unit for each variable that the rule reads and one for each byte of the UTF-8 encoding of each string value that such a variable holds, and checks the counter as units.check does when it counts any. A rule that reads no variable counts nothing. |
| `host op stream.check() [stream] noreturn` | reads budget; writes budget | `0` | Observes cancellation and the limits of the parse at a stream boundary, and ends the parse with the interruption that applies. |

## Nesting Depth

The operations of the component `depth`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op depth.enter() noreturn` | reads depth; writes depth | `0` | Enters a rule call: adds one to the depth. When the depth then exceeds the limit, the parse ends with a nesting interruption. |
| `op depth.exit()` | reads depth; writes depth | `0` | Leaves a rule call: subtracts one from the depth. Requires: The depth is greater than zero. |
| `op depth.current() int` | reads depth | `0` | Returns the depth. |

## Variable Environment

The operations of the component `env`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op env.lookup(slot int) Datum noreturn` | reads env | `0 + 1*env_path` | Returns the current value of the variable in slot. Requires: slot is the slot of a declared variable. Law (stable read): two lookups with no write of env between them return the same value. |
| `op env.bind(slot int, v Datum) noreturn` | reads env; writes env | `1 + 1*value_compare + 1*env_rebind` | Makes v the value of the variable in slot, in a new environment that shares what it can with the current one. When the host's value equality holds between v and the value the slot holds, the environment stays the one it is, so that equal bindings keep what depends on its identity. Environments that env.scope returned earlier do not change. Requires: slot is the slot of a declared variable, and v is a value that it may hold. |
| `op env.scope() Scope` | reads env | `0` | Returns the current environment, which env.restore takes. |
| `op env.restore(s Scope)` | writes env | `0` | Makes s the current environment: the bindings made since it was current are gone. |
| `op env.key(r Rule) Values` | reads env | `0` | Returns the values of the variables the rule reads, as its memo key. Two keys are equal when they have the same length and the host's value equality holds for each pair of their values. In a Document parse, the key of a rule that only a Document memoizes also tells for each string variable which assignment bound its value: each env.bind of a string that makes a new environment is an assignment of its own, over the life of the Document, and env.restore brings back the assignments of the environment it restores. Taking the key costs nothing: the call charged or counted it (budget.chargeKey, units.key). |

## Recovered Errors

The operations of the component `recovered`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op rec.mark() int` | reads recovered | `0` | Returns the number of entries of the list, which rec.truncate and rec.since take. |
| `op rec.push(e Err)` | writes recovered | `0` | Records e after the recovered errors, as one entry. |
| `op rec.entries(mark int) int` | reads recovered | `0` | Returns the number of entries after the mark: those that rec.since takes as its list. Requires: mark is a mark of the list: a result of rec.mark at most its current length, and no rec.since with a smaller mark has run since rec.mark returned it. |
| `op rec.truncate(mark int)` | writes recovered | `0` | Drops the entries after the mark, and with them their errors. Requires: mark is a mark of the list: a result of rec.mark at most its current length, and no rec.since with a smaller mark has run since rec.mark returned it. |
| `op rec.since(mark int) Errs noreturn` | reads recovered; writes recovered | `0 + 1*segs` | Returns the list of the errors recovered after the mark, and puts it in place of the entries after the mark, so that the errors stay the same and the list has one entry after the mark. When there is one entry after the mark, an error or a list, returns it as the list, and the list does not change. When there is none, returns an empty list and the list does not change. A list never changes once made: later writes of the list do not change it. Requires: mark is a mark of the list: a result of rec.mark at most its current length, and no rec.since with a smaller mark has run since rec.mark returned it. |
| `op rec.add(errs Errs)` | writes recovered | `0` | Records the errors of errs again, after the recovered errors, as one entry that shares errs; an empty list records nothing. |
| `op rec.none(errs Errs) bool` | none | `0` | Reports whether the list of errors is empty. The null handle is the empty list. |
| `op stream.settleErrors() [stream] noreturn` | writes recovered | `16` | Forgets the recovered errors, which were delivered and collected, and counts none as delivered. It costs one step for each slot it clears of the scratch it keeps for the next element, which holds at most 16; a larger list is released without clearing it. Requires: The parse retains the tail (stream.tail), and no call holds a mark of the list. |
| `op missing.active() bool [missing]` | reads recovered | `0` | Reports whether the parse runs in the missing-token mode. |
| `op missing.recompute(r Rule) bool [missing]` | reads recovered, rules | `0` | Reports whether a result of the rule is evaluated again instead of being reused from the memo: the parse runs in the missing-token mode and the rule or a rule it calls can insert a missing token. |
| `op missing.mark() int [missing]` | reads recovered | `0` | Returns a mark of the missing-token record, for missing.reset. |
| `op missing.reset(mark int) [missing] noreturn` | writes recovered | `0 + 1*tokens` | Removes the tokens inserted after mark, in the missing-token mode, charging one unit for each; does nothing outside it. Requires: mark is a result of missing.mark at or before every later mark that is still in use. |
| `op missing.replay(e Entry) [missing] noreturn` | writes recovered | `0 + 1*tokens + 1*errs` | Records again the tokens that the call of entry e inserted and the errors it recovered, as rec.add does for the errors outside the missing-token mode. It charges one unit for each token; when one of the tokens is already present, it records the errors one by one without that token's, for one unit each, and otherwise it shares them as rec.add does. Requires: The parse runs in the missing-token mode. |
| `op missing.replayResult(s CtxResult) [missing] noreturn` | writes recovered | `0 + 1*tokens + 1*errs` | Records again the tokens that the call of the result s that a context kept inserted and the errors it recovered, as missing.replay does for a memo entry, at the same cost. Requires: The parse runs in the missing-token mode. |
| `op opr.replay(a Attempt) [missing] noreturn` | reads operators; writes recovered | `0 + 1*tokens + 1*errs` | Records again the tokens that the match a inserted and the errors it recovered, as missing.replay does for the call of a memo entry and at the same cost; outside the missing-token mode, where a holds no token, it records the errors as rec.add does. Requires: opr.found(a). |

## Expectations

The operations of the component `expected`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op exp.isolate(pos int) ExpMark` | reads expected; writes expected | `0` | Sets the record aside and starts an empty one at pos. |
| `op exp.unisolate(m ExpMark) (int, ExpSet)` | reads expected; writes expected | `0` | Ends the record that the isolation m started and restores the one set aside; returns the farthest position of the ended record and its expectations. Requires: m is the innermost isolation that has not ended. |
| `op exp.farthest() int` | reads expected | `0` | Returns the farthest position of the current record. |
| `op exp.recording() bool` | reads expected | `0` | Reports whether the parse records expectations. A parse that does not keeps one placeholder entry in the record of a failure, which stands for the failure at the farthest position and is not an expectation. |
| `op exp.reset(pos int)` | writes expected | `0` | Starts the current record anew at pos: the farthest position becomes pos and the entries are dropped. |
| `op exp.fresh() bool` | reads expected | `0` | Reports whether the current record has no entry. |
| `op exp.placeholder()` | reads expected; writes expected | `0` | Adds the placeholder entry to the current record. |
| `op exp.maybe(id ExpID) bool` | reads expected | `0` | Reports whether id may be in the current record: false means it is not; true means it may be. Requires: id is an expectation, not the placeholder. |
| `op exp.count() int` | reads expected | `0` | Returns the number of entries of the current record. |
| `op exp.at(i int) ExpID` | reads expected | `0` | Returns entry i of the current record, in the order they were added. Requires: 0 <= i < exp.count. |
| `op exp.add(id ExpID)` | reads expected; writes expected | `0` | Adds id to the current record, after its other entries. |
| `op exp.setCount(s ExpSet) int` | none | `0` | Returns the number of expectations of s. |
| `op exp.setAt(s ExpSet, i int) ExpID` | reads expected | `0` | Returns expectation i of s, in the order they were recorded. Requires: 0 <= i < exp.setCount(s); s is the set that exp.unisolate returned last, no isolation began or ended since, and every entry written to the record since then was written at an index below n + i, where n is the number of entries of the record when exp.unisolate returned s. Merging s into the record entry by entry, reading each entry before the write that follows it, meets this. |
| `op exp.keptCount(k ExpKept) int` | none | `0` | Returns the number of expectations of k. |
| `op exp.keptAt(k ExpKept, i int) ExpID` | none | `0` | Returns expectation i of k, in the order they were recorded. Requires: 0 <= i < exp.keptCount(k). |
| `op exp.empty() ExpKept` | none | `0` | Returns the kept set with no expectations. |
| `op exp.last() ExpKept` | reads expected | `0` | Returns the set that exp.copy made last in the parse, and the empty set before the first. |
| `op exp.copy(s ExpSet) ExpKept noreturn` | reads expected; writes expected | `0 + 1*set` | Copies the expectations of s, in order, into a set that outlives the record, makes it the set that exp.copy made last and returns it. The copy costs what the target's copies of values cost, and the memory it takes is accounted as the target accounts it. Requires: s has at least one expectation, and exp.setAt may read each of them. |

## Left-Recursion Growth

The operations of the component `growth`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op ctx.enter(r Rule, min int) (Ctx, CtxResult) noreturn` | reads growth, rules; writes growth | `1 + 1*present` | Begins a memoized call of the rule at the position: returns its context state and, if the rule is a leader that grows at the position, its current seed, which the call reads (with the dependency on its identity); otherwise the null result. The context of a call of a rule of a left-recursive cycle is the growths of its cycle in progress at the position; another call has none. |
| `op ctx.found(s CtxResult) bool` | none | `0` | Reports whether s is a result, not the null result. |
| `op ctx.ok(s CtxResult) bool` | reads growth | `0` | Returns whether the result s succeeded. |
| `op ctx.value(s CtxResult) Value` | reads growth | `0` | Returns the value of the result s. |
| `op ctx.end(s CtxResult) int` | reads growth | `0` | Returns the end of the match of the result s; for a failure, the position of its call. |
| `op ctx.errs(s CtxResult) Errs` | reads growth | `0` | Returns the recovered errors of the result s. |
| `op ctx.far(s CtxResult) int` | reads growth | `0` | Returns the farthest position of the expectations kept with the result s. |
| `op ctx.expected(s CtxResult) ExpKept` | reads growth | `0` | Returns the expectations kept with the result s. |
| `op ctx.examined(s CtxResult) int [document]` | reads growth | `0` | Returns the examined end that the parse recorded with the result s, or 0 if it recorded none. |
| `op ctx.before(s CtxResult, pos int) bool [document]` | reads growth | `0` | Reports whether the input that the result s, kept for a call at pos, examined starts before pos. |
| `op ctx.allows(c Ctx, e Entry) bool` | reads growth, memo | `0` | Reports whether the call with the context state c may reuse the memo entry e: always outside a context, and in one only when the leaders growing there are among those that grew where e was stored (its present set). An entry holds a present set of one word of bits, so it answers no call in a context with a leader that has no bit there. |
| `op ctx.active(c Ctx) bool` | none | `0` | Reports whether the call with the context state c is in a context. |
| `op ctx.lookup(c Ctx, r Rule, min int, key Values) CtxResult noreturn` | reads growth; writes growth | `0 + 1*table + 1*deps` | Returns the result kept in the context of the call for the rule at the position, the level and the key that holds there: the seeds it read have the same identities, and the lookaheads it observed the same outcomes, in the current context; it records those dependencies for the enclosing call. Otherwise the null result. Requires: c is in a context. |
| `op ctx.push(c Ctx)` | writes growth | `0` | Starts recording the dependencies of the evaluation of the call, if it is in a context. |
| `op ctx.pop(c Ctx) int noreturn` | reads growth; writes growth | `0 + 1*deps` | Ends recording the dependencies of the evaluation and returns where its result goes: ctx.storeMemo for the memo (it read no seed, made no observation and its context's leaders all have a bit in a word of bits), ctx.storeCtx for the table of the context, or another value for nowhere (its context records no dependencies). |
| `op ctx.storeMemo() int` | none | `0` | Returns the value of ctx.pop for a result stored in the memo. |
| `op ctx.storeCtx() int` | none | `0` | Returns the value of ctx.pop for a result kept in the table of its context. |
| `op ctx.keep(c Ctx, r Rule, pos int, min int, key Values, ok bool, end int, silent bool, value Value, errs Errs, far int, k ExpKept) noreturn` | reads growth, call; writes growth | `0 + 1*table + 1*deps` | Keeps the result of the call in the table of its context with the dependencies that ctx.pop ended, and, in a parse that tracks it (doc.tracking: a Document or an observed parse), its examined end. Requires: The last ctx.pop of c returned ctx.storeCtx. If r leads a cycle, its growth at pos has ended (grow.end) and the result passed is the one grow.end returned: the result of a leader is final only then. |
| `op ctx.done(c Ctx) noreturn` | writes growth | `0 + 1*deps` | Passes the dependencies that ctx.pop ended to the evaluation that encloses the call. |
| `op grow.begin(r Rule, min int) Grow noreturn` | reads growth, rules, pos; writes growth | `1 + 1*present` | Starts the growth of the leader at the binding level min at the position from a failure, at a cost of one step, and present for naming the present set of its context when it is in one: the growth becomes the innermost, with a failed seed that ends at the position. Every failed seed has the same identity, and every seed that grow.adopt or grow.adoptStep adopts has a new one. The growth is in a context when other leaders of its cycle grow at the position. Requires: r is a leader that does not grow at the position among the growths of its cycle there, and no growth of another cycle at the position is the innermost. |
| `op grow.ctx(g Grow) bool` | reads growth | `0` | Reports whether the growth g memoizes its steps in its context (grow.memoStep and grow.keepStep): when other leaders of its cycle grow at its position, unless the parse keeps no results in contexts. A growth that does not takes its steps with grow.longer and grow.adopt. Requires: g is the innermost growth. |
| `op grow.longer(g Grow, ok bool) bool` | reads growth, pos | `0` | Reports whether a step of the growth g with success ok, ending at the position, matched more than the seed: it succeeded, and the seed failed or ends before the position. Requires: g is the innermost growth, and grow.ctx(g) is false. |
| `op grow.adopt(g Grow, value Value, errs Errs) noreturn` | reads growth, recovered; writes growth, pos | `1` | Adopts the successful step of the growth g that ends at the position, with its value and the errors errs that it recovered, as the seed of g with a new identity, at a cost of one step. In the missing-token mode the seed also holds the tokens that the step inserted. The seed it replaces is no longer a valid result. The position then returns to the start of the growth. Requires: g is the innermost growth, grow.ctx(g) is false and grow.longer(g, true) holds. |
| `op grow.memoStep(g Grow) CtxResult noreturn` | reads growth, silent; writes growth, pos | `0 + 1*steps + 1*table + 1*deps + 1*merge + 1*run` | Takes the steps of the growth g that the table of its context gives, following the jumps it records: while the table keeps a step from the current seed that holds in the current context, as ctx.lookup finds a result, and kept outside a lookahead unless the parse is in one, it passes the dependencies of the step to the enclosing evaluation, merges its expectations into the record and adopts every step that matched more than the seed, as grow.adoptStep does. It returns the first step that did not match more, which ends the growth, or the null result when the table keeps no step from the seed, whose evaluation grow.isolate then begins. A jump takes several of the steps at once with the same effects as taking them one at a time, so only the seed the steps reach and the step that ends them are observable. A step taken from the table counts no compute unit: only a step that evaluates the body does. Requires: grow.ctx(g) holds, g is the innermost growth, no step of g is being evaluated, and no step that grow.keepStep returned is pending: each was adopted by grow.adoptStep or did not grow. |
| `op grow.isolate(g Grow)` | reads growth; writes growth | `0` | Starts evaluating a step of the growth g: it records the dependencies of the step, as ctx.push does, and keeps its examined range and its expectations apart from those of the parse until grow.keepStep. Requires: As for grow.memoStep, and the last call of grow.memoStep(g) returned the null result. |
| `op grow.keepStep(g Grow, value Value, ok bool, errs Errs) CtxResult noreturn` | reads growth, pos, silent; writes growth | `65 + 1*merge + 1*segs + 2*deps + 1*table` | Ends the evaluation of the step of the growth g that grow.isolate started, with its value, its success ok and the errors errs that it recovered, and returns its result, which ends at the position or, for a failure, at the start of the growth: it keeps the result in the table of the context with the dependencies of the step, passes them to the enclosing evaluation, and merges the expectations of the step into the record. A result equal to an earlier step's of the growth's leader at the position has that step's identity as a seed, and otherwise a new one. It does not change the recovered errors: the caller lists the errors of the step with rec.since and drops the entries after the mark of the growth with rec.truncate. Requires: grow.isolate(g) started the evaluation of a step that has not ended, and the calls that the body made have ended. errs is the list that rec.since returns for the mark of the growth, after the entries of a step that failed were dropped. |
| `op grow.grew(g Grow, s CtxResult) bool` | reads growth | `0` | Reports whether the step s of the growth g matched more than its seed: s succeeded, and the seed failed or ends before s. Requires: s is the step that grow.keepStep returned last for g. |
| `op grow.adoptStep(g Grow, s CtxResult) noreturn` | reads growth; writes growth, pos | `1 + 1*run` | Adopts the step s of the growth g as its seed, with the identity of s, at a cost of one step, and returns the position to the start of the growth. Requires: grow.grew(g, s) holds, and s is the step that grow.keepStep returned last for g. |
| `op grow.end(g Grow) CtxResult noreturn` | reads growth; writes growth, pos, silent | `0 + 1*run + 1*errs` | Ends the growth g and returns its last seed: the position moves to its end. It does not change the recovered errors: the caller drops the entries recorded since the growth began with rec.truncate and records the errors of the seed, as rec.add does or, in the missing-token mode, as missing.replayResult does. The result is the last seed of the growth, in a context or not: the one that grow.adopt or grow.adoptStep adopted last, or the failed seed if none was; it may be that same result or a copy of it, which is not specified, as no operation changes a result. Requires: g is the innermost growth, and no step of g is being evaluated. |

## Call Bookkeeping

The operations of the component `call`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op call.tracked() bool [document]` | reads call; writes call | `0` | Reports whether a rule call of the engine takes its traced path: in a traced parse, for a call that the traced path has not yet entered. The call that call.track makes back is not traced a second time: for it the operation reports false and clears the mark that says so. |
| `op call.track(r Rule, min int) (Value, bool) [document] noreturn` | writes call, pos, silent | `1 + 1*body` | Calls the rule at the binding level min on the engine's traced path and returns its result. |
| `op doc.examinedBegin() int [document]` | reads call, pos; writes call | `0` | Starts measuring how far an observed call examines the input: the examined end of the call starts at the position. Returns the enclosing call's examined end, for doc.examinedEnd. The call itself, and any memo entry it reads or records, extends the end as in an unobserved parse, so that observation does not change reuse. Requires: The parse tracks the examined input (doc.tracking). |
| `op doc.examinedEnd(enclosing int) int [document]` | reads call; writes call | `0` | Ends measuring the examined end of an observed call and merges it into the enclosing one; returns the call's examined end: one past the last unit that the call's result depends on. Requires: enclosing is the result of the matching doc.examinedBegin. |
| `op doc.enter(pos int) (int, int) [document]` | reads call; writes call | `0` | Begins a rule call at pos in a Document parse: returns the examined input of the enclosing call, its end and its start, and starts the call's examined input as the empty range at pos: a call has examined nothing when it begins, and each read extends the range by exactly what it examined. Requires: The parse reads a Document. |
| `op doc.leave(examined int, low int) [document]` | reads call; writes call | `0` | Ends a rule call in a Document parse: the examined input of the enclosing call becomes the smallest range that holds [low, examined) and the call's. Requires: The parse reads a Document; examined and low are the results of the matching doc.enter. |
| `op doc.active() bool [document]` | reads call | `0` | Reports whether the parse reads a Document. |
| `op doc.tracking() bool [document]` | reads call | `0` | Reports whether the parse tracks how far each call examines the input: a Document parse, which reuses results by it, and an observed parse, which reports it. Every input unit that can influence a call's result then lies below the call's examined end. |
| `op doc.touch(end int) [document]` | reads call; writes call | `0` | Extends the examined end of the current call to end, if end is past it: end is exactly one past the last unit that what the call read depends on. A parse that does not track the examined input (doc.tracking) keeps none. |
| `op doc.touchFrom(from int) [document]` | reads call; writes call | `0` | Extends the examined input of the current call back to from, if from is before its start. A parse that reads no Document keeps none. |
| `op doc.low() int [document]` | reads call | `0` | Returns the start of the input that the current call examined so far. Requires: The parse reads a Document. |
| `op doc.examined() int [document]` | reads call | `0` | Returns the examined end of the current call: the exact end of the input it examined so far, exclusive. Requires: The parse tracks the examined input (doc.tracking). |
| `op doc.reused() [document]` | writes call | `0` | Counts a reused memo entry in the statistics of the Document. Requires: The parse reads a Document. |
| `op doc.evaluated() [document]` | writes call | `0` | Counts an evaluated rule body in the statistics of the Document. Requires: The parse reads a Document. |

## Observation

The operations of the component `observe`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op obs.memoHit() [observe]` | writes observe | `0` | Counts a reuse of a memo entry in the observation, if the parse is observed. |
| `op obs.active() bool [observe]` | reads observe | `0` | Reports whether the parse is observed. |
| `op obs.enter(r Rule, pos int, min int) [observe] noreturn` | writes observe | `0` | Reports to the observation of the parse that a call of the rule at pos and binding level min begins. Requires: The parse is observed. |
| `op obs.exit(ok bool, pos int, examined int) [observe] noreturn` | writes observe | `0` | Reports to the observation of the parse that the call that the last unmatched obs.enter began ends, with its success ok, at pos, and with the end of the input it examined. Requires: The parse is observed, and a call that obs.enter began has not ended. |

## Rules

The operations of the component `rules`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op rule.memo(r Rule) bool` | none | `0` | Reports whether the rule is memoized. A parse in the missing-token mode memoizes no rule that can insert a missing token. |
| `op rule.leader(r Rule) bool` | none | `0` | Reports whether the rule leads a left-recursive cycle. |
| `op rule.plain(r Rule) bool` | none | `0` | Reports whether the rule has no captures, so that its plain body serves its invocation. |
| `op rule.docMemo(r Rule) bool` | none | `0` | Reports whether a Document parse memoizes the rule. A Document parse in the missing-token mode memoizes no rule that can insert a missing token other than a leader, whose growth keeps its seed in the memo. |
| `op rule.docOnlyMemo(r Rule) bool` | none | `0` | Reports whether a Document parse memoizes the rule and a parse from scratch does not; never for a rule that can insert a missing token in a parse in the missing-token mode. |
| `op rule.docPlain(r Rule) bool` | none | `0` | Reports whether a parse from scratch calls the plain body of the rule directly, at a call site, without the call protocol. |
| `op rule.positional(r Rule) bool` | none | `0` | Reports whether the results of the rule may depend on positions, not only on the input they examine. |
| `op rule.cyclic(r Rule) bool` | none | `0` | Reports whether the rule is on a cycle of the call graph, so that a parse that counts compute units checks them at the entry of each call of the rule. |
| `op rule.novalue(r Rule) bool` | none | `0` | Reports whether the rule makes no value: its calls return the null value. |
| `op rule.id(r Rule) int` | none | `0` | Returns the rule's index, the rule of its memo keys. |
| `op rule.slots(r Rule) int` | none | `0` | Returns the number of the capture slots of the rule's body: the slots of the frame that its invocation opens. |
| `op rule.seen(r Rule) int` | none | `0` | Returns the rule's index among the rules whose memoization a parse may defer to their second call at a position (seen.first), or -1 for a rule without one: a rule that a parse from scratch does not memoize, and a leader of a left-recursive cycle. The rules with an index are numbered from 0 in the order of their indexes (rule.id). |
| `op rule.invoke(r Rule, min int) (Value, bool) noreturn` | writes rules, pos, silent | `1 + 1*body` | Runs the rule's body at the binding level min in a frame of its own and makes the rule's value; on failure the position is unspecified. The evaluation counts in the statistics of a Document parse, and that of a rule on a cycle of calls takes a nesting level while it runs. The unit of the call is charged or counted before the nesting level is taken, so that a call that both the compute limit and the nesting limit stop stops on the compute limit. |
| `op rule.invokePlain(r Rule, min int) (Value, bool) noreturn` | writes rules, pos, silent | `1 + 1*body + 1*tokens` | Runs the plain body of a rule without captures and returns its result, at the start position and without the errors it recovered on failure; in the missing-token mode a failure also removes the missing tokens that the body inserted, as missing.reset does. The evaluation counts, and the unit of the call comes before the nesting level, as in rule.invoke. Requires: rule.plain is true of r. |
| `op rule.body(r Rule, min int) (Value, bool) noreturn` | writes rules, pos, silent | `0 + 1*body` | Runs the rule's body at the binding level min in the current frame and returns the value of the body, from which rule.finish makes the rule's value; on failure the position is unspecified. It counts no unit of the call and takes no nesting level: the invocation around it does, as rule.invoke does. |
| `op rule.finish(r Rule, f Frame, v Value, start int) Value noreturn` | reads frames, pos; writes rules | `0 + 1*body` | Returns the rule's value made from the value v of its body, which matched from start to the position with the captures of f: the value of its action, or v shaped as the rule's value, converted to the rule's node type if it has one. Making the value charges what its action charges. Requires: v is the value of a successful evaluation of the body of r, whose captures f holds. |
| `op rule.run(inv Body) (Value, bool) noreturn` | writes rules, pos, silent | `0 + 1*body` | Runs the invocation inv of a rule at binding level 0 and returns its result; on failure the position is unspecified. The evaluation does not count in the statistics of a Document parse, which the caller counts with doc.evaluated. |
| `op opr.led(r Rule) Operators` | none | `0` | Returns the infix and postfix operators of the Operators Rule r, in declaration order. |
| `op opr.prefix(r Rule) Operators` | none | `0` | Returns the prefix operators of the Operators Rule r, in declaration order. |
| `op opr.count(l Operators) int` | none | `0` | Returns the number of operators of l. |
| `op opr.at(l Operators, i int) Operator` | none | `0` | Returns operator i of l. Requires: 0 <= i < opr.count(l). |
| `op opr.operands(r Rule) int` | none | `0` | Returns the number of operand lines of the Operators Rule r. |
| `op opr.operand(r Rule, i int) Line` | none | `0` | Returns operand line i of the Operators Rule r, in declaration order. Requires: 0 <= i < opr.operands(r). |
| `op opr.line(op Operator) Line` | none | `0` | Returns the line of the operator op. |
| `op opr.level(op Operator) int` | none | `0` | Returns the binding power of the operator op: the higher, the tighter it binds. |
| `op opr.infix(op Operator) bool` | none | `0` | Reports whether the operator op is an infix operator. |
| `op opr.postfix(op Operator) bool` | none | `0` | Reports whether the operator op is a postfix operator. |
| `op opr.right(op Operator) bool` | none | `0` | Reports whether the operator op associates to the right. |
| `op opr.nonAssoc(op Operator) bool` | none | `0` | Reports whether the operator op does not associate. |
| `op opr.scope(l Line) int` | none | `0` | Returns the number of capture slots of the line l. |
| `op opr.hasSkip(r Rule) bool` | none | `0` | Reports whether the Operators Rule r skips trivia between its operands and operators. |
| `op opr.skip(r Rule) bool noreturn` | writes rules, pos, silent, cut | `0 + 1*body` | Runs the trivia expression of the Operators Rule r and reports whether it matched; on failure the position is unspecified. Requires: opr.hasSkip(r). |
| `op opr.match(l Line) (Value, bool) noreturn` | writes rules, pos, silent, cut | `0 + 1*body` | Runs the expression of the line l in the current frame and returns its value and success; on failure the position is unspecified. A cut in the expression sets the cut register. |

## Host

The operations of the component `host`. The operation that writes the range of a node is specified with the values in
[Operations That Write Host State](values.md#operations-that-write-host-state), and the elements that action expressions gather and
the values they make in [Elements Gathered](values.md#elements-gathered) and [Values Made](values.md#values-made).

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `host op action.ctx() Act` | reads host, pos, made; writes host | `0` | Returns the context in which an action expression runs: the current capture frame, an empty span at the position, and the mark of the values made so far, as action.created returns it. |
| `op opr.lineValue(r Rule, l Line, f Frame, v Value, start int) Value noreturn` | reads frames; writes host | `0 + 1*body` | Returns the value of the operand line l of the rule r that matched from start to the position with the value v and the captures of f: the line's action, or its shaped value with its captures. Requires: l is an operand line of r, and f a frame of opr.scope(l) slots. |
| `op opr.build(r Rule, a Attempt, lhs Value, rhs Value) Value noreturn` | reads operators, frames; writes host | `0 + 1*body` | Returns the value of the application of the operator of a to lhs (null for a prefix operator) and rhs (null for a postfix operator), which ends at the position, or for a postfix operator with it: the action of the operator's line, or the node of the tuple (lhs, operator, rhs). The operator is the value of its line, which takes the line's captures if it is the node of a tuple, or a node of its match if the line has no value. When the operators of r have a named node type, the node of the tuple is of that type and carries no rule name, and the value of the action is converted to it as an action expression converts (the failure not convertible of the Exec IR values): one that is not a string, tuple or list node ends the parse with the error "cannot convert <type> to <type> in result of rule <name>". Requires: The operator of a is one of r, and the frame of a has the slots of its line. |
| `op opr.buildTyped(r Rule, a Attempt, lhs Value, lstart int, rhs Value) (Value, int) [typed] noreturn` | reads operators, frames; writes host | `0 + 1*body` | Is opr.build for typed values, where lhs, if it is not a node, starts at lstart; it also returns where the value of the application starts. Requires: As for opr.build. |

## Capture Frames

The operations of the component `frames`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op frame.mark() Mark` | reads frames, env, recovered | `0` | Returns a backtracking point at the position. |
| `op frame.markPos(m Mark) int` | none | `0` | Returns the position of the backtracking point m. |
| `op frame.markRec(m Mark) int` | none | `0` | Returns the number of the errors recovered before the backtracking point m, as rec.mark does. |
| `op frame.markEnv(m Mark) Scope` | none | `0` | Returns the variable environment of the backtracking point m. |
| `op frame.markMissing(m Mark) int [missing]` | none | `0` | Returns the mark of the missing-token record at the backtracking point m, as missing.mark does. |
| `op frame.markTrail(m Mark) int` | none | `0` | Returns the length of the trail at the backtracking point m. |
| `op frame.undo(trail int)` | reads frames; writes frames | `0` | Undoes the writes of the trail after its first trail writes, in reverse order, and drops them: every slot they wrote has the value it had before. Captures are not an item of the cost, so undoing them costs nothing: each write was made by a step that was counted. Requires: trail is at most the length of the trail. |
| `op frame.trail() int` | reads frames | `0` | Returns the length of the trail. |
| `op frame.drop(trail int)` | writes frames | `0` | Drops the writes of the trail after its first trail writes without undoing them, as a rule's call does when it ends: the slots they wrote belong to the frames of the calls that wrote them, which no reset reaches any more. Dropping them costs nothing. Requires: trail is at most the length of the trail. |
| `op frame.current() Frame` | reads frames | `0` | Returns the frame that captures write to. |
| `op frame.use(f Frame)` | writes frames | `0` | Makes f the frame that captures write to. |
| `op frame.open(n int) Frame` | writes frames | `0` | Returns a new frame of n empty slots, which lives until the parse ends. A frame of no slots is never written, so it may be shared. Opening it costs nothing: n is a constant of the grammar for the call that opens it. |
| `op frame.set(slot int, v Value)` | reads frames; writes frames | `0` | Writes v to the slot of the current frame and adds the slot's old value to the trail. The write is free: its size depends on nothing and the match that made v paid for itself. Requires: slot is a slot of the current frame. |
| `op frame.close(f Frame)` | writes frames | `0` | Ends f, which nothing refers to any more: a rule's frame when the rule returns. A target may reuse it, and one without a pool of frames does nothing. Requires: No trail entry and no current frame refer to f. |
| `op frame.slots(f Frame) int` | none | `0` | Returns the number of slots of f. |
| `op frame.save(f Frame)` | reads frames; writes frames | `0` | Copies the slots of f aside. The copy costs nothing: the slots are a constant of the grammar for the operator whose try saves them. |
| `op frame.restore(f Frame)` | reads frames; writes frames | `0` | Copies the slots that the last frame.save set aside back into f, which costs nothing, as frame.save does not. Requires: The last frame.save copied the slots of f, and nothing parsed since. |

## Operators Rule Loop

The operations of the component `operators`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op opr.attempt(op Operator, f Frame, v Value, start int, cut bool, errs Errs) Attempt` | reads env, pos | `0` | Returns the match of op from start to the position with the value v, the captures of f, the variable environment, the cut flag cut and the recovered errors errs. |
| `op opr.attemptMissing(op Operator, f Frame, v Value, start int, cut bool, errs Errs, mark int) Attempt [missing] noreturn` | reads env, pos, recovered | `0 + 1*tokens` | Is opr.attempt in a parse that can insert missing tokens: the match also holds a copy of the tokens inserted after mark, none outside the missing-token mode. It costs one step for each token it copies. Requires: mark is a result of missing.mark at or before every later mark that is still in use. |
| `op opr.found(a Attempt) bool` | none | `0` | Reports whether a is a match, not the null attempt. |
| `op opr.op(a Attempt) Operator` | none | `0` | Returns the operator of the match a. Requires: opr.found(a). |
| `op opr.end(a Attempt) int` | none | `0` | Returns where the match a ended. Requires: opr.found(a). |
| `op opr.cut(a Attempt) bool` | none | `0` | Reports whether the match a cut. Requires: opr.found(a). |
| `op opr.env(a Attempt) Scope` | none | `0` | Returns the variable environment after the match a. Requires: opr.found(a). |
| `op opr.errs(a Attempt) Errs` | none | `0` | Returns the errors that the match a recovered. Requires: opr.found(a). |
| `op opr.setCutFailed(failed bool)` | writes operators | `0` | Records whether a candidate of the longest match failed after a cut. |
| `op opr.cutFailed() bool` | reads operators | `0` | Reports what the last opr.setCutFailed recorded. |
| `op opr.setNudBound(bound int)` | writes operators | `0` | Records the binding level above which no operator can follow the start of an expression. |
| `op opr.nudBound() int` | reads operators | `0` | Returns what the last opr.setNudBound recorded. |
| `op opr.setAfterEmpty(empty bool)` | writes operators | `0` | Records whether the right operand that the next expression parses follows an infix operator applied without consuming input. |
| `op opr.afterEmpty() bool` | reads operators | `0` | Returns what the last opr.setAfterEmpty recorded. |

## Suspended Calls

The operations of the component `suspended`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `op call.state() CallState` | writes suspended | `0` | Returns a record for the state of a call that the caller holds until the call ends. |
| `op call.save(st CallState, start int, min int, key Values, c Ctx, rec int, m ExpMark)` | writes suspended | `0` | Saves in st the state that the end of the call needs: its position, its binding level, the values of its key, its context state, the mark of its recovered errors and the record that its isolation set aside. |
| `op call.saveExamined(st CallState, examined int, low int) [document]` | writes suspended | `0` | Saves in st the examined input of the enclosing call, which doc.enter returned. |
| `op call.saveMissing(st CallState, mark int) [missing]` | writes suspended | `0` | Saves in st the mark of the missing-token record, which missing.mark returned. |
| `op call.start(st CallState) int` | reads suspended | `0` | Returns the position of the call that st holds. Requires: st holds a call that call.save saved. |
| `op call.min(st CallState) int` | reads suspended | `0` | Returns the binding level of the call that st holds. Requires: st holds a call that call.save saved. |
| `op call.key(st CallState) Values` | reads suspended | `0` | Returns the values of the key of the call that st holds. Requires: st holds a call that call.save saved. |
| `op call.ctx(st CallState) Ctx` | reads suspended | `0` | Returns the context state of the call that st holds. Requires: st holds a call that call.save saved. |
| `op call.rec(st CallState) int` | reads suspended | `0` | Returns the mark of the recovered errors of the call that st holds. Requires: st holds a call that call.save saved. |
| `op call.mark(st CallState) ExpMark` | reads suspended | `0` | Returns the record that the isolation of the call set aside that st holds. Requires: st holds a call that call.save saved. |
| `op call.examined(st CallState) int [document]` | reads suspended | `0` | Returns the end of the examined input of the enclosing call that st holds. Requires: st holds a call that call.save saved. |
| `op call.low(st CallState) int [document]` | reads suspended | `0` | Returns the start of the examined input of the enclosing call that st holds. Requires: st holds a call that call.save saved. |
| `op call.missing(st CallState) int [missing]` | reads suspended | `0` | Returns the mark of the missing-token record that st holds. Requires: st holds a call that call.save saved. |
| `op oprLongest.l(st LongestState) Operators` | reads suspended | `0` | Returns the field l of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setL(st LongestState, x Operators)` | writes suspended | `0` | Sets the field l of the record st to x. |
| `op oprLongest.i(st LongestState) int` | reads suspended | `0` | Returns the field i of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setI(st LongestState, x int)` | writes suspended | `0` | Sets the field i of the record st to x. |
| `op oprLongest.m0(st LongestState) Mark` | reads suspended | `0` | Returns the field m0 of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setM0(st LongestState, x Mark)` | writes suspended | `0` | Sets the field m0 of the record st to x. |
| `op oprLongest.best(st LongestState) Attempt` | reads suspended | `0` | Returns the field best of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setBest(st LongestState, x Attempt)` | writes suspended | `0` | Sets the field best of the record st to x. |
| `op oprLongest.cutFailed(st LongestState) bool` | reads suspended | `0` | Returns the field cutFailed of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setCutFailed(st LongestState, x bool)` | writes suspended | `0` | Sets the field cutFailed of the record st to x. |
| `op oprLongest.prevFrame(st LongestState) Frame` | reads suspended | `0` | Returns the field prevFrame of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setPrevFrame(st LongestState, x Frame)` | writes suspended | `0` | Sets the field prevFrame of the record st to x. |
| `op oprLongest.prevCut(st LongestState) bool` | reads suspended | `0` | Returns the field prevCut of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setPrevCut(st LongestState, x bool)` | writes suspended | `0` | Sets the field prevCut of the record st to x. |
| `op oprLongest.f(st LongestState) Frame` | reads suspended | `0` | Returns the field f of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprLongest.setF(st LongestState, x Frame)` | writes suspended | `0` | Sets the field f of the record st to x. |
| `op oprNud.m0(st NudState) Mark` | reads suspended | `0` | Returns the field m0 of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprNud.setM0(st NudState, x Mark)` | writes suspended | `0` | Sets the field m0 of the record st to x. |
| `op oprNud.a(st NudState) Attempt` | reads suspended | `0` | Returns the field a of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprNud.setA(st NudState, x Attempt)` | writes suspended | `0` | Sets the field a of the record st to x. |
| `op oprNud.i(st NudState) int` | reads suspended | `0` | Returns the field i of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprNud.setI(st NudState, x int)` | writes suspended | `0` | Sets the field i of the record st to x. |
| `op oprNud.prevFrame(st NudState) Frame` | reads suspended | `0` | Returns the field prevFrame of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprNud.setPrevFrame(st NudState, x Frame)` | writes suspended | `0` | Sets the field prevFrame of the record st to x. |
| `op oprNud.prevCut(st NudState) bool` | reads suspended | `0` | Returns the field prevCut of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprNud.setPrevCut(st NudState, x bool)` | writes suspended | `0` | Sets the field prevCut of the record st to x. |
| `op oprNud.f(st NudState) Frame` | reads suspended | `0` | Returns the field f of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprNud.setF(st NudState, x Frame)` | writes suspended | `0` | Sets the field f of the record st to x. |
| `op oprExpr.min(st ExprState) int` | reads suspended | `0` | Returns the field min of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setMin(st ExprState, x int)` | writes suspended | `0` | Sets the field min of the record st to x. |
| `op oprExpr.lhs(st ExprState) Value` | reads suspended | `0` | Returns the field lhs of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setLhs(st ExprState, x Value)` | writes suspended | `0` | Sets the field lhs of the record st to x. |
| `op oprExpr.lastNone(st ExprState) int` | reads suspended | `0` | Returns the field lastNone of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setLastNone(st ExprState, x int)` | writes suspended | `0` | Sets the field lastNone of the record st to x. |
| `op oprExpr.bound(st ExprState) int` | reads suspended | `0` | Returns the field bound of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setBound(st ExprState, x int)` | writes suspended | `0` | Sets the field bound of the record st to x. |
| `op oprExpr.guard(st ExprState) int` | reads suspended | `0` | Returns the field guard of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setGuard(st ExprState, x int)` | writes suspended | `0` | Sets the field guard of the record st to x. |
| `op oprExpr.m0(st ExprState) Mark` | reads suspended | `0` | Returns the field m0 of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setM0(st ExprState, x Mark)` | writes suspended | `0` | Sets the field m0 of the record st to x. |
| `op oprExpr.a(st ExprState) Attempt` | reads suspended | `0` | Returns the field a of the record st: what the last set of it stored, and before the first the zero value of its type (0, false, the list of no operators, the zero backtracking point, no frame, value or match). |
| `op oprExpr.setA(st ExprState, x Attempt)` | writes suspended | `0` | Sets the field a of the record st to x. |

## Stream Window

The operations of the component `window`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `host op stream.emits() bool [stream]` | reads window | `0` | Reports whether the parse delivers the elements of a #stream repetition to a callback. |
| `host op stream.start() int [stream]` | reads window | `0` | Returns the id of the start rule of the stream parse. Requires: The parse delivers elements (stream.emits). |
| `host op stream.report() [stream] noreturn` | reads window; writes window | `0` | Delivers the recovered errors committed since the last report to the diagnostic callback and the retained tail. An error of the callback ends the parse with it, before the element is delivered. |
| `host op stream.deliver(v Value) [stream] noreturn` | reads window; writes window | `0` | Passes the element v, nil for a value-free repetition, to the callback; v is final from then on. An error of the callback ends the parse unchanged. Requires: The parse delivers elements. |
| `host op stream.discard(keep int) [stream] noreturn` | reads window; writes window | `0 + 1*stream_scan` | Discards the input before keep, when at least half of the buffer can go; later reads before keep are not supported. The line and column of the new base are kept. |
| `host op stream.cutText(pos int) [stream] noreturn` | reads window; writes window | `0` | Makes pos the commit point of the text: from then on the text of a range [start, end) is the input of [max(start, pos), max(end, pos)), whatever input the stream still holds before pos, so the text and its units never depend on how the input arrives. |
| `host op stream.release() [stream] noreturn` | reads window; writes window | `0 + 2*stream_frames` | Releases what the elements committed so far held: the trail, the capture frames and the node chunks that no active call refers to, so that a delivered element is not kept alive through the parser. The effect is memory held, which the parse result does not show. Requires: Every call of the delivered elements has returned. |
| `host op stream.dropRecords(pos int) [stream] noreturn` | reads window; writes window | `0 + 1*stream_inactive` | Discards what the parse keeps about the positions before pos besides the memo, which no later call reads: the records of skips and of the profile, and what the inactive capacity of the trail and of a target's value stack refers to. Later calls never read them, so the parse result does not show the difference. |
| `host op stream.tail() bool [stream]` | reads window | `0` | Reports whether the parse retains the tail of its recovered errors (a diagnostic limit is set). |

## Repetition Runs

The operations of the component `runs`.

| Declaration | State | Cost | Contract |
|:--|:--|:--|:--|
| `host op run.init(r Run, site int, min int, max int, shiftable bool) [document]` | reads runs, pos, recovered; writes runs | `0` | Starts the run r of the repetition site at the position: its fields are the zero values except min, max and shiftable, j is -1, editStart and editEnd are the greatest int; it has no old run and no new elements. It notes the recovered errors and the reads of growing results so far, for run.clean. |
| `host op run.takeHere(r Run) bool [document]` | reads runs; writes runs | `0` | Takes over as the old run of r the record that this parse holds for the site of r at its start, and reports whether there is one. The records that this parse holds are those it made, and those of the parse before when no edit came since. |
| `host op run.previous() bool [document]` | reads runs | `0` | Reports whether the parse may take over the records of the parse before: an edit came since it, and its records were kept. |
| `host op run.takeLast(r Run, at int, gen int) bool [document]` | reads runs; writes runs | `0` | Takes over as the old run of r the record of the parse before for the site of r at the position at, if that parse recorded it when the Document had had gen edits, removes it from the records of that parse, and reports whether it did. |
| `host op run.oldCount(r Run) int [document]` | reads runs | `0` | Returns the number of elements of the old run of r, 0 if it has none. |
| `host op run.oldStart(r Run, i int) int [document]` | reads runs | `0` | Returns, for the old element i of r, where it started, as of the edits that r took up to now. |
| `host op run.oldEnd(r Run, i int) int [document]` | reads runs | `0` | Returns, for the old element i of r, where it ended, as of the edits that r took up to now. |
| `host op run.oldFrom(r Run, i int) int [document]` | reads runs | `0` | Returns, for the old element i of r, the start of the input it examined, as of the edits that r took up to now. |
| `host op run.oldExamined(r Run, i int) int [document]` | reads runs | `0` | Returns, for the old element i of r, the end of the input it examined, exclusive, as of the edits that r took up to now. |
| `host op run.oldFar(r Run, i int) int [document]` | reads runs | `0` | Returns, for the old element i of r, the position of the expectations it recorded, as of the edits that r took up to now. |
| `host op run.oldKept(r Run, i int) ExpKept [document]` | reads runs | `0` | Returns the expectations that the old element i of r recorded at run.oldFar(r, i). |
| `host op run.take(r Run, i int, shift int, gen int) Value [document]` | reads runs, host; writes runs | `0` | Moves the old element i of r by shift: its positions, examined input and far position, and the nodes of its value, which the parse that recorded it made when the Document had had gen edits (doc.move). Returns its value. With shift 0 it changes nothing. |
| `host op run.resumed() [document]` | reads runs; writes runs | `0` | Counts an element that the parse reused from a run, in the statistics of the Document (as doc.reused) and in its count of resumed elements. |
| `host op run.add(r Run, after bool, start int, end int, from int, examined int, far int, kept ExpKept, v Value) [document]` | reads runs; writes runs | `0` | Adds to r an element that the parse evaluated: from start to end, having examined [from, examined), with the expectations kept at far and the value v. It belongs to the new elements before the old elements after the edit that r reuses, or, with after, to those after them. |
| `host op run.clean(r Run) bool [document]` | reads runs, recovered | `0` | Reports whether, since run.init(r), no recovered error remains and no call read the current result of a growth, directly or by reusing a result that read one, even of a growth that started and ended within the run. |
| `host op run.minimum() int [document]` | reads runs | `0` | Returns the least number of elements of a run that a Document records. |
| `host op run.record(r Run) [document]` | reads runs; writes runs | `0` | Records in this parse the run r for its site and start, replacing any record there, with the generation of the Document: the old elements before k, the new elements before the old elements after the edit, the old elements from j to j + t when j >= 0, and the new elements after them. |
| `host op doc.log() EditLog [document]` | reads memo | `0` | Returns the log of the edits of the Document since its memo was last dropped; the parse is of the text after them all. |
| `host op run.min(r Run) int [document]` | reads runs | `0` | Returns the field min of the run r: The least number of elements of the repetition. |
| `host op run.setMin(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field min of the run r to x. |
| `host op run.max(r Run) int [document]` | reads runs | `0` | Returns the field max of the run r: The greatest number of elements of the repetition, or -1 for no bound. |
| `host op run.setMax(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field max of the run r to x. |
| `host op run.shiftable(r Run) bool [document]` | reads runs | `0` | Returns the field shiftable of the run r: Whether the values of the elements may move with an edit (no position in them). |
| `host op run.setShiftable(r Run, x bool) [document]` | reads runs; writes runs | `0` | Sets the field shiftable of the run r to x. |
| `host op run.count(r Run) int [document]` | reads runs | `0` | Returns the field count of the run r: The number of elements of the run so far, reused or evaluated. |
| `host op run.setCount(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field count of the run r to x. |
| `host op run.done(r Run) bool [document]` | reads runs | `0` | Returns the field done of the run r: Whether a reused element ended the repetition: it was empty, or the run reached max. |
| `host op run.setDone(r Run, x bool) [document]` | reads runs; writes runs | `0` | Sets the field done of the run r to x. |
| `host op run.moved(r Run) bool [document]` | reads runs | `0` | Returns the field moved of the run r: Whether the old run is the one that the edit moved here: none of it lies before the edit. |
| `host op run.setMoved(r Run, x bool) [document]` | reads runs; writes runs | `0` | Sets the field moved of the run r to x. |
| `host op run.editStart(r Run) int [document]` | reads runs | `0` | Returns the field editStart of the run r: The start of the input that the one edit since the old run replaced; the greatest int when the old run is of this text. |
| `host op run.setEditStart(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field editStart of the run r to x. |
| `host op run.editEnd(r Run) int [document]` | reads runs | `0` | Returns the field editEnd of the run r: The end of the input that the edit replaced; the greatest int when the old run is of this text. |
| `host op run.setEditEnd(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field editEnd of the run r to x. |
| `host op run.editDelta(r Run) int [document]` | reads runs | `0` | Returns the field editDelta of the run r: How many units longer the replacement of the edit is than what it replaced; 0 when the old run is of this text. |
| `host op run.setEditDelta(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field editDelta of the run r to x. |
| `host op run.prefixed(r Run) bool [document]` | reads runs | `0` | Returns the field prefixed of the run r: Whether the run is past its reused prefix (runNext). |
| `host op run.setPrefixed(r Run, x bool) [document]` | reads runs; writes runs | `0` | Sets the field prefixed of the run r to x. |
| `host op run.k(r Run) int [document]` | reads runs | `0` | Returns the field k of the run r: The number of old elements before the edit that the run reused. |
| `host op run.setK(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field k of the run r to x. |
| `host op run.j(r Run) int [document]` | reads runs | `0` | Returns the field j of the run r: The index of the first old element after the edit that the run reuses, or -1 before the run is back in step. |
| `host op run.setJ(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field j of the run r to x. |
| `host op run.t(r Run) int [document]` | reads runs | `0` | Returns the field t of the run r: The number of old elements from j on that the run reused. |
| `host op run.setT(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field t of the run r to x. |
| `host op run.seek(r Run) int [document]` | reads runs | `0` | Returns the field seek of the run r: The old element at which the search for the old element after the edit that the run resyncs at resumes (runResync). |
| `host op run.setSeek(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field seek of the run r to x. |
| `host op run.barred(r Run) int [document]` | reads runs | `0` | Returns the field barred of the run r: The number of old elements that the run cannot resync at: one past the last old element that the scan found to examine input before the end of the edit (runResync). |
| `host op run.setBarred(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field barred of the run r to x. |
| `host op run.elemStart(r Run) int [document]` | reads runs | `0` | Returns the field elemStart of the run r: Where the element being evaluated started. |
| `host op run.setElemStart(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field elemStart of the run r to x. |
| `host op run.outerExamined(r Run) int [document]` | reads runs | `0` | Returns the field outerExamined of the run r: The examined end of the enclosing call, set aside while an element is evaluated (doc.enter). |
| `host op run.setOuterExamined(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field outerExamined of the run r to x. |
| `host op run.outerLow(r Run) int [document]` | reads runs | `0` | Returns the field outerLow of the run r: The examined start of the enclosing call, set aside while an element is evaluated (doc.enter). |
| `host op run.setOuterLow(r Run, x int) [document]` | reads runs; writes runs | `0` | Sets the field outerLow of the run r to x. |
| `host op run.mark(r Run) ExpMark [document]` | reads runs | `0` | Returns the field mark of the run r: The isolation of the expectations of the element being evaluated (exp.isolate). |
| `host op run.setMark(r Run, x ExpMark) [document]` | reads runs; writes runs | `0` | Sets the field mark of the run r to x. |
