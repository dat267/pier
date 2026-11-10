# Reproduced audit findings

## A483 an idle bash append holds the run-lifecycle lock across the session-file append

- Severity: low, downgraded from medium. Re-measured after A484
  (`990d6d03`): one idle bash append holds `state.mu` for **p50 26us, p99 254us**
  (was p50 182us, p99 454us). A484's O(n^2) rescan was most of the original
  cost, not the append.
- Where: `coding/agent_session_bash.go` `recordBashResultAt` (commit `4262b919`,
  D438) and `coding/agent_session_prompt.go` `flushDeferredLocked`.
- Port: `recordBashResultAt` takes `state.mu` to decide idle-vs-deferred and,
  on the idle branch, calls `appendBashMessage` while holding it, which reaches
  `SessionManager.appendEntry` -> `persistEntry` -> `appendLine`. With no write
  queue wired, `appendLine` writes the session file synchronously.
- Evidence (re-measured, 400 appends each, conversation already started so
  `writeStarted` is true): with a write queue p50 17us / p99 181us; without one
  p50 24us / p99 128us. The queue moves only the file write, so the residual
  ~17us is the JSON marshal plus `consume`, both of which run under the lock by
  the write-queue contract (`session_manager.go:118-126`: "the line is
  marshaled synchronously (the caller's lock stays authoritative)").
- Authority: `ARCHITECTURE.md`'s lock inventory says no I/O under a retained
  lock; the synchronous write path violates that, the marshal does not, because
  the contract names it.
- Cause: D438 correctly moved the idle-vs-deferred decision under the lock but
  moved the append with it.
- Attempted and reverted: releasing `state.mu` before the append breaks D438's
  two-store ordering. `TestAgentSessionConcurrentAPIKeepsTranscriptAndPersistenceInStep`
  fails at -count=10 with the agent transcript and the session file disagreeing,
  because a concurrent writer interleaves between the two stores. Holding the
  lock across the append is what makes the pair atomic.
- Remaining scope: only the synchronous write when `writeQueue == nil`
  (~7us of the 24us p50, and the disk-dependent part of the p99 tail). Every
  production entry point wires a queue (`cmd/main.go:350`, `rpcmode.go:457`,
  `rpcmode.go:653`, the interactive rebinds), so the affected configuration is
  a session created without one.
- Verification: a test asserting `state.mu` is not held across the synchronous
  write AND D438's ordering test green at `-count=10`. The naive fix satisfies
  the first and fails the second.


## A485 an interactive session switch drops the write queue, so writes return to the UI loop

- Severity: medium (after `/resume`, `/fork` or import, session writes run
  synchronously on the calling goroutine, which for the interactive path is the
  owner loop — the exact condition `cmd/main.go:343-351` wires a queue to
  prevent).
- Where: `coding/interactive/sessionswitch.go` `switchSession` (`OpenSession`
  at :20, `a.sessionMgr = sessionManager` in `applySessionReplacement` at :133)
  and the import path at :80.
- Port: `OpenSession` (`coding/session_manager.go:257`) builds a manager with
  no `WriteQueue`, and `switchSession` never calls `SetWriteQueue` afterwards,
  although `sessionNew` (:122) explicitly forwards `WriteQueue:
  a.sessionMgr.GetWriteQueue()`. So a switch replaces a queued manager with an
  unqueued one and nothing restores it.
- Evidence: `grep -c SetWriteQueue coding/interactive/sessionswitch.go` -> 0.
  `cmd/main.go:350` sets it only for the initial manager, and its comment says
  why it must: "A resumed, continued or forked session is created without a
  queue, so wire it here rather than only on the fresh path — otherwise those
  sessions wrote synchronously on the loop." The interactive switch takes the
  same `OpenSession` path but lacks that wire-up.
- Authority: `ARCHITECTURE.md` ("Terminal output is serialized by an off-loop
  FIFO"; nothing on the main event loop may block) and the port's own
  `SessionManagerOptions.WriteQueue` contract: nil "keeps writes synchronous",
  which is documented as correct only for one-shot consumers, not the UI loop.
- Cause: the queue is treated as a property of the initial wiring rather than of
  the session manager, so every replacement path must remember to forward it.
  `sessionNew` remembers; `switchSession` and the import path do not.
- Fix: forward `WriteQueue` on every replacement, as `sessionNew` does — or
  read it in `applySessionReplacement` from the manager being replaced.
- Verification: a test asserting the manager installed by `switchSession`
  carries the previous manager's write queue.

