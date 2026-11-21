// Package tsconformtest runs the Exec IR conformance suite (internal/ir/code/exec/conform) on the
// generated TypeScript library, by trace replay.
//
// The harness of package conform runs a case on the reference model and on an implementation in
// lock-step. The TypeScript library lives in another process, so a case runs in three passes:
//
//  1. The harness runs the case against a Recorder, an implementation that is a second copy of the
//     model. The Recorder logs every call of an operation, with its arguments, as a trace. Because
//     it agrees with the model, the harness makes the calls the case makes and no others.
//  2. Node replays the trace on the library through the bindings of harness.ts, and answers each call
//     with what the library did: its results, whether it interrupted the parse, the budget it had
//     used, and the state that the harness inspects after calls (the recovered errors, observations).
//  3. The harness runs the case again against a Replay, an implementation that returns the answers of
//     the library in order. Every comparison of package conform (results, handles by identity,
//     interruptions, the cost bound of each call, exact costs, recovered errors, observations)
//     applies to the library unchanged, and a difference is the usual *conform.Failure.
//
// A Replay answers the calls in the order of the trace: when the library differs at call k, the
// harness stops at k and never reaches the calls after it.
//
// A family may list operations whose cases the library does not run yet (Family.Pending): the runner
// reports those cases as pending, not as passed. No family lists one now.
package tsconformtest
