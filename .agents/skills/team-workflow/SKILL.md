---
name: team-workflow
description: Run ZeroParse development as a team of parallel lanes on one machine. Covers the per-machine state directory, worktrees, the landing train and its gates, the heavy-run queue, briefs, reviews, handovers and Issue #1 sync. Use when you lead or join a multi-lane session, integrate branches onto main, or run heavy tests or measurements next to other agents.
---

# Team Workflow

Several lanes develop in parallel. Each lane is one stream of work with its own worktree,
branch and issue. One leader plans, decides and talks to the owner. The members implement.
One integrator lands ready branches on main in trains. Independent reviewers judge
high-impact work. A light backlog writer keeps the issues and Issue #1 current.

Durable state lives in remote branches and GitHub issues. A fresh clone on a clean machine
can work the same way. It does not resume someone else's intermediate files. The per-machine
state directory below is disposable coordination scratch.

## Set up a machine

1. Pick the state directory: `ZP_TEAM_STATE=${XDG_STATE_HOME:-$HOME/.local/state}/zp-team`,
   and create `wt/`, `runs/` and `review/` in it.
2. Copy the queue scripts (`benchlock.sh`, `benchstop.sh`, `testslot.sh`) from the "Team
   workflow scripts" comment on [Issue #1](https://github.com/zeroparse/zp/issues/1) into
   `$ZP_TEAM_STATE/bin/` and make them executable. They are coordination tools, not product
   tooling, so they are kept out of the repository.
3. Create `$ZP_TEAM_STATE/landing-train.md` with the format line from
   [landing train](references/landing-train.md).

Layout: `landing-train.md` (the live queue of candidates), `landing-train-archive.md` (history
moved out of it), `runs/` (queue records; only the scripts write them), `review/<lane>/`
(handovers, review reports, design notes), `wt/<name>/` (worktrees),
`benchlock-stalls.log` (a non-empty file means a queue bug).

## References

Read the ones your role needs.

- [Lanes, worktrees and commits](references/lanes-and-worktrees.md): every member.
- [Machine load and the run queue](references/machine-load.md): before you run any test,
  generation or benchmark.
- [Landing train](references/landing-train.md): before you submit READY, and for the integrator.
- [Briefs, reviews and handovers](references/briefs-reviews-handovers.md): the leader, reviewers,
  and any member that hands over.
- [Decisions and recommendations](references/decisions.md): before you recommend an option or
  ask the owner anything.
- [Issue sync](references/issue-sync.md): the leader and the backlog writer. The
  [backlog skill](../backlog/SKILL.md) defines the issue lifecycle itself.
