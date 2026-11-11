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
- Step 2 was implemented and does not fire, for a reason worth keeping: the
  seam rule skips a wiring field only when every call site of the field runs
  inside a detached closure, and `/reload` calls `ReloadNow` and `ReloadMCP`
  twice in one function, once inside `RunDetached` and once as the inline
  fallback taken when `RunDetached` is nil (`interactivemode_commands.go:399`,
  the test-app path). Statically that is a loop call site, so both fields stay
  rooted: 44 and 14 findings survive the rule.
- Step 3 measured with the pointer form applied: about 196 findings over 22
  surfaces, of which 63 sit under *definitive* roots (`runSessionFileWork ...
  done` 28, `EventDispatcher.HandleEvent` 11, `tui.Renderer.
  HandleTerminalInput` 10, `ToolExecutionComponent.Invalidate` 7, `TreeList.
  Render` 4, `FooterComponent.Render` 2, `RunWiring.Run` 1). So splitting the
  gate into "strict" and "inventory" halves would still leave 63 decisions that
  are not wiring-field judgement calls.
- Conclusion for the next attempt: A493 is a work item of its own, and the
  highest-value first slice is the 28 `... done` findings, because they are the
  post-replacement rebind and A494's fix already removed the largest of them.

## A503 — medium: the editor renders escape sequences from its buffer

- Category and affected paths: terminal-control injection in `tui/editor.go`
  `Render`/`layoutTextLocked`; the buffer it lays out can hold bytes the user
  never typed.
- Scenario: complete an `@` suggestion whose file name carries escape sequences
  (a cloned repository can contain one), or paste text containing them. The
  completion inserts the exact name so the path still works, and the editor then
  draws it. Expected: the port's rule for untrusted terminal text (D342, D422).
  Actual: probed directly before D451, `SetText("hi \x1b[2J…")` renders the
  sequences raw, and D451 fixes only the suggestion list that leads to it.
- Evidence: the D451 probe renders the list and, separately, an editor holding
  the same bytes; the list is fixed, the editor is not. `just check` cannot see
  it because no existing test feeds control bytes into a buffer.
- Authority: D342/D422, and D451's own reasoning: sanitizing where the bytes
  enter the terminal is the rule.
- Root cause: the editor assumes its buffer holds keystrokes, which can carry no
  raw escape byte; completions and paste are not keystrokes.
- Repair plan: needs a design decision, not a one-liner. Layout, cursor mapping,
  selection and highlighting all run over the raw buffer, so sanitizing at the
  output stage would misalign them; sanitizing on entry would rewrite paths the
  user needs exact. Options: a display-width-preserving replacement at layout
  entry, or an explicit "unrenderable bytes" marker per cell. The 76 editor
  goldens pin the current bytes, so whichever option lands needs a D-row.
- Verification owed: an editor test that a buffer with ESC renders no non-theme
  sequence, plus the existing goldens.
