# SyntaxIR Bytecode Specification

This document specifies the bytecode (the *module*) that a SyntaxIR grammar compiles to, and the behavior of the virtual machine (VM) that executes it.
A runtime for another language must implement this document. The meaning of the language that the bytecode implements is defined by the [Grammar Language Specification](../language/README.md); for the results a parse must produce, see [Parsing](../runtime/parsing.md).

A bytecode VM must produce the same results as the closure backend.

## Terminology

| Term | Meaning |
|:--|:--|
| Flow body | The instructions that match input for a rule, a line or the skip of an Operators Rule ([Flow Bodies](#flow-bodies)) |
| action code | The code of one action expression: an action, the action expression of a Predicate Action or the value of a binding ([Action Code](#action-code)) |
| expectation | A string describing what was expected at a position, used in syntax errors |
| farthest failure | The farthest position at which a failure was recorded, together with the set of expectations recorded at that position |
| level | The Operators Rule binding level passed to a rule call (`min`); 0 for an ordinary call |
| value-free twin | A copy of a rule compiled so that it builds no value ([Rule table](#rule-table)) |
| transient rule | A rule that is not memoized in normal parses ([Rule calls](#rule-calls)) |

## Values

| Value | Description |
|:--|:--|
| Node | Type name, rule name (optional), range `[start, end)`, text, list of children, and fields (name → value). A node also carries a flag that marks it as holding text: a string node or an error node. |
| Integer, string, boolean | Used by Value Actions and Predicate Actions |
| nil | The absence of a value |

The reserved bytecode node types are the [kinds](../language/types.md#node-type-names) `string`, `tuple`, `list` and `error`.
A node is *fresh* only while the body of the rule that created it is executing ([Rule completion](#rule-completion)).

## Input and Positions

The input is a sequence of characters. The position unit is chosen for each parse: Unicode code points or UTF-8 bytes.
Reading a character returns the character (a code point) at the current position and its size in position units. Each invalid UTF-8 byte is read as one U+FFFD character whose size is one unit (one byte, or one code point).

## Module

A module consists of the following tables.

| Table | Elements |
|:--|:--|
| String table | Strings. All other tables and instructions refer to strings by index. |
| Character class table | A list of ranges (pairs of lower and upper bounds), a negation flag, and an expectation string |
| Type table | Type name, and either the field names and zero descriptors of a struct type or, for a named node type `type N = node[T]`, the text of its underlying node type `node[T]` |
| Scope table | The capture names of a repetition element, in slot order |
| Variable table | The declared variables: name, type (`int`, `string` or `bool`) and initial value. Every parse begins with the environment holding each variable bound to its initial value. |
| Rule table | See [Rule table](#rule-table) |
| Operators Rule table | See [Operators Rule table](#operators-rule-table) |
| Action table | The action code of the module's action expressions ([Action Code](#action-code)) |
| Run key table | The variables that key the records of each run site that is not free of the environment ([Run key table](#run-key-table)) |
| Flow bodies | The Flow body of each rule but a rule whose body is the expression of its Operators Rule, and the stream body of each rule with a `#stream` repetition ([Flow Bodies](#flow-bodies)) |

The public grammar AST treats every negative repetition maximum as unbounded.
Compilation canonicalizes it to -1. A repetition bound is an integer immediate
of a Flow body, which holds the full implementation-int range.

### Rule Table

| Item | Description |
|:--|:--|
| Name | Rule name |
| Scope | The capture names of the body, in slot order |
| Action | The action's action expression (an index of the action table), or -1 if there is none |
| Node type | Name of the named node type a rule declared with one converts its value to (see Rule Completion), or -1 if there is none. For a rule with an Operators Rule and no action, it is the named node type every operator's value is converted to (see Operators Rule); with an action, it is the type the action's result is converted to |
| Memoize, left-recursion leader, member, position-dependent | Results of static analysis. A member is a rule of a left-recursion cycle that is not its leader. |
| Cycle | The number of the left-recursion cycle of a leader or member, from 1, the same for the rules of one cycle; 0 for any other rule |
| Operators Rule | Index into the Operators Rule table, or -1 if there is none |
| Value-free twin | Whether this entry is a value-free twin (see below). The value of a value-free twin is always nil. |
| Transient | The rule is not memoized in normal parses ([Rule calls](#rule-calls)) |
| Recursive | The rule is on a cycle of the call graph or calls itself; only calls of such rules count against the nesting limit ([Limits](../runtime/parsing.md#limits)) |
| Where | The text that names the rule in the message of a failed action, as an index into the string table, or -1 for the name. The hidden rule of an inline action is named by the rule that holds the action. |
| Splice | The rule makes no node of its own (a hidden rule without an action). Its body returns the tuple node of its elements, if the body builds one, else its one element or nil; the value of the rule is that result as it is, without the captures of the body, and its callers take its elements with `SPREAD`. |
| Missing | The rule or a rule it calls contains `#missing`. While missing tokens are inserted, such a rule is not memoized and its memo keys carry the insertion state ([`#missing`](../language/pragmas.md#missing)) |
| Value | The shape of the value the rule makes from the value of its body (see Rule Completion), or none if the body's value is the rule's value: a rule with an action, a splice, a rule with an Operators Rule or a value-free twin. A rule's Flow bodies are framed functions that leave this step to the call, as the rule tables of the generated parsers do. |
| Variables | Names (string table indices, sorted) of the variables read by predicates in the rule or the rules it calls; memo entries are keyed by their values |

**Value-free twins.** When a rule that returns a CST is called from a place where its value is discarded (`#atomic`, `-`, `!`, or a value-free body), the compiler compiles a second version of its body that builds no value of its own: only its captures build values, as in the rule. The twin is placed in the rule table after all original rules, and the `CALL` refers to it. The twin has the same name as the original rule; lookups of a start rule by name ignore twins. Because the twin matches the same input in the same way, results do not change. Rules that belong to a left-recursive cycle have no twin.

**Value-free bodies.** The body of a rule with an action reads only captures, so it is compiled without building its own value. Capture targets still build their values.

### Operators Rule Table

| Item | Description |
|:--|:--|
| skip body | The [Flow body](#flow-bodies) of the skip, or none. It builds no value, and runs in the frame of the parse it is run in, with no slots of its own. |
| operand | A list of lines |
| Prefix operators / infix and postfix operators | Lists of operators (index, kind, associativity, level, line) |

A *line* has a scope, an action (an index of the action table, or -1), a [Flow body](#flow-bodies), which runs in a frame with a slot for each name of its scope, and a shape. The body returns the value of the line's expression, not the line's value: the shape makes the line's value from it, as `SCOPEVALUE` does for an operand line, whose value is the value of its scope as for a rule body, and as `SHAPESINGLE` does for the operator of an operator line, whose value is the operator's value: for a sequence, the tuple node of the operator's values, with the line's captures attached to it.

Operator kinds are encoded as 0 = prefix, 1 = postfix, 2 = infix. Associativity is encoded as 0 = left, 1 = right, 2 = none.

### Run Key Table

A run site that is not free of the environment (see "Run Sites" in [parsing](../runtime/parsing.md#run-sites)) has an entry: its site, the variables of its key, and the variables it binds, by name. The variables of the key are those that the predicates of the site's element read or bind, and those that the rules the element calls read, directly or indirectly (their Variables in the rule table); the variables it binds are those that the predicates of the element bind, a subset of the key. A binding made in a called rule ends when the rule returns, so the variables it binds are not among them unless the element reads them.

A walk of the site keeps a record of a position for the values that the variables of the key held at the start of the walk from there, and a walk takes the record only where they hold equal values, compared as memo keys compare them. A walk that takes a record binds each variable that the site binds to the value the walk of the record left in it, and keeps the values of the others.

## VM State

| State | Description |
|:--|:--|
| `pos` | Current position |
| Registers | The frame of registers of each running Flow body ([Flow Bodies](#flow-bodies)) |
| Frame | The capture slots of the current scope |
| Capture trail | Writes to captures (frame, slot, previous value) |
| Environment | The values of the declared variables, as a persistent vector indexed by variable slot (the position in the variable table). Saved environments are never modified: rebinding a variable copies only the path to its slot. A parse starts from the environment built from the variable table, so every declared variable is bound before the start rule runs. |
| Recovered errors | Syntax errors recovered by `#recover` |
| `silent` | While positive, expectations are not recorded |
| Farthest failure | A position and the set of expectations recorded there. A parse that does not [record expectations](../runtime/parsing.md#recording-expectations) tracks the position only: the set holds at most a placeholder that stands for a failure at the position and describes nothing |
| Memo | (rule, position, level) → result |

A Flow body saves and restores the state as its function does, with `MARK` and `RESET` (`flow.markState` and
`flow.reset`): restoring the state returns the position, the capture trail, the environment and the recovered errors to
the saved ones, writing each undone capture's previous value back into its slot. A Flow body fails by returning failure;
there is no stack of backtrack entries.

## Rule Calls

`CALL r, min` calls rule r as follows. The call is memoized if the rule is a leader, or if it is marked for memoization and is not transient. In an incremental parse, a transient rule is memoized when it is marked for memoization, so that results from the previous parse can be reused.

A rule is marked transient when it calls no other rule, or when it is referenced only once in the grammar. Re-evaluating the former only scans the input once more; the latter is re-evaluated at the same position only when its single caller is. In both cases a memo entry would never be reused.

1. Look up the memo. A leader whose growth for (r, pos, min) is in progress reads its growing result, whatever the variable values. Otherwise look up a result for (r, pos, min) and, when the rule reads variables, for the current values of those variables (the rule table's variable list). In the context of the growths of the rule's cycle at the current position, a result in the memo is used only if it was computed while at least those growths were in progress, and a result kept for the context is used under the conditions of [Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules). If there is one, and it is growing, or it was not computed inside a lookahead, or the VM is currently inside a lookahead, use it: record the expectations stored with the result (unless it is growing) and its recovered errors; on success, advance the position and return the value.
2. If the call is memoized, save the farthest failure and reset it to (start position, empty).
3. If the rule is a leader, evaluate it by [growing the seed](#left-recursion); otherwise [evaluate the body](#body-evaluation) once. On failure, remove the errors recovered during this call.
4. If the call is memoized, store in the memo, replacing the entry for the same key, the result, the end position, the recovered errors, whether the call ran inside a lookahead, and the expectations recorded inside; then merge those expectations into the saved farthest failure and restore it. A call in a context records the leaders of the growths of its context with its result. A result that read a growing result of its context, or observed a lookahead at the position that read one, is kept for the context instead, with what it read and observed ([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules)).
5. On failure, reset the position to the start position.

### Body Evaluation

To evaluate a body, the VM creates a new frame (sized for the rule's scope), saves the frame, the environment and the length of the capture trail, and runs the rule's Flow body in a frame of registers of its own, or, for a rule whose body is the expression of its Operators Rule, parses that expression ([Operators Rule](#operators-rule)).
When the body finishes, the VM restores the frame and the capture trail; on success, it performs [rule completion](#rule-completion); finally it restores the environment, so variables defined in the body are not visible to the caller.

### Left Recursion

A leader rule is evaluated as follows:

1. Start the growth of (r, pos, min) with a failure as its growing result.
2. Reset the position to the start position, reset the recovered errors to their state before the call, and evaluate the body.
3. If the body fails, or does not advance beyond the previous success, stop. Otherwise make the result the growing result and go back to step 2.
4. End the growth and return the last result.

A growth that begins where another growth of its cycle is in progress reuses, in step 2, the steps that an earlier growth of the leader took in the same context ([Evaluations of Rules](../runtime/parsing.md#evaluations-of-rules)).

### Rule Completion

The value of the rule is built from the body value v and the start position:

- **With an action:** evaluate the action with the captures of the scope. For a rule with an Operators Rule, evaluate it with the Operators Rule local `$value`, the folded value v, as well. If the result is a node newly created by the action, its range becomes the range of the rule. An `int`, `string` or `bool` result becomes a value node with the range of the rule.
- **Otherwise:** if the rule table gives a value shape, v becomes the value that `SCOPEVALUE` makes of v and the start position with that shape. Then, if the scope has captures, attach them. If v is fresh, give it the rule name if it has none, and clear its fresh mark.

When the rule's node type is a named node type, the value built above is then converted to the named node type, whose underlying node type the type table gives, as `convert` converts it ([Action Code](#action-code)); a value that is not a string, tuple or list node fails the parse with `cannot convert <value type> to <type> in result of rule <name>`.

**Attaching captures:** if no capture is non-nil, do nothing. If v is nil, is not fresh, or is itself one of the captures, wrap v in a tuple node whose only child is v, whose range runs from the start position to the current position, and which is fresh. Then add each non-nil capture as a field under its name.

## Operators Rule

The algorithm is the same as `operatorsRuleParse` in the closure backend; see the parsing rules in [Operators Rule](../language/operators-rule.md#parsing-rules). Each part's Flow body runs with its own frame of registers and capture frame.

- **Skip:** increment `silent` and run the skip body; on success remove the errors recovered in it, and on failure reset the position.
- **Longest match:** for each candidate operator, save the state, run the line's Flow body in a new frame, and on success record the end position, the value, the frame, the environment, whether a cut was passed, and the recovered errors; then restore the state (writing the recorded captures back into the frame). A prefix or postfix candidate that succeeds without consuming input is treated as not matching. Select the candidate that advanced farthest; on a tie, select the one declared first. Also record whether some candidate failed after passing a cut, including an empty prefix/postfix match rejected as a candidate. A successful selected candidate takes precedence over this aggregate failure; each candidate's cut scope is isolated.
- **Head:** after skip, try the longest match among the prefix operators. If none matches and some candidate failed after passing a cut, the whole expression fails without trying operands. Otherwise, if one matches, apply it and parse the right operand at the operator's level. If the right operand fails, fail if a cut was passed; otherwise backtrack and try the operands. Operands are tried in declaration order; if an operand fails after passing a cut, the whole expression fails.
- **Tail:** after skip, try the longest match among the infix and postfix operators. If there is no candidate, stop (or fail, if a candidate failed after a cut). Each loop keeps a bound, the tightest level that may follow the start or the last applied operator. It starts as the level of the prefix operator that began the expression, or unbounded if an operand began it; an infix application sets it to the operator's level (level - 1 for a right-associative operator), and a postfix application makes it unbounded. If the operator's level is less than or equal to `min` or greater than the bound, or the operator is non-associative and a non-associative operator of the same level was just applied, backtrack and stop. A postfix operator is applied immediately. For an infix operator, parse the right operand at the operator's level (level - 1 for a right-associative operator); if that fails, fail if a cut was passed, and otherwise backtrack and stop. The bound is what keeps a non-associative operator from chaining in the right operand of a looser operator: `a | a < a < a` ends after `a | a < a`, because the loop of the right operand of `|` stops at the second `<` and the loop of `|` does not accept an operator tighter than `|`.
- **Values:** if the operator has an action, evaluate it with the Operators Rule locals `$rhs` (except for a postfix operator), `$lhs` (except for a prefix operator) and `$op` (a string node of the operator part's text); `LOCAL` looks a name up by walking them in that order. Otherwise build a tuple node whose children are `[op, rhs]`, `[lhs, op]` or `[lhs, op, rhs]` according to the operator kind, whose `operator` field is the operator's index, and which carries the rule name. The value of an operand is its action's result if it has an action; otherwise its captures are attached and, if it is fresh, it is given the rule name. When the rule's node type is a named node type and the rule has no action, the value of every operator (the action's result, or the tuple node, which is then built with that type) is converted to it as `convert` converts it, with the failure `cannot convert <value type> to <type> in result of rule <name>`; the value of an operand is not converted, and the rule's result as a whole is not converted.

## Execution Models

A runtime implements one or both of two execution models, which differ in how rule calls and the Operators Rule are executed. Both must produce the same results.

- **Recursive model:** `CALL` and `CALLJUMP`, and the body of a rule whose body is the expression of its Operators Rule, invoke the procedures in [Rule calls](#rule-calls) and [Operators Rule](#operators-rule) as host-language functions. The maximum nesting depth is bounded by the host stack.
- **Iterative model:** execution suspends at `CALL` and `CALLJUMP` and resumes when the result is available, with the value and the success in the instruction's destinations. Rule calls, left-recursion growth, the Operators Rule loop, longest match and skip are implemented as state machines whose state is kept in stack entries, and they perform the steps above in the same order. Host stack usage is constant, independent of how deeply the input nests.

Both models perform the same instruction steps and the same stages of a rule call: the call begins, a left-recursive rule grows its seed, the rule body is invoked, and the call ends. They differ only in the order in which these are invoked.
The reference implementation also limits the nesting depth of recursive rule calls, the rules on a cycle of the call graph (`Rec` in the rule table; 100,000 by default, 10,000,000 for the iterative model; configurable with `runtime.Limits.Depth`; see [Limits](../runtime/parsing.md#limits)), and reports a parse error when the limit is exceeded. A call of a rule without `Rec` does not change the depth.
Actions and predicates may be evaluated with host function calls in either model, because their nesting depth is fixed by the grammar and does not depend on the input.

## Action Code

An action expression of the action language, an action, the action expression of a Predicate Action or the value of a binding, is evaluated by its
action code: the instructions selected from the function of its action site in the Action module of the table (the
[lowering of action expressions](../exec-ir/lowering.md#action-expressions)), which call the
[compositions of action expressions](../exec-ir/compositions.md#action-expressions) in the order the action expression evaluates its operands.
Each use of a site in the module, with the capture slots of the frame it is evaluated in, is an entry of the action
table. The closure backend runs the same action code, selected from the same functions by the same selection, so the
two backends evaluate an action expression alike.
A failure of a composition ends the evaluation with that failure. A failure in an action is an error of the whole
parse (`action in <rule name>: <message>`); in a predicate or a binding it is a failure.

Each evaluation has registers of four files, by the type of what they hold: values (`a`), integers and booleans (`n`,
a boolean as 0 or 1), sequences of elements (`s`) and strings (`t`). An action expression gives the size of each file; every register
starts as nil, 0, an empty sequence or the empty string. An instruction has an opcode, a destination D and operands
A, B and C; the table names the file of each register operand (`a[A]` is register A of the values) and the pool of
each pool operand. Lambdas are evaluated in place, so no value of an action expression is a function.
Action code has one loop, a counted loop over the elements of a sequence: `LOOP` counts them and
starts the index at 0, the body runs once for each element, and `ENDLOOP` advances the index and goes back to the start
of the body until the index reaches the count. Every other jump goes forward, so each evaluation of action code ends.

| Code | Instruction | Behavior |
|--:|:--|:--|
| 0 | `NOP` | Nothing. |
| 1 | `CONST` | a[D] = constant A. |
| 2 | `INT` | n[D] = A. |
| 3–6 | `MOVEA`, `MOVEN`, `MOVES`, `MOVET` | D = A in the values, integers, sequences or strings. |
| 7 | `CAP` | a[D] = the value of capture slot A of the frame, with a box opened. |
| 8 | `LOCAL` | a[D] = the Operators Rule local that name A names (`$lhs`, `$rhs`, `$op` or `$value`), with a box opened; the evaluation fails if there is none. |
| 9 | `VAR` | a[D] = variable A. |
| 10 | `UNBOX` | a[D] = a[A] with a box opened. |
| 11 | `FROMB` | a[D] = the boolean n[A]. |
| 12 | `FROMT` | a[D] = the string t[A]. |
| 13 | `NOT` | n[D] = 1 − n[A]. |
| 14–16 | `LT`, `ADD`, `EQN` | n[D] = n[A] < n[B], n[A] + n[B], n[A] = n[B]. |
| 17 | `ISNIL` | n[D] = whether a[A] is nil. |
| 18 | `MEMBER` | a[D] = `member` of a[A] with member read B. |
| 19 | `UNARY` | a[D] = `unary`(B, a[A]); B indexes the [unary operators](../exec-ir/values.md#operators). |
| 20 | `BINARY` | a[D] = `binary`(C, a[A], a[B]); C indexes the binary operators. |
| 21 | `LOGIC` | n[D] = `logic`(B, a[A]); B indexes the logical operators. |
| 22 | `TEXT` | t[D] = `text`(a[A]). |
| 23 | `TEXTEQ` | n[D] = whether t[A] and t[B] are equal (`action.textEqual`). |
| 24 | `LENGTH` | a[D] = `length`(a[A]). |
| 25 | `CONVERT` | a[D] = `convert`(node type B, a[A]). |
| 26 | `CONFORMS` | n[D] = `conforms`(the type of cast B, a[A]). |
| 27 | `CASTERR` | a[D] = the failure of cast B of a[A] (`action.castError`): `cannot cast <value type> to <type> at input position <N> (line <L>, column <C>)`, where N is the position where the evaluation started, followed by `; cast at grammar position <line>:<column>` when the cast's line is positive. |
| 28 | `KIDS` | n[D] = the number of gathered elements (`action.kids`). |
| 29 | `PUSH` | `push`(name B, a[A]). |
| 30 | `PUSHALL` | `pushItems`(name B, a[A]). |
| 31 | `ABOVE` | s[D] = the elements gathered above n[A], taken off (`action.elemsAbove`). |
| 32 | `HULL` | a[D] = `hull`(B = 1, s[A]), counting one compute unit for each element of the list or tuple it builds. |
| 33 | `ITEMS` | s[D] = `items`(name B, a[A]). |
| 34 | `ITEMAT` | a[D] = `itemAt`(s[A], n[B], C = 1). |
| 35 | `SEQLEN` | n[D] = the number of elements of s[A]. |
| 36 | `STEP` | Count one compute unit: the element that `map`, `filter` or a fold visits (`seq.step`). |
| 37 | `KEEP` | Gather element n[B] of s[A]. |
| 38 | `FILTER` | n[D] = `filterKeep`(a[A]). |
| 39 | `COPY` | a[D] = a new list value of the elements s[A], counting two compute units for each element: the element that `map` visits and the one it builds. |
| 40 | `STRUCT` | a[D] = a new struct of struct literal A, with as many unset fields as it has. |
| 41 | `SETFIELD` | Set field name C of the struct a[A] to a[B]. |
| 42 | `COVER` | n[D] = `cover`(a[A], a[B], n[C] ≠ 0). |
| 43 | `MADE` | `made`(a[A], n[B] ≠ 0). |
| 44 | `JUMP` | Continue at instruction D, which follows it. |
| 45 | `JUMPF` | Continue at instruction D, which follows it, if n[A] is 0. |
| 46 | `RET` | End the evaluation with the value a[A]. |
| 47 | `RETERR` | End the evaluation with the failure a[A]. |
| 48 | `FIELD` | Set field name C of the struct a[A] to a[B], and n[D] = `cover`(a[A], a[B], n[D] ≠ 0): `SETFIELD` and `COVER` with D = C in one instruction. |
| 49 | `UNITS` | Count A compute units (`units.add`); A is not negative. |
| 50 | `UNITSOF` | Count B compute units for each element of the list or tuple a[A], none if a[A] is neither (`units.items`); B is not negative. |
| 51 | `UNITSSINCE` | Count one compute unit for each element gathered above n[A] (`units.since`). |
| 52 | `LOOP` | n[C] = the number of elements of s[A] and n[B] = 0; continue at instruction D if n[C] is 0. |
| 53 | `ENDLOOP` | n[B] = n[B] + 1; continue at instruction D if n[B] < n[C]. |

The pools of an action expression hold its constants (nil, an int, a string or a bool), its names (the names of built-in functions,
Operators Rule locals and struct fields), its member reads (the [member kind](../exec-ir/values.md#member-kinds), the
name the action expression reads and the field or capture it selects), the named node types of its conversions, its casts (the
resolved type text, written as the type table writes an underlying node type, and the grammar position, or 0 and 0
without source information) and its struct literals (the struct type and its number of fields). A cast to a type that
every value fits has no instruction.

A runtime checks the action code of a module when it loads it, and rejects the file if any check fails: the pools refer
to strings of the string table, a conversion to a named node type, a struct literal to a struct type, a cast to a type
text of the form the load-time checks of casts accept, and a member read to a member kind; an action expression has code; every
opcode is in the table; every operand is in range for its kind: a register within its file, a pool operand within its
pool, a jump target within the code, a variable within the variable table, an operator within its operators, a flag 0
or 1 and a capture slot not negative; the code ends with `RET` or `RETERR`, so that evaluation cannot run
past it; and its loops are counted loops. That is, every `JUMP` and `JUMPF` goes forward; a `LOOP` at L continues at
E + 1, where the `ENDLOOP` at E is the end of no other loop, continues at L + 1 and names the same index B and count C,
two distinct registers; every `ENDLOOP` ends a loop; the instructions from L + 1 to E, the loop's body, write neither
n[B] nor n[C]; no instruction outside the body jumps into it; and two loops are apart or one is in the body of the
other. A body is thus entered only at its start with the index at 0, and goes back at most as many times as the count
that its `LOOP` fixed, so that evaluation ends. A runtime does not check that registers hold what an instruction reads.

## Flow Bodies

A rule has a Flow body: the code of the [Flow function](../flow-ir/functions.md) that runs the rule's body in the
Node table of the code lowered with logical compute units, as a flat list of instructions. The function is local, a whole
rule whose captures are variables, or framed, whose captures are slots of the rule's frame. In both models
([Execution Models](#execution-models)) the rule's entry, its call and its completion ([Rule Calls](#rule-calls)) are
those of the rule table, and the body returns the rule's value and success, or failure. A `CALL` calls its rule with a
host function call in the recursive model, and in the iterative model by suspending the body and resuming it with the
value and the success when the call ends, so that the iterative model's host stack does not grow with the calls of
Flow bodies. A module file holds the Flow bodies of rules after the run key table ([File Format](#file-format)). The
skip and the lines of an [Operators Rule](#operators-rule-table) have Flow bodies too, the framed functions of the same
Node table; the Operators Rule table holds them. Only a rule whose body is the expression of its Operators Rule has no
Flow body: it runs that expression ([Operators Rule](#operators-rule)).

A rule whose body has a `#stream` repetition has a stream body: the code of the framed Flow function of the rule's body
whose `#stream` repetition delivers its elements, which the Go code generated for a stream parse runs for its start
rule. Any such rule may start a stream parse, so every one has its stream body. A call of the rule that delivers the
elements of the repetition, as `STREAMON` tests it, runs the stream body in place of the rule's body, in the rule's
frame of captures, in both models; any other call runs the rule's body. The module file holds the stream bodies after
the Flow bodies.

`EMITSTREAM` commits the input and the recovered errors before the position, so nothing after it may go back before
it. A body therefore never restores a state saved before an `EMITSTREAM` that has run since: on no path from an
`EMITSTREAM` does a `RESET`, `SETPOS`, `RESTORE` or `RESETMISSING` read a register that held, when the `EMITSTREAM` ran, a
mark, a position, a number of recovered errors or a number of insertions (written by `MARK`, `POS`, `RECLEN` or
`MISSINGLEN`, or by a `MOVE`, `MARKPOS`, `ADD` or `SUB` of one). The rule holds whatever takes the path back, a choice, a loop or a recovery, and a runtime rejects a
module that breaks it when it loads it ([Load-Time Validation](#load-time-validation)).

A run of a body ends, whatever the limits of the parse. A loop steps (`STEP`) once in each iteration, and an iteration
that goes on to the next step has advanced the position. The register of a step holds the position of the loop's last
step, or -1, which the body sets before the loop; the step requires the position to be past it, sets it to the
position, and otherwise ends the parse with an error of invalid bytecode. A step counts no compute unit. A cycle of
the instructions is therefore bounded when it passes through a `STEP` whose register no other instruction of the cycle
writes: the position at that step increases from one pass to the next, and the input is finite. A cycle is also
bounded when it reuses an element of a run: it passes through a `RESUMENEXT` whose success the next instruction, a
`JUMPFALSE`, tests to leave the cycle, and no `RESUMESTART` is on the cycle, so that the cycle repeats at most once for
each element of a run that it does not start again. The cycles are those of the instructions, each continuing at the
next one unless it is `JUMP` or a return (`RETURN`, `RETURNOK` or `RETURNFAIL`) and at its target if it has one,
whatever the registers hold. A runtime rejects a module with any other cycle when it loads it, so that a run of each
body of a loaded module ends ([Load-Time Validation](#load-time-validation)).

A Flow body has a frame of registers, which hold the variables of the function, each register a value of the type of
its variable. An instruction has an operation, destination registers, argument registers, a jump target and an
immediate. A body writes each register before it reads it: on every path from its first instruction to an instruction
that reads a register as an argument, some instruction names that register as a destination, where an instruction
continues at the next one unless it is `JUMP` or a return (`RETURN`, `RETURNOK` or `RETURNFAIL`), and also at its target if it has one. A frame of registers
therefore starts without values, so a runtime need not clear it, and a body starts by writing the zero value of each
variable that some path reads before any other write. The control of the function, its blocks, exits and conditionals,
becomes jumps: in a local function an exit to the failure label returns failure, and running to the end of the body
returns the result variable and success; a framed function returns by its own returns. A framed body runs in the frame of captures that the rule's entry makes, with
a slot for each name of the rule's scope, and the rule's completion attaches them; the scope of the rule is the capture
layout of its function, and a module whose Flow body would run in a frame of other slots is not built. The frame
registers of a body hold capture frames: the current frame, which `SETCAPTURE` writes, and the frames of repetition
elements, each with a slot for each name of a scope of the scope table. `flow.declareNames` has no instruction: it does
nothing to the state. A capture slot of an immediate is below the number of slots of the largest frame the body can run
with, the rule's or one that its `NEWFRAME` makes; a slot past the frame it indexes when the body runs fails the parse.

Each operation has a code, the code a module file stores, and fixed numbers of destination registers (D) and argument
registers (A); `JUMP`, `JUMPFALSE` and the instructions from `JUMPTRUE` to `CALLJUMP` also have a target. The
instructions from `MATCHCLASS` to `CALLJUMP` and from `RETURNOK` on are superinstructions: each does in one instruction what a sequence of
other instructions does, a test of the input that leaves the block when it fails or a return of a constant success.

| Code | Instruction | D | A | Behavior |
|:--|:--|:--|:--|:--|
| 0 | `CONST` | 1 | 0 | The destination is the immediate: a boolean, an integer, or the null value. |
| 1 | `MOVE` | 1 | 1 | The destination is the argument. |
| 2 | `NOT` | 1 | 1 | The destination is the negation of the boolean argument. |
| 3 | `POS` | 1 | 0 | The destination is the position. |
| 4 | `SETPOS` | 0 | 1 | The position is the integer argument. |
| 5 | `JUMP` | 0 | 0 | Continue at the target. |
| 6 | `JUMPFALSE` | 0 | 1 | Continue at the target when the boolean argument is false. |
| 7 | `RETURN` | 0 | 2 | Return the value of the first argument with the success of the second. |
| 8 | `MARK` | 1 | 0 | `flow.markState`. |
| 9 | `RESET` | 0 | 1 | `flow.reset`. |
| 10 | `PEEK` | 3 | 0 | `flow.peek`: the character, its width and whether there is one. |
| 11 | `GUARD` | 1 | 2 | `flow.guardFails` for the guard of a class of the immediate, given the character and whether there is one: a class of the character class table and the depth of calls the guard adds. |
| 12 | `EXPECT` | 0 | 1 | `flow.expect` of the expectation of the immediate at the position of the argument. |
| 13 | `UNFRESH` | 0 | 1 | `flow.unfresh`. |
| 14 | `CALL` | 2 | 0 | `flow.callRule` of the rule of the immediate at its binding level: the value and the success. |
| 15 | `ADD` | 1 | 2 | The destination is the sum of the integer arguments. |
| 16 | `SUB` | 1 | 2 | The destination is the first integer argument minus the second. |
| 17 | `EQ` | 1 | 2 | The destination is whether the arguments are equal; an integer and a character compare as integers. |
| 18 | `NE` | 1 | 2 | The destination is whether the arguments differ. |
| 19 | `LT` | 1 | 2 | The destination is whether the first integer or character argument is less than the second. |
| 20 | `LE` | 1 | 2 | The destination is whether the first integer or character argument is at most the second. |
| 21 | `STEP` | 1 | 1 | `flow.step` of the loop whose register is both the argument and the destination: the argument holds the position of the loop's last step, or -1 before its first; if the position is not past it, the parse ends with an error of invalid bytecode, and otherwise the destination becomes the position. |
| 22 | `SILENT` | 0 | 0 | `flow.silent` of the lookahead of the immediate: entering (1) or leaving (-1) it, its site, the Core IR site of the lookahead, or -1 for a lookahead that is no observation, and on leaving whether its expression matched. |
| 23 | `ADVANCE` | 0 | 1 | `flow.advance` by the integer argument. |
| 24 | `INCLASS` | 1 | 1 | `flow.inClass` of the class of the immediate, an index of the character class table, for the character argument. |
| 25 | `ATANCHOR` | 1 | 0 | `flow.atAnchor` and `flow.atEnd` of the anchor of the immediate: 0 the beginning of the input, 1 its end, 2 the beginning of a line, 3 the end of a line. |
| 26 | `MATCHLIT` | 2 | 0 | `flow.matchLit` of the literal of the immediate: the null value and whether it matched. |
| 27 | `ISOLATE` | 1 | 0 | `flow.isolate`. |
| 28 | `UNISOLATE` | 2 | 1 | `flow.unisolate`: the farthest failure and the expectations. |
| 29 | `MERGEEXPECTED` | 0 | 2 | `flow.mergeExpected` of the expectations of the second argument at the position of the first. |
| 30 | `NEWTERMINAL` | 1 | 2 | `flow.newTerminalNode` from the integer first argument to the position, with the text of the second. |
| 31 | `NEWSEQ` | 1 | 2 | `flow.newBranchNode` of a sequence from the integer first argument to the position, with the list of values of the second as its children. |
| 32 | `NEWLIST` | 1 | 2 | `flow.newBranchNode` of a list, as `NEWSEQ`. |
| 33 | `NEWVALS` | 1 | 0 | `flow.newVals` with room for the immediate, a count. |
| 34 | `APPEND` | 1 | 2 | `flow.append` of the value of the second argument to the list of the first. |
| 35 | `SPREAD` | 1 | 2 | `flow.append` of the children of the node of the second argument to the list of the first (Spread). |
| 36 | `TEXT` | 1 | 1 | `flow.text` from the integer argument to the position. |
| 37 | `LITERALTEXT` | 1 | 1 | `flow.literalText` of the literal of the immediate, a string index, matched from the integer argument. |
| 38 | `STR` | 1 | 0 | `flow.str` of the string of the immediate, a string index, with its bytes as the string table holds them. |
| 39 | `KIDSLEN` | 1 | 0 | `flow.kidsLen`. |
| 40 | `KIDS` | 1 | 1 | `flow.kids` above the integer argument. |
| 41 | `DROPKIDS` | 0 | 1 | `flow.dropKids` to the integer argument. |
| 42 | `PUSHKID` | 0 | 1 | `flow.pushKid` of the argument, as the body of a rule frame pushes it, counting nothing. |
| 43 | `SHAPESINGLE` | 2 | 2 | `flow.shapeSingle` of the shape of the immediate for the value of the first argument matched from the integer second: the value and whether there is one. |
| 44 | `SCOPEVALUE` | 1 | 2 | `flow.scopeValue` of the shape of the immediate for the value of the first argument matched from the integer second. |
| 45 | `SETCAPTURE` | 0 | 1 | `flow.setCapture` of the slot of the immediate in the current frame to the argument, recorded on the capture trail. |
| 46 | `MARKPOS` | 1 | 1 | `flow.markPos`: the position of the mark argument. |
| 47 | `FRAME` | 1 | 0 | `flow.frame`: the current frame. |
| 48 | `NEWFRAME` | 1 | 0 | `flow.newFrame` of the scope of the immediate, an index of the scope table: a frame with a slot for each of its names. |
| 49 | `SETFRAME` | 0 | 1 | `flow.setFrame`: the frame argument becomes the current frame. |
| 50 | `CLEARVALS` | 0 | 1 | `flow.clearVals` of the frame argument. |
| 51 | `FRAMEVAL` | 1 | 1 | `flow.frameVal`: the value of the slot of the immediate of the frame argument. |
| 52 | `ATTACHFRAME` | 1 | 3 | `flow.attachFrame` of the values of the frame of the third argument, as the fields named by the scope of the immediate, to the value of the first argument matched from the integer second. The scope has a name for each slot of the frame. |
| 53 | `SHAPECAPTURE` | 1 | 2 | `flow.shapeCapture` of the shape of the immediate for the value of the first argument matched from the integer second. |
| 54 | `RUNHOT` | 1 | 0 | `flow.runHot` of the run site of the immediate. |
| 55 | `RUNBEGIN` | 0 | 0 | `flow.runBegin` of the walk of the immediate: its run site, whether its iterations have values and whether its records are keyed by the variables of the site's run key. It is also `flow.skipBegin` of a skip-run site, whose iterations have no values and whose records are keyed by the variables as `SKIPREPEAT` keys them; skip-run sites and run sites share one numbering. |
| 56 | `RUNWALKED` | 0 | 1 | `flow.runWalked` of the run site of the immediate, for the walk that began at the integer argument. |
| 57 | `SKIPVISIT` | 0 | 0 | `flow.skipVisit`. |
| 58 | `SKIPHIT` | 1 | 0 | `flow.skipHit`. |
| 59 | `SKIPVALUE` | 0 | 1 | `flow.skipValue` of the argument. |
| 60 | `SKIPEND` | 0 | 0 | `flow.skipEnd`. |
| 61 | `SKIPKIDS` | 1 | 0 | `flow.skipKids`. |
| 62 | `SCANREACH` | 1 | 2 | `flow.scanReach` of the scan site of the immediate for the walk that began at the integer first argument and matched the integer second characters. |
| 63 | `SCANEND` | 0 | 2 | `flow.scanEnd` of the scan site of the immediate, as `SCANREACH`. |
| 64 | `SCANKIDS` | 1 | 3 | `flow.scanKids` of the scan site of the immediate for the walk that began at the integer first argument, gathered its elements from the height of the integer second and matched the integer third characters. |
| 65 | `PRED` | 1 | 0 | `flow.pred` of a framed body: whether the predicate whose action expression is the immediate holds, evaluated in the current frame ([Action Code](#action-code)). |
| 66 | `BIND` | 1 | 0 | `flow.bind` of a framed body: binds the variable of the immediate to the value of its action expression, evaluated in the current frame ([Action Code](#action-code)), and the destination is whether the evaluation succeeded. The value is not checked: the compiler has proved it assignable to the declared type of the variable, and a variable holds any value, so a module that binds another cannot break memory safety. The rule's entry restores the variables when the call ends ([Body Evaluation](#body-evaluation)). |
| 67 | `CUT` | 1 | 0 | The destination is the cut flag of the rule call the body runs in (the register `flow.cut`), which the rule's entry clears on entry and restores when the call ends. |
| 68 | `SETCUT` | 0 | 1 | The cut flag of the rule call the body runs in is the boolean argument. |
| 69 | `KEEP` | 1 | 1 | `flow.keep`: the expectations of the argument, kept beyond the scope that recorded them, as those of a failed `#recover` body are kept while its skip runs. |
| 70 | `MAKEERROR` | 1 | 2 | `flow.makeError`: the syntax error at the integer first argument with the expectations of the second. |
| 71 | `APPENDERROR` | 0 | 1 | `flow.appendError`: appends the syntax error of the argument to the recovered errors. |
| 72 | `NEWERRORNODE` | 1 | 2 | `flow.newErrorNode`: the error node of the syntax error of the second argument, from the integer first argument to the position: its text is the text of that range and its `message` field is the string form of the syntax error. |
| 73 | `RECLEN` | 1 | 0 | `flow.recLen`: the number of recovered errors. |
| 74 | `RESTORE` | 0 | 2 | `flow.restore`: the position is the integer first argument, and the recovered errors after the first integer second argument are dropped. |
| 75 | `GUARDLIT` | 1 | 0 | `flow.guardLitFails` for the guard of a literal of the immediate: the first character of the literal, whether it is case-folded, and the depth of calls the guard adds. It compares the character with the input at the position as the literal does, in bytes up to the first byte that differs. |
| 76 | `JUMPTRUE` | 0 | 1 | Continue at the target when the boolean argument is true. |
| 77 | `MATCHCLASS` | 3 | 0 | `PEEK` into the destinations; if there is no character or it is not in the class of the immediate, `EXPECT` of the immediate's expectation at the position and continue at the target. |
| 78 | `GUARDJUMP` | 0 | 2 | `GUARD` of the guard of the immediate for the arguments; if it fails, `EXPECT` of the immediate's expectation at the position and continue at the target. |
| 79 | `GUARDLITJUMP` | 0 | 0 | `GUARDLIT` of the guard of the immediate; if it fails, `EXPECT` of the immediate's expectation at the position and continue at the target. |
| 80 | `ASSERTJUMP` | 0 | 0 | `ATANCHOR` of the anchor of the immediate; if the position is not at it, `EXPECT` of the immediate's expectation at the position and continue at the target. |
| 81 | `MATCHLITJUMP` | 2 | 0 | `MATCHLIT` of the literal of the immediate into the destinations; continue at the target if it did not match. |
| 82 | `CALLJUMP` | 2 | 0 | `CALL` of the rule of the immediate into the destinations; continue at the target if the call failed. In the iterative model it suspends the body as `CALL` does. |
| 83 | `STREAMON` | 1 | 0 | `flow.streamOn`: whether the call of the rule of the immediate delivers the elements of its `#stream` repetition: the parse is a stream parse with a consumer, the rule is its start rule, and, if the rule is recursive, the call is its top call. |
| 84 | `STREAMSTART` | 0 | 0 | `flow.streamStart`: the repetition that delivers the elements is about to run; the elements' values are allocated apart from those of the input before it. |
| 85 | `EMITSTREAM` | 0 | 1 | `flow.emitStream`: the element of the argument, a node or the null value, is passed to the consumer (no longer fresh) and the input up to the position is committed, as `NEXT` with mode 2 does. |
| 86 | `RESUMEON` | 1 | 0 | `flow.resumeOn`: whether the repetitions resume their runs: the parse is a Document's, outside lookaheads and the evaluation of a skip expression. |
| 87 | `RESUMESTART` | 1 | 0 | `flow.resumeStart`: starts the run of the repetition of the immediate at the position, with the record of the run that it takes over ([Reuse Model](../runtime/documents.md#reuse-model)); the destination is the run. |
| 88 | `RESUMEMORE` | 1 | 1 | `flow.resumeMore`: whether the run of the argument may take another element. |
| 89 | `RESUMENEXT` | 2 | 1 | `flow.resumeNext`: reuses the next element of the run of the argument that its record holds, replaying what evaluating it did; the destinations are its value and true, or the null value and false when the repetition evaluates its element instead. A reuse counts no unit and polls the budget controls. |
| 90 | `RESUMEBEGIN` | 0 | 1 | `flow.resumeBegin`: starts evaluating an element of the run of the argument. |
| 91 | `RESUMEEND` | 0 | 3 | `flow.resumeEnd`: ends evaluating the element of the run of the first argument, with the success of the boolean second and the value of the third, and records the element if it matched. |
| 92 | `RESUMEFINISH` | 0 | 1 | `flow.resumeFinish`: records the run of the argument of a repetition that matched. |
| 93 | `MERGERENAMED` | 0 | 2 | `flow.mergeRenamed` of the expectations of the second argument at the position of the first, each that is no message renamed to the expectation of the immediate (`@diagnostic`). |
| 94 | `SCANREACHAT` | 1 | 2 | When the integer second argument, the count of a walk of the scan site of the immediate that began at the integer first argument, is the scan hot span of the parse, `SCANREACH`; otherwise the destination is the count. It stands for the test `count == ScanHotSpan` around `flow.scanReach`, with the span of the parse, which is `ScanHotSpan` (16) but in tests that lower it. |
| 95 | `SCANENDAT` | 0 | 2 | When the integer second argument, the count of a value-free walk of the scan site of the immediate that began at the integer first argument, is at least the scan hot span of the parse, `SCANEND`; otherwise nothing. It stands for the test `ScanHotSpan-1 < count` around `flow.scanEnd`. |
| 96 | `RETURNOK` | 0 | 1 | `RETURN` of the argument with success: what `CONST` of true into a register and `RETURN` of the argument and that register do. |
| 97 | `RETURNFAIL` | 0 | 0 | `RETURN` of failure: what `CONST` of the null value and of false into two registers and `RETURN` of them do. |
| 98 | `SCANCLASS` | 3 | 0 | A repetition of a character of the class of the immediate that keeps no value: `MATCHCLASS` of the immediate into the destinations, which ends the repetition with the character that is not in the class; otherwise `ADVANCE` of the character's size and again. It is the loop that steps (`STEP`) before each `MATCHCLASS`, a register of its own holding the position of the last step: every pass but the last moves past a character, so the step's check holds and the repetition ends. |
| 99 | `SCANCLASSCOUNT` | 4 | 1 | `SCANCLASS` that counts: the fourth destination is the integer argument plus one for each character the repetition moved past, as `CONST` of 1 and `ADD` after each `ADVANCE` do. |
| 100 | `SCANCLASSAT` | 4 | 2 | `SCANCLASSCOUNT` of the count in the second argument for a walk of the scan site of the immediate that began at the integer first argument: after each `ADD`, `SCANREACHAT` of the site sets the count. The immediate is the class test, as for `MATCHCLASS`, and the site. |
| 101 | `MATCHCLASSNODE` | 5 | 0 | `MATCHCLASS` of the immediate into the first three destinations, continuing at the target if the character is not in the class; otherwise `POS` into the fourth destination, `ADVANCE` of the character's size, and `NEWTERMINAL` into the fifth of the fourth and of the `TEXT` from it. |
| 102 | `MATCHLITNODE` | 4 | 0 | `POS` into the first destination, then `MATCHLITJUMP` of the literal of the immediate into the second and third, continuing at the target if it did not match; otherwise `NEWTERMINAL` into the fourth of the first and of the `STR` of the immediate's string. |
| 103 | `INSERTING` | 1 | 0 | `flow.inserting`. |
| 104 | `INSERTMISSING` | 2 | 1 | `flow.insertMissing` of the `#missing` site and literal text of the immediate with the expectations of the argument: the syntax error into the first destination and whether it inserted into the second. |
| 105 | `NEWMISSINGNODE` | 1 | 2 | `flow.newMissingNode` at the integer first argument of the insertion whose error is the second, with the literal text of the immediate. |
| 106 | `MISSINGSCOPE` | 1 | 0 | `flow.missingScope` of the expectation of the immediate: the label entered before into the destination. |
| 107 | `MISSINGSCOPEEND` | 0 | 1 | `flow.missingScopeEnd` of the argument. |
| 108 | `MISSINGLEN` | 1 | 0 | `flow.missingLen`. |
| 109 | `RESETMISSING` | 0 | 1 | `flow.resetMissing` of the argument. |

A destination of -1 discards the value.

An instruction named after an operation of the [Flow dialect](../flow-ir/operations.md) does what the operation does
and counts the [compute units](../runtime/parsing.md#compute-units) that its contract names for a function lowered with
logical units, and no more; a superinstruction counts what the instructions it stands for count; the other
instructions count nothing. Its cost bound bounds its work. A Flow body therefore counts what its Flow function counts.

The code generated from the same Flow function counts what a Flow body counts: the units are the logical events of
[Compute Units](../runtime/parsing.md#compute-units), which are the same on every backend, so the total of a parse is
the same whichever of them runs a rule. They are the calls, the keys of memoized rules, the input that terminals
advance over, the values that actions build, the syntax errors built and the other events of that section, each where
it happens; peeking, guards, iterations, backtracking and the values that the parse itself builds, gathers, takes off
and drops count nothing. The Flow bodies keep only the guards whose skip makes no call: an alternative whose evaluation
calls rules before its first terminal is tried, whether or not the parse has limit controls.

## Error Messages

A syntax error (see [Syntax errors](../runtime/parsing.md#syntax-errors)) from a parse that records expectations is formatted as `line:column: syntax error: expected A, B`, with the expectations in the order and with the cut after ten that the syntax error contract defines. If `@error` messages apply, it is formatted as `line:column: message` (multiple messages joined with `; `). `@diagnostic` changes ordinary expected-item descriptions but does not replace explicit messages. A parse that does not record expectations formats it as `line:column: syntax error`.
Expectation strings are those the compiler placed in the string table: literals in quotes, character classes in `[...]` form, `any character`, and so on.

## File Format

A compiled grammar uses container version 9. It stores one execution-only compile result: the module of one mode and the start rule, and for a grammar with `#missing` the module of the same mode that the parses inserting missing tokens run, with no grammar AST and no output schema (see [Compiled Grammars](../runtime/compiled-grammars.md)). The container version and the module's instruction-set version cover separate layers: the container version covers the outer frame, the header and the order of the parts below, and the instruction-set version covers the encoding of the module's contents, action code included. A change to the module layout therefore bumps only the instruction-set version. A loader accepts only container version 9. Integers are variable-length: unsigned integers use LEB128 and signed integers use zigzag-encoded LEB128. A boolean is one byte (0 or 1).

| Part | Contents |
|:--|:--|
| Magic | `SXC\x00` (4 bytes) |
| Version | 1 byte: 9. A loader refuses any other value. |
| Mode | 1 byte: 0 parse, 1 recognize, 2 document. The module serves the operations of this mode only. A parse-mode or document-mode module is the module of the grammar; a recognize-mode module is the module of its recognizer. A loader refuses any other value. |
| Instruction-set version | Unsigned integer for this module: 41. Every module uses this version, whatever instructions it contains; a runtime rejects a module of another version and asks for the grammar to be compiled again. |
| String table | Count, then each string (byte length and UTF-8 bytes), all unsigned. The module's string table is a prefix of this table. |
| Start rule | String index, unsigned: the start rule the saved parser uses. It is a start rule of the module, or the empty string for the module's default start rule. |
| Module | See below |
| Insertion part | A boolean: whether an insertion module follows; then, if it does, its own string table, laid out as the string table above, of which the insertion module's string table is a prefix, and the insertion module, laid out as a module and referring only to that table. The insertion module is the module of the same mode of the grammar built with the rule flags of insertion and lowered with the insertions of its `#missing` sites; the parses with a positive limit of missing tokens run it. A grammar that uses `#missing` or the type `missing` has one; any other grammar has none. |
| Checksum | CRC-32 (IEEE) of everything from the magic up to this point, 4 bytes little-endian |

The module is laid out in the following order. A *list* is a count (unsigned) followed by its elements. Indices, positions and other integers inside the module are signed unless stated otherwise.

1. The number of module strings, unsigned (this many strings from the start of the string table form the module's string table).
2. Source information, a boolean: whether the values a parse returns carry the rule names and capture names of the grammar. A loaded program keeps it.
3. Default value, a boolean: whether the program was compiled with default values, which drop value actions and the declared types of rules (except for the rules that make values read by predicates) before compilation. A loaded program keeps it.
4. Character class table: a list of (negated, expectation string, number of ranges (unsigned), followed by the lower and upper bound of each range).
5. Type table: a list of (name, list of field names, list of the zero descriptors of the fields, underlying node type). A zero descriptor is a string index; it tells how a field that a constructor left unset reads (see `member` in [Action Code](#action-code)), and is written as in the type chapter of the language specification: `i`, `s` and `b` for 0, `""` and false, `n` for nil, `l` for the empty list, the descriptors of the elements in parentheses for a tuple, `{Name}` for the zero value of the struct type Name, and `-` for a type that has no zero value. The underlying node type is a string index, or -1 for a struct type.
6. Scope table: a list of lists of string indices.
7. Variable table: a list of (name, type, integer, string). The type is 0 for `int`, 1 for `string` and 2 for `bool`. The integer is the initial value of an `int` variable, and 0 or 1 for a `bool` variable. The string is the string table index of the initial value of a `string` variable, and -1 for the other types.
8. Rule table: a list of (name, scope, action, node type, memoize, leader, member, cycle, position-dependent, Operators Rule, value-free twin, transient, recursive, where, splice, missing, value shape, variables). The value shape is a boolean, whether there is one, followed by the shape as the immediate of `SCOPEVALUE` if there is.
9. Start rules: a list of the rule table indices of the start rules, the rules marked `@start`, in increasing order, followed by the rule table index of the default start rule. Parsing begins only at these rules.
10. Operators Rule table: a list of (skip body, list of operand lines, list of prefix operators, list of infix and postfix operators). A line is (scope, action, Flow body, shape), the Flow body as a body of the Flow bodies (item 13) without its rule index and the shape as the immediate of `SHAPESINGLE`; an operator is (index, kind, associativity, level, line). A skip body is a boolean, whether there is one, followed by the body if there is, as a line's Flow body.
11. Action table: a list of action expressions. An action expression is the sizes of its four register files (unsigned: values, integers, sequences, strings); its constants, a list of (kind as 1 byte: 0 nil, 1 int, 2 string, 3 bool; signed integer: the int, the string index, or 0 or 1); its names, a list of string indices; its member reads, a list of (member kind, name, field); its node types, a list of string indices; its casts, a list of (type text, line, column), the type text -1 for a cast that checks nothing; its struct literals, a list of (type name, number of fields); and its code, a list of instructions. Between the register sizes and the constants, a byte whose bits 0 to 5 mark the pools that are not empty (constants, names, member reads, node types, casts, struct literals) precedes them; only those pools are written, and a marked pool is not empty. An instruction is its opcode (1 byte), a byte whose bits 0 to 3 mark the operands D, `A`, `B` and `C` that are not zero, and those operands; a marked operand is not zero.
12. Run key table: a list of (run site, list of string indices of the key's variables, list of string indices of the variables it binds), by increasing run site. Each list is sorted by name.
13. Flow bodies ([Flow Bodies](#flow-bodies)): a list of (rule table index, number of registers (unsigned), list of instructions), by increasing rule index. An instruction is its operation (1 byte: its code), then its destination registers, its argument registers, its target and its immediate, each only if the operation has it and in the numbers the operation fixes. An immediate is, by operation: for `CONST` a kind (1 byte: 0 nil, 1 int, 3 bool, as for action expression constants) and a signed integer (0 for nil, the int, or 0 or 1); for `GUARD` (class index, depth); for `GUARDLIT` (character, case-folded (boolean), depth); for `EXPECT` and `MERGERENAMED` (expectation, message (boolean)), the expectation 0 to 4 one of the fixed expectations (any character, beginning of input, end of input, beginning of line, end of line) and 5 + i the string i of the string table; for `CALL` (rule table index, binding level); for `INCLASS` the class index; for `ATANCHOR` the anchor; for `MATCHLIT` (string index of the literal, for a case-insensitive literal its text with ASCII letters lowercased, expectation as for `EXPECT`, case-insensitive (boolean)); for `SILENT` (change of depth, site, matched (boolean)); for `NEWVALS` the count; for `LITERALTEXT` and `STR` the string index; for `SETCAPTURE` and `FRAMEVAL` the slot; for `NEWFRAME` and `ATTACHFRAME` the scope table index; for `RUNHOT`, `RUNWALKED`, `SCANREACH`, `SCANEND`, `SCANKIDS`, `SCANREACHAT` and `SCANENDAT` the site, numbered as the compiler numbers the run and scan sites of the grammar; for `RUNBEGIN` (site, values (boolean), keyed by the variables (boolean)); for `PRED` the action table index; for `BIND` (variable table index, action table index); for `MATCHCLASS`, `SCANCLASS` and `SCANCLASSCOUNT` the class index and then the expectation as for `EXPECT`; for `SCANCLASSAT` the immediate of `MATCHCLASS` and then the site, as for `SCANREACHAT`; for `MATCHCLASSNODE` the immediate of `MATCHCLASS`; for `MATCHLITNODE` the immediate of `MATCHLIT` and then the string index of the node's text, as for `STR`; for `INSERTMISSING` and `NEWMISSINGNODE` (`#missing` site, numbered from 1 as the compiler numbers the `#missing` sites of the grammar, string index of the text that its literal inserts); for `MISSINGSCOPE` the expectation as for `EXPECT`; for `GUARDJUMP` the guard as for `GUARD` and then the expectation; for `GUARDLITJUMP` the guard as for `GUARDLIT` and then the expectation; for `ASSERTJUMP` the anchor and then the expectation; for `MATCHLITJUMP` the immediate of `MATCHLIT`; for `CALLJUMP` the immediate of `CALL`; for `STREAMON` the rule table index; for `RESUMESTART` (repetition, numbered from 1 among those that a Document parse resumes, least number of elements, most number of elements or -1 for no bound (signed integers), shiftable (boolean)); for `SHAPESINGLE`, `SHAPECAPTURE` and `SCOPEVALUE` the shape (elements (boolean), shared (boolean), tuple (boolean), number of valued elements, single mode: 0 no value, 1 the element, 2 the tuple node).
14. Stream bodies ([Flow Bodies](#flow-bodies)): a list of (rule table index, Flow body), by increasing rule index, each written as an element of the Flow bodies is.

### Load-Time Validation

On load, a runtime checks the following and rejects the file if any check fails:

- The checksum, the version, the mode and the instruction-set version are valid, the start rule is empty or a start rule of the module, and no data remains after the insertion part.
- There is an insertion module when a rule of the module is marked missing. The insertion module passes every check below on its own, has a rule marked missing exactly when the module has one, and has the start rules and the default start rule of the module.
- Each variable is named once, has a valid type, and has an initial value that matches its type.
- Every reference in the tables and instructions (strings, character classes, scopes, rules, Operators Rule tables, action expressions) is in range.
- There is at least one start rule. The start rules are in increasing order and have distinct names, and the default start rule is one of them.
- Every rule has a Flow body or an Operators Rule, and every line has a Flow body. A rule with a value shape has no action, is not a splice, has no Operators Rule and is not a value-free twin.
- Every run site that a `RUNBEGIN` keys by the variables has an entry in the run key table. The entries are in increasing order of run site, every site is positive, every variable of an entry is declared, and the variables an entry binds are among its variables.
- The node type of a rule names a named node type, or a declared type with a field `Text`. The node type of a rule with an Operators Rule names a named node type.
- A named node type has no fields, and its underlying node type is a type text, parsed as the type of a cast is, of the form `node[T]`.
- Action code satisfies the checks of [Action Code](#action-code).
- The line and column of a cast are not negative. The type text of a cast parses as a field type naming only declared types, `error`, `missing`, `node[T]`, tuple types `(T, ...)`, `int`, `string`, `bool` and `nil`. These type texts are resolved types, which contain no `&` or `_|_`, and no `_` other than the argument of `node[_]`.
- Every Flow body, a stream body among them, belongs to a rule of the rule table, or to a skip or a line of the Operators Rule table, and ends with `JUMP` or a return. Each of its instructions has a known operation with the registers, target and immediate the operation fixes; every register is within the frame (a destination may be -1), every target is an instruction of the body, no reachable instruction may read a register before the body writes it, no instruction may restore a state saved before an `EMITSTREAM` that has run since ([Flow Bodies](#flow-bodies)), every cycle of the instructions passes through a `STEP` whose register no other instruction of the cycle writes, or reuses the elements of a run it does not start ([Flow Bodies](#flow-bodies)), and the classes, expectations and rules of the immediates are in range (the character of a guard of a literal is a Unicode scalar value, not an upper-case ASCII letter when case-folded), with depths and binding levels not negative; a literal is a string of the string table, a `#missing` site is positive, an anchor is 0 to 3, a lookahead enters (1) or leaves (-1) with a site not below -1, and only on leaving says whether it matched, a scope is an entry of the scope table, and a capture slot is not negative and below the number of names of the scope of its rule or line (none for a skip) or of a scope of a `NEWFRAME` of the body, a run site is from 1 to the number of instructions of the module's Flow bodies, and a scan site from 1 to three times that number plus two, a resumed repetition of `RESUMESTART` is numbered from 1 with a least count not negative and a most count of -1 or not below the least, and an action expression or a variable is an entry of the action expression or variable table.
- Every rule on a cycle of calls is marked recursive (`Rec`), so that every unbounded nesting of calls counts against the nesting limit. The cycles are those of a graph that over-approximates the calls a run can make: a rule leads to the rules that the `CALL` and `CALLJUMP` instructions of its Flow body, of its stream body and of the Flow bodies of the lines and skip of its Operators Rule call. A rule is on a cycle when the graph leads from the rule back to it. A rule may be marked without being on a cycle.

A runtime does not verify that registers hold what an instruction reads, and it trusts the mode as it trusts the module's types: it checks that the mode byte names a mode, not that the module is the one a compiler writes for that mode. A file whose mode does not match its module MAY return wrong results without an error, such as no value from a parse. Runtime errors caused by invalid code are reported as parse errors. A run of a Flow body ends ([Flow Bodies](#flow-bodies)), but the termination of action code is not guaranteed, so load files only from trusted sources.
