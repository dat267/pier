# Lock inventory

Retained and retired locks, and the invariant that governs retirement.
Referenced from `AGENTS.md`.

The `tui/` and `coding/interactive/` refactor retired every UI-state mutex.
This is the authoritative inventory for that ownership boundary: retained UI
handoff, terminal I/O and logging locks, plus selected cross-goroutine `coding/`
collaborators. It is not an inventory of every core, provider or storage lock.
File paths intentionally omit line numbers so ordinary edits do not stale them.

`ai.EventStream.mu` protects its FIFO, waiters and terminal state. Completion
closes a shared notification channel so every result waiter observes the same
immutable result. Caller-supplied completion predicates and result extractors
run outside the mutex and may reenter `Push`/`End`; terminal admission stops
new pushes before extraction, while EOF waits for publication of the terminal
event unless `End` explicitly closes delivery. A terminal claim is made under
that mutex after predicate evaluation, so concurrently evaluated pushes cannot
both publish terminal events.

`ai.InMemoryCredentialStore.mu` protects the credential map and per-provider
completion-chain tails, never callback execution. Callbacks receive deep owned
snapshots and commit under the data mutex after their error/cancellation checks;
read/list calls expose only committed snapshots (D272). Modify/delete share FIFO
admission per provider, while unrelated providers progress independently. A
cancelled queued caller returns immediately; a small drain goroutine releases
its tail only after the preceding operation settles. Completed tails are removed
when still current, so completed provider IDs are not retained by the queue.

`agent.Agent.State` copies the pending-tool-call map under the agent mutex;
snapshots never expose the map that tool-start/tool-end events mutate. Successful
runs release their parent-context cancellation registrations only after their
listeners settle, without changing the reported abort status.

MCP listener registries retain stable entry pointers, so slice growth and
snapshot compaction cannot invalidate unsubscribe handles. Callbacks remain
outside their existing registry mutexes. `mcp.Client.mu` admits incoming request
cancellation before its handler starts on a worker; the stdio reader remains
free to dispatch cancellation notifications. Close snapshots and clears incoming
cancellation handles under the mutex, then invokes them outside it. User handlers
run outside locks and their panics are converted to JSON-RPC error responses.
No new mutex or interactive UI worker is introduced. In-memory transports use
that existing mutex to append copied messages and claim one FIFO delivery
worker per peer. The worker removes a message under the mutex, then invokes
listeners after unlocking; close discards queued messages without waiting for
a reentrant listener. Stdio transports own their output pipes instead of using
`Cmd.StdoutPipe`/`StderrPipe`, so `Wait` cannot close unread final output. The
watcher publishes process exit before joining pipe readers, allowing a reader
callback to call `Close` without waiting on itself; the final close event and
incomplete-tail check occur only after both readers finish. Pipe resources are
closed on startup failure and by their respective readers after startup.

`ai.idleStreamBody` marks close atomically before stopping its timer. Every timer
reset checks close again afterwards, so a final buffered read or an in-flight
timer callback cannot rearm a response that has already closed.

Settings storage uses a `settings.json.lock` directory, matching proper-lockfile.
It reclaims artifacts older than 10 seconds (including legacy regular files),
refreshes live leases every 5 seconds, and joins its refresher before release.
Release checks file identity before removing the lock. Storage callbacks and
file I/O remain off the interactive owner loop; no new UI-state lock is added.

