# Lock inventory

Retained and retired locks, and the invariant that governs retirement.
Referenced from `AGENTS.md`.

The `tui/` and `coding/interactive/` refactor retired every UI mutex. What
remains in non-test code are narrow handoff and logging locks that never guard
UI state, plus one documented `coding/` exception:

| Lock | Where | Protects | Retirement |
|---|---|---|---|
| `Renderer.postMu` | `tui/render.go:143` | the posted-callback queue (`Post` is called from off-loop goroutines: loaders, watchers) | **retained (D146)**: serializes the handoff only; the owner drains it in the render pass and never runs a callback under it |
| `ProcessTerminal.writeMu` | `tui/terminal.go:169` | terminal writes, raw-mode transitions, Kitty negotiation bookkeeping shared with the reader | **retained (D147)**: not UI state |
| `ProcessTerminal.writesMu` | `tui/terminal.go:191` | the console-write FIFO (`Write` appends in submission order; a dedicated goroutine drains it) | **retained**: off-loop writer; the UI loop never writes to the console itself |
| `resizeWatcherMu` | `tui/terminal_windows.go:30` | the Windows console-resize poller's stop/done channels | **retained**: the poller runs off the loop |
| `prerenderState.mu` | `coding/interactive/transcript.go:105` | the transcript warm-ahead handoff (ready chunk, width, generation) | **retained**: loop ↔ warm-worker handoff, not UI state |
| `systemThemeState.mu` | `coding/interactive/theme.go:739` | the terminal-report colors the system theme is generated from (written by the terminal query reply path, read at theme load) | **retained**: a pointer swap of two values, never UI state |
| `terminalColors.colorMu` | `tui/terminalqueries.go:69` | the terminal-colors query queue the blocking caller appends to while the owner loop's input dispatch reads it | **retained**: query bookkeeping, not UI state |
| `inputLatencyRecorder.mu` | `coding/interactive/inputlatency.go:29` | keystroke-latency log appends from the off-loop logger | **retained**: log I/O only |
| `stallWriteMu` | `coding/interactive/interactivemode_run.go:610` | the stall-log append, shared by the UI goroutine and the watchdog timer | **retained**: log I/O only |
| `FooterDataProvider.mu` | `coding/footerdata.go` | cwd/git/status + listener registry, shared with its 500 ms git-HEAD watcher | **retained (D149)**: closing it needs the poll result posted to the loop and the listener fan-out delivered outside the lock; a `coding/` change outside this refactor |
| `AgentSession.promptOptionsMu` | `coding/agent_session.go` | `SystemPromptOptions`, re-read by the prompt/tool loadout between turns | **retained**: the work goroutine re-applies the loadout while the UI loop rebuilds the options on `/reload` and tool changes; only a struct copy is taken under it |

Retired along the way (all struck from the code; `grep 'sync.Mutex'
tui/ coding/interactive/` outside tests returns only the locks tabulated above):

| Lock | Where | Retired in |
|---|---|---|
| `Renderer.renderMu` | `tui/render.go` | D146 (input and painting share the owner goroutine) |
| `Renderer.mu` | `tui/render.go` | D146 (loop-owned) |
| `Container.mu` | `tui/component.go` | D146 (loop-owned) |
| `Editor.mu` | `tui/editor.go` | D146 (loop-owned) |
| `AltScreen.mu` | `tui/altscreen.go` | D146 (loop-owned) |
| `ScrollView.mu` | `tui/scrollview.go` | D146; the scrollbar hide timer is a lazy deadline |
| `Loader.mu` | `tui/selectlist.go` | stage 4 |
| `AltScreenFlashContainer.mu` | `tui/selectlist.go` | stage 4 |
| `StdinBuffer.mu` | `tui/stdinbuffer.go` | D147 (consumer-driven flush) |
| `negotiationResult.lastDataMu` | `tui/terminal.go` | stage 3 (reader stamps an atomic) |
| `KeybindingsManager.mu` | `tui/keybindings.go` | stage 4 |
| `globalKeybindingsState.mu` | `tui/keybindings.go` | stage 4 |
| `kittyProtocolState.mu` | `tui/keys.go` | stage 4 |
| `lastEventTypeState.mu` | `tui/keys.go` | stage 4 |
| `widthCacheMu` | `tui/width.go` | stage 4 |
| `ModelSelectorComponent.mu` | `coding/interactive/modelselector.go` | stage 4 |
| `ScopedModelsSelectorComponent.mu` | `coding/interactive/scopedmodelsselector.go` | stage 4 |
| `SessionSelectorComponent.mu` | `coding/interactive/sessionselector.go` | stage 4 |
| `scheduleOnce` local `mu` | `coding/interactive/sessionselector.go` | stage 4 |
| `editCallComponent.previewMu` | `coding/interactive/toolrenderer_edit.go` | stage 4 |
| `FooterComponent.cacheMu` | `coding/interactive/footer.go` | stage 4 |
| `ModelCatalogRefreshCoordinator.mu` | `coding/interactive/catalogrefresh.go` | stage 4 (D148) |
| `activeCatalogRefresh.mu` | `coding/interactive/catalogrefresh.go` | stage 4 (D148) |
| `Lifecycle.mu` | `coding/interactive/interactivemode_lifecycle.go` | stage 4 |
| `StartupWiring.mu` | `coding/interactive/interactivemode_startup.go` | stage 1 (input handoff is a channel) |
| `Theme.mu`, `themeState.mu`, `trueColorState.mu`, `customThemesDirState.mu` | `coding/interactive/theme.go` | stage 4 (atomics / copy-on-write) |

When retiring a lock, the invariant is unchanged: no user code under a lock;
snapshot under and deliver outside.

The D136–D139 mutex-deadlock PTY watchdog flows consumed this list: while a UI
mutex existed, a flow could park the loop behind one, so the suite drove the real
binary through the offending interaction and checked the `SIGQUIT` dump. With
every UI-state lock retired, no such flow can park the loop; `internal/uiblock`
carries the invariant statically instead, and the watchdog flows' functional
coverage moved to `coding/interactive/ptyflow_test.go` (**D192**).

