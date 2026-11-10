# Reproduced audit findings

## A483 an idle bash append holds the run-lifecycle lock across a session-file write

- Severity: medium (blocks run starts and prompt submission for ~105 us per
  idle bash result; grows with session size and with a slow disk).
- Where: `coding/agent_session_bash.go` `recordBashResultAt` (commit `4262b919`,
  D438) and `coding/agent_session_prompt.go` `flushDeferredLocked`.
- Port: `recordBashResultAt` takes `state.mu` to decide idle-vs-deferred and,
  on the idle branch, calls `appendBashMessage` while holding it. That reaches
  `SessionManager.appendEntry` -> `persistEntry` -> `appendLine`, which writes
  the session file **synchronously on the calling goroutine when no write
  queue is wired** (`m.writeQueue == nil`). `flushDeferredLocked`, called from
  the run teardown with `state.mu` held, does the same for deferred bash
  messages.
- Evidence: 200 idle `RecordBashResult` calls on a real session with no write
  queue took 20.95 ms, **104.76 us each** of `state.mu`-held time. A
  controlled handshake (a goroutine acquiring `state.mu` while the appends
  ran) observed a stall above 200 us, proving the write happens inside the
  critical section rather than beside it.
- Authority: the local invariant in `ARCHITECTURE.md`'s lock inventory (no I/O
  under a retained lock) and `AGENTS.md` ("never run user code under a retained
  mutex"; the off-loop rule "nothing on the main event loop may block").
  Upstream `agent-session.ts` appends on one event loop with no lock and no
  synchronous file write, so this is a port-only consequence of the fix.
- Cause: D438 correctly moved the decision under the lock, but moved the
  append with it. The ordering requirement is on the *order* of the two
  stores, not on holding the lock across disk.
- Reachable: `ExecuteBash` (`coding/interactive/interactivemode_handlers.go:662`)
  is called from off-loop handler work; `cmd` and `rpcmode` wire a write queue,
  but the queue is `nil` for sessions created without one.
- Attempted and reverted: releasing `state.mu` before the idle append (the
  obvious fix) breaks D438's two-store ordering.
  `TestAgentSessionConcurrentAPIKeepsTranscriptAndPersistenceInStep` fails at
  -count=10 with the agent transcript and the session file disagreeing (e.g.
  agent `[custom bashExecution bashExecution bashExecution user assistant]` vs
  persisted `[custom bashExecution bashExecution user bashExecution
  assistant]`), because a concurrent writer interleaves between the two
  stores. Holding the lock across the append is what makes the pair atomic, so
  the cost is the price of the ordering guarantee, not an oversight.
- Remaining option, unverified: make the append cheap rather than unlocked.
  The measured cost is the synchronous `os.OpenFile`+`WriteString`+`Close` in
  `writeSessionLine` (plus the JSON marshal), so keeping an append-only file
  handle open, or ensuring the write queue is always wired, removes the disk
  cost while the lock keeps the ordering. Neither is a one-line change and
  neither has a test yet, so this stays open rather than patched.
- Verification: a test asserting `state.mu` is not held across the append (a
  concurrent acquirer must not stall) AND D438's ordering test green at
  `-count=10`. The naive fix satisfies the first and fails the second.
