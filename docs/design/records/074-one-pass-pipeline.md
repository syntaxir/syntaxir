# 074. One Pass Pipeline per Compilation

- **Status**: Accepted
- **Author**: @ornew
- **Date**: 2026-11-20

> **Note (2026-11)**: The migration order is amended; the end state is unchanged. The VM switch to the Flow route,
> which deletes the Core route, comes before step 12. Until step 12, `FlowTable` was the VM's Flow route in production;
> step 12 assembles the VM pipeline in `Compile` and deletes it (see the amendment of step 12).

## Summary

A compilation is one pipeline of passes, and a pass is one kind of value whatever it does: a transform, a lowering, the
planning of a program and the printing of source are all a sealed `Pass[I, O]` of `internal/pass`, which takes a unit
of one kind and returns a unit of the same or another kind. The driver is the only package that assembles and runs
pipelines. It builds the pipeline of each backend from typed combinators that the Go compiler checks, down to the stage
of a Code IR module, and one manager runs it: every step, required or optional, is numbered, observed, dumped, verified,
timed and counted the same way. The manager also owns the copies: a pass may change its unit in place, and the manager
copies a unit once per segment of transforms when its start must survive, and once per pass only for an observer that
needs the state before each pass. Several backends, or several tables of one backend, share the steps whose input,
configuration and body are the same, without changing what an observer sees. Several backends can be built in one
compilation from the command line (`sxir gen -lang go,ts`) and from the API (`compiler.Generate`). Printers take only
units of the Exec stage, and the stage-less compositions module of the libraries, and are named after the primitives
they print; the run-time backends only consume the results, and the optimization settings reach every backend, the VM
and the closures included. A pipeline prints as text; reading a text back is internal (tests and reproduction): the
reader takes the build's options from the text's headers, assembles the reference pipeline for them, and checks the text
against it by kinds and by its required skeleton before anything runs.

This record makes the model of [the pass pipeline](../pass-pipeline.md) the implemented structure, decides the
interfaces that [record 063](063-configurable-optimization-pass-pipeline.md) left to the code, and supersedes the
non-goal of 063 that excluded a textual pipeline language. The interfaces below were prototyped and tested as a
standalone module before the decision; the Limitations section lists what the prototype does not cover.

## Motivation

The pass infrastructure has two models of a step and leaves the order of a compilation to its callers.

- **Two models of a step.** `internal/pass` has one pipeline type for transforms and a separate stage type, with its
  own combinators, for lowerings and composition. A lowering gets no event before it runs, and the event after it
  carries no change bit, counters or time. There are two analysis mechanisms: named recomputations on the pipeline and
  an analysis manager with typed keys.
- **Callers sequence the stages.** The driver exports its Core, Flow and Exec stages. The Go generator runs the Core
  stage, then plans the table and lowers it through the shared code generation package, which runs the Flow and Exec
  stages. The TypeScript printer runs the Flow stage for one function at a time while it prints, and the Exec stage
  from its Exec printer. Any package can call a pass function or a lowering directly.
- **Plans read printed output.** The Go generator prints the whole parser once to learn which rules are reachable, how
  a call of each rule is printed, the expectation table and the scopes of pruned rows, and takes the frame layouts of the
  typed table from the scopes of the printed Node table.
- **The run-time backends run passes outside the manager and ignore the settings.** The VM compiles Core IR to
  bytecode and runs the Core passes with a zero environment, which turns every pass on. The closure backend does the
  same lazily, when a parse first needs a family, under the parse's context. The Flow route of the VM exists only in
  tests. Nothing carries the settings there: `compiler.WithOptimize` is checked and dropped, since the options of the
  compile entry have no field for them.
- **Printers read the Flow stage and lower.** The Go and TypeScript printers print functions at the Exec stage, but key
  their switches on the names of Flow operations, read the Flow dialect's function attributes, types and immediates, and
  read the Core module, origin map and lowering options through the Flow state of the generator; the TypeScript printer
  runs the Flow and Exec passes itself. Their types, files and comments are named after the Flow stage, although
  [record 073](073-code-ir-dialects.md) turned every Flow operation into a primitive of the Exec stage.
- **The stage boundary of Code IR is checked at run time only.** Flow and Exec passes take the same module type, so a
  composition that leaves out `execlower` type-checks, and the stages are compared when the pipeline runs.
- **No run-time identity of a pipeline.** Composition is generic code only, so a pipeline cannot be printed completely,
  read back, or changed for a test except through per-stage hooks of the driver.

## Goals

1. One step type, sealed: a pass is created only by the pass constructor and run only by the manager.
2. Static assembly is typed: a pipeline written in Go is a `Pipeline[I, O]` built with typed combinators, and a
   mismatch between steps, including two stages of one Code IR module, is a Go compile error.
3. Text assembly may be type-erased: a pipeline text is resolved through registries and checked by kinds, by the
   required steps and by the declared features before any step runs, into the same step tree that the typed door
   builds; one executor runs both.
4. The driver assembles one pipeline per backend under the compilation's settings and exports the compilation, not its
   stages. Every backend follows the optimization settings: the run-time backends get them through the compile entry,
   with the program they build.
5. Every step emits the same events with the same data. Dumps, statistics, timing, bisection and test hooks are
   observers. Verification and the copies it needs are a policy of the manager.
6. Sharing of common steps between backends and between tables is a capability of the manager that no observer can
   detect except through a marked replay.
7. A new IR, stage, pass, route or backend is added by declaring it and composing it; the manager needs no change.
8. A run is deterministic and cancellable, its parameters are declared, and the text format is versioned.
9. A printer takes units of the Exec stage only, lowers nothing, and is named after the Exec primitives it prints; what
   it needs from earlier stages reaches it through the Exec module and the Exec-stage units. The one other input is the
   libraries' compositions module (the kind `library`), which is at no stage and holds no Flow operation.

## Non-Goals

- A public API for passes written outside the module. Passes and the text form stay internal.
- Text parsers for Core IR and Code IR, a failure reproducer, optimization remarks, parallel per-function execution and a
  compile cache: follow-ups that this design must not block.
- Changing generated output. Every migration step keeps `sxir gen` output identical, and the dumps identical except for
  the changes of dump names and headers listed under Migration.
- The switch of the VM to bytecode selected from the Flow stage. This record describes the VM pipeline before and after
  that switch; the switch itself is separate work.

## Design

### Units and Kinds

A kind describes one unit type to the manager: how to verify, print and copy it. A kind is identified by its Go type,
so one Go type is one kind, and the name is its spelling in the text form.

```go
type IR[M any] struct {
	Name   string                 // the spelling of the kind in the text form
	Verify func(m M) error        // the invariants of the kind
	Print  func(w io.Writer, m M) // the dump; nil: a unit of the kind is never dumped
	Copy   func(m M) M            // a deep copy; nil for a value kind
}

// RegisterIR makes a kind known to the manager and the text form. A name or a Go type registered
// twice panics. It returns ir, for package-level declarations.
func RegisterIR[M any](ir *IR[M]) *IR[M]
```

Code IR is one IR whose modules are at a stage ([record 073](073-code-ir-dialects.md)); the stage is part of the Go
type of a unit, so two stages are two kinds and the compiler rejects a composition that skips the conversion between
them. The stage markers and the kinds of a module at each stage are declared together in the `code` package:

```go
// Stage is a stage of a Code IR module; its values are marker types.
type Stage interface{ stage() }

// At is a module at the stage S. Only EnterFlow and EnterExec make one at a stage; a pass keeps
// the stage with With.
type At[S Stage] struct{ m *Module }

func (a At[S]) Module() *Module
func (a At[S]) With(m *Module) At[S]

// FlowStage and ExecStage are the unit kinds of a module at the Flow and the Exec stage.
var (
	FlowStage = stageIR[Flow]("code@flow", ...)
	ExecStage = stageIR[Exec]("code@exec", ...)
)

func EnterFlow(m *Module) At[Flow] // only flowlower calls it
func EnterExec(m *Module) At[Exec] // only execlower calls it
```

`With` panics unless the module already names the stage `S`, and the kind's verifier checks the same. The run-time
stage of a module ([record 073](073-code-ir-dialects.md)) stays, since dumps and the verifier read it, but only
`EnterFlow` and `EnterExec` set it, so a pass cannot move a module between stages through `With`. Leaving out
`execlower` is a compile error that names both stages:

```text
in call to pass.Then, type pass.Pipeline[code.At[code.Exec], code.At[code.Exec]] of
pass.PerFunc(code.ExecFuncs, toy.ExecPasses) does not match inferred type
pass.Pipeline[code.At[code.Flow], C] for pass.Pipeline[B, C]
```

The kinds of a compilation:

| Kind | Unit | Made by |
| --- | --- | --- |
| `program` | the checked program, the common options and the options of every target | the driver |
| `table` | one table of a program and its configuration | a backend's split of the program |
| `core` | a Core IR module, with the origin map of its elaboration | `elaborate` |
| `cores` | the program and the Core module of each of its tables | the join of the table split |
| `plan` | the plan of the program: reachable rules, call routes, layouts | `plan` |
| `planned` | one table's plan, its Core module and origin map | the split of a plan |
| `table@flow`, `table@exec` | the planned table and its module at the stage (`Table[S]`) | `flowlower`, `execlower` |
| `code@flow`, `code@exec` | a module at the stage (`At[S]`) | the module of a table unit, and each function of it |
| `lowered@exec` | one function in its module at the Exec stage | a view of `code@exec` |
| `tables@exec` | the plan with every table of a generating backend at the Exec stage, and the expectation table numbered from the Node and recognizer tables | the join of the planned tables |
| `library` | a compositions module of the libraries (at no stage), the specializations requested of it, and one specialized function per request | the driver, for the Go library; a lens of `tables@exec`, for the TypeScript library |
| `vm-input`, `closure-input` | the program and the Core module of the one table that the backend builds | the join of the backend's table split |
| `output` | the result of one backend: files, bytecode or the closures' Core module | the last required step of every backend |

Names are unique across kinds, splits, lenses, views, keys, providers and slots, and a name that ends in a stage
(`@flow`, `@exec`) names that stage. Stage suffixes are only for Code IR modules at a stage: `library` has none, because
the compositions module is at no stage and its operations have no dialect or the Action dialect. Splits are named after
the parts they give (`go-tables`, `ts-tables`, `vm-tables`, `closure-tables`, `planned-tables`) and are not kinds.

