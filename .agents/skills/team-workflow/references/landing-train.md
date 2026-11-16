# Landing Train

Full gates are expensive: the long-tier root suite, generation and the alpha-parsers check
together take hours. They run only on train tips, never once per lane.

## Queue file

`$ZP_TEAM_STATE/landing-train.md` is append-only for lanes. It starts with this line:

```text
Format: READY <branch> <sha> | scope | targeted checks | leader go (yes/pending)
```

- **READY**: a lane appends `READY <branch> <sha> base <sha> | <scope, Refs #N> | <checks
  run, each with its exit status> | <review verdict file>`, then messages the integrator. A
  new READY line for the same work says which line it supersedes.
- **LEADER go**: the leader appends `LEADER go <branch> <sha> (<why: review verdict, perf
  result>)`, and names the train when ordering matters. A candidate enters a train only after
  its acceptance conditions are met: review, perf and the leader's go.
- **Other lines**: `WITHDRAWN`, `HOLD`, `NOTE` and `ANNOUNCE` (for example a rename that
  every lane must rebase onto). The integrator writes `TRAIN-<n>` and `LANDED train-<n>
  <date>: main <old>..<new> (<commits, issues>)`.
- The leader moves finished history (LANDED, WITHDRAWN and superseded lines) into
  `landing-train-archive.md`. Do it in one quick read-modify-write, and re-read the file
  right before writing so that no concurrent append is lost. Keep every current READY line,
  every LEADER line and every open note.

## Lane checks before READY

- Targeted checks: `go vet`, then the changed packages and their dependents
  (`hack/affected-tests.sh [base]` is advisory). Follow the throttling rules in
  [machine load](machine-load.md).
- Lanes do not run `go generate ./internal/codegen/... ./bench` or
  `hack/test-alpha-parsers.sh`; the train gate does. When the generator changed, diff the
  `zp gen` output of the affected grammars against main.
- A change to evaluation charges, emitted code or generated output runs the golden and
  mutant tests that pin them, even if they sit in the long tier. Run them targeted:
  `-tags zp_long -run 'TermCharges|OutputManifest|Mutant|DetectsMutation|Charges'` in the
  affected packages. Updated goldens land in the commit that changed them, with the reason in
  its body.
- A new or changed test that asserts charges, budgets or limits runs under
  `hack/test-reference-tags.sh`. Budget controls compile out in the reference build, so skip
  only the charge assertion there and keep the behavior checks. Missing this once failed a
  train after a two-hour gate.
- A change that removes or renames an exported name that alpha-parsers imports runs
  `go vet ./...` in a private alpha-parsers clone pointed at the branch. It lists the
  hand-written alpha files to edit in its READY line. Hand-written alpha-parsers edits need
  the owner's OK for each landing.
- Lanes never use the integrator's alpha-parsers checkout. Clone it into `.scratch/alpha` and
  set `ZP_ALPHA_PARSERS` to the clone.
- Profile structural and feature changes before READY (see the
  [performance skill](../../performance/SKILL.md)).

## The integrator

- Stacks ready candidates with a go on current main in a train worktree, in the order the
  leader set. Runs one full gate on the tip through the queue, as one script that prints
  each step's exit status: `go build ./...`, `go vet ./...`,
  `go test -count=1 -timeout 0 -tags zp_long ./...` (the long tier contains the default tier,
  so do not also run the default suite), the site tests and `site/build.sh`,
  `go generate ./internal/codegen/... ./bench` with no diff, `hack/sync-alpha-parsers.sh` and
  `hack/test-alpha-parsers.sh`, and `hack/test-reference-tags.sh`.
- On a pass: push main, append the LANDED line, regenerate and push alpha-parsers when the
  protocol or generated output changed (generated files only, never force), run the backlog
  close step, and delete the landed remote branches.
- On a failure: bisect with targeted checks. Send the failure to the owning lane, or fold
  a small fix into its owning commit with the lane's agreement.
- Trains run about twice a day and take every READY item with a go. After every train, list
  the READY items it left out, and give each one a rebase owner in the same step. Items left
  out of several trains fall far behind and rot.

## Fast lane

A candidate that changes only docs, spec, site or skills, or only moves or edits test files
(no non-test Go code), takes the light gate from `AGENTS.md`: `go vet ./...`,
`cd site && go test -count=1 ./...` and the tests of the packages whose files changed. It
skips zp_long, generation and alpha. It may land between trains. A train then rebases onto it
without rerunning its gate when the intervening commits are fast-lane only. Before queuing a
full gate, check its scope against what the batch actually touches.
