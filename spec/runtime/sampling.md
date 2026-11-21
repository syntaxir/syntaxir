# Input Generation

A grammar describes the inputs that it accepts, so it can also produce them. The `sample` package and the `sxir sample`
command generate inputs that a parser accepts, near-miss inputs that it rejects, and a report of which parts of the
grammar the accepted inputs exercised. They serve tests of code that consumes parse results and the seed corpora of
fuzz tests. This chapter defines what the generator guarantees, its options and its Go API.

> The Go API of this chapter is experimental and MAY change incompatibly. Which inputs a seed gives MAY change between
> versions.

## Scope and Terminology

A **generator** is a value of `sample.Generator`: it is made for one parser, from the grammar and the start rule of
that parser, and it keeps its state between calls. A **candidate** is a text that the generator writes by walking the
grammar from the start rule, deciding at each ordered choice, repetition, optional expression and
[Operators Rule](../language/operators-rule.md). An **accepted input** is a candidate that the parser parsed without
errors; a candidate that the parser accepts only by recovering from errors with `#recover` is not an accepted input. A **near-miss invalid input** is an accepted input, the
**base**, changed by one **mutation** so that the parser rejects it. **Coverage** is the part of the rules and
alternatives of the grammar that accepted inputs exercised.

Walking the grammar does not give an accepted input under the semantics of a [parsing expression
grammar](../language/parser-expressions.md): an ordered choice may never reach a later alternative, a greedy
repetition takes all that it can, and lookaheads, predicates and variables depend on the text around them. The
generator therefore steers away from the candidates that it can tell the parser would not accept, by evaluating
predicates on the text that it has written and by checking what the parser would match as the text grows, and it
parses every candidate. It does not evaluate what depends on the results of actions, which only parsing the candidate
decides.

## Contract

### Entry Points

| | Command line | Go (`syntaxir.dev/sxir/pkg/sample`) |
|:--|:--|:--|
| Make a generator | `sxir sample -g g.sxir` | `sample.New(p, opts...)` |
| Accepted inputs | `-n` | `sample.Generate(p, n, opts...)`, `Generator.Generate(n)`, `Generator.Next()` |
| Near-miss invalid inputs | `-invalid` | `Generator.GenerateInvalid(n)`, `Generator.Invalid()` |
| Prefer what is not exercised yet, and report it | `-coverage` | `sample.WithCoverage()`, `Generator.Coverage()` |
| Seed a fuzz test | | `sample.Seed(f, p, n, opts...)` |
| Counts of the work | `-f json`, member `stats` | `Generator.Stats()`, `Generator.Cuts()` |

### Generators

`sample.New(p, opts...)` returns a generator for the parser `p`, starting at the start rule of `p`. It returns an error,
and no generator, if

- `p` is not a parser that the compiler built from a grammar, such as one loaded from a compiled grammar, which holds no
  grammar (see [Compiled Grammars](compiled-grammars.md));
- the start rule of `p` is not defined; or
- the start rule can never match, so that no input exists.

A generator is not safe for concurrent use.