The table unit `Table[S]`, in a data package of the planning code (`internal/middleend/flowplan/plandata`, beside
`internal/middleend/flowlower/lowerdata` for the data that the lowering takes), is the one value that carries the plan,
the table's Core module, the origin map and the module to the printer, so no printer needs a lowering, the plan or the
driver to get them. The origin map is recorded by `elaborate` in the Core module, keyed by the stable IDs of Core nodes,
so it travels with every Core unit, a shared one included, and survives copies. Kind names are not dump names: a dump is
named after the pass instance that produced it, and the kinds without a printer (`program`, `table`, `cores`,
`planned`, `tables@exec`, the backend inputs and `output`) add no dumps. `plan` prints the entry points, and the route
and the reach of each rule of each table; it is dumped only when `-print-after` names it, not under `all`, so the dumps
of the IR forms stay as they are. `cores` has no printer because its Core IR is dumped as it is built and changed, and
the plan shows its entry points and routes.

Every backend ends in the one kind `output`, so the branches of a `Fork` have one result type whatever the backends
build. `Output` is an interface implemented by the result of each backend; a caller that needs the concrete result
asserts it once. The result types live in a data package of the driver that the backend packages import and that
imports no pass code.

### Passes

A pass is declared once, sealed and registered:

```go
// Decl is what a pass declares besides its types and its function.
type Decl struct {
	Name       string      // unique among registered passes; the flag, dump and error name
	Required   bool        // runs whenever reached; has no flag
	Test       bool        // an optional pass without a flag, always enabled (tests)
	After      []string    // passes that must precede it in the pipeline
	Obligation string      // what the pass preserves, in one sentence
	Features   []FeatureID // the features it reads (Feature panics on any other)
	Params     []string    // the registry flags it reads (Ctx.Value panics on any other)
	Patterns   []string    // the sub-pass decisions it asks Ctx.Allow about
	Counters   []string    // the statistics it counts (Ctx.Count panics on any other), in report order
}

// Def is the typed definition of a pass, given once to New. Its Run stays in the declaring package.
type Def[I, O any] struct {
	Decl
	In      *IR[I]
	Out     *IR[O]
	Applies func(c *Ctx[I]) bool
	Run     func(c *Ctx[I], in I) (O, Preserved, error)
	// Post, if set, is the postcondition this conversion establishes, checked at its own output in
	// place of the verifier of its output kind. Only a conversion (In other than Out) has one.
	Post func(out O) error
}

// Pass is a sealed step from units of I to units of O. Only the manager runs it.
type Pass[I, O any] struct{ d *passDesc }

// New seals and registers a pass. A name registered twice panics.
func New[I, O any](d Def[I, O]) *Pass[I, O]
```

**The pass contract.** A pass returns its result unit and the analyses it preserved. A transform may change its input
in place and return it, or return a new unit: the input belongs to the segment that runs it, and the manager decides
when a copy is needed (Copies, below). A lowering returns a new unit of another kind and does not change its input. A
kind whose units are pointers declares a copy; a value kind needs none.

**Parameters.** The registry flag is the one source of a parameter's default and documentation; a pass lists the flags
it reads. A read returns the instance's override from the pipeline text if there is one, else the setting. Library
controls stay registry entries that no pass declares.

**The context** of a run is typed by the input kind:

```go
type Ctx[M any] struct{ s *ctxState }

func (c *Ctx[M]) Label() Label                         // backend, table and function of the unit
func (c *Ctx[M]) Value(flag string) int                // a declared parameter
func (c *Ctx[M]) Count(counter string, n int)          // a declared statistic of this pass on this unit
func (c *Ctx[M]) Context() context.Context             // for cancellation polling
func (c *Ctx[M]) Allow(pattern string) bool            // a sub-pass decision (canonicalize)
func Feature[T, M any](c *Ctx[M], k *FeatureKey[T]) T  // a declared feature that an enclosing scope provides
func Query[M, R any](c *Ctx[M], k AnalysisKey[M, R]) (R, error) // an analysis of the current unit
```

The context replaces the environments of the Core, Flow and Code IR passes: the Flow target, observation and the
configuration of a table become features, and the flag lookups become declared parameters. Every analysis is a typed
analysis key; the string-named analyses of the pipeline go, and the standalone query that tools use outside any pipeline
stays. The attributes of Core nodes (nullability, first characters and effects) are not an analysis but content of the
unit: `elaborate` outputs a Core module with its attributes computed, the Core passes read them from the nodes, a pass
that changes nodes keeps them current, and the verifier of the `core` kind rejects a module whose attributes are stale.

**Patterns.** A pass with sub-pass decisions, such as `canonicalize`, declares its patterns. Each pattern is a registry
flag (`-opt`, `-no-opt`), and each application asks `Allow`, which the manager numbers as a run of its own and the
bisection observer answers. `-print-pipeline` lists the patterns under the pass, and `-stats` counts each.

### Pipelines: the Typed Door

A pipeline is an immutable step tree. The typed combinators and the text reader build the same tree, and one executor
runs it.

```go
type Pipeline[I, O any] struct{ n node }

func Of[I, O any](p *Pass[I, O]) Pipeline[I, O]                             // one pass
func Seq[M any](ir *IR[M], ps ...*Pass[M, M]) Pipeline[M, M]                 // transforms; may be empty
func Then[A, B, C any](a Pipeline[A, B], b Pipeline[B, C]) Pipeline[A, C]
func Then3[A, B, C, D any](a Pipeline[A, B], b Pipeline[B, C], c Pipeline[C, D]) Pipeline[A, D]

// PerFunc runs the passes of p on each function of a unit, pass-major: the first pass on every
// function in order, then the second. p must be a sequence of passes and slots.
func PerFunc[W, M any](f *Funcs[W, M], p Pipeline[M, M]) Pipeline[W, W]

// Each runs p on each part of a unit, in order, and joins the results.
func Each[W, U, V, R any](s *Split[W, U, V, R], p Pipeline[U, V]) Pipeline[W, R]

// Within runs p on the part of a unit that l selects; the part may change kind (P to Q),
// which changes the whole (W to V).
func Within[W, V, P, Q any](l *Lens[W, V, P, Q], p Pipeline[P, Q]) Pipeline[W, V]

func Shared[I, O any](k *Key[I], p Pipeline[I, O]) Pipeline[I, O]
func Fork[I, O any](bs ...Branch[I, O]) Pipeline[I, []O]
func B[I, O any](name string, p Pipeline[I, O]) Branch[I, O]
func WithConst[I, O, T any](k *FeatureKey[T], v T, p Pipeline[I, O]) Pipeline[I, O]
func WithDerived[I, O, T any](d *Derive[I, T], p Pipeline[I, O]) Pipeline[I, O]

func Slot[M any](k *SlotKey[M], p Pipeline[M, M]) Pipeline[M, M] // a typed extension point
func Fill[M any](k *SlotKey[M], part string, p Pipeline[M, M]) Edit
func SetConst[T any](k *FeatureKey[T], v T) Edit       // the value of every constant provider of k
func InBranch(branch string, e Edit) Edit               // e in the branches of a Fork named branch only
func Apply[I, O any](p Pipeline[I, O], edits ...Edit) (Pipeline[I, O], error)
```

The arguments that are functions are registered values with a name, so that the text form can name them: the view of a
unit as functions (`Funcs[W, M]`, where `M` may differ from `W`, so that the target passes run on one lowered function
at a time), splits, lenses, keys, derived providers, feature keys and slot keys. Each is registered with the kinds it
reads, so the text form can be checked by kinds. A split declares the labels of the parts it may give; a part with
another label fails the split, and a slot body for a part must name one of them. A split also declares the features it
reads (`SplitFeature`), as the enclosing blocks provide them: the check rejects a split whose features no enclosing
block provides, and a `Shared` block keys its result by the features of the splits inside it as by those of its
passes. So the program unit holds no target, and the split of a backend reads the target of its branch. (Amended at
step 9: the program unit first held the one target of the compilation.) A feature key has a format and a
parser, which the text form and the sharing keys use. `Within` routes the module of a table unit through the Flow or
Exec passes; it only routes units, so it is not a second step model. `Fork` returns a list kind derived from its
branches' kind, whose verifier verifies each element. `Seq` takes the kind explicitly so that an empty sequence is a
valid slot body.

### The Manager

```go
// Run is the manager of one compilation.
type Run struct {
	Settings   Settings
	Observers  []Observer
	VerifyEach bool              // a manager policy: verify after every pass
	NoShare    bool              // run every use of a Shared pipeline (for tests that compare)
	Recover    func(x any) error // turns a cancellation panic of a step into an error; nil re-panics
	Keep       bool              // the caller keeps the input of Execute: the run changes a copy
	Stop       bool              // the first failure breaks the run: no rollback, so no rollback copy
}

// Execute checks p and runs it on in. An invalid pipeline fails before any step runs.
func Execute[I, O any](ctx context.Context, r *Run, p Pipeline[I, O], in I) (O, error)

// Needs says what an observer reads, so the manager computes only that.
type Needs struct {
	Before      bool // the dump of the input (Before's in)
	TextChanged bool // Result.TextChanged: the text form before and after the pass compared
	Units       bool // Result.Unit (typed test hooks); disables sharing
	Veto        bool // Before may return false (bisection); disables sharing
	Print       bool // Result.Print (dumps); without it no step prints its unit
}

type Event struct {
	Seq      int // the number of a decided run of an optional pass, from 1; 0 for a required step
	Step     StepInfo
	Label    Label
	Replayed bool // the event of a Shared result that was reused
}

type StepInfo struct {
	Name, Instance string // instance: the name, with #k for the k-th occurrence (k > 1)
	Required       bool
	Pattern        bool // the decision of one application of a pattern (Ctx.Allow)
	In, Out        string
}

type Result struct {
	Ran         bool // false: an observer vetoed the run
	Changed     bool // the pass's report: !PreserveAll; the same whatever observers watch
	TextChanged bool // only if an observer needs it: the text form changed
	Counters    map[string]int
	Patterns    map[string]Decisions // the applied and skipped applications of each pattern
	Elapsed     time.Duration
	Unit        any             // only if an observer needs units
	Print       func(io.Writer) // only if an observer needs dumps; valid during After only
	Err         error
}

type Observer interface {
	Needs() Needs
	Before(e Event, in func(io.Writer)) bool
	After(e Event, r *Result)
}
```

