# Passes

A pass transforms a well-formed function into a function that does the same: for every state of a parse, running the
result consumes the same input, records the same expectations, builds the same values, calls the same rules, counts
the same compute units and ends the same way as running the original. A pass relies only on the meaning of the statements and
expressions of this specification and on the way the function was lowered (local or framed, with or without logical
units or a Document), never on how a target prints it.

## Passes in the Pipeline

The compiler is one pipeline of passes. Every function that [lowering from Core IR](lowering.md) produces, and that a backend prints
from the Flow stage, goes through the passes of that pipeline that run at the Flow stage, in their declared order, before the
function is converted to the Exec stage or printed.

- A pass has a name that no pass of Core IR has, and that no pass of the Exec stage has unless it is one pass of Code IR
  that runs at both stages, such as `canonicalize` (below). The name of an optional pass is also the name of the
  optimization that turns it off; the name of every pass is that of the dump that shows the function after it.
- A pass that is off does not run, and neither does one that does not apply to the build, whatever its optimization
  says. A build that reports rule events applies only the passes that keep them.
- Where the pipeline leaves the stage the function MUST satisfy the [invariants](invariants.md). Verification runs right
  after the lowering and again where the pipeline leaves the stage, and also after every pass when asked, and a
  violation names the pass after which it was found. Lowering has verified the function, so a run in which no pass runs
  does not verify it again.
- A dump of the function in the [text form](text-form.md) can be asked for as the lowering made it and after each
  pass that ran.

## Forms

The lowering writes each construct in one form. The first passes of the stage replace the form of a construct with
another that consumes the same input, records the same expectations and counts the same compute units, where the target
prints the other form and the function allows it: a terminal region matched as one program, a class that consumes the
character that the guard of its alternative read, consecutive classes matched as one, a class matched through a
membership table, and a literal compared in place before it is matched. A form that skips recording what a
read examines applies only to a function lowered without a Document or the records of a traced parse.

## Canonicalization

The last pass of the stage, `canonicalize`, applies named local patterns to a function until none applies, trying them
in a fixed order, so that the result is the same on every run. It is the pass of the same name at the Exec stage (see
[Canonicalization](../exec-ir/passes.md#canonicalization) there), with the patterns that keep the invariants of this
stage: every block keeps its label, so no label is removed and no block is spliced. The patterns of the stage, in the
order they are tried:

- `fold-constants`: an operator or a conversion whose operands are constants becomes its value, computed as running it
  computes it, except a division or a remainder by zero; `!` of a constant becomes its value, and `&&` and `||` with a
  constant operand that decides them become that constant, or their other operand when the constant does not decide
  them; an operand that is dropped never holds a call;
- `fold-branches`: an `If` whose condition is a constant becomes its Init and the statements of the branch that the
  condition selects, a `Loop` whose condition is the constant false becomes its Init, and the constant true condition
  of a `Loop` is removed;
- `drop-after-exit`: the statements after an `Exit`, `Break`, `Continue` or `Return` in the same block are removed.

Each pattern is an optimization with its own name, which turns it off, and each application of a pattern is counted on
its own when passes are bisected.
