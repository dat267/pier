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