`Execute` first instantiates the tree, then checks it: every feature a pass declares is provided by an enclosing
`With*`, every `After` holds, a `PerFunc` body is a sequence of transforms and slots, a sharing key and a derived
provider read the kind their block starts on, and every body of a slot for a part names a label of the enclosing
`Each`'s split and has the required skeleton of the slot's default body. Only then does a step run. The checks hold for
both doors: the typed door cannot get the kinds wrong, but its edits can fill a part. The needs of all observers are
joined once per run, each field on its own. Without observers the manager builds no result; the dump of a step is
produced exactly when an observer needs dumps and the step's output kind prints, and the text forms of units are
computed only for dumps, the input dump, the text comparison and `-verify-each`. A dump is valid only during the call
that receives it, since a later transform may change the unit in place.

| Setting | Implemented by |
| --- | --- |
| `-print-before`, `-print-after`, `-print-changed` | an observer: the input dump, the result dump, the text comparison |
| `-stats`, `-time-passes` | an observer summing counters and time per instance, in pipeline order |
| `-opt-bisect-limit N` | an observer that may veto, answers each decision with `Seq <= N` and reports it |
| typed test hooks | an observer that needs units |
| `-verify-each` | the manager: verification, the copies before every pass and the preserve-all check |

**Labels.** A label names the unit a step runs on: its backend, its table and its function. Each combinator sets only
the field it decides and keeps the others: a `Fork` branch sets the backend, an `Each` part the table, a `PerFunc`
block the function. So a `Fork` nested in an `Each` part keeps that part's table, a slot body for that part runs in
every branch, and dumps and errors inside the branches name the table. The check of slot part bodies passes the
enclosing `Each`'s labels into the branches for the same reason, so the check and the execution agree.

**Instances.** A pipeline value may be used in several places, so the executor and the printer of the text form copy
the tree and name each occurrence of a pass: the k-th occurrence on one path of execution is `name#k` for k > 1. The
branches of a `Fork` and the alternative bodies of a slot are numbered apart, since no unit runs through two of them;
after a slot the count continues from the largest alternative. Settings (`-opt`, `-no-opt`) address every instance of a
name; `-print-after`, `-print-before` and the reports accept a name, which selects every instance, or an instance; dumps,
errors and reports print the instance.

**Verification** follows record 072 and the pass-pipeline design. The output kind's verifier runs right after every pass
that changes the kind, as its postcondition, and an error names the pass. A conversion that makes part of the
invariants of its output kind by construction declares the rest as its postcondition (`Def.Post`), which replaces the
kind's verifier there and only there: Elaborate computes the attributes of its Core module with `Analyze`, so its
postcondition is the structural Core verifier, and the attributes are compared where the Core passes end. Where a run
of transforms of one kind ends, which is the end of a stage, the unit is verified once if an optional pass of that run
ran; for a `PerFunc` segment this is each function at the end of the segment, so the Flow passes' segment verifies its own functions before `flow-finish`.
Outside `-verify-each` the manager therefore verifies only where the stages before it did, at the stage boundary:
required transforms are trusted, and a run of them only, such as the required steps after that segment, is not verified
after it, since verification exists to catch the optional passes, whose rollback depends on it. Under `-verify-each`
every pass, required or optional, is verified after it runs, and a pass that returned that it preserves everything must
leave the text form unchanged.

**Changed.** `Changed` is the pass's own report in every run, so `-stats` and every other reader of it give the same
values whatever other observers watch the run. When an observer needs the text comparison, the manager also compares
the text form before and after the pass into `TextChanged`, so `-print-changed` prints nothing after a pass that changed
nothing whatever it reported, and `-verify-each` cross-checks the report against the same comparison. The two are
separate fields because needs are joined per run: `-print-changed` must not change what `-stats` reports. In general, the value of every field that an observer reads depends only on the
run and the pipeline, never on which other observers watch.

**Copies.** The manager follows one policy, and no pass declares anything about copies.

- A *segment* is a maximal run of consecutive optional transforms of one kind on one unit: the passes of a sequence
  and its slots, or the per-function passes of a `PerFunc` block. A required transform, a kind-changing step or a
  combinator boundary (`Each`, `Within`, `PerFunc`, `Shared`, `Fork`) ends it. A required transform ends the segment
  before it because what it did must survive a later failure: an optional pass after it starts a new segment, whose
  rollback point is the unit the required transform returned.
- The manager copies the unit once per segment, before the first pass of the segment that runs, when the segment's
  start must survive it: the unit is *kept* by someone else (the input of `Execute` under `Keep`, the result of a
  `Shared` body for every use, the first included, the input of a `Fork`, and the parts of a kept unit), or a failure
  must roll back to it (the segment runs an optional pass and the run continues after failures, without `Stop`). A kept
  original is never changed, so it is itself the rollback point; otherwise the original is, and the passes change the
  copy. A run with `Stop` and no kept unit takes no copy: its units are consumed.
- A `PerFunc` segment copies the whole unit once and takes each function's rollback point from the original, so a
  table unit pays one copy per segment, not one per function.
- When an observer needs the state before each pass (`-verify-each`, `-print-changed`, unit hooks, bisection), the
  manager also copies before every transform, so that every unit an observer was given stays as its pass returned it.
  The rollback point stays the segment's, so the outputs and dumps of a run do not depend on which observers watch it.
- After a rollback the unit is marked kept, so a required transform after it copies before changing it.

Normal builds pay one copy per segment, consuming runs (`Stop`, the run-time backends) pay none, and debugging pays one
per pass.

**Failure.** A failure is held by a scope: the root, each part of an `Each`, each branch of a `Fork`, and each function
of a `PerFunc` segment.

- A failed optional transform rolls its unit back to the start of its segment, and that unit goes on. The start is
  never before a required transform that ran, so a rollback undoes optional work only. The later optional passes of
  the scope are skipped; the required steps still run, so a printer still gets a lowered unit, and the first error is
  reported with the instance and the label.
- In a `PerFunc` segment, a failure on one function rolls that function back and ends its optional passes; the other
  functions finish the segment. The enclosing scope is then failed, so the later optional segments of that table are
  skipped for every function.
- Under `Stop` there is no rollback point: the first failure breaks the run, in a `PerFunc` segment too. The copy of a
  kept table unit protects its keeper, but gives no function a rollback point.
- A failed lowering, a failed required step, and a failure without a rollback point break their scope: nothing after it
  runs in that scope, and the enclosing `Each` or `Fork` is broken after its other parts or branches ran. `Execute`
  then returns no result and the first error in execution order.
- `Each` parts and `Fork` branches start from the state their combinator was entered with: a failure in one table or
  backend skips nothing in another. A `Fork` returns the errors of all its branches, joined in branch order.

**Cancellation.** The manager checks the context before every step and returns a step error that names the instance
that was about to run, wrapping the context's error. A step that polls deeper (elaboration, the Core passes for the
closures) may unwind with the cancellation panic of `internal/cancel`; the driver sets `Recover` to turn it into an
error, so `internal/pass` keeps importing no package of the module.

**Determinism.** Given the same input, settings, pipeline and observers, a run produces the same output, dumps, events,
numbers and statistics apart from time. `Each`, `PerFunc` and `Fork` run in a fixed order, and a future parallel
executor must keep this order of events.

**Cost.** A run with no observers allocates no event, result, dump or counter map per step beyond the pass's own
context, whose counters are allocated on the first count.

### Sharing

`Shared(k, p)` runs `p` at most once per effective key within one `Execute`.

- **Key.** The effective key joins three things: the registered key's value for the input; the value of every feature
  and parameter that the passes of `p` declare, as the scope provides them at that point, each written with its key's
  format; and a fingerprint of the body as it would run on this unit (the canonical form of `p` with its instances and
  parameter overrides, and every slot resolved for the current part). So a Core pass that started reading the Flow
  target would stop the sharing of Core IR between Go and TypeScript instead of sharing it wrongly; a slot filled for
  one table, or a text that edits the body in one branch, never reuses another body's result; and two uses whose
  instances are numbered differently do not share, so a replay always reports the instances that would have run. The
  fingerprint is computed once per use from the step tree, without running anything.
- **Kept result.** The entry keeps the result; every use, the first included, gets it as a kept unit, so a later
  segment that changes it copies it first. The analyses of the shared unit stay valid for the same reason and are
  carried with it.
- **Same observation.** On a reuse the manager replays the recorded events of the first run to the observers, with the
  current backend and table in the label, fresh numbers in the same order, the recorded counters, no time and
  `Replayed` set. When an observer needs dumps, a step inside a `Shared` body records the dump it gives that observer,
  and the replay carries the recorded text where the first run's event carried a dump. So the event stream, the
  numbering, the dumps and the statistics are those of a run without sharing. Observers that could tell the difference
  turn sharing off: unit hooks and bisection, whose decisions could differ between two uses.
- **Errors.** A reuse reports the error of the first run again, relabeled with the current backend and table, so each
  target reports what it would report without sharing.

Sharing pays inside one backend: the Core IR of a table depends only on whether it builds values, so the Node table and
its traced copy share elaboration and the Core passes in every Go build. Across backends it pays when several are built
in one compilation: `sxir gen -lang go,ts`, `compiler.Generate`, the tests and the benchmarks. The VM and the closure
backend are not built in one compilation today, since the engine builds each on first use, so they share nothing unless
the engine changes; the `output` kind lets them be built together, as differential tests do.

### The Text Door

The printer turns a pipeline into a description whose string is the canonical text. The reader parses a text, and the
resolver checks it against the registries and builds the step tree:

```go
func Describe[I, O any](p Pipeline[I, O], h ...Line) *Description
func (d *Description) String() string
func Parse(text string) (*Description, error)

// Resolve checks d against the registry and returns the pipeline typed at its edges. It checks by
// kinds what the compiler checks for a typed pipeline, and that its required skeleton is that of
// ref, so a text can reorder, repeat, insert or remove optional passes only.
func Resolve[I, O any](d *Description, ref Pipeline[I, O]) (Pipeline[I, O], error)
```

`ref` is the pipeline the driver assembled for the same build. A text that drops a required transform such as
`flow-finish` is well typed but fails this check.

**The skeleton.** The resolver compares the text with `ref` by structure, not as a list of names: the required steps,
the blocks that hold them, in order, and every constant feature with its value must be those of `ref`. A text that
moves `flow-finish!` into the per-function block, or that says `with target=ts` in the Go branch, is rejected. The
skeleton of a slot is that of its default body, and every body of the slot for a part must have that same skeleton, so
a part body cannot add, drop or move a required step either.

**Kinds of keys and providers.** A `shared` key and a derived `with` provider read the kind of the unit their block
starts on; the resolver rejects one placed on another kind, naming it, before anything runs. The executor repeats this
check and the checks of slot parts for the typed door.

