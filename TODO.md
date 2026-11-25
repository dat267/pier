# A session switch reopens the target on the owner loop

Status: reproduced, not fixed. `internal/uiblock` fails on it.

`applySessionReplacement` (coding/interactive/sessionswitch.go:263) calls
`coding.OpenSession` on the owner loop when `SessionFileAppeared()` reports that the
outgoing run's last append created the target after the off-loop open. The reopen
exists for correctness (D493): it is the only way to load the settled tail and
avoid the `file exists` claim failure. But it runs the whole load on the loop:
`os.Stat`, `os.Chmod`, then `LoadEntriesFromFileBuffered` with its parallel scan,
which the loop then waits for.

Measured: 16.9ms for a 20,000-entry session, growing linearly, during which no
frame or key is processed. `internal/uiblock` reports it as a bare blocking
goroutine reachable from the loop:

    goroutine coding/session_store.go:959:4: blocking work on a bare goroutine
    reachable from the UI loop; use internal/offloop
      chain: runSessionFileWork.done -> applyOpenedSession ->
      applySessionReplacement -> OpenSession -> LoadEntriesFromFileBuffered

That chain is right, unlike my first read of it: `applyOpenedSession` itself does
not open, but `applySessionReplacement` does.

Why it is not fixed here: the reopen must happen AFTER `teardownCurrentSession`
settles the outgoing run, and that teardown runs on the loop. Moving the reopen to
the worker needs the worker to wait for a loop-side teardown, so it is a dispatch
change like the R quadratic (D-XXX pending), not an inline edit. Until then
`internal/uiblock`'s `TestBareBlockingGoroutinesAreAllowlisted` fails, so CI is red
on this alone.

# Automatic backgrounding of long tool calls# Automatic backgrounding of long tool calls

Status: implemented (D538). The plan below is kept because the order it chose is
why the fast path stayed byte-identical.

## What

A foreground tool call (shell or MCP) that outlives a threshold stops freezing
the turn. On expiry the harness:

1. moves the call to the background,
2. returns a handle to the model in place of the result,
3. keeps the process running with its output going to a file,
4. wakes the model once, with one notification, when the call ends.

The harness decides alone. No `run_in_background` argument, no key binding, no
`TaskStop`, no user- or model-requested move. The threshold covers those cases.

## Hard constraint

Transcript parity with pi v1.6.0 (`faac5858`) is non-negotiable:

- no new entry types, no new message roles;
- `CurrentSessionVersion` stays 3;
- the handle is an ordinary tool result (`toolResult`), and the wake-up is an
  ordinary user message, so both already have a shape the reference can read.

The reference has no equivalent: `packages/agent/src/agent-loop.ts` has no
timeout or backgrounding, so this is pier-only and needs a D-row.

## Design

### Where the clock starts

`executePreparedToolCall` (`agent/loop.go`) is the one place a tool runs, for
both the sequential and the parallel executor. The threshold belongs there, not
in a tool: a tool cannot know the harness's policy.

### Mechanism

Run the call in a goroutine. `select` on its completion and a timer:

- completed first: current behavior, unchanged, byte for byte;
- timer first: emit the handle result, leave the goroutine running, and register
  a completion callback.

The call's `ctx` must not be the run's `ctx`, or an abort would kill a
backgrounded process. It needs its own lifetime, tied to the session.

### The handle

An ordinary tool result whose text names the handle and says the call is still
running and that its output is being written to a file. `IsError` is false: the
call has not failed. The details carry the handle id so the UI can render it,
reusing an existing details shape.

### The wake-up

One notification when the backgrounded call ends, delivered as ordinary input
through the same seam `/loop` fires through, so it lands in the transcript as a
user message. It must happen exactly once per backgrounded call, including when
the call fails or the process is aborted.

### Output to a file

Reuse `OutputAccumulator`'s temp-file path from `coding/tools_bash.go` rather
than a second mechanism. An MCP call has its own result shape; its file holds the
serialized results.

## Order

1. Threshold plumbing on `AgentLoopConfig`, off by default, no behavior change.
2. The goroutine + timer in `executePreparedToolCall`, with a test that a fast
   call is byte-identical.
3. The handle result and its rendering.
4. Background registry with its own lifetime, and the single wake-up.
5. Wire the interactive host: threshold value, wake-up delivery, teardown.
6. D-row, README, and a test per behavior.

## Verification

- A fast call produces exactly the bytes it produces today.
- A slow call returns a handle, the process keeps running, and exactly one
  notification arrives when it ends.
- A slow call that fails still notifies once.
- Closing the session stops backgrounded work and leaves no goroutine.
- Goldens and transcripts stay byte-identical (existing fixtures).
