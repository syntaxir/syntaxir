# Attributes and Effects

Every node carries attributes. Some are set when the node is built; analysis computes the rest from the rule bodies and
recomputes them after a rewrite. The analysis also computes the [facts of the rules](#rule-facts).

| Attribute | Set by | Meaning |
|:--|:--|:--|
| ID | Building | Identifies the node in its module. A copy of the module keeps it, and a node that a rewrite puts in the place of another takes the ID of the node it replaces, so what is known about a node by its ID, such as the expression of the grammar it was built from, holds across rewrites |
| Want | Building | The node's value is used. A node with Want false builds no value |
| Shape | Building | The static [shape](modules.md#shape) of the node's value |
| Nullable | Analysis | The node can succeed without consuming input when no token is inserted |
| NullableIns | Analysis | The node can succeed without consuming input when token insertion is enabled |
| First | Analysis | The terminal the node must begin with, if it has one |
| Effects | Analysis | A summary of what the node can do |

## Nullability

Nullability is the least fixed point over the rules. There are two: Nullable, for a parse that inserts no token, and
NullableIns, for a parse with token insertion enabled. They differ only through `Missing`. In summary:

- `Lit` is nullable if its text is empty; classes, `Any` and `Scan` with a positive minimum are not. Anchors, `Cut`,
  `Succeed`, `Look`, `Opt`, `Pred`, `BindNode` and a `Scan` with minimum 0 are.
- `Seq` is nullable if every item is; `Choice` if any alternative is. `Repeat` is nullable if its minimum is 0, or is 1
  and its body is nullable.
- `Call` is nullable as the callee is; `Nest` as its body is. An `OperatorsRule` is nullable if an operand line's body
  is. `Text`, `Drop`, `Capture`, `Label`, `Terminal` and `Stream` are nullable as their bodies are.
- `Recover` is nullable as its body is: when the body fails, a skip that consumes nothing is no recovery, so the node
  succeeds without consuming input only through its body.
- `Missing` is nullable as its body is, and NullableIns: an insertion consumes no input.
- `FailNode` and `ClassRun` are not.

## First Terminals

The first terminal of a node is what its first character must be, if it has one: either the first code point of a
literal, either case of it if the literal is case-insensitive and the code point an ASCII letter, or membership in a
class. If the next character is not that, the node fails at once, recording only the expectation of the literal or the
class at the current position, after entering as many rule calls as the terminal counts as its depth.

The first terminal follows the first item of a sequence, the bodies of `Capture`, `Text`, `Drop`, `Nest` and
`Terminal`, the first class of a `ClassRun`, and calls of rules that are neither left-recursion leaders nor Operators
Rules, counting one level of depth for each call and `Nest` on the way. Every other node has none.

## Rule Facts

The analysis also computes facts about the rules: those that the language's static analyses define, such as
[left recursion](../language/parser-expressions.md#left-recursion), and those that decide how a rule is called. They
are computed on the rules as written: a value-free twin has the facts of its rule, and a call of a twin counts as a
call of its rule. They read nothing from the rules' policies. The Rec and Leader policies are set from them before the
first terminals and the effects are computed, since both depend on those policies.

| Fact | Meaning |
|:--|:--|
| Nullable, NullableIns | Those of the rule's body |
| NullableAtEnd | The rule can succeed without consuming input when the input is at its end (below) |
| NullableBeforeEnd | The rule can succeed without consuming input at a position before the end of the input (below) |
| CanMatch | The rule can succeed at all (below) |
| Rec | The rule is on a cycle of calls: a component of the call graph with several rules, or a rule that calls itself. Calls in lookaheads, recovery skips and Operators Rule lines count |
| Succeeds | The rule is off every left-recursion cycle and its body always succeeds (below) |
| Left calls | The rules the body may call before it consumes input (below) |
| Cycle | The rule's left-recursion cycle, if it is on one: a component of the graph of left calls with several rules, or a rule with a left call of itself |
| Leader, Member | A rule on a cycle is a leader or a member. Repeatedly, while some cycle of left calls among the rules of a component passes through no leader, the rule declared first among all the rules on such cycles becomes a leader; given the graph of left calls, the choice depends on the declaration order only (the graph itself depends on the order of the alternatives where Succeeds cuts a choice) |
| Left calls, Cycle, Leader and Member with insertion | The same facts for a parse with token insertion enabled: computed alike with NullableIns in place of Nullable, with proofs of success of their own. They differ only where a call may follow a `Missing` node before input is consumed |
| Positional | The rule's action, predicates, bindings or Operators Rule line actions read `.Start` or `.End`, or a rule it calls is positional |
| Recovering | A rule whose `Recover` can put an error value in the rule's value: the rule itself, if it has a `Recover` outside lookaheads and Operators Rule skip lines, or a recovering rule that it calls outside them |
| Variables | The variables that the action expressions of the rule or of a rule it calls read |
| Memo, Transient | Every rule but a leader may be memoized. Ordinary parses do not memoize a transient rule: one that calls no rule, or that is called at fewer than two places, unless it is a start rule on a cycle of rules that are each called at one place, which a parse can enter without a call. Incremental parses memoize it |
| Missing | The rule has a `Missing` node, or a rule it calls is Missing |

`NullableAtEnd` is the least fixed point over the rules of the nullability of the body at the end of the input: a node
can succeed there without consuming input. The end of the input is the position after the last character. An end-of-input
anchor `$$` succeeds only there; `.`, a character class and a nonempty literal fail there; a positive lookahead succeeds
there if its body can succeed there without consuming input, and a negative one if its body does not certainly succeed
there. A call takes the fact of its rule, and the other nodes that can succeed without consuming input at any position
(an option, a predicate, a cut, `Succeed`, a beginning-of-input or line anchor) do so at the end too, and the nodes that
contain a sequence, choice, repetition, operators rule or wrapper take the same rule as elsewhere.

`NullableBeforeEnd` is the same least fixed point at a position before the end of the input. There the end-of-input
anchor fails; `.`, a character class and a nonempty literal are not nullable, as anywhere; a positive lookahead is
nullable whatever its body; and a negative one is nullable if its body does not certainly succeed there. A call takes
the `NullableBeforeEnd` fact of its rule there, as it takes the `NullableAtEnd` fact at the end.

`CanMatch` is the least fixed point over the rules of whether the body can succeed at all. A `FailNode` cannot. A negative
lookahead can unless its body always succeeds. A `Missing` literal and a `Recover` can, since their error recovery
succeeds. A call takes the fact of its rule, and every other node can unless its parts cannot.

A node always succeeds when it cannot fail at any position, given the rules proven to succeed: an empty literal,
`Succeed` and `Cut` do; a call does if its rule does; a sequence if every item does; a choice if an alternative does and
no earlier alternative has a loose cut; an option, and a repetition with minimum 0, if its body has no loose cut or
always succeeds; a repetition with maximum 0 if its minimum is 0; a repetition with minimum 1 if its body always
succeeds; a positive lookahead if its body always succeeds, a negative one if its body always fails; `Capture`, `Text`,
`Drop`, `Label`, `Recover`, `Stream` and `Terminal` as their bodies do. A node always fails if it is `FailNode`, a
sequence with an item that always fails, a choice whose alternatives all do, a positive lookahead whose body always
fails, a negative one whose body always succeeds, or `Capture`, `Text`, `Drop`, `Label`, `Stream` or `Terminal` whose
body always fails. A loose cut is a `Cut` outside nested choices, options, repetitions, lookaheads and calls; the skip of
a `Recover` is in the scope of its body.

The left calls of a node are those of its call (`Call` or `Nest`); of a sequence's items up to the first that is not
nullable; of a choice's alternatives up to the first that always succeeds; of a `Recover`'s body, and of its skip unless
the body always succeeds; of a repetition's body unless its maximum is 0; of the bodies of `Opt`, `Look`, `Text`, `Drop`,
`Capture`, `Label`, `Stream` and `Terminal`; and, for an `OperatorsRule`, of its skip line, its operand lines up to the
first that always succeeds, its prefix operator lines, and, when some operand line is nullable, its infix and postfix
operator lines: an operand that matched nothing leaves them to be tried before the rule consumes input, so a call that they
make there is a left call and grows as other left recursion does. With a consuming skip line, they run after it; the
analysis counts them as left calls in that case too. Left calls depend on which rules always succeed, which depends
on the cycles, which depend on the left calls: the analysis starts with no rule proven to succeed and repeats until the
left calls no longer change. Each round only removes left calls, so it ends.

## Effects

An effect is one thing a node can do besides returning success or failure. A node's effect summary is the set of effects
it may have, with the capture slots it may write, in the order they are first written.

| Effect | The node may |
|:--|:--|
| Consume | Advance the position |
| Fail | Fail |
| Expect | Record expectations |
| Capture | Write capture slots of the enclosing scope |
| Bind | Bind a variable |
| Recover | Append recovered errors |
| Cut | Set the cut flag of the enclosing choice, option or repetition |
| Raise | End the parse with an error |
| Call | Call rules or enter a `Nest` |
| Commit | Deliver stream elements |

The summary of a node is the union of its children's, with these differences.

- Matching nodes. A non-empty literal, a class, `Any`, `ClassRun`: consume, fail and expect. An anchor: fail and expect.
  `FailNode`: fail. `Cut`: cut. `Succeed`: none.
- `Choice`, `Opt`, `Repeat` and `Look` do not pass on Cut. `Opt` does not pass on Fail, nor does `Repeat` with a
  minimum of 0, unless its body can cut: a cut commits the option or the iteration, which then fails when its body
  fails after the cut. `Look` neither consumes nor records expectations, and can fail. A `Repeat` with a scope does not pass
  on the captures of its element.
- `Call` has the effects of the callee, except its captures, bindings and cut, which are the callee's own, and adds Call
  and Raise: a call can exceed the nesting limit. `Nest` is the same for its body. An `OperatorsRule` has consume, fail,
  expect, call and raise, and the effects of its lines.
- `Capture` adds Capture and its slot. `BindNode` is Bind. `Pred` is Fail. A `Pred`, a `BindNode`, a rule's action and
  an `OperatorsRule` line's action add Raise when their action expression can raise.
- `Recover` can fail only when its body can fail and its skip can fail or match without consuming input, and adds
  Recover. `Missing` is consume, fail, expect, recover and raise: its literal fails where it may not be inserted, and an
  insertion can exceed the limit of missing tokens. `Stream` adds Commit in a start rule.
- `Scan` consumes and expects, and can fail if its minimum is positive.

A rule's own effects, as its callers see them, are those of its body without Capture, Bind and Cut.
