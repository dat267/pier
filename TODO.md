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

## A484 persistEntry re-decodes every entry before the first conversation message

- Severity: medium (`AppendMessage` cost grew 5.8x across 1200 appends, all of
  it under `SessionManager.mu`; the same lock an idle bash append holds, so it
  compounds A483).
- Where: `coding/session_manager.go` `persistEntry` -> `hasConversation`.
- Port: `persistEntry` calls `hasConversation()` on **every** append while
  `writeStarted` is false, and `hasConversation` walks all of `fileEntries`,
  `json.Unmarshal`-ing each message's JSON to read its role. A session that
  has not yet seen a user or assistant message therefore pays O(n) per append,
  O(n^2) overall.
- Evidence: 1200 `AppendMessage` calls of a bash-execution message (a
  `message` entry whose role is neither user nor assistant, so the scan never
  short-circuits): first 200 appends 29.8ms, last 200 appends 173.0ms, **5.8x**.
  Isolated, `hasConversation` alone measured 899us at 900-1200 entries.
- Authority: pi v1.5.0 `f9a26365` `core/session-manager.ts:1167`
  `_hasConversation` reads `e.message.role` from the already-parsed entry, so
  it never decodes; the scan is linear in the array with no per-entry parse.
  Local invariant: one append's cost must not grow with entries already
  appended.
- Cause: the port keeps entries as raw JSON, so reading a role needs a decode,
  and the decision was recomputed instead of remembered.
- Fix: memoize the answer, kept exact incrementally. An entry that starts a
  conversation sets it true; any other entry leaves the previous answer
  correct, because the scan only looks for a user or assistant message. Bulk
  changes (reset, load) invalidate.
- Verification: `TestAppendCostDoesNotGrowBeforeFirstConversation` (5.8x before,
  flat after), `TestConversationMemoMatchesAFreshScan` (ordering and
  late-first-message cases), `TestConversationMemoRefreshesOnLoad`.