**Grammar.** The text is line-oriented; indentation is two spaces per level and is not significant to the reader.

```ebnf
text     = "sxir-pipeline" SP version NL { header } "pipeline" SP kind SP "->" SP kind SP "{" NL { item } "}" NL .
version  = digits "." digits .
header   = word { SP key "=" value } NL .      (* sxir, options, program, settings, debug, target *)
item     = step NL | block .
step     = pass [ "#" digits ] [ "!" ] [ "(" param { "," param } ")" ] [ "[" pattern { "," pattern } "]" ] .
param    = flag "=" integer .
block    = opener SP "{" NL { item } "}" { SP "for" SP label SP "{" NL { item } "}" } NL .
opener   = "fork" | "branch" SP label | "each" SP split | "func" SP view | "within" SP lens
         | "shared" SP key | "with" SP ( provider | feature "=" value ) | "slot" SP slot .
```

`!` must match the declaration of the pass. `#k` is informational: the reader numbers instances itself. `for label`
bodies follow only a slot inside an `each` and replace its body for the part with that label; the label must be one
that the split of the nearest enclosing `each` declares, so a misspelt label is an error instead of a body that never
runs. The pattern list is informational and must equal the declared patterns.

**Headers.** The headers carry every option of the build that is not a pass, so the driver can rebuild `ref` and the
run from the text alone (with the grammar). Each option has one scope, and each scope one header word:

- `sxir` is the version of sxir that printed the text. Library files come with that version.
- `options` holds the options of the program common to every target, which the check of the grammar reads: `start`,
  `source-info`, `default-value`, `left-recursion-bindings`.
- `program` (run-time backends only) holds what makes a run-time program other than `options`: the remaining options of
  the check (`recognition`, `disable-memo`, `no-projections`, `eager-attribute-copies`) and `role`, which names the
  program of the three that the compiler side of a run-time program makes: `parser`, or `recognizer` or `insertion`,
  which are derived from the parser in one place, so the parser's options and the role reproduce the program exactly.
  A text whose `pipeline` line starts at `library` is the text of the Go library; otherwise a text without a `program`
  line is the text of a generation.
- `settings` (the call) holds the profile and the overrides.
- `debug` (the call) holds the debugging settings, which apply to the whole compilation: the manager's policies
  (`verify-each`, and `opt-bisect-limit`, since bisection numbers one sequence of decisions across the run) and the
  observation flags (`print-after`, `print-changed`, `stats`, `time-passes`, and `print-before` when it is added). One
  writer receives the output of every target; each dump and each report line names the backend in its label.
- `target <label>` (one line per target, in order) holds every other generation option of that target, in declaration
  order.

The writer of the dumps and the library file systems are the only options not written; the reader supplies them. A
reflection test lists the exported fields of the generation options and fails unless each is written by exactly one
header word or is on that exclusion list, so a new option cannot be forgotten and cannot be given two scopes; a second
reflection test does the same for the options of the check over `options` and `program`. `Stop` has no header: the
reader derives it from the backends, as the driver does, and `Keep` is the driver's choice for its own input. Values are
escaped so that a module path or a library specifier round-trips. A run-time text needs the program that the compiler
side of a run-time program makes, which the driver cannot make, so its reader lives in that package beside the
derivation of the recognizer and the insertion program, for tests and the internal development tool.

**Versions.** The reader rejects another major version and a newer minor version. A text printed by another sxir version
is read with a warning, since its passes may behave differently; an unknown name is an error that names it.

**Example.** The Go pipeline of a parser without streams, as `-print-pipeline` prints it (header values abbreviated):

```text
sxir-pipeline 1.0
sxir 0.9.0
options start= source-info=false default-value=false left-recursion-bindings=false
settings profile=speed opt= no-opt=
debug verify-each=false print-after= print-changed=false stats=false time-passes=false
target go typed-only=false recognize-only=false limits=false expectations=false ...
pipeline program -> output {
  with target=go {
    with reach=none {
      each go-tables {
        shared core-config {
          elaborate!
          slot core-passes {
            guards
            scans
          }
        }
      }
      plan!
      each planned-tables {
        with table-config {
          flowlower!
          within module@flow {
            func funcs@flow {
              slot flow-passes {
                terminals
                guarded-classes
                class-runs
                class-tables
                inline-literals
              }
            }
            flow-finish!
          }
          execlower!
          within module@exec {
            func funcs@exec {
              slot exec-passes {
              }
            }
            func lowered-funcs@exec {
              slot target-passes {
              }
            }
          }
        }
      }
      print-go!
    }
  }
}
```

`go-tables` lists the typed table, the Node table, the recognizer and the traced copies that the target's options
select, labeled as the dumps label them; its join gives `cores`. `core-config` keys Core IR on whether the table builds
values. `plan` is a program-level step: reachability starts from every entry point of the program, the typed ones
included, so it cannot be decided per table. `planned-tables` splits the plan into its tables again and joins them, at
the Exec stage, into `tables@exec`, which `print-go` and `print-ts` take; the join numbers the expectation table across
the Node and recognizer tables, which is likewise not a fact of one table. `table-config` derives the features of a table (budget controls, observation,
Document, traced) from its unit. A TypeScript target has `target=ts`, the split `ts-tables`, the pass `outline`, the
target passes `ts-merged-records ts-tested-inits`, `ts-printable!` and `print-ts!`. Several targets are
the branches of one fork, each named by its backend: `pipeline program -> []output { fork { branch go { with target=go
{ ... } } branch ts { with target=ts { ... } } } }`. (Amended at step 9: the example first showed a fork of one Go
branch; a single target is not forked, so its pipeline, its text and its dumps stay those of the one target.)

### The Driver

The driver is the only package that assembles and executes pipelines.

```go
package driver

// Program is the input of a compilation: the checked program and the options that every target
// shares (start rule, source information, default values, left-recursion bindings).
type Program struct{ ... }

// Check checks the grammar for generation.
func Check(g *ast.Grammar, o common.CompileOptions, start string) (*Program, error)

// ProgramOf returns the Program of a program checked elsewhere: the run-time programs, which are
// checked with rule flags and a cancel control. role and parser, the options the grammar's parser
// was checked with, are what the program header writes. It checks nothing again.
func ProgramOf(p *frontend.Result, role Role, parser check.Options) *Program

// Role names which of the run-time programs of a grammar a program is.
type Role uint8

const (
	Parser     Role = iota // the program of the grammar
	Recognizer             // derived for recognition
	Insertion              // derived for missing-token insertion
)

type Backend = result.Backend // Go, TypeScript, VM, Closure

// Target is one backend of a compilation with its options. Gen holds the options of Go and
// TypeScript. Its fields that Program holds and that the call holds (optimization, debugging) must
// be zero or equal to the call's values; otherwise Compile fails before any step, naming the target
// and the field.
type Target struct {
	Backend Backend
	Gen     common.GenOptions
	GoHooks *goprint.Hooks // tests only: the hooks of the Go printer's decisions; production leaves it nil
	TSHooks *tsprint.Hooks // tests only: the same for the TypeScript printer
}

// Options are the settings of a compilation, common to every target.
type Options struct {
	Settings  pass.Settings
	Debug     Debug           // the debugging settings, which apply to the whole call
	Edits     []pipe.Edit     // internal: typed edits of the assembled pipeline (tests)
	Observers []pipe.Observer // internal: observers of every step and its unit (tests)
	Pipeline  string          // internal: a pipeline text whose headers and body replace the assembled ones
}

// Debug holds the debugging settings of the call (common.Debug): the dumps, the verification
// after every pass, the statistics and times and the bisection limit. Every target's
// output goes to its one writer, labeled with the backend.
type Debug = common.Debug

type Output = result.Output

// Compile runs one pipeline for the targets under one manager: the targets are the branches of one
// Fork, in order, and share what their keys allow. On an error it returns no output and the errors
// of every failed target, joined in target order.
func Compile(ctx context.Context, p *Program, o Options, targets ...Target) ([]Output, error)

// Describe returns the assembled pipeline of the targets as text, for -print-pipeline and tests.
func Describe(p *Program, o Options, targets ...Target) string

// GoLibrary runs the pipeline of the Go library under the same manager.
func GoLibrary(ctx context.Context, o Options) (Output, error)
func DescribeGoLibrary(o Options) string

// FlowTable builds the Node table of p at the Flow stage for the VM's Flow route, at the default
// settings and with no pruning. It is an adapter: it runs the Core part and the Flow part of a
// table's pipeline, and plans the one table between the two runs. It is the VM's route in
// production until step 12 of the migration assembles the VM pipeline in Compile, which deletes it.
func FlowTable(ctx context.Context, p *frontend.Result) (*plantypes.Table[code.Flow], error)

// The slots of the assembled pipelines, for Edits.
var CoreSlot, FlowSlot, ExecSlot, TargetSlot = ...
```

Each target is a branch labeled by its backend (`go`, `ts`, `vm`, `closure`; a second target of one backend is
`go#2`). Generation runs with the failure rule: required steps continue and segments roll back. The run-time backends
run with `Stop` and consume their units, since the build of a parser fails on any internal error.

**Dumps per target.** The driver makes one dump observer per target that selects dumps: it needs dumps, takes the
events whose label names its branch and whose instance its selection names, and writes them to that target's writer. A
shared step's events are replayed in each branch with that branch's label, so each target gets the dumps it would get
alone. The options of the call hold only what the manager applies to the whole run.

**Several targets from the command line.** `sxir gen` accepts several languages: `-lang go,ts`, each at most once.

- *Output layout.* With one language, `-o DIR` is the package directory. With several, `-o DIR` is a parent and each
  language writes to `DIR/<lang>`; `-go.o` and `-ts.o` give a language its own directory instead. The ownership rules
  of a generated directory apply per directory, and every check of every directory happens before the first write: a
  failing target writes nothing for any target.
- *Flags.* Common flags configure the one check and every target: `-g`, `-s`, `-source-info`, `-default-value`,
  `-optimize`, `-opt`, `-no-opt`. Target flags (`-types`, `-document`, `-observability`, `-expectations`, `-no-limits`,
  `-recognize`, `-max-depth`, `-lib`) apply to every listed language; the qualified form (`-go.types`,
  `-ts.max-depth`) applies to one language and wins over the unqualified one. Flags that name something of one
  language (`-module`, `-pkg`, `-nodoc` for Go; `-lib-module` for TypeScript) apply to that language. A flag that a
  listed language does not support (`-stream`, `-typed-only`, `-recognize-only` with `ts` listed) is an error that
  names its qualified form. With one language every flag means what it means without the list.