D205 adds no lock: tool result generations, displayed lines and shell elapsed
state stay on-owner. Workers mutate only private component snapshots and return
owner-applied completions through D204's bounded optional queue and UI-post gate.
D206 extends those private snapshots through Box padding/background preparation.
Only the owner adopts the completed cache, rebinding cache and mouse metadata to
its original children; elapsed clocks and arbitrary callbacks never run on the
worker. No additional lock or queue is introduced.
D207's completed-frame retention, live clock-row refresh and displayed-width
mouse dispatch are owner-only state. Prepared and displayed metadata remain
separate, and parent render-version skipping cannot suppress pending admission.
D208 uses owner revisions for accepted pending clock updates, mutating only the
owner's retained frame rows. Unadmitted work remains unversioned and copies rows
when needed; detached worker snapshots remain immutable after handoff.
The tool frame lifecycle is consolidated in `coding/interactive/toolframes.go`;
callers supply captured input and delegate render/hit-test metadata without
reading its admission or publication state. The executor seam and lock inventory
are unchanged.
D209 extends owner-only in-place Container flattening to first-child changes.
Revision/change-offset metadata guards parent caches, and removed string slots
are cleared on shrink. No shared UI mutation or additional lock is introduced.
Syntax highlighting adds no UI lock or off-loop path: D231 wires the
`highlight` port in, `coding/interactive/codehighlight.go` is pure and calls the
`highlight` package, whose compiled-language cache is guarded by that package's
own mutex, so no UI lock or off-loop path is introduced.

| Lock | Where | Protects | Retirement |
|---|---|---|---|
| `Renderer.postMu` | `tui/render.go` | the posted-callback queue (`Post` is called from off-loop goroutines: loaders, watchers) | **retained**: serializes the handoff only; the owner drains it in the render pass and never runs a callback under it |
| `ProcessTerminal.writeMu` | `tui/terminal.go` | terminal writes, raw-mode transitions, Kitty negotiation bookkeeping shared with the reader | **retained**: not UI state |
| `ProcessTerminal.writesMu` | `tui/terminal.go` | the console-write FIFO, byte backlog counters, open-frame buffer, deferred metadata hints and cancellation-aware optional-admission waiters (`Write` appends essential output in submission order; a dedicated goroutine drains it) | **retained**: off-loop writer; the UI loop never writes to the console itself |
| `resizeWatcherMu` | `tui/terminal_windows.go` | the Windows console-resize poller's stop/done channels | **retained**: the poller runs off the loop |
| `prerenderState.mu` | `coding/interactive/transcript.go` | the transcript warm-ahead handoff (ready chunk, width, generation) | **retained**: loop ↔ warm-worker handoff, not UI state |
| `systemThemeState.mu` | `coding/interactive/theme.go` | the terminal-report colors the system theme is generated from (written by the terminal query reply path, read at theme load) | **retained**: a pointer swap of two values, never UI state |
| `terminalColors.colorMu` | `tui/terminalqueries.go` | the terminal-colors query queue the blocking caller appends to while the owner loop's input dispatch reads it | **retained**: query bookkeeping, not UI state; completed colors and delivery are snapshotted under the lock, then the callback runs after unlocking so it may start another query |
| `FooterDataProvider.mu` | `coding/footerdata.go` | cwd/git/status + listener registry, shared with its 500 ms git-HEAD watcher | **retained (D149)**: closing it needs the poll result posted to the loop and the listener fan-out delivered outside the lock; a `coding/` change outside this refactor |
| `AgentSession.promptOptionsMu` | `coding/agent_session.go` | `SystemPromptOptions`, re-read by the prompt/tool loadout between turns | **retained**: the work goroutine re-applies the loadout while the UI loop rebuilds the options on `/reload` and tool changes; only a struct copy is taken under it |
| `cellDimensionsState.mu` | `tui/terminalimage.go` | the global terminal cell pixel size, written by the size-report reply and read during image layout | **retained**: package-global value shared with the terminal query path, not UI state |
| `kittyRegistry.mu` | `tui/terminalimage.go` | the package-global kitty placement registry (order, id index, transmission generation) | **retained**: image-protocol bookkeeping, not UI state |
| `imageTranscodeState.mu` | `tui/imagecomponent.go` | the package-global image transcode cache and injected transcoder | **retained**: shared decode cache, not UI state |
| `modelCompletionCache.mu` | `coding/interactive/interactivemode_helpers.go` | the autocomplete model-item cache keyed by the session scope revision | **retained**: caches immutable item slices across completion queries, not UI state |

