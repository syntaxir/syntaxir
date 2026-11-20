# Briefs, Reviews and Handovers

## The leader

- The leader decides, coordinates and reviews hand-backs. Its context is the largest, so
  each of its tool calls costs the most. Landings, issue edits, golden fixes, doc touch-ups
  and lookups go to members or light sidekicks.
- Pick the model by difficulty. Light models do mechanical work: rebases, landings, backlog
  edits and docs from exact instructions. Strong models do critical-path design, proofs and
  high-impact reviews. Reserve the strongest model for a single piece that stays stuck
  after normal attempts, with a narrow brief and the earlier evidence. Cut lanes, not model
  quality, when tokens are short.
- Do not poll running agents or jobs. Completion notices are the only wake signal: act on
  real hand-backs and ignore interim notices. A slot whose lane only waits on review,
  validation or the queue counts as free, so start the next parallel item.
- Near a token-limit reset, unused budget is waste: open every unblocked lane. Make every
  lane safe to stop at any moment. Push at each coherent step, and keep the handover and
  the issue checklist current after each step.
- Keep a documentation lane running beside the code lanes. It closes documentation gaps
  early, one commit per issue, in files that active branches do not touch. Every
  implementation brief still says "update affected docs in the same change". A full
  documentation audit gates the release.
- Run a stall sweep at least daily, and after every train or scope cut. Look for READY
  lines not on main, unmerged remote branches, issues In progress, Pending or In review
  with no activity for over 12 hours, milestone rows past their start date, open questions
  that wait on the leader, and a stuck queue. When you pause a lane, write its resume
  trigger in a Pending comment on its issue and in the waiting list of Issue #1.

## Briefs

A brief is the member's only context. It states:

- the goal, the issue and its acceptance criterion, the decisions in force with links, and
  what is already ruled out;
- the worktree, branch, base SHA and exclusive paths, and `.scratch/` as the scratch place;
- the check budget (this skill's [machine load](machine-load.md) and
  [landing train](landing-train.md) rules) and "update affected docs in the same change";
- the issue lifecycle steps the member must do, including dates and fields
  ([backlog skill](../../backlog/SKILL.md)), and for issue filing, the template and every
  project field;
- the [decision criteria](decisions.md);
- for any action the owner approved that a permission check may block, such as a push to
  another repository or a package rename: "Explicit owner approval (<date>): <action, scope,
  limits>", with the owner's words. A permission classifier judges the agent's own prompt.
  An approval that sits only in a shared rules file has been denied more than once. If an
  action is still denied, relay the approval and retry once. After that, the leader acts or
  hands the action to a fresh member whose brief carries the approval;
- the hand-back format: at most 12 lines with the branch and SHA, what changed, the exact
  checks with their exit statuses, and open questions.

For the backlog writer and other light sidekicks, give exact targets and one literal example line of the target
format. A light model copies the wrong style when it is told to "match existing entries".
Audit its result with a cheap diff or grep.

## Reviews

- High-impact changes get a critical review from a reviewer whose context is independent of
  the worker. Share the requirements, the pinned range, the diff and the validation
  evidence. Reuse existing reviewers. The report goes to `review/<lane>/review-<step>.md`
  with a verdict: LAND, or FIX with numbered findings. Rechecks append to the same lane
  directory.
- For a change whose correctness rests on an invariant, give the reviewer the invariant and
  ask for counterexamples in a throwaway copy. Random tests must cover the hard shapes per
  grammar (lookahead captures, empty results, large edit batches). New tests must fail on a
  deliberately broken variant (a negative control). Reviewers have found real bugs that
  randomized tests missed.
- Every finding, however small, gets a disposition: fixed, decided with a logged reason, or
  escalated to the owner. "Minor" is not a disposition.

## Handovers

- Keep the same member on the same stream until it is done. Automatic compaction handles a long
  context, and a fresh member spends more re-reading the state than it saves. Start a fresh
  member only for an independent review, a different task, or a member that ended confused.
  Keep the handover current at each commit anyway, so that a crash loses nothing.
- The handover lives at `review/<lane>/handover.md`: the worktree, branch and SHA (pushed or
  not), what is done, the next steps, the jobs queued with their log paths, and the open
  questions. A fresh member resumes from it plus the issue.