- *Diagnostics.* The debugging flags (`-print-after`, `-print-changed`, `-verify-each`, `-stats`, `-time-passes`) are
  common flags: they apply to the whole call, and dumps and report lines name the backend in their label. Every table
  runs a stage before the next stage of any, so the driver groups the dumps by table: the program's dumps (the plan)
  first, then each table's, from its Core IR to the Exec stage, in the order of the table's first dump. The report
  lines follow the order in which each step first ran, which is the order of the pipeline. `-lib only` writes the
  library of each listed language.

**Several targets from the API.** `pkg/compiler` gets one function over the existing configurations:

```go
// Target is a language to generate: a GoConfig or a TSConfig.
type Target interface{ target(options) (driver.Target, error) }

// Generated is the files of one target.
type Generated struct {
	Target Target
	Files  []File
}

// Generate generates a parser for each target from one check of src, in order. The Options apply
// to every target (start rule, source information, default values, optimization, debugging, whose
// dumps and reports name the backend); each config's fields apply to its target. The targets share the steps whose
// input and settings are equal. Each result equals what GenerateGo or GenerateTypeScript returns
// for that target alone. On an error no files are returned.
func Generate(src grammar.Source, targets []Target, opts ...Option) ([]Generated, error)
```

`GenerateGo` and `GenerateTypeScript` become `Generate` with one target, and the command line's language list maps to
one call. Optimization settings are common to the targets of one call, which is what lets them share Core IR; a caller
that wants different settings per language makes two calls.

**Planning.** `plan` decides everything that a printer decides before or while printing: the functions of each table
and their forms, the reachable rules and the route of each call, the frame layouts of the typed table from the Core
rules' scopes, and for TypeScript the same routes. The plan keeps every TypeScript function printable, and the
analysis step `ts-printable`, right before `print-ts`, checks it on the Exec-stage tables (amended, see step 5). A planned table holds only functions that its target prints: a typed-only Go
parser, which prints the Node rule bodies only for the frame layouts they fix, plans no lines of the Node table's
Operators Rules, so its dumps show no such lines either. The refusal of flat functions is an analysis of the Flow
stage that `flowlower` runs on each flat function right after writing it, before any Flow pass: a refused function is
replaced by its lowered fallback in the same lowering, so a table is lowered once and no target's form is read.

**The expectation table.** The Go parser numbers an expectation the first time one of its functions records it, and
the Flow passes decide where that is: a guard or a dispatch records the first-character expectations of a choice
ahead of the bodies of its alternatives. Numbering the Core IR of the tables in plan order gives the same expectations
in another order for most grammars of the corpus, so the numbering is not part of `plan`. The join into `tables@exec`
numbers the table from the lowered Node and recognizer tables, in that order, function by function in plan order and
operation by operation in the order of their code, in the Exec lowering, after the fixed expectations. That is the
order in which printing every function records them, which a test checks over the corpus. A rule of the Node or
recognizer table that the plan does not reach keeps its table row, and the IDs are those of a parser that prints
every rule. So `plan` puts the functions that pruning leaves out of these two tables into a part of their own, a
planned table labeled with the table's name and `Unreachable` (`rulesUnreachable`, `recRulesUnreachable`), which holds
functions disjoint from the printed table and runs the same steps in the same run, observed and dumped under its own
label. The join numbers the expectation table from both parts in the order of the whole table, and gives the printers
only the tables they print, so every function is lowered once. (Amended: the numbering first lowered the two tables a
second time, without observers, which lowered each function twice.) The planned tables that
`print-go` prints are the pruned ones, and printing them fails with an internal error if it records an expectation that
the numbering lacks. The typed table and the traced copies add their expectations after this table as they are
printed, since the typed table has no row of a rule it leaves out.

**The fallback for planning.** `plan` takes the reach of the program from the feature `reach` when the compilation
sets it, and computes it from the Core IR of the tables after their passes otherwise. A printer whose printed code
enters other rules than the computed reach returns that reach, as its output, instead of the files; the driver then
drops what the debugging observers collected of the run and executes the same pipeline again with the printed reach as
a constant `reach`, and the printer must print the files with it. No pass runs a pipeline. The fallback belongs to
every backend: no step of it names one. The TypeScript printer uses it; the Go printer prints what the plan reaches, as
migration step 4 made it. Under a bisection limit the pipeline runs once, so
that one execution numbers every decision: the Go target keeps every rule, and the TypeScript printer prints with the
reach that the plan computed. The observers of `Options.Observers`, which only tests set, see both runs. (Amended: the
fallback was first a Go-only first run with `reach` unset, whose files the second run replaced.)
`-print-pipeline` prints the second run with `with reach` as a derived provider, whose value a reader recomputes by the
first run. (Amended at step 8: it prints the first run, with the constant `reach` that the second run replaces, and a
build from the text repeats the fallback.) Over several targets, the fallback of the branches whose printers return a
reach reruns the whole fork once, with each printed reach set only in its branch (`InBranch`); a branch that asks for
another reach in the second run is an internal error. The other branches build their outputs again: sharing lasts one
execution, so the second run replays nothing of the first, and within it the branches share their Core IR as in any run.
(Amended at step 9.)

**The VM** pipeline before the switch is the production route, run through the manager:

```text
pipeline program -> output {
  with target=vm {
    each vm-tables {
      shared core-config {
        elaborate!
        slot core-passes {
          guards
          scans
        }
      }
    }
    vmlower!
  }
}
```

`vm-tables` gives the one table that the VM builds, and its join gives `vm-input`: the program and the optimized Core
module, which `vmlower` needs together. So `elaborate` takes `table` in every pipeline, and `vmlower` has one input
kind. After the switch, the VM's table runs `plan`, `flowlower`, the Flow passes for the bytecode target and
`flow-finish` like a generating backend, and `vmlower` takes the Flow module of its table instead of the Core module; the
Core route is deleted, and a text printed before the switch then fails to resolve by kinds.

**The closure backend** builds one Core module for the whole program, and every family is built from it. Its pipeline
is the same front over `each closure-tables`, joined into `closure-input`, and `closure-core!`, which returns the
optimized Core module of the program. A closure target names no family: the engine keeps its cache of that module
(built for the first family that a parse needs, shared by every family of the program and of its metered program,
dropped after the last pending family) and keeps asking the compiler side of the program for it, which runs one
compilation with the closure target. The runtime reaches the compiler only through the interface of the parser state,
and no runtime package imports the driver. The build stays lazy and runs under the context of the parse that needs it,
so its cancellation is attributed to that parse. Building the closures of a family from the result is the backend
consuming the result, not a step.

The Core analyses poll the cancellation control that the Core module holds, so `elaborate` stores the control it builds
from the step's context in the module, and none when the context cannot end. The module that the engine caches must
poll no parse's control after that parse, and `closure-core` may run on the kept result of a `Shared` entry, so it
changes nothing in its input: its result is a shallow copy of the module header without a control, sharing the rules,
which the closure build only reads.

**The run-time entry.** The compiler side of a run-time program already holds a program checked with rule flags and a
cancel control, and derives the recognizer and the insertion program from it. It wraps each such program once with
`ProgramOf` and keeps the parser's settings beside it; nothing is checked a second time. The bytecode is built with the
VM target once per program, under a background context, since it has no context and is not cancelled; the closures'
Core module is built with the closure target and the parse's context wrapped as before, or a background context when
the caller passes none. So the manager's check before each step and the polls of `elaborate` and the Core passes see
the same context and attribution as before. The driver's `Recover` turns only the cancellation panic into a step error
and re-panics every other value, and the compiler side maps a step error that wraps a context error or a cancellation to
the build-cancelled error of the parse.

**The settings of a run-time program.** One carrier takes the optimization settings from the API to every run-time
program:

- `pkg/compiler` converts `Optimize` to settings in the one function that builds the options of the compile entry,
  which also validates them, so every function that builds a parser goes through it.
- The options of the compile entry stop being the options of the check and become a struct with two named fields, so
  each caller states what it sets:

  ```go
  // Options configures the compilation of a grammar to a run-time program.
  type Options struct {
  	Compile  check.Options   // what the program is: the check reads them
  	Settings pass.Settings   // how its backends build it: the VM and the closures
  }
  ```

  The zero settings are the speed profile, in which every Core pass is on, so a caller that sets none builds what it
  builds today.
- The compiler side of a run-time program takes the settings when it compiles, checks or wraps a program, holds them
  beside the program, and copies them into the recognizer and the insertion program it derives, so the three programs
  of a grammar build under the parser's settings.
- The bytecode depends on the settings, so the once that builds it and its result move from the checked program to the
  compiler side that holds the settings; the checked program keeps no output of a backend. A saved program holds the
  bytecode that its settings built, and loading it builds nothing.

The settings do not go into the options of the check: the check reads none of them, so they would have two scopes (the
`options` and the `settings` header), the common options of `ProgramOf` would include them, and the frontend's options
package would import `internal/pass` for a value that only backends read.

**Settings for every backend.** The VM and the closure backend follow the optimization settings. The reference evaluator
of Code IR is the oracle of the differential tests, which run each backend at the default settings and with every
optional pass off. `compiler.WithOptimize` applies to every backend. The Core passes are on in every profile, so the
bytecode and the closures change only when an override turns a Core pass off.

