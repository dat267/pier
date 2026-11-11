# Reproduced audit findings

## A493 — high: uiblock's traversal refuses to descend into pointer-receiver methods

- Category and affected paths: verification gap in `internal/uiblock`
  (`isPier`, the traversal's descent guard).
- Scenario: a loop root reaches a blocking call through any method with a
  pointer receiver, which is nearly every method of `*App`. Expected: the walk
  follows it. Actual: `isPier` accepts `github.com/dat267/pier/...` and
  `(github.com/dat267/pier/...` but not `(*github.com/dat267/pier/...`, so it
  returned false and the call was skipped. Only value-receiver methods were
  ever followed.
- Evidence: on `9d2070d2` (the tree that had A492), rooting
  `(*App).applyOpenedSession` or `(*App).switchSessionAsync$1` reported nothing,
  while a probe printed `isPier((*App).applySessionReplacement) = false`. The
  same tree's gate was green with 39 findings while a measured 2.5 s stall sat
  inside `(*App).reloadMCP`.
- Authority: the package doc ("the traversal follows static call edges (direct
  calls to named functions and methods)"), and `AGENTS.md`'s UI-ownership rule
  that this gate enforces.
- Root cause: the same value-receiver spelling as A490 and A491, in the one
  place that gates every descent.
- Repair plan: accept an optional `*` after `(`; extend the fixture with a
  blocking call behind a pointer-receiver method; triage whatever the corrected
  walk reports, allowlisting with a reason rather than by silence.
- Verification: fixture case red before the fix; module gate green afterwards;
  `just check`.
- Blast radius measured: fixing `isPier` takes the gate from 39 findings to
  about 240 across 20 chain surfaces. The largest are `wiring field ReloadNow`
  (44, `AgentSession.Reload` -> `reloadResources` reading skills, prompt
  templates, context files and auth storage), `runSessionFileWork ... done`
  (28), `wiring field WarnAnthropic` (21, `authstorage.go` reads),
  `wiring field ReloadMCP` (14), `Prompt` (14), `ExportToHTML` (13), `Stop`
  (11). Several are real blocking work the gate has never seen; each needs
  either a repair or an allowlist reason that is true today.
- Not done: the triage above. `isPier` is left unfixed with a TODO beside it so
  the gate stays green; the loop-callback rooting landed and is inert until this.
- Triage evidence gathered. The 20 surfaces split into two kinds, and the
  biggest are **false positives from the wiring-closure root rule**:
  `ReloadNow` (44) and `ReloadMCP` (14) are invoked inside
  `w.RunDetached(func(){...})` (`interactivemode_commands.go:370-390`), which is
  the port's off-loop seam for `/reload`; the analyzer roots every wiring
  closure as loop code, so it reports the detached worker's body. The inline
  branch below `RunDetached` is the test-app path only.
- The rest look like genuine loop work and need repairs, not exemptions:
  `via runSessionFileWork ... done` (28) is `bindSessionManager` doing
  `SetCwd -> FindGitPaths` (a `git` exec), `CreateAgentSession ->
  LoadModelConfig` (file reads) and `CreateBranchedSession -> buildIndex ->
  scanMessageFacts` (CPU over the session) on the loop after every replacement;
  `FooterComponent.Render` (4) estimates context tokens per frame;
  `ToolExecutionComponent.Invalidate` (5) decodes tool arguments; `Prompt` (3)
  appends a context edit; `OnModelCycleBackward` (2) sends over the RPC client;
  `WarnAnthropic` (21, startup) reads auth storage and can run a
  `!command` credential through the shell.
- So landing A493 needs three steps, in order: (1) the pointer-receiver form;
  (2) an off-loop seam rule so a wiring closure invoked only inside a detached
  worker is not rooted (the `/reload` pair); (3) repairs or reasoned allowlist
  entries for the remainder, with `bindSessionManager`'s rebind work first.

## A494 — medium: a session replacement's rebind redoes the expensive work on the owner loop

- Category and affected paths: performance on the loop in
  `coding/interactive/sessionswitch.go` `bindSessionManager`
  (`coding.CreateAgentSession`, `SessionManager` index/context scans,
  `FooterDataProvider.SetCwd`) reached from `applyOpenedSession`, which D442
  posts onto the loop after the file work finishes.
- Scenario: switch to, fork from, import or tail a session with 12,500 turns
  (25,000 entries, the size of this user's largest session). Expected: after
  the off-loop open, the posted rebind applies prepared state. Actual: the loop
  blocks for the rebind itself.
- Evidence: probe (deleted) that opened the file with `coding.OpenSession` and
  then timed only `app.applyOpenedSession`, which is the loop-side half:
  5 turns 0.88 ms, 3,000 turns 48.6 ms, 12,500 turns 222.3 ms, so roughly
  18 µs per entry and linear. The CPU profile is dominated by
  `encoding/json` scanning of every message, `ai.appendCanonicalJSONString`,
  and the resulting GC work (`scanObjectsSmall`, `tryDeferToSpanScan`).
- Authority: `AGENTS.md` UI-ownership rule; D442 moved the file work but
  assumed the rebind was cheap, which the measurement contradicts.
- Root cause: the rebind reconstructs session-derived state (agent session,
  model config, index, context projection) from entries instead of receiving it
  with the prepared session.
- Repair plan: build the `AgentSession` and the session index inside the
  off-loop work function that already produces the `*SessionManager`, and keep
  only attached-state rebinding (transcript, footer, events, MCP pointers) on
  the loop. Then re-measure the same three sizes.
- Verification: a test that the loop-side callback no longer scales with entry
  count, plus the existing switch/fork/import suites and `just check`.
