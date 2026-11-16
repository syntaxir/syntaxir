# Issue Sync

The [backlog skill](../../backlog/SKILL.md) defines the issue model, the lifecycle and the
Issue #1 page. This page says who does what in a team.

- **Members** keep the issues they work on current, in the same step as each event: start,
  decision, progress, ready and pause.
- **The integrator** runs the Landed/close step of the backlog skill for each landed issue
  right after the LANDED line, or hands it to the backlog writer in the same step, and
  comments the SHAs on any issue that is only partly done.
- **The backlog writer** (a light sidekick) owns the Issue #1 body. The leader sends it
  facts only: issue numbers, SHAs, outcome sentences, lane changes. It does the whole loop
  itself: fetch, edit, the issue-number set check, upload and a short report. Issue #1
  edits are pre-approved and need no confirmation.
- **The leader** keeps Issue #1 true as the team's shared state: the lanes in progress, what
  each waits on, the next steps and the order. A fresh machine must be able to resume
  planning from Issue #1, the branches and the issues alone. Link branches and issues
  instead of copying their content. Update it after every train, every lane start or pause,
  and every owner decision that changes the order.
- Category decides priority and ordering. Pick it by the definitions in the backlog skill
  before filing, never by impression; a moved item costs an audit.
- Every brief that starts, closes, files or rebases an issue names the field updates the
  backlog skill requires in that step. Sidekicks do not infer them.