**Test hooks** become edits and observers: a typed hook on Core units is an observer that needs units; a replaced
pipeline of one stage is a typed fill of the stage's slot for one table or every table; a broken test pass is a fill
whose body ends in that pass, counted by an observer; test passes are declared as test passes, always enabled and
without a flag; per-table text overrides are slot bodies for a part. The hooks of the printers' own decisions, which
are not steps (the generator that printed, the routes it chose for each rule, the expectation table and the reach as
the printer reads them), stay hooks of the printers, given as the fields `GoHooks` and `TSHooks` of a target; production leaves them nil, and an
architecture test checks that no non-test package sets them. (Amended: the record first moved every hook to edits and
observers; the printers' hooks observe no step, so no observer can carry them.)

### Printers Take Exec-Stage Units

A printer's input is only units of the Exec stage: `print-go` and `print-ts` take `tables@exec`, and within it the
printers print each function as `lowered@exec`. The one exception is the libraries' compositions module, the kind
`library`, which `print-ts` reads through `tables@exec` and `print-libgo` takes: it is at no stage, and it holds no Flow
operation by construction, since the compositions are written in the runtime's own operations and the Action dialect,
never through `flowlower`. A printer runs no lowering, no pass and no pipeline; every lowering is a step of the
pipeline. What a printer reads from the Flow stage reaches it through the Exec module, and what it reads from earlier
stages through the Exec-stage table unit:

- **Primitive names.** The primitives that [record 073](073-code-ir-dialects.md) made of the Flow operations get their
  own data package, `internal/ir/code/exec/prim`, which imports only the Code IR and Core IR packages. It declares a
  constant per primitive, and the printers switch on these instead of mapping each call back to its Flow operation.
  `execlower` keeps mapping a Flow operation to its primitive, and a specification test checks that every Flow
  operation has its primitive constant.
- **Immediates.** The Go types of the immediate operands, with the loop-label functions and the register names, move
  from the Flow dialect's package to `prim`. A primitive has the immediate of its Flow operation (073), so the Flow
  dialect declares its operations with the types of `prim`: one type, no alias.
- **Types.** The classification of the runtime's handle types moves to `prim`; it classifies the Code IR types of the
  Exec module's registers, handles and locals.
- **Function attributes.** `execlower` converts the Flow attributes of each function into Exec-stage attributes of
  `prim`: every field except the forms, which `flow-finish` consumes. The Exec-stage verifier rejects a function whose
  attributes are of another type, as it rejects a Flow operation, so no Flow attribute survives the conversion. The Code
  IR listing prints no function attributes, so the dumps do not change.
- **Earlier stages.** The Core rules, rule actions and Operators Rules, the expectation table, the origin map and the
  rules of the program come from the Core module, origin map and program that the Exec-stage table unit carries, not
  from a Flow state of the generator, which goes. The lowering options that the printers read are function attributes.

**Lowering leaves the printers.** Each call that lowers or runs passes inside a printer becomes a step: the Core IR of a
table is `elaborate` and the Core passes of the table's pipeline, and the Core IR that reachability and the used-features
analysis build is the input of `plan`; the functions of a table are listed by `plan` and lowered by `flowlower` and the
Flow passes of the table; TypeScript's Exec passes are the table's `execlower!` and its target passes; the action terms
are listed by `plan` per table, with the options the printers pass, and lowered in the table's pipeline before
`execlower` into Action-dialect functions of the table's module, which are legal at both stages (whether that lowering
is part of `flowlower` or of the redesigned action lowering is decided by that redesign; either way it is a step). The
specialization of the TypeScript library's compositions for the parser's features is a required step of the TypeScript
branch on `tables@exec`: the TypeScript backend's required step `ts-library!` sets the unit's `library` to the
compositions module with the requests of the TypeScript library's specification table, each keeping only the features
that the parser has as the plan's used-features analysis reports them, and `within ts-library { specialize! }` adds the
specialized compositions that `print-ts` prints. The TypeScript library alone (`-lib only`), which no grammar determines,
is a library route of the TypeScript target: `Compile` without a program assembles
`pipeline library -> output { specialize! print-ts-lib! }`, whose source unit holds the requests of the same
specification table for every feature, and `print-ts-lib` is a printing pass of the TypeScript backend package beside
`print-ts`. (Amended: the record first left the library alone to the printer, which would have run the specialization
itself.)

**The Go library is a route of the same pipeline.** `golang/libprint` prints the compositions of the Exec dialect into
the Go runtime library, the engine state and the conformance bindings, from `go generate`. It goes through the driver and
the manager like the TypeScript library:

```text
sxir-pipeline 1.0
sxir 0.9.0
settings profile=speed opt= no-opt=
debug verify-each=false
pipeline library -> output {
  specialize!
  go-cold-path!
  print-libgo!
}
```

The two libraries enter differently because they differ: the TypeScript library is specialized per parser, for the
parser's features, inside the `ts` branch; the Go library is a checked-in artifact that no grammar determines. They
share the `specialize` pass.

- The source unit is `library`: the driver makes it from the compositions module and the specializations that the
  library printer's specification tables list, which it exports as data, grouped by output file.
- `specialize` is the one pass that both libraries run. A request names the composition, the features, the name of
  the copy and the file it is printed into; the name is unique within its file. The unit keeps one specialized function
  per request, in request order, outside the compositions module, so the module does not change and many requests of
  one composition with different features do not collide.
- `go-cold-path` is a required step of the Go library: it splits the cold path off each guard that generated code
  inlines, as it did inside the library printer. A request asks for the split by naming the cold function, and only
  such requests are split, so the engine's copy of the same composition stays whole. The function that a pass makes of
  a copy belongs to the request of the copy, and the unit keeps it beside the copies; the verifier checks that a
  requested cold function has no name of a composition and none of another request or cold function of its file, and
  that a made function belongs to a request, has no name of a composition and has a name unique among the functions
  made for its file. (Amended: the record first listed
  `specialize` and `print-libgo` only, leaving the split to the printer.)
- `print-libgo` is a printing pass of the Go backend package, beside `print-go`, which takes `library` and returns the
  files that the library printer writes. The library printer builds no module, runs no pass and writes no file; a
  printer that edits a function copies it first, so an edit of a documentation comment for one output file does not
  reach another file that prints the same composition. The `go generate` program writes the files of the output, and
  the generated files do not change.
- A library text has the headers `sxir`, `settings` and `debug` only, and its `pipeline` line starts at `library`. The
  reader selects the reference pipeline by that source kind and the absence of a `target` header, since the text of
  the TypeScript library alone also starts at `library` but is a TypeScript target with its options. (Amended: the
  record first selected by the source kind alone.) The library pipeline has no slots, so an edit that names a
  slot fails before any step, naming the slot. A program text given to the library route, and a library text given to
  `Compile`, fail before any step, naming the source kind.
- The library printer is a print package: every import rule of the print packages applies to it, and its references
  into the Go printer are limited to the printing API over Exec-stage functions.
- The tests that specialize compositions run the `specialize` pass from their test files through `Execute`.

The analysis of dead stores, which the printers apply with their own purity, changes nothing, so it stays in the
printers but moves out of the pass package of Code IR into an analysis package of the Exec stage that imports no pass
code. The check whether a flat rule can run a lowered function, which reads no entry position or action context where
the printers reach and keeps no value in a variable, sits beside the analysis of dead stores and moves with it: it reads
only the function and the entry variables and variables it is given, so the plan decides with it on the Flow function
and the Go printer asserts with it, on the Exec function in the forms of its passes, that the decision still holds,
each with the attributes of its own stage. The flags that the Go printer registers move to the package that declares
the lowering that reads them, or to the
Go backend package that declares `print-go`, which declares them as parameters and gives their values to the printer as
data.

**Names.** The printers are named after the Exec primitives and functions they print: no file of a print package is
named `flow*`, and no identifier it declares contains `Flow` as a word. Helpers that only start a Flow lowering or hold
Flow state are deleted, since the pipeline elaborates and lowers and the table unit holds the functions. The error of a
construct that a printer cannot print says that the printer does not print it, not that Flow IR does not. A name that
collides with an existing one takes the prefix `exec`. Tests that check a primitive against the contract of its Flow
operation keep naming the Flow operation, since 073 states the contract per Flow operation.

### What Makes the Wrong Shape Impossible

By construction:

- A pass's function is reachable only through the manager: a pass is opaque, the constructor keeps the run function in
  the step tree, and declaring packages keep their run functions unexported and export only the pass values and data
  types.
- A composition is a step tree made only by the combinators or the resolver; there is no stage API to call.
- A missing conversion between two Code IR stages, or between two IRs, does not compile.
- `Execute` is the only executor.

Detected by architecture tests, because Go cannot restrict callers:

- Outside `internal/pass` and the package `internal/driver` itself (not its subpackages), no non-test package executes
  a pipeline. The subpackages and every other caller of a compilation go through the driver's `Compile` and `Describe`;
  they may import `internal/pass` for settings and edits only. Packages that declare passes, kinds or analyses import it
  for those, and run nothing.
- `internal/frontend/check` imports no `internal/pass`, so the settings stay out of the check's options, and no non-test package
  under `internal/runtime` imports `internal/driver` or `internal/pass`.
