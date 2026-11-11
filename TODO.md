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

## A495 — low: the A488 cancellation test cannot fail for the reason it asserts

- Category and affected paths: flaky verification test,
  `coding/session_listing_progress_test.go`
  `TestListSessionsWithProgressStopsOnCancel`.
- Scenario: `go test -race -count=5 ./coding -run
  TestListSessionsWithProgressStopsOnCancel` (the race shape the repo asks for on
  concurrency changes). Expected: green. Actual: "the listing read every file
  after cancellation: 12" on every run.
- Evidence: the fixture has 12 files and `currentSessionListPublishInterval` is
  10, so the first progress callback (where the test cancels) cannot fire before
  10 files are read; with 10 concurrent workers the remaining two are already
  dispatched, so `seen` reaches 12 by construction. The test passed without
  `-race` only because scheduling let the cancel land between files 10 and 12.
- Authority: the test's own comment ("a cancelled load stops instead of reading
  every file"); A488's design publishes partial results every 10 files.
- Root cause: the fixture is too small to leave work that cancellation could
  skip, so the assertion depends on scheduling.
- Repair plan: enlarge the fixture so a large tail remains after the first
  publish, keep both assertions, and re-run the race shape.
- Verification: `-race -count=5` on the test, then `just check`.