`StreamableHttpTransport.Send` links its caller context to the transport stop
context. Synchronous responses release that link on return; an SSE response
worker owns it through stream consumption. Closing the transport therefore
cancels both POSTs and GETs, without canceling a live SSE body at the initial
Send return or changing shared auth recovery ownership. No new mutex is added.

The typed `coding/rpcclient.RpcClient` snapshots request/listener state under
`mu` and serializes stdin writes under `writeMu`. Event callbacks run outside
both. Output readers own their pipe handles, so `Cmd.Wait` cannot close unread
final events. Natural process exit still rejects pending requests immediately;
`Stop` waits for that process-exit publication, not reader completion, since a
reader callback may itself call `Stop`. Startup reserves admission under `mu`
before spawning outside it, and releases that reservation on setup failure.
No callback queue or UI lock is added.

D235's stdio RPC runtime adds no UI lock. The existing `AgentSession.mu` also
guards steering/follow-up display queues across command admission and agent
events; snapshots are taken under it and listeners are called outside it.
`rpcRuntime.outputMu` guards only the first output error. The byte writer and
all callbacks run outside it. Prompt admission and cancellation handles belong
to the command owner; four pending preflights and an unbuffered byte handoff
bound concurrent work without unbounded queues.

Retired along the way (all struck from the code). Searching non-test Go files
in `tui/` and `coding/interactive/` for mutex declarations finds only the
retained locks listed above (UI-state handoff plus the package-global caches):

| Lock | Where | Retired in |
|---|---|---|
| `Renderer.renderMu` | `tui/render.go` | owner loop (input and painting share the owner goroutine) |
| `Renderer.mu` | `tui/render.go` | owner loop (loop-owned) |
| `Container.mu` | `tui/component.go` | owner loop (loop-owned) |
| `Editor.mu` | `tui/editor.go` | owner loop (loop-owned) |
| `AltScreen.mu` | `tui/altscreen.go` | owner loop (loop-owned) |
| `ScrollView.mu` | `tui/scrollview.go` | owner loop; the scrollbar hide timer is a lazy deadline |
| `Loader.mu` | `tui/selectlist.go` | stage 4 |
| `AltScreenFlashContainer.mu` | `tui/selectlist.go` | stage 4 |
| `StdinBuffer.mu` | `tui/stdinbuffer.go` | consumer-driven flush |
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
| `ModelCatalogRefreshCoordinator.mu` | `coding/interactive/catalogrefresh.go` | stage 4 |
| `activeCatalogRefresh.mu` | `coding/interactive/catalogrefresh.go` | stage 4 |
| `Lifecycle.mu` | `coding/interactive/interactivemode_lifecycle.go` | stage 4 |
| `StartupWiring.mu` | `coding/interactive/interactivemode_startup.go` | stage 1 (input handoff is a channel) |
| `Theme.mu`, `themeState.mu`, `trueColorState.mu`, `customThemesDirState.mu` | `coding/interactive/theme.go` | stage 4 (atomics / copy-on-write) |

When retiring a lock, the invariant is unchanged: no user code under a lock;
snapshot under and deliver outside.

The mutex-deadlock PTY watchdog flows consumed this list: while a UI
mutex existed, a flow could park the loop behind one, so the suite drove the real
binary through the offending interaction and checked the `SIGQUIT` dump. With
every UI-state lock retired, those specific deadlock paths are gone. Core
locks and blocking work can still stall the loop, so `internal/uiblock` guards
the owner-loop invariant statically. The watchdog flows' functional coverage
moved to `coding/interactive/ptyflow_test.go`, which drives the app
in process rather than spawning a PTY. Other real-binary PTY tests and the
deliberately deadlocking parser helper remain.
