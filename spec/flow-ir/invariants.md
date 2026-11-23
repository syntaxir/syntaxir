# Invariants

A function at the Flow stage is well formed when it satisfies the rules of [Code IR](../code-ir/invariants.md) and those of
this chapter, from lowering on. The Flow verifier first applies the verification of Code IR to the module, which requires that it hold only operations of
the Flow and Action dialects, and then checks each function against the rules of this chapter; it reports the first
violation it finds in a function, naming the function. Every function that lowering from Core IR produces is verified.

## Structure

- Every block and loop has a label of its own.
- An Exit names an enclosing block, the innermost enclosing loop or the function's failure label. A Continue names the
  innermost enclosing loop. A loop is left or continued only from its own body, outside nested loops, as a break or a
  continue without a label is.
- A loop without a bound, neither a Counter nor a While, can be left: its body exits to it.

## Variables

- Every variable that a statement or an expression reads or assigns is an entry variable of the function or one of
  its variables, and each is listed once, not both as a variable and as an entry variable.
- A local function has its entry variables, start, result and context; a framed function has none. Only a framed
  function has a value or a cut flag, and its cut flag is one of its variables.
- A Set has as many places as values, or one value with that many results. A Peek has a variable for whether there is
  a character.

## Runtimes

- A function builds the values of one runtime. A Node function builds no match, tuple or list node, error value, fresh
  node or result conversion, uses no action context, and attaches no captures from the values of variables; a typed
  function builds no terminal, branch or error node of the node runtime.
- StreamStart and EmitStream appear only in a framed Node function.
- DropTrail and FreeFrame appear only in a typed function.
- The site of a SkipBegin is positive.

## Tables

Every immediate that names an expectation or a class names an entry of the module's [tables](functions.md#tables), and
the expectation of each class of the tables is an entry of them.

## Documents

In a function lowered for a build with Documents, a read of the input in place, HasLit, is the whole condition of an
If, and the If's alternative is the recording primitive, MatchLit, which the target takes when the parse is a
Document's. Otherwise a Document parse would read input without recording how far it examined it.

The operations of the runs that a Document parse resumes, ResumeOn, ResumeStart, ResumeMore, ResumeNext, ResumeBegin,
ResumeEnd and ResumeFinish, appear only in a Node function lowered for a build with Documents, and the site of a
ResumeStart is positive.