- `internal/driver` exports only an allowlist of identifiers: the compilation API above, the slot keys and the
  vocabulary of the settings (profiles, their documentation and values, the parsing of overrides and dump selections,
  and the check of the registry against the driver's step names). A new export fails the test. Its subpackages keep their
  own exports, which the import rules cover.
- No package exports a pass definition or a function that a pass's run function names; lowering, planning and pass
  packages export their pass values and data types only. In particular the pass package of Code IR exports no
  specialization function: both libraries reach it through the `specialize` pass.
- `EnterFlow` is called only by `flowlower`, `EnterExec` only by `execlower`.
- The print packages of every backend, the library printer included, and `internal/backend/codegen/common` import none
  of `internal/pass`, `internal/driver`, the pass packages, the lowerings, `vmlower` or the planning code. Until the
  action lowering of the pipeline lowers the action expressions as a step, the two printers' files that print action
  expressions call the action lowering's entry with its options; the test lists exactly those uses, fails when one
  of them goes, and the ban becomes total when the action lowering leaves the printers. They may
  import the result and data types: the planning data types, Code IR, Core IR and the lowered function. To make this
  checkable by package, the data types move into packages of their own apart from the code that computes them.
- The printing passes and the last steps of the run-time backends are declared in the backend packages above the print
  packages, which import `internal/pass`; the print packages only receive results. The result package of the driver
  imports no pass, lowering or planning code.
- Printers take Exec-stage units only. The print packages and `common` import none of the Flow-stage packages (the
  Flow dialect, `flowlower`, `flowpass` and the planning code) nor the Core builder `internal/frontend/elaborate`; they
  read primitives, immediates, types and function attributes from `prim`. A type-checked walk of the print packages
  finds no use of the Flow stage marker, a module or table at the Flow stage, or `EnterFlow`, so no printer function
  takes a Flow-stage unit. Every registered pass whose output is `output` for Go or TypeScript has an input kind whose
  name ends in `@exec` or is `library`; `library` is accepted by name, since another stage-less kind could carry
  anything. No non-test file of the print packages is named `flow*`, and no identifier they declare contains `Flow` as
  a word, with an empty allowlist.
- The Exec-stage verifier rejects a function whose attributes are not the Exec-stage attributes.

A rule that holds before the code it checks exists (no print package uses the Flow stage marker before the marker
exists; no pass outputs `output` before the kind exists) proves nothing by passing, so each such rule lands with a
negative control: a planted violation, in a testdata package or a registry built only for the control, that the rule
must report. The kind rule's control plants a printing pass on `code@flow` and one on a stage-less kind other than
`library`.

## Alternatives Considered

- **Kind identity by name, checked at run time** (several kinds per Go type). Simpler types, but the most important
  boundary of the compilation would be a run-time error again. Rejected for stage-typed units.
- **A wrapper type per stage** that embeds the module. Embedding exposes the module and a conversion is one literal
  away. Rejected for a phantom-typed handle with restricted constructors.
- **Keep stage entry points on the driver and move the calls out of the printers.** No single value describes the
  compilation, so the text form, `-print-pipeline` and sharing have nothing to work on. Rejected.
- **Type-erased pipelines only**, or **typed pipelines only.** The first loses the compiler's check of the assembled
  pipelines; the second loses the text form and reading back. Rejected; both doors build one tree.
- **A copy per pass for passes that change their unit**, or a pass contract that forbids changing the input. Measured:
  a copy per rewriting pass costs 5.8% of generation time, against an A/A spread of 0.3%. Rejected for one copy per
  segment, which the manager decides from who keeps the unit.
- **A per-segment keep declaration by the caller.** The manager can tell from the pipeline which units are kept, so the
  declaration moves into one field of the run. Rejected.
- **Each backend's own result kind and no common one.** `Fork` needs one result type; an adapter step per backend would
  add a step that does nothing to every dump and text. Rejected for the one kind `output`.
- **Planning per table.** Reachability is a fact of the program, and the expectation numbering one of its joined
  tables. Rejected for a program-level `plan` and the numbering at the join.
- **Numbering the expectation table in `plan`.** The plan holds Core IR, whose order of expectations is not the one the
  Flow passes give the printed functions: it renumbers the generated tables of most grammars. Rejected for the
  numbering of the lowered tables at the join into `tables@exec`, which keeps the generated parsers.
- **A multi-target entry in the API only, or in the command line only.** Both are thin over the driver; offering one
  without the other would make the command line and the API generate differently. Rejected.
- **Shared results that emit no events.** Simpler, but dumps and bisection numbers would depend on which backends are
  built together. Rejected for replay.
- **`-verify-each` as an observer.** It needs copies and turns a failed verification into a pass failure, which only
  the manager can do. Rejected.
- **A required transform inside a segment, rolled back with it**, or a check that rejects a required transform between
  optional ones. The first undoes required work after a later optional failure, against the failure rule; the second
  forbids a shape that typed code can build and that costs only one more copy. Rejected for ending segments at required
  transforms.
- **A closure target per family.** Each family's compilation would elaborate the program and run the Core passes again,
  since the Core module is one for the program. Rejected for one closure target per program, with the engine's cache
  around it.
- **Dumps per target and manager policies per call**, or **every debug option per target.** The first puts the
  debugging settings in two places, the call and each target's configuration, so the command and the Go API need two
  surfaces for one concern; it only buys separate writers, which the backend in each label already gives a reader. The
  second gives verification and bisection, which one manager applies across shared steps, values that could differ
  between targets that share a step. Rejected for one set of debugging settings for the whole call, with one writer
  and the backend in every label.
- **A second check with rule flags and a cancel control** for the run-time programs. It duplicates the check that the
  compiler side of a run-time program already does. Rejected for wrapping the program it holds.
- **The optimization settings in the options of the check.** The derivation of the recognizer would carry them without
  a change, but the check reads none of them, so they would have two scopes and the check would import the pass
  package. Rejected for a settings field of the compile entry's options.
- **Read-back for generation texts only.** Simpler, but a VM or closure text could not reproduce its run. Rejected for
  the `program` header and a reader beside the run-time programs.
- **A `Fork` that starts a fresh label**, or a check that rejects slot part bodies under a nested `Fork`. The first runs
  no part body and drops the table from the branches' dumps; the second forbids a meaningful shape. Rejected for labels
  that each combinator sets one field of.
- **`Changed` by text comparison for every observer once one needs it.** A per-target `-print-changed` would change
  what `-stats` reports. Rejected for the pass's report and a separate text comparison.
- **Printers that keep importing the Flow dialect for names, attributes and types**, or aliases to the new types. The
  printers would still depend on the Flow stage, and an alias gives one thing two names. Rejected for the primitives'
  own package and Exec-stage attributes that `execlower` writes.
- **The primitives' names and immediates in the Exec dialect's package.** That package already declares names for the
  compositions that would collide. Rejected for the subpackage `prim`.

## Testing

The structural tests fail on the code before the change that lands them and pass after it; a rule that holds before
its subject exists is proven by its negative control instead.

- The architecture tests of the previous section, with their negative controls.
- A testdata package composes the Flow passes with the Exec passes without `execlower`; `go build` must fail and name
  both stages. The same text fails to resolve, naming the step. `With` on a module of another stage panics.
- Golden `-print-pipeline` texts for Go, TypeScript, Go with TypeScript, the VM and the closure backend list every step
  from the program to the output; parsing a text and printing it gives the same text; building from the printed text
  reproduces the output and the dumps.
- The resolver rejects a missing or extra required step, a required step moved to another block, a changed constant
  feature, a wrong `!`, an unknown name, a kind mismatch, an unprovided feature, a body for a part outside a slot,
  another major or a newer minor version, a required step added in a slot's body for one part, a part label that the
  split does not declare, and a `shared` key or a derived provider on a block of another kind. `Execute` rejects a
  typed fill for an undeclared part, or with a required step in a part body, before any step runs.
- The headers: the reflection tests over the generation options and the options of the check; a text read back builds
  the same `ref`; changing a header value changes the run; `Compile` rejects a target that sets a program or call-wide
  field to another value than the call's. For each role, the VM and the closure text of a program read back builds the
  same bytecode and Core module as the run that printed it.
- Dumps per target: Go and TypeScript with different dump selections and writers in one compilation; each writer gets
  its target's dumps only, equal to a run without sharing.
- One counting observer sees every step of the four backends, the lowerings, `plan` and the printers included.
- Sharing: Go and TypeScript built together elaborate each configuration once, and their outputs and events, with
  non-empty dumps, replayed ones included, equal those of a run without sharing; each output equals the output of its
  target built alone, byte for byte. Within one Go build, the Node table and its traced copy share Core IR. A slot
  filled for one table and a text that edits one branch's shared body give the output of a run without sharing; a
  failure in a shared body gives the outputs and errors of a run without sharing, each error labeled with its own
  target; a pointer-valued feature with equal values shares.
- Copies: a pass that changes a shared result in place does not change it for the next use, also under `Stop`; the
  number of copies is one per segment, none under `Stop`, more under `-verify-each`.
- The failure rule: a failed optional pass rolls back to its segment's start and the required steps go on; a required
  transform between two optional passes keeps its effect when the later one fails; a failing test pass on one function
  of one table leaves the other functions, the other tables and the other backend running, skips the later segments of
  the table, runs its required steps and names the instance and the label; under `Stop` the run breaks, also in a
  `PerFunc` segment of a kept unit.
- Labels: a slot filled for a part, inside a `Fork` inside an `Each`, runs in the branch, and the branch's events carry
  the table in their label.
- Observers do not change each other's values: `-stats` is the same with and without `-print-changed`, and a pass that reports a change without
  making one gives `Changed` true and `TextChanged` false. No unit is printed without an observer that needs dumps, and
  an observer that needs only dumps gets a non-empty dump on every event whose kind prints, live and replayed.
- The closure target: one compilation gives the program's module, every family is built from it without elaborating
  again, and the engine's cache compiles again only after it dropped the module. The VM and the closure built in one
  compilation share the Core front, and `closure-core` on the kept unit leaves it unchanged and returns a module
  without a control.
- Cancellation: a cancelled context names the step; a cancellation panic in the closure build is a cancelled build; a
  panic of the parse's context unwinds unchanged; a closure build without a context builds the module.
- Settings: turning a Core pass off through `compiler.WithOptimize` changes the parser's bytecode, which equals the
  driver's build with those settings, and the recognizer's and the insertion program's bytecode equal their direct
  builds; the closures follow the same settings; both agree with the reference evaluator; the derived programs hold the
  parser's settings. Dropping the settings anywhere on the way fails these tests.
- Printers: a specification test that every Flow operation has its constant in `prim` with the same immediate type, the
  verifier's negative control with a Flow attribute left, and identical generated output and dumps across the change
  that moves the printers onto the Exec units.
- The Go library: a golden library text that reads back to an equal text and an equal output; an edit naming a parser
  slot fails on the library route, naming the slot; `go generate` through the driver leaves every generated file
  unchanged; one `library` unit with two requests of the same composition under different features keeps two
  functions, and its module is unchanged; printing the Go terms file and then the engine's term file from one unit
  gives the engine file printed alone; a program text given to the library route, and a library text given to
  `Compile`, fail before any step, naming the source kind.
- A toy kind, a lowering to it and a transform on it compose typed and as text and run with dumps, `-verify-each`,
  statistics and bisection with no change to `internal/pass` or `internal/driver`.
- `sxir gen -lang go,ts` writes `DIR/go` and `DIR/ts` equal to two single-language runs; a qualified flag applies to one
  language; an unsupported unqualified flag is an error naming the qualified form; a failing target writes nothing.
- Each backend runs twice, with `GOMAXPROCS` 1 and N: identical outputs, dumps, events and statistics.
- A run with no observers allocates nothing per step beyond the pass context.
- At every migration step the generated output over the generation corpus and the `-print-after all` dumps equal those
  of the step before, except as listed under Migration.

## Performance

The manager adds one instantiation per run and one event per step when observers watch. Copies follow the segment
policy: a generation pays one copy of each table's module per Flow and Exec segment and one Core copy per `Shared` entry,
for the rollback that the failure rule needs; the run-time backends pay none. The fingerprint of a `Shared` use is a
walk of its body, computed once per use. All of this is measured against the generation-time gate, within the A/A
spread of the base, and the run-time compile benchmark. Building Go and TypeScript in one compilation checks the grammar
once and shares the Core IR of the tables whose keys are equal; the saving is reported only after it is measured.

## Migration

Each step keeps the generated output and the `-print-after all` dumps identical. The dumps change three times, each as
its own change: the removal of the stage names `core`, `flow` and `exec` from `-print-after` and the dump headers; the
header form that a kind's `Print` requires; and the attributes in the dump after `elaborate`. A kind prints the unit
alone, its functions included, so the dump observer writes one header per dump from the event,
`-- after <instance> [<label>] --`, in place of the header that each function's listing carried; the label names the
backend once branches exist, the table, and the function of a per-function step. `elaborate` makes a Core module with
the attributes of its nodes computed, since a kind's verifier checks the unit and never writes it, so the dump after
`elaborate` shows the attributes that the dumps after the Core passes show already.

1. **The new core of `internal/pass`** beside the old one: kinds, the pass constructor, the context, the step tree, the
   combinators, the manager with its copy policy and its events, labels that each combinator sets one field of, and
   `Changed` beside `TextChanged`. No user.
2. **Stage-typed Code IR units.** Core, Flow, Exec and target passes are declared with the new constructor unchanged;
   the context replaces the environments of the Core, Flow and Code IR passes, every analysis they query is an analysis
   key, and the per-segment keep declarations become the manager's policy. The driver's stages run `Execute` internally and keep
   their signatures. Copies do not increase, measured against the generation gate.
3. **The table unit** with its lenses; the lowering of a table becomes one `Execute` of a table pipeline; the origin map
   moves into the Core module; the planning and lowering data types move into their data packages. Until step 6 makes
   the compilation one `Execute`, the driver runs one `Execute` per stage or table, and these share one numbering of the
   decided runs (`Event.Seq`), so a compilation numbers its runs once, in the order they run; the shared counter becomes
   internal to the manager at step 6. Until step 5 plans the functions that the TypeScript printer lowers one at a time,
   the driver runs each through the same steps as a table of its own: a plan of its functions in order before
   `flowlower`, and a table that no plan holds before `execlower`.
4. **Planning as a program-level pass**, the expectation table numbered from the lowered tables where they are
   joined, and the Go generator prints once. Neither renumbers the expectation table nor changes the scopes of pruned
   rows, so the planning fallback is not needed; it stays the route if a later change makes planning differ from
   printing.
5. **TypeScript:** routes in the plan; the printer calls no pipeline. The TypeScript table is planned and lowered
   once, like the Go tables, with its outlined nodes chosen by the table's configuration, so its
   `-print-after` dumps show one run over the table instead of one run per function. (Amended: the optional pass
   `outline`, right before `plan`, chooses the outlined nodes in place of the table's configuration, so the choice is
   a named step of the pipeline text that `-no-opt outline` turns off.) The printability analysis lands
   with step 7, where the printers read the primitives that the TypeScript backend declares: built on those
   declarations, it needs no second list of the printed operations. (Amended at step 7: no grammar reaches a refusal
   of the TypeScript printer. A capture read from variables exists only in a flat function, which the plan gives no
   predicate and no value, and the other refused constructs come from the typed table, the Go-only forms of the Flow
   passes or streaming, none of which a TypeScript target has. So the analysis is the required step `ts-printable` on
   `tables@exec` right before `print-ts`: every function of grammar code calls only primitives of the set that the
   TypeScript backend declares, reads the captures of the frame in its predicates and binds, reads neither its start nor
   its action's context, and has variables only of the types the printer declares. A table without these properties is
   an internal error; the printer's refusals become panics of an admitted construct, and a test ties the printer's
   dispatch to the declared set in both directions. The facts that hold for every target, that a mark is set before it
   is reset, read or has its trail dropped, join the verifier of the Exec stage.)
6. **The driver assembles the Go and TypeScript pipelines.** The printers become passes in the backend packages; the
   driver's stage API, the pass and lowering entry points of the code generators and the hooks go, and their users move
   to `Compile` with edits, or to `FlowTable` where they need only the table. The architecture tests land with the rules
   that hold at this step: the caller rule and the export rules. The caller rule lists, in an allowlist that must match
   exactly, the callers that a later step removes (the Core passes of the run-time builds, step 12; the Go library,
   step 10) and one that stays: the runner of the Flow passes on a module in the flowpass package, for the tests of the
   Flow passes and of the lowering, since the package's own internal tests use it and a shared helper in test support
   would make an import cycle with them. No pass exports its run function; the tests of a single pass run it through
   `Execute`. The printers' test hooks get a rule of their own.
7. **Printers on Exec-stage units**, each change with identical output and dumps: the `prim` package; Exec-stage
   attributes with the verifier's rule, and the flat-function check beside the analysis of dead stores, which the plan
   and the Go printer call each on its own stage; the printers reading `prim` and the table unit; the lowering left in
   the printers as steps, with the TypeScript library's specialization, its library route for the library alone and the
   printing-pass kind rule with its negative control, and the import ban of the print packages, which lists the action
   lowering that the printers still call as its one exception; then the renames and the name rules, and the walk for
   Flow-stage units with its negative control. The action lowering leaves the printers in the change that lowers action
   expressions once in the pipeline, which comes next and makes the ban total.
8. **The text form:** the printer, the reader and the resolver with the skeleton and kind checks, the headers with their
   reflection tests and per-target dumps, `-print-pipeline`, and the internal reader for tests and a development tool.
   (Amended: `-print-pipeline` and the reader land for one target per call; the per-target dumps come with the fork of
   step 9. The driver's `Describe` returns the description of the pipeline without headers, and an error for options
   that the target rejects; the generation writes the headers, since the header scheme sits in its package, and reads a
   text back into the options of the build, whose pipeline text replaces the assembled one. A feature without a text
   form, such as the target, takes its value from the assembled pipeline at the same place, whose printed value must be
   the same, instead of making the text unreadable; the resolver takes a pass registered at several kinds, such as
   `canonicalize`, at the kind of its place; and the fallback of planning replaces the constant `reach` of the pipeline
   of the first run, so `-print-pipeline` prints the first run, which a build from the text repeats with its fallback.)
9. **Sharing, `Fork` and the `output` kind**, `Compile` over several targets, and the sharing of Core IR between a table
   and its traced copy. (Amended: a single target is not forked; the splits of the backends read the target of their
   branch as a declared feature, the program unit holding none; the dumps and statistics of a fork are grouped by
   target, the programs first; the fallback of planning reruns the fork with the reach set in the falling branches
   only, by the edit `InBranch`; and the errors of the failed targets are joined in target order, each naming its
   target. `Describe` prints the fork, while the headers of the generation and the command line stay single-target
   until step 11.)
10. **The Go library route**, with the library text door and `print-libgo`; the library printer joins the import rules.
    (Amended: the library kind, the `specialize` pass, the deletion of the exported specialization function and the
    printing-pass kind rule land with step 7, with the TypeScript library's route; until step 10 the Go library's
    printer runs the pass through its own pipeline. At step 10 the split of the cold paths becomes the required step
    `go-cold-path` of the route, the requests name their files, and the `go generate` program of the library printer
    writes the files that the route returns. The architecture tests read no file built only under the tag
    `ignore`, such as a `go generate` program, as part of the package of its directory.)
11. **Several targets from the command line and the API.** (Amended: the sealing method of `Target` turns a configuration and
    the options of the call into the driver's target, so `Generate` has no switch over the configurations. A generation writes the library alone (`-lib only`, `LibOnly`) for every target or for
    none. The pipeline text of several targets repeats the `target` header, one per target in order, and the build that
    the text reader returns holds the list of targets.)
12. **The VM and the closure backend through `Compile`** with the parser's settings: first the settings carrier, with
    unchanged outputs; then `ProgramOf` and the run-time builds through the driver, the `program` header and its reader,
    and the settings tests, which fail before this step. Outputs are identical under every profile and change only
    under an override of a Core pass. (Amended: the VM pipeline is `vm-tables` with the shared Core part, the
    program-level passes that run before `plan` for the generators, `plan!`, then `vm-planned-tables`, which lowers
    the one planned table to the end of the Flow stage for the bytecode target and joins it with the program into
    `vm-input`, and `vmlower!`. The VM's one unit plans the stream body of every rule with a `#stream` repetition, so a
    unit holds a list of stream rules (`Unit.Streams`); a Go parser keeps the one of its start rule. The closure
    pipeline is `closure-tables`, whose join gives `closure-input`, which holds only the Core module, and
    `closure-core!`, which returns it without the cancel control of the compilation; both targets build the module
    under the key `program`, so one compilation of both shares it. A compilation of run-time targets only runs with
    `Stop`. A cancellation is a failure of its own: the step that finds the context ended
    fails with it, and the compiler side of the program turns it into the parse's cancelled build. The `program`
    header writes the settings as the default profile with its overrides, and the run-time builds take no debugging
    settings, so their text is for tests.)
13. **Gates and documentation:** the generation-time gate, the run-time compile benchmark, the determinism and allocation
    tests, and the design pages updated for the parts this record implements. (Amended: the generation-time gate is
    the benchmark `BenchmarkGeneration`, over the grammars of `testdata/grammars` with the options of their
    ready-made parsers, for Go, TypeScript and both in one compilation; the run-time compile benchmark is
    `BenchmarkRuntimeBackendCompile`. The determinism test runs the generation of Go and TypeScript in one
    compilation, so the backends that generate run twice there; the VM and the closures, once step 12 moved them onto
    `Compile`, join it as a run-time build of both in one compilation. The allocation test covers steps on a module and steps run per function.)

The VM switch to the Flow route, which deletes the Core route, comes before step 12 as separate work and changes only
the VM pipeline: the VM selects its bytecode from the table that `FlowTable` builds. Step 12 then moves the VM, already
on the Flow route, onto `Compile`.

## Follow-Ups

- Text parsers for Core IR and Code IR, and a reproducer: on an internal error the manager writes the pipeline text and
  the unit before the failing pass, and a tool reruns that pass on it.
- Optimization remarks through the context.
- Events for computing and invalidating analyses, so that `-time-passes` can charge analysis time apart from the pass
  that asked for it. Until then it is charged to that pass.
- Parallel execution of per-function passes, keeping the observable order.
- A compile cache keyed by the sharing keys, for the language server and watch mode.

## Limitations and Open Questions

- The prototype does not cover the cancellation context, patterns, analysis queries, the semantics of the `debug`,
  `target`, `program` and `sxir` headers, `ProgramOf` and the run-time entry with its settings carrier, the command line
  and API over several targets, the planning fallback, the printers on Exec-stage units, the Go library route, or the
  allocation bound. Their shapes are fixed here; their behavior is specified, not demonstrated.
- A generation copies the Core module of each `Shared` entry once, for the rollback that the failure rule needs, where
  the current driver consumes it. The generation gate decides whether this is visible.
- Sharing is off under bisection and under unit hooks, so those runs do the full work.
- Go cannot restrict callers of exported functions; the rules on `Execute`, `EnterFlow`, `EnterExec` and the run
  functions hold because the architecture tests check them.
- Reading a pipeline text back stays internal: there is no public API or command-line flag for it.