- `Generator.Next()` returns an accepted input. Inputs MAY repeat between calls. It returns an error that wraps
  `sample.ErrNoInput` if no candidate was accepted within the attempts of one call (see [Options](#options)); test for it
  with `errors.Is`. The message tells what became of the attempts: how many candidates the parser rejected, how many
  searches ran out of budget or had no way to finish, how many ways the checks and the predicates cut, and the rules
  with the most cuts. The wording is not part of the contract.
- `Generator.Generate(n)` returns up to `n` distinct accepted inputs. It returns fewer if it cannot find more: it gives
  up after as many inputs in a row that it has already returned as the attempts for one input. It returns an error that
  wraps `sample.ErrNoInput` only if it finds none, and no error and an empty list for `n` of 0 or less.
- `sample.Generate(p, n, opts...)` is `sample.New(p, opts...)` followed by `Generate(n)`.

Every input that the functions of this chapter return as accepted has been parsed by `p` without errors, and `Seed`
parses it once more.

### Determinism

The same parser, options and seed give the same inputs, in the same order, with the same version of the package,
whichever Go release built it: the package draws its random numbers with its own generator and not with
`math/rand`. The algorithm MAY change between versions, so a program that needs inputs that never change MUST keep
the inputs themselves.

### Options

A `sample.Option` configures a generator. Values that are out of range are raised to the minimum, and are not errors.

| Option | Default | Effect |
|:--|:--|:--|
| `WithSeed(seed)` | 0 | The seed of the random decisions |
| `WithMaxDepth(d)` | `DefaultMaxDepth`, 5 | A soft bound on recursion: once rule calls are nested `d` deep inside calls of the same rules, the generator prefers the shortest way to finish. Calls of rules that are not active do not count. The minimum is 0 |
| `WithMaxRepeat(n)` | `DefaultMaxRepeat`, 3 | How many iterations an unbounded repetition aims for beyond its minimum, at most, and a bound on the operators of an Operators Rule chain. The minimum is 0 |
| `WithMaxLen(n)` | `DefaultMaxLen`, 512 | A soft bound on the length of an input in bytes: once the input is that long, the generator prefers the alternatives with the shortest text to finish it. The minimum is 0 |
| `WithBudget(steps)` | `DefaultBudget`, 20000 | The number of steps that one attempt may take before it gives up. A step is a parsing expression that is generated or an Operators Rule scope that is inspected. The minimum is 1 |
| `WithAttempts(n)` | `DefaultAttempts`, 100 | The number of candidates that the generator tries for one input before it gives up. The minimum is 1 |
| `WithCoverage()` | off | Prefer the rules, the alternatives of ordered choices and the operands and operators of Operators Rules that no accepted input has exercised yet |

The bounds of depth, repetition and length are soft: when the text requires more, such as a predicate that counts the
nesting or a longer match, the search goes beyond them. The search has hard limits, which the options move: a call of a
rule never nests more than `d + 8` deep inside calls of the same rules, and a repetition never takes more than
`8 × n + 4` iterations beyond its minimum, where `d` and `n` are the values of `WithMaxDepth` and `WithMaxRepeat`. The
budget of steps also bounds the stack of the search, so an input that needs many steps needs a larger budget.

The budget of `WithBudget` is not a limit of parsing, and does not bound the analysis that `sample.New` makes of the
grammar.

### Near-Miss Invalid Inputs

`Generator.Invalid()` takes an accepted input as the base, applies one mutation to it, and returns the result if the
parser rejects it. It tries up to eight mutations of a base, and up to as many bases as the attempts for one input.
It returns an error that wraps `sample.ErrNoInput` if it finds no accepted input to mutate, and `sample.ErrNoInvalid`
if it finds bases but no mutation of them that the parser rejects, as for a grammar of `.*`, which accepts every text.

A `sample.Invalid` has these members.

| Member | Meaning |
|:--|:--|
| `Input` | The mutated text, which the parser rejects. It differs from `Base` |
| `Base` | The accepted input that it was made from |
| `Mutation` | A description of the mutation, such as `delete "]" at 12`, where the positions are byte offsets in `Base` |
| `Err` | The error that the parser returned for `Input`, with the expectations of the syntax error |

The mutations delete a character or a run of up to 8 characters, insert a token, replace a character by a token,
duplicate a run of up to 8 characters, swap two neighboring characters that differ, and truncate the input. A token is
a literal or a character of the grammar, or a character chosen from a fixed list of punctuation, white space and a few
other characters. A mutation cuts the text only at character boundaries, so a valid UTF-8 base gives a valid UTF-8
input.

`Generator.GenerateInvalid(n)` returns up to `n` near-miss inputs with distinct `Input`. It returns fewer if it cannot
find more, and the error of `Invalid` only if it finds none.

### Coverage

`Generator.Coverage()` returns a `sample.Coverage` for the accepted inputs so far. It is measured on the way the
generator derived each input, which is the way the parser takes unless the generator could not evaluate a lookahead, a
predicate or an earlier alternative of a choice; the counts are close to the parser's own coverage, and are not
guaranteed to equal it.

| Member | Meaning |
|:--|:--|
| `Rules`, `RulesCovered` | The number of rules that the start rule can reach and that can match, and the number of them that an accepted input exercised |
| `Alternatives`, `AlternativesCovered` | The same for the alternatives of ordered choices and the operands and operators of Operators Rules of those rules. It leaves out alternatives that can never match, that occur only inside an expression that can never match, or that are in the tail of an Operators Rule that no reachable entry or prefix operator permits |
| `MissedRules` | The rules that were not exercised: each a `sample.MissedRule` with the `Rule` and the `Why` |
| `MissedAlternatives` | The alternatives that were not exercised: each a `sample.MissedAlternative` with the `Alternative` and the `Why`. A `sample.Alternative` has the `Rule`, the `Kind` (`choice`, `operand` or `operator`), the `Index` of the alternative in its choice or of the operand or operator in its Operators Rule, counting the operators of all levels, and the `Expr` in grammar syntax. `Alternative.String()` is `rule: kind index: expr` |
| `Unreachable` | The rules that generation cannot exercise from the start rule: rules that are not called, or that are called only inside negative lookaheads, the skip of `#recover`, expressions that can never match or blocked operator parts of an Operators Rule |
| `Impossible` | The rules that the start rule calls outside negative lookaheads and `#recover` but that can never match, such as a rule that ends in `_|_` to report an error |

`Unreachable` and `Impossible` are not counted in `Rules`.

A `sample.Why` tells what the generator knows about why no accepted input exercised a target. An attempt **tried** a
target if its search generated the target at some point; the counts are of the attempts that tried the target and gave
no accepted input that exercises it.

| Member | Meaning |
|:--|:--|
| `Bypassed` | The attempts that found an accepted input without the target: the search left every way through it that it tried |
| `Rejected` | The candidates that the parser rejected |
| `Failed` | The searches that ended without a candidate |
| `Exhausted` | The failed searches that used up their budget |
| `Cuts` | The rules whose checks and predicates cut the most ways in those attempts, at most three, each a `sample.RuleCuts` (see [Statistics](#statistics)), in the order of `Generator.Cuts()` |
| `MinLen` | The length in bytes of the shortest input that exercises the target as far as the structure of the grammar tells, leaving out lookaheads, predicates and the order of choices: a lower bound |

No attempt means that no search took the target: the generator preferred other options, or the bounds kept it away,
as when `MinLen` is beyond `WithMaxLen`. Attempts with many cuts name the checks and predicates that stopped them, and
rejected candidates point at what the generator cannot evaluate, such as an earlier alternative of a choice that
matches the same text; attempts that only bypassed it, with cuts, point at the checks that ruled out the ways through
it. The counts are kept with and without `WithCoverage`.

The members have the JSON names `rules`, `rulesCovered`, `alternatives`, `alternativesCovered`, `missedRules`,
`missedAlternatives`, `unreachable` and `impossible`; the members of a missed rule `rule`, those of a missed
alternative `rule`, `kind`, `index` and `expr`, and in both the members of `Why`: `bypassed`, `rejected`, `failed`,
`exhausted`, `cuts` and `minLen`. An empty list is omitted.

`Coverage.String()` is `rules a/b (p%), alternatives a/b (p%)`, where a percentage is rounded to a whole number and is
100% when the total is 0. `Coverage.Report()` is that line followed by a line for each missed rule (`missed rule:
name`), for each missed alternative (`missed ` followed by `rule: kind index: expr`), and for the unreachable rules
(`unreachable: names`) and the impossible rules (`never match: names`) when there are any. A missed line ends with
`Why.String()` in parentheses: `not tried`, or `tried in n attempts:` followed by the counts that are not zero among
`b accepted without it`, `r rejected by the parser`, `e out of budget` and `w without a way to finish` (the failed
searches within their budget), and by `; most cuts in rule (c by checks, p by predicates)` for the rules of `Cuts`;
then `; shortest input n bytes`. An inline action is shown as written in the grammar and not as the anonymous rule
that implements it.

### Statistics

`Generator.Stats()` returns the counts of the work done so far.

| Member | Counts |
|:--|:--|
| `Candidates` | Candidates that the generator passed to the parser |
| `Rejected` | Candidates that the parser rejected |
| `Failed` | Searches that ended without a candidate |
| `Exhausted` | The failed searches that used up their budget; the others tried every way to finish that they knew |
| `Duplicates` | Inputs that `Generate` and `GenerateInvalid` found again and skipped |
| `CheckCuts` | Ways that the search abandoned because a lookahead, the order of a choice or `$$` showed that the parser would not match them |
| `PredicateCuts` | Ways that the search abandoned because a predicate would certainly fail |

`Generator.Cuts()` splits the cuts by rule: a `sample.RuleCuts` has the `Rule`, the number of `Checks` and the number of
`Predicates` (JSON names `rule`, `checks` and `predicates`), for each rule that has any, the rules with the most cuts
first and rules with the same number in the order of their names. A check belongs to the rule whose body holds the lookahead, choice, optional expression,
repetition or `$$` that installed it, even when the text that decides it comes from another rule.

### Seeding Fuzz Tests

`sample.Seed(f, p, n, opts...)` adds up to `n` distinct accepted inputs of `p` to the seed corpus of the fuzz test `f`,
each as a single string argument, and returns them; the fuzz target therefore takes one string. `f` is any value with
the methods `Helper`, `Add` and `Fatalf` of `*testing.F`, as `sample.F` says. `Seed` fails the test through `Fatalf` if
it cannot generate any input, or if `p` does not accept a generated input when it parses it again.

### The Command

`sxir sample -g g.sxir` prints distinct accepted inputs: `-s` selects the start rule, `-n` the number (10; a negative number is
an error), `-seed`, `-max-depth`, `-max-repeat`, `-max-len` and `-budget` the options with the same names, `-coverage`
the coverage and `-invalid` near-miss inputs instead. The grammar is SyntaxIR source or grammar JSON; a compiled
grammar is an error, as is an unknown value of `-f`.

- With `-f lines`, the default, each input is printed as a Go-quoted string on a line, so that an input with line breaks
  stays on one line, and `strconv.Unquote` reads it back. With `-coverage`, the report of `Coverage.Report()` goes to
  the standard error.
- With `-f json`, the command prints one document with the members `start`, `seed`, `inputs` (or `invalid`, a list of
  objects with `input`, `base`, `mutation` and `error`, with `-invalid`), `coverage` (with `-coverage`, the JSON form
  of `Coverage`) and `stats`, which has the members of `Stats` in camel case and the list of `Generator.Cuts()` as the
  member ruleCuts, with the members `rule`, `checks` and `predicates`. Empty lists are omitted.
- If the generator finds fewer inputs than `-n` but at least one, the command prints them, says
  `sxir sample: found only N distinct inputs` on the standard error and succeeds. If it finds none it fails with the
  error of the generator; with `-coverage` the report, and with `-coverage` and `-f json` the document, are still printed.

## Validation and Errors

| Condition | Result |
|:--|:--|
| `sample.New` for a parser without the grammar, with an undefined start rule, or whose start rule can never match | An error |
| No accepted input within the attempts | An error that matches `sample.ErrNoInput` |
| Accepted inputs but no mutation that the parser rejects | `sample.ErrNoInvalid` |
| `Seed` that cannot generate an input | `Fatalf` of the test |
| An option out of range | The nearest value that is in range |

## Compatibility and Support

The generator needs the grammar, so it works for a parser built from grammar source or grammar JSON and not for one
loaded from a compiled grammar. It is part of the Go API and the `sxir` command and not of generated parsers.

## Conformance

The tests of the `sample` package establish that every generated input parses, that the same seed gives the same inputs,
the options and their limits, the coverage counts on grammars with known coverage, the explanations of missed targets,
and the predicates and the Operators Rules that the generator has to evaluate. The tests of the command establish its output.
