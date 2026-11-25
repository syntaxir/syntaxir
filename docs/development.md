# Development

This document states the rules a contribution follows: how the repository is built, which checks a change needs and
what every test must do. [Architecture](design/architecture.md#the-package-layout) describes the packages and
[Package Layering](design/layering.md) the import rules; each package's documentation (`go doc`) describes its own
contents.

## Modules and Generated Files

The repository holds two modules: `syntaxir.dev/sxir` at the root, and the documentation site `syntaxir.dev/sxir/site`
in `site/` ([website maintenance](site.md)). The committed `go.work` joins them, but a pattern such as `./...` matches
the packages of the current directory's module only: the root suite is `go test ./...`, and the site's is
`cd site && go test ./...`. The root module requires no other module, so a generated Go parser adds only
`syntaxir.dev/sxir` to its module's requirements. Tests that build generated programs write temporary modules outside
the checkout, which `replace` `syntaxir.dev/sxir` with a directory.

Generated files come only from their generators. A generator program lives beside the files it renders and runs through
a `//go:generate` directive of that package; there is no shared tools directory. Regenerate committed files with
`go run ./cmd/sxir` or `go generate`, never with a built `sxir`: a built binary stamps its version in the header, while
committed files carry `(devel)`, which a test checks. Edit a template rather than the page rendered from it.

The interface between generated Go parsers and their library is versioned by the
[library protocol](releasing.md#the-library-protocol). Its tests fail when the declarations change under the same
version, and their message says how to record the new one. A change of what the declarations mean, or of how generated
parsers encode their data, needs the same increase, which no test detects.

## Go Style

An import block has at most one blank line: between the standard library group and the group of all other imports,
the module's own included. Each group is sorted by import path, as `goimports` sorts it. A package whose path has no
dot in its first element counts as standard library. A test in `internal/testing/archtest` checks every tracked Go file
and every Go file the generators print, so a generator prints its imports in this form. `hack/fix-imports.sh [-n]
[path ...]` rewrites files to the rule; it also resolves an import-block merge conflict, by taking either side and
rerunning it.

## Testing

### Test Tiers

The tests come in two tiers:

- **The default tier**, `go test ./...`, runs on every change. It is meant to finish in about 3 minutes: the wall time of
  `go test ./...` with a warm build cache on [the reference machine](performance.md#the-reference-machine).
- **The long tier**, `go test -tags sxir_long ./...`, adds the files named `*_long_test.go` with `//go:build sxir_long`:
  negative controls and mutants that rebuild or rerun generated corpora, full corpora and full-size measurements. It
  includes every test of the default tier at the same or a larger size, so a run that needs it runs it instead of the
  default tier, not after it.

A new slow test goes to the long tier. A test whose run approaches the CI timeout moves there too, with a
representative subset kept in the default tier, together with the parity or invariant test that checks the unmutated
behavior. The default tier's subsets are fixed: they depend on test and case names only, so they do not change between
runs. `-timeout 45m` for the default tier and `-timeout 90m` for the long tier are safety bounds, not targets: Go stops
a package's tests after 10 minutes by default, which some packages still exceed on a loaded machine. CI's timeouts are
in [its workflow](../.github/workflows/ci.yml).

A runner of mutants, or of another negative control that rebuilds the code under test, runs only when something its
verdict depends on changed since the base revision that the gate exports. With no base, as in a plain
`go test -tags sxir_long`, every mutant runs; package `internal/testing/mutanttest` states the rule.

Tests that compare wall-clock times compare ratios measured within one attempt and pass if any of a few attempts does,
so a loaded machine slows them but does not fail them; a ratio that fails every attempt is a real growth.

### Choosing the Checks

Choose the checks by what a change touches. `hack/gate.sh [--base REV] LEVEL...` runs them in a fixed order, so that
every contributor validates the same way; its header describes its options. It checks the changes of `REV..HEAD`
(default `origin/main`), so commit first.

| Level | For | Runs |
|:--|:--|:--|
| `docs` | Documentation, specifications, the site | `go vet ./...`, and the site tests and a site build with its link check when `docs/`, `spec/`, `site/` or `README.md` changed |
| `code` | Go changes | Build, vet, and the default tier of the changed packages and every package that depends on them (`hack/affected-tests.sh`) |
| `engine` | The engine, a generator or the runtime | `code` with the long tier, the ready-made parsers, the reference tags when the engine packages are affected, and `go generate` with a check that the tree did not change |
| `full` | The long tier alone | The long tier of the affected packages |
| `all` | A release | The whole tree in the long tier |
| `auto` | Any change | The levels above that the changed paths need |

The gate runs only what a change can affect, and every step it leaves out is recorded with its reason, never silently.
Its conditions fail closed: when the changed files or the affected packages cannot be computed, every conditional step
runs and the tests cover every package. A mechanical rewrite (a rename, a formatting pass) is checked by its
invariants instead of a full run: build and vet under every build tag, generation without drift, unchanged test-file
counts per tag, and the tests of the packages edited by hand.

`go test` does not run benchmarks, so a change of the language rules can leave a benchmark's grammar invalid without
failing the suite. After such a change, run every benchmark once:

```bash
go test -run '^$' -bench . -benchtime=1x ./...
```

**Reference builds.** The build tags `sxir_budget_controls_reference`, `sxir_reference_*` and
`sxir_snapshot_*_reference` select the simpler or former form of an optimized mechanism, and
`sxir_snapshot_position_overlays` an opt-in representation that the default build has not adopted. Every build must
compile: test files that use helpers of untagged files keep each tagged build compiling, and a test whose feature a tag
removes skips under that tag. `hack/test-reference-tags.sh [tag...]` vets the engine packages and every package with a
tagged file, and tests them with `-short`, under every tag of the tree or the tags it is given; it takes tens of minutes, and a change of budget-control code, a reference variant or the tests of the engine
packages runs it.

**Ready-made parsers.** The ready-made parsers are in the separate
[alpha-parsers](https://github.com/syntaxir/alpha-parsers) repository, generated from `testdata/grammars/` with the
options of `testdata/grammars/generate.txt`. To check a change of the generator, the library or the grammars against
them, keep a checkout at `../alpha-parsers`, or name another directory with `SXIR_ALPHA_PARSERS`:

```bash
git clone https://github.com/syntaxir/alpha-parsers ../alpha-parsers
hack/sync-alpha-parsers.sh     # copy the grammars and inputs, and regenerate every parser
hack/test-alpha-parsers.sh     # check the checkout is in sync, then vet and test it
```

Both scripts use the working tree they are run from, not a pushed commit. `hack/test-alpha-parsers.sh` fails when there
is no checkout or when the checkout is out of sync; `SXIR_ALPHA_PARSERS=skip` skips it on purpose. The checkout's
`generate.go` is edited by hand. Commit the regenerated files in the alpha-parsers repository whenever a change alters
them.

### Writing Tests

- **Both forms of generated parsers.** A parser generated with limit controls and one generated without them are printed
  by different parts of the generator. Every test that builds generated fixtures declares which form it checks, through
  the helpers of package `internal/testing/codegentest`; a test that builds a fixture without declaring a form fails the
  run. `go test ./...` therefore covers both forms, and `SXIR_TEST_ORDINARY` (`0` or `1`) restricts a run to one form to
  reproduce a failure.
- **Engine tests** test package `engine` from outside it, through its exported API, and write grammars in SyntaxIR
  source. Their differential helper checks that the result is the same on every backend, with memoization off, in both
  position units, in recognition and with tracing.
- **Parallel tests.** A test calls `t.Parallel()` unless it measures heap or time in the test process, changes a
  process-wide setting, or shares mutable fixtures with other tests. A test that needs other tuning sets the test-only
  settings of its own parse, and a test that observes a generator passes hooks to its generation, rather than changing
  package state.
- **Goldens.** A change of what a golden file freezes is rewritten with `go test <package> -run <test> -update`, and
  its diff is reviewed.
- **Required tools.** The TypeScript and playground tests need Node.js 22.18 or later and `tsc`. A test that needs a
  missing tool fails and names it, so that a comparison cannot pass by not running; `SKIP_NODE=1` skips those tests on a
  machine without them.
- **Test support.** Code that the tests of several packages share lives under `internal/testing/`, one package per
  concern, named `<subject>test` when it serves the tests of `<subject>` ([its rule](../internal/testing/doc.go)).
  Non-test code does not import it; the import-graph test fails when it does.

The site has its own tests, described in [website maintenance](site.md#tests).

### Continuous Integration

GitHub Actions run a light check on every push to main: `go vet ./...` and the `go generate` step of the gate. The test
suites run only on demand: start [the CI workflow](../.github/workflows/ci.yml) by hand
(`gh workflow run ci.yml --ref <branch> -f suite=<suite>`) and choose a suite, such as `root`, `long` or `all`. They
complement the local checks and replace none of them. Timing benchmarks never run there, because shared runners give no
stable timings.

## Adding an Optimization

An optimization is a flag of the [pass registry](design/pass-pipeline.md#the-registry). To add one:

1. Declare the pass, lowering or library control, and register its flag from the package that reads it.
2. Add its entry to the optimization flag tests of the Go generator, which compare a parser generated without it with
   the engine and fail for a flag that changes neither the code nor the Core IR.
3. Add a row to the optimizations table of the [code-generation guide](guide/code-generation.md#optimizations).

`sxir gen -print-after all` shows the Core IR after each pass, and `-no-opt name` isolates a miscompilation to one
optimization ([Bisecting Passes](design/pass-pipeline.md#bisecting-passes)). Commits follow
[the commit message rules](commit-messages.md), which `hack/check-commits.sh` checks before a push.
