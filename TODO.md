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
- Not done in this session: the triage above. `isPier` is left unfixed with a
  TODO beside it so the gate stays green; the loop-callback rooting landed and is
  inert until this is fixed.
