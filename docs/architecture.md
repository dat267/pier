# Architecture and performance notes

Deep reference for the port's runtime architecture and the performance work
behind it. `AGENTS.md` keeps the rules; this file holds the narrative and the
measurements needed only when working on these areas.

## Concurrency architecture (UI event loop refactor)

The interactive mode uses a single-writer UI loop, replacing the earlier
mutex-guarded shared state (upstream is single-threaded). Input dispatch,
painting and UI mutation share the owner goroutine; producers only hand off
work or data. UI-state locks and the internal render timer have been retired
(D146/D147). Narrow handoff and I/O locks remain; `docs/locks.md` is the
authoritative inventory. The sections below describe the completed stages,
with historical measurements retained to explain the design.

### Session events and background work (stage 1)

- **`sessionEventQueue`** (`interactivemode_eventqueue.go`) decouples the
  session-event producers from the UI. Two buffered channels:
  `lossless` (cap 256: message start/end, tool end, agent settled, compaction,
  retries) and `partial` (cap 8: `message_update`, `tool_execution_update`,
  `bash_execution_update`), where partials are latest-wins with an explicit
  drop-oldest policy so a fast token stream cannot back-pressure the agent or
  starve input. The subscription callback only enqueues; producers never touch
  UI state or UI locks. `Close` releases a producer parked on a full channel.
  The channels do not preserve ordering across event classes. **D196** stamps
  assistant starts and updates with a UI-local generation; both loop-side
  application paths reject a partial from another stream. A partial that
  arrives before its start can be dropped because message end carries the
  complete content.
- **`runLoop`** (`interactivemode_run.go`) is the single writer: a `select`
  over the two event channels, the submission channel
  (`StartupWiring.inputs`, cap 64), work completion, and `ctx.Done()`.
  Blocking work (a turn, the compaction-queue flush, which can start a turn)
  runs in its own goroutine through `RunWiring.RunWork`, so a turn's events
  drain while it runs; the loop only accepts submissions when idle, matching
  upstream's awaited prompt. Initial messages are seeded as pending loop work
  ahead of submissions.
- The main run-loop select has a `ctx.Done()` arm. Pre-paint event drains use
  non-blocking selects with a 64-event budget (D197); they return to the main
  loop instead of waiting for a channel to become empty. Blocking event and
  terminal-input sends select on cancellation and queue/input closure. The
  standalone startup/picker loop exits when its selection is settled, not
  through the app's run context.
- **The main loop must never stall more than 100 ms.** Any handler, render path
  or watcher callback that can exceed the budget moves its blocking or heavy
  work off-loop (`RunWork`/detached work) or chunks it.
  `TestStreamingDeltaCostIndependentOfTranscriptSize` bounds the streaming delta
  path against transcript size. Session event drains are bounded (64 per pass),
  paints coalesce, and large markdown/tool work prepares off-loop (D204/D205).
- Heavy tool-side commands (test suites, builds) run `nice`d: on a 4-CPU machine
  an ungated test run competes with the UI goroutine and looks like a freeze.
- The loop calls a **watchdog beat** once per iteration
  (`RunWiring.LoopBeats`, read by the app's `loopBeats` test seam): a stalled loop stops
  advancing it, so a watchdog can detect a hang.
- **Blocking work runs off the loop on `internal/offloop` queues** (one
  goroutine per domain, strict submission order, optional keyed coalescing):
  session file writes, settings persists, theme loads, the transcript
  pre-render, and clipboard/paste. An `offloop.Group` owns the queues of one
  composition root, so teardown uses `StopAll` rather than a hand-written
  enumeration. **D199** separates mandatory session/settings writes (`Queue`)
  from optional theme loads, transcript warming and keyboard clipboard reads
  (`OptionalQueue`). `StopAll` rejects new work and cancels optional contexts
  before waiting for every accepted mandatory save. Optional waiting tasks are
  discarded; a running worker that ignores cancellation does not delay exit.
  `App.StopMode` and `App.Close` both close the owned queues. Theme completions
  and paste inserts check cancellation again when their UI posts execute;
  canceled loads and warms do not publish late results.
  The queues are opt-in: a nil queue keeps the synchronous path for one-shot
  CLI/SDK consumers and tests, and the interactive wiring is what opts in.
  Offloading alone is not a backlog bound. **D201** caps clipboard copies at
  four accepted jobs (one running, three waiting), detached jobs at four parallel
  slots with no pending list, and theme loads at one running plus one newest
  pending selection shared by settings and previews. Excess copies/detached jobs
  report busy status; detached rejection notices coalesce while pending, and
  rejected reloads restore the editor. Existing paste coalescing and one-chunk
  transcript warming remain unchanged. These are per-domain count bounds, not
  byte caps or a global queue/output limit. Mandatory `Queue.Stop`
  still drains accepted saves without a deadline. **D200** extends optional
  ownership to `/copy`, fullscreen selections, OAuth URL copies and explicit
  detached bash/compaction work. Clipboard subprocesses receive cancellation
  contexts, and right-click reads run off the input loop. The mode's forwarding
  UI reference rejects posts at both submission and execution after teardown.
  Optional cancellation cannot forcibly interrupt filesystem reads, component
  preparation, legacy providers or console syscalls; such workers may finish
  later. Legacy standalone clipboard APIs retain their defaults. Explicit
  `Flush`/`FlushAll` still wait for running optional work and are not used to
  cancel it.
  `Queue.Backlog` snapshots queued and running task counts;
  `ProcessTerminal.WriteBacklog` snapshots queued bytes, bytes in the writer's
  current batch, and bytes in an uncommitted frame. Both are O(1) observations
  under existing handoff locks, not memory-allocation measurements or admission
  limits. The blocked-worker regression tests hold a worker/console on a
  channel, measure retained work, then release it and verify lossless ordered
  draining. The queue shutdown test uses `testing/synctest` to prove `Stop`
  waits and rejects later submissions without a wall-clock sleep.
- **D143** records the divergence: upstream is single-threaded (await +
  microtask order), the port is an explicit select loop with off-loop work and
  partial coalescing.

### Input, signals and terminal I/O (stage 3)

- **Terminal producers.** A raw-capable terminal posts stdin chunks on
  `loopRawInputs` (cap 256); the loop feeds the input buffer and dispatches
  complete sequences. Other terminals post sequences on `loopInputs` (cap
  256). The resize watcher posts to `loopResizes` (cap 1); the lifecycle's
  signal handlers post to `loopSignals` (cap 4, non-blocking so shutdown
  signals coalesce). `Renderer.EnableLoopInput` (wired
  in `App.newLoopTui`, so renderer swaps keep it) stops the renderer from
  dispatching input inline; the run loop calls `HandleTerminalInput`, renders
  on resize, and runs the signal shutdown work on its own goroutine.
  `ProcessTerminal` caches the terminal size (`Start` primes it, the watcher
  refreshes it): the render path calls `Columns()`/`Rows()` and must never
  make the console query itself — on Windows that query is slow and blocks
  during a mouse selection, which stalled every paint 100–400 ms. Windows has
  no SIGWINCH, so its resize watcher is a 200 ms poller (off the loop); a
  refresh repaints only when the size actually changed. Console **writes** run
  on their own goroutine too: `Write` appends to a FIFO the goroutine drains,
  so console backpressure parks the writer rather than the UI loop. Windows
  Terminal stops draining the pty while a mouse
  drag-selection is active, and a synchronous write parked the UI loop for the
  whole duration of the drag; `Stop` flushes the queue before restoring the
  terminal, and `Renderer.Stop` flushes **again** after its post-stop hook, which
  enqueues the alt-screen exit: the resume hint is written straight to stdout, so
  without that flush it raced the writer and landed on the still-active alt
  screen, mangled into the last frame (pinned by
  `TestRendererStopFlushesAfterThePostStopHook`). **D200** scopes graceful
  permanent CLI teardown to one 2 s output grace, started after mandatory saves drain.
  Every stop, replay, post-stop flush and optional resume hint shares that
  deadline. A timeout skips the hint and permits exit with undelivered display
  output; the saved session is unaffected. A successful hint uses the FIFO and
  remaining budget rather than direct stdout. Finalization rejects later writes
  and lets the writer drain and exit if the console recovers. Temporary stops
  remain unbounded/lossless, and an explicit `FlushWritesContext` can request
  a caller-controlled wait without discarding queued bytes. The renderer brackets each
  paint (`BeginFrame`/`EndFrame`) so a
  paint's writes are submitted as one ordered batch. Frames are **not dropped**
  when superseded: the screens are differential, so a queued frame is still
  needed to reach the state the next diff was computed against. Dropping one
  lost its content (the startup trust prompt vanished this way, caught by
  `TestResumedSessionAsksTrustForItsOwnProject`); the async writer, not frame
  dropping, is what keeps a paused console from blocking the loop.
- The `DrainInput` last-input tracking is an atomic stamp written by the reader
  instead of an `OnData` swap from another goroutine (one lock retired).
- **`/compact` no longer blocks the UI**: the command is split into
  `ClearCompactionStatus` (loop side) and `CompactSession` (off-loop through
  `RunWiring.RunWork`), so the loop keeps dispatching input — including the
  advertised ESC cancel — while the summarization runs.

### Rendering on the owner loop (stage 2)

- **Every renderer is caller-driven (D146).** Construction creates the
  capacity-1 `renderTicks` channel. `RequestRender` coalesces onto it; the owner
  consumes requests and calls `RenderNow`. There is no internal render timer
  or renderer-owned throttle. `EnableRenderTicks` and `DisableAutoRender` are
  compatibility no-ops. The interactive app owns its frame schedule; the
  standalone picker and startup selectors use `RunStartupScreenLoop`, while
  library clients must drive their own input/render loop.
- The run loop selects on `UI.RenderTicks()`, applies a bounded batch of
  queued session events (`RunWiring.drainReadyEvents`) and paints once
  (`renderUI`), so a small burst produces one render, not one per event.
  **D197** caps each drain at 64 events and gives both ready event classes a
  turn per round. Continuously refilled channels cannot keep the loop inside
  a pre-paint drain or starve partial updates there; remaining events wake
  subsequent iterations, allowing input, cancellation and watchdog beats.
- **D198 output backpressure defers painting, not input.** The interactive
  schedule uses the terminal's optional lock-free `PendingWriteBytes` snapshot
  (queued plus in-flight committed bytes). At 256 KiB it pauses new paints;
  it resumes at 128 KiB, retrying a retained paint on the existing timer every
  16 ms without requiring a new render request. Input, session events and
  cancellation continue. Committed differential frames and durable writes
  remain lossless. One large frame can overshoot the threshold, and direct
  generic SDK writes remain unbounded. **D202** adds admission for interactive
  OSC 52 packets on clipboard workers, and newest-pending title/progress hints;
  essential protocol/lifecycle writes do not wait. The inventory, packet limits
  and oversized-title exception are in [terminal-output.md](terminal-output.md).
  Startup screens and standalone renderer clients are unchanged.
- **D203 transcript resize preparation yields cooperatively.** The composed chat
  document retains its last complete frame while warming at most 64 children
  per Render call for the new width. It requests continuation, prevents ancestor
  cache skipping while pending, and restarts on width/child changes. Attached
  components remain exclusively owner-loop state. `BenchmarkOwnerLoopContent`
  measures paste, tool-update and resize phases; the resize subbenchmark times
  the first chunk, completing remaining chunks outside its timed region.
  Profiling 500 message pairs found a 35.53 ms/10.48 MB resize before this change;
  the first chunk afterward is 1.45 ms/0.59 MB (Linux/amd64, 10 iterations).
  This bounds width-cold child count per call, not individual child latency,
  total resize work, initial cold renders or multiple layout calls in a paint.
- **D204 large assistant Markdown uses detached snapshots.** At 64 KiB, built-in
  text/thinking blocks render on an app-owned optional worker queue (four total
  admitted jobs). The owner keeps complete prior lines or a cold pending label;
  worker completions pass content/width/theme generation checks and mode-lifetime
  UI-post guards. Only private immutable snapshots are mutated on workers.
  Detached `Prepare` remains synchronous, while custom transformers retain their
  existing contract. The single-message benchmark measured 296.63 ms/114.11 MB
  cold work versus 0.024 ms/3,704 bytes owner admission (Linux/amd64, three runs).
  Admission timing uses a fake sink, not an end-to-end claim. Background parse,
  GC, final flattening and plain-text tool-result work are not reduced by it.
- **D205 large built-in tool results use private snapshots.** Text-only expanded
  read and both bash/powershell display modes at 64 KiB share D204's four-job
  optional queue. Workers sanitize, style and wrap captured output; elapsed
  timers and generation checks remain on-owner. Completed output is identical
  to synchronous rendering. Unknown metadata objects, JSON metadata above
  16 KiB, image-bearing results and custom callbacks remain synchronous.
  Detached transcript warming also remains synchronous. A 1,050,000-byte
  expanded fixture measured 60.31 ms/28.39 MB cold read work and 59.00 ms/23.18 MB
  cold bash work, versus 0.077 ms/21,216 bytes and 0.034 ms/5,592 bytes for owner
  admission (Linux/amd64, three runs, admission-only sink). This moves formatting
  and wrapping off-owner, not total work, GC or final container flattening.
- **D206 tool completion adopts a prepared Box frame.** Eligible D205 tools with
  built-in headers/default shells finish padding and backgrounds on the same
  private worker. Header lines, theme and clock labels are captured on-owner;
  generation checks guard cache adoption, which preserves original owner mouse
  targets. Custom headers/self shells retain their current contracts. A
  1,050,000-byte fixture's owner completion/render pass dropped from 15.90 ms
  (read) and 14.65 ms (bash), about 10.63 MB/90,000 allocations, to 1.47 ms and
  0.994 ms, 1,024,312 bytes/8 allocations each (Linux/amd64, three runs). Worker
  work and construction are excluded; this is not end-to-end latency. Timer
  advances repaint only the trailing rows. Final flattening, large headers,
  retained-output repaints after invalidation and global GC remain limits.
- **D207 pending tools retain the complete frame.** Eligible D206 tools admit
  replacement work without repainting retained raw output after content, width
  or theme changes. Completed header/padding/background stay visible; only live
  clock rows change. Display metadata is separate from prepared metadata, and
  mouse dispatch uses displayed width. Pending tools do not report a stable
  version, so skipping parents still retry busy admission. For a 1,050,000-byte
  fixture, pending owner work dropped from 14.83 ms (read)/15.88 ms (bash), about
  10.15 MB/90,000 allocations, to 0.061 ms/1,997 bytes/48 allocations and
  0.039 ms/1,800 bytes/43 allocations (Linux/amd64, three runs). Initial/worker
  preparation is excluded, not an end-to-end latency claim. Large headers, clock
  slice copies, final flattening and global GC remain limits.
- **D208 accepted pending clocks update in place.** Only unadmitted/busy work
  remains unversioned to force parent-cache retries. Accepted pending work uses
  owner revisions and reports the changed tail row, so live clock rows can update
  without copying the complete retained frame. For a 1,050,000-byte fixture,
  isolated clock tick/render dropped from 1.055 ms/484,070 bytes/17 allocations
  to 0.002456 ms/684 bytes/16 allocations (Linux/amd64, 20 runs). Worker work,
  parent flattening and end-to-end input latency are excluded. Busy clock copies,
  large headers, final flattening and global GC remain limits.
- **D209 first-child Container changes reuse storage.** The changed suffix is
  rewritten in owner storage even when it begins at child zero, avoiding a
  scratch flatten plus fresh allocation/copy. Growth remains geometric and
  shrink clears removed string slots. Existing revisions and change offsets
  protect version-aware parents from stale in-place output. A warm 30,000-line
  fixture changed at child zero measured 1.406 ms/540,672 bytes/one allocation
  before, 0.0265 ms/zero bytes/zero allocations after (Linux/amd64, 20 runs).
  This isolates flattening, not end-to-end latency. Changed-suffix copy work,
  invalidation, growth, large headers, busy clock copies and global GC remain.
- **D229 a skip container serves its cached frame from an aggregate content
  revision.** A package-level `contentRevision` counter is bumped by every
  content mutation; a `SkipUnchangedChildren` container caches the revision as
  of the frame's start and returns its cached lines while it still matches,
  reading no child revision. This removes the per-wheel-frame O(mounted
  children) walk the fullscreen transcript paid. Caching the start revision lets
  a child that bumps while rendering keep invalidating until it settles, and the
  cooperative resize warm (D203) calls `tui.BumpContentRevision` to keep its
  ancestors re-rendering across frames. For expanded thinking with the transient
  scrollbar, 500 pairs 304 µs/173 KB/865 allocs and 1500 pairs 478 µs fell to
  238 µs and ~260 µs (Linux/amd64), so the frame is now flat in transcript size.
  This measures the render, not the terminal write; an unrelated mutation costs
  one extra walk, and global GC remains.
- **D230 the scrollbar repaints a cell without re-segmenting the line.**
  `scanGraphemes` visits clusters without building the `[]string`
  `segmentGraphemes` allocated, and `GetGraphemeCellRange`, `SliceWithWidth` and
  `GetOsc8LinkAtColumn` use it. The scrollbar cell bytes are unchanged (a golden
  table pins wide glyphs, tabs, ANSI and OSC 8); `replaceScrollbarCell` fell
  from 13,156 to 857 bytes/call (Linux/amd64). This is one cell replacement, not
  a whole-frame claim.
- `Renderer.RenderCount()` counts paints (test seam).
- **D144**: the interactive renderer is caller-driven (the loop owns the frame
  schedule) where upstream schedules its own throttled frames. The loop keeps
  upstream's 16 ms frame throttle for coalesced render ticks, so a streaming
  delta burst cannot paint back-to-back; resize and animation paints stay
  immediate.
- **D164 input does not paint by default.** The terminal input arms paint only
  when the dispatch queued a render request (`loopSchedule.paintIfRequested`,
  which consumes the coalesced tick the request left) and first drain every
  raw chunk already queued, so one input burst is one paint. Fullscreen mode
  enables `?1003h`, so a bare pointer movement arrives as an event per pixel,
  and one frame is O(the whole transcript): painting per chunk spent the loop's
  entire budget on full repaints. Measured on the port: 60 mouse moves plus a
  keystroke now cost 2–3 paints, down from 63. The loop also caches the
  renderer's animation walk between paints (`loopSchedule.scanValid`, dropped
  by a paint or, for a "nothing animates" answer, boxed by
  `loopSchedule.scanAt`) because the walk visits every mounted component and
  the loop asked for it once per input event. Asking for a frame also invalidates what animates
  (upstream's tick calls `context.invalidate()`): clock-driven content changes
  with no Invalidate firing, so a revision-carrying wrapper would report an
  unchanged revision while its output has moved and a parent's cache would
  serve the previous frame — a shell elapsed label stuck until a click
  invalidates the block. That walk descends through
  `childrenHolder` (`Container`, `Box`, and now `ScrollView`/`MouseRegion`): a
  single-child wrapper that forgets `childComponents` hides any animator inside
  it, which is how the elapsed-time label on a running tool froze (the
  transcript's scroll view held its content in a field, not in the embedded
  Container's Children, and the mouse region wrapped the result), and
  `VisibleWidth` short-circuits
  printable-ASCII text after stripping sequences (5.0 µs → 1.0 µs, 1104 →
  324 B/op on a styled tool-output line) since styled lines never reached the
  plain-ASCII fast path. The behavior change is that input which asks for no
  paint is not painted; the tui renderer's own contract is that components call
  `RequestRender`, and every keyboard path signals one in
  `Renderer.HandleTerminalInput`, so only events that changed nothing skip a
  frame. Tests: `TestMouseMotionBurstDoesNotRepaint`,
  `TestAnimationScanCacheIsDroppedByAPaint`,
  `TestAltScreenMouseMoveRequestsNoRender`,
  `TestVisibleWidthStyledTextMatchesPlainText`.
- **D163 upstream-parity rendering.** Interactive startup does not enable
  low-bandwidth rendering. `tui.SetLowBandwidth` remains available to explicit
  renderer clients, but SSH sessions receive the upstream padded frame output.
  SSH Escape timeout and wheel acceleration follow upstream behavior.
- **D165 terminal theme detection is a deferred listener, not a query.** The UI
  loop is the only dispatcher of terminal replies, so upstream's synchronous
  `queryTerminalBackgroundColor` can never complete on the loop (it always times
  out), and a query written before raw mode is echoed into the input stream (the
  SSH freeze that killed the first attempt). The port writes one OSC 11 query
  (`Renderer.RequestTerminalBackgroundColor`) from `RunWiring.OnStarted`, after
  `UI.Start`, and returns; the reply is consumed by `HandleTerminalInput`,
  classified by luminance, and applied by an `OnTerminalBackgroundColorChange`
  listener bound in `NewInteractiveThemeController` and re-bound on a renderer
  swap (`RebindTUI`, called from `LifecycleOptions.OnTuiModeSwitched`). The
  `CSI ? 2031` notification toggle is deferred the same way: before `Start` it
  only flips a flag, and `Start` replays it. A terminal that does not answer
  keeps the `COLORFGBG`/fallback theme.
- **D166 the global theme is a stable handle (upstream's `theme` Proxy).**
  Upstream's `theme` reads the global theme on every property access, so a
  switch reaches any component that resolves colors at render. The port used to
  return the current concrete `*Theme`, which components captured at
  construction and therefore never recolored. `ActiveTheme()` now returns one
  stable `*Theme` whose `Fg`/`Bg`/`GetFgAnsi`/`GetBgAnsi`/`ColorMode` resolve
  `CurrentTheme()` at call time; factories (`GetMarkdownTheme`,
  `GetEditorTheme`, …) and `GetThinkingBorderColor` closures built once from the
  handle recolor after `Renderer.Invalidate()` on the next paint. This is what
  lets `onChanged` stay `updateEditorBorderColor` (upstream's), with no
  `rebuildForTheme`. Colors baked into a `Text` at construction stay baked in
  both implementations.

- Stage 1 also fixed the read side of `coding.SessionManager`
  (`GetEntries`, `BuildContextEntriesForLeaf`, `BuildSessionContext` now take
  `m.mu`; `AppendCompaction`/`GetSessionName` use the locked helpers): those
  accessors were the first legitimate off-thread readers, and the agent writes
  the same state from its own goroutine.

## Architecture of the interactive mode

The tool frame lifecycle is a deep module in
`coding/interactive/toolframes.go`. Its interface accepts captured rendering
input and delegates render, revision/change reporting, detached preparation and
hit testing. It hides admission, generations, private Box cache adoption,
prepared-versus-displayed metadata and live retained clock updates.
`toolresult_prepare.go` only selects safe built-in renderers and captures their
immutable input; `toolexecution.go` assembles the shell and delegates lifecycle
operations without inspecting pending/ready state. The owner-supplied preparation
executor is the seam for deterministic rejection and reordered-completion tests.
The former `toolframe.go` and `toolretained.go` splits are removed. D205-D208
behavior and the existing shared four-job budget are unchanged.

Upstream `interactive-mode.ts` is one ~4000-line class. The Go port splits it
into narrow, injectable wirings (all in `coding/interactive`):

- `app.go` — **the composition layer** (port of the `InteractiveMode`
  constructor). It builds the containers, editor, footer, transcript, event
  dispatcher, queue, selector slot, lifecycle, and every wiring, then exposes
  `App.Init`/`App.Run`.
- `interactivemode_lifecycle.go` — mount/stop, renderer swap, signals, shutdown.
- `interactivemode_run.go` / `_startup.go` — init, the main input loop, helpers.
- `interactivemode_handlers.go` — key wiring (`KeyWiring`) and submit dispatch
  (`SubmitWiring`, the full slash-command chain).
- `interactivemode_selectors.go`, `_settings.go`, `_models.go`, `_auth.go`,
  `_commands.go`, `_queue.go`, `_events.go`, `_ui.go` — the named surfaces.
- `transcript.go`, `footer.go`, `customeditor.go`, `tuirenderer.go`,
  `extensionsselector.go`, `sessionswitch.go`, `interactivemode_helpers.go`.
- `main.go` (thin) and `cmd/main.go` — the `cmd` package boots
  settings/auth/model-runtime/agent-session and runs the `App` (port of
  `main.ts`'s boot); the root file only calls `cmd.Execute`.
- Each `*Wiring` has a constructor next to its struct (`newCommandWiring(app)`,
  `newRunWiring(app)`, …) that owns its full field wiring; `NewApp` is the
  object graph plus a sequence of constructor calls. A field cannot be silently
  skipped in the composition root because it lives with its struct —
  `TestAppWiringCompleteness` pins the required hooks (the `/debug`
  `WriteDebugLog` and right-click-paste hooks shipped unwired this way). Read the
  target before wiring an unassigned seam: some seams are unassigned because
  assigning them is wrong. `ConfigureHTTPIdleTimeout` fed a helper that wrote the
  process-wide `http.DefaultTransport` — a data race against in-flight requests —
  and the setting already reached the wire per request (D40).

`tui/render.go` (+ `mainscreen.go`, `altscreen.go`, `terminal.go`,
`stdinbuffer.go`) is the differential renderer core. Its owner-loop and
handoff discipline is load-bearing; see `docs/locks.md`. Historical deadlock
fixes do not imply that component/render mutexes still exist.

**The session projection lives in one module.** `coding/session_projection.go`
owns what the next request carries: `SessionManager.Projection()` resolves the
branch path, the compaction window, the context settings and the messages in one
walk, caches the result by branch version (leaf + entry count), and hands its
slices out with no spare capacity so a caller's append cannot write into the
cache. `CurrentSystemMessage` uses that projection. `LatestCompaction` and
`ContextSignature` inspect the current branch without decoding the full
message projection. Before this, ten call sites assembled the projection by hand
from `buildSessionPath` / `applyCompactionWindow` / `getSessionContextSettings` /
`getEntriesLocked`, which is how the cache-warmer currency check ended up
re-projecting the whole session per request: nothing owned the answer and the
cost was invisible at the call site. Measured on the 45 MB / 19.4k-entry session:
cold `Projection` 37 ms, cached 0, `AppendCompaction` 0 ms with a warm
projection (247 ms originally, 56 ms before the cache).

A resolution now walks the branch **by pointer** under the session lock
(`branchPointersLocked`) and copies only the entries the compaction window
keeps, instead of copying the whole entry tree and rebuilding an id map on every
append. Profiling the 45 MB / 19.4k-entry session's projection after an append
(the version changes on every append, so this ran per request) put 11 ms in
`getEntriesLocked`'s entry copy, 17 ms in `buildSessionPath`'s value walk, 1 ms
in the window, and **8 µs in decoding every message**: the session has 17
compactions, so the decoded window was 3 entries. Decoding was never the cost;
the copies were. With the pointer walk the projection is 3.3 ms cold and 2.3 ms
after an append, and `coding/session_messagecache.go` memoizes each message
entry's decoded messages and role (entries are append-only, so a memo never
needs invalidation) for the uncompacted case, where the window is the whole
path. `TestCompactedProjectionCostsTheWindowNotTheSession` pins the shape: a
2001-entry session compacted to an 8-entry window resolves in 9.5 KB of
allocation, where the tree copy measured ~800 KB. `BuildSessionContext` and
`BuildContextEntries` stay as the value-based reference (the manager's
resolution is compared against them by `TestProjectionCacheMatchesTheReference`),
and `applyCompactionWindow` became a wrapper over `walkCompactionWindow`, which
lets `ContextSignature` count the window without materializing it.

**Cache-miss notices are O(1).** `SessionManager.CacheMissFor` answers the
notice for an assistant message from `cacheScanState`, which advances as entries
append (`appendEntry` consumes) and is seeded over the loaded entries in
`buildIndex`. Upstream rescans parsed entries per assistant message
(`detectCacheMiss(getEntries(), …)`); the port stores raw JSON, so the same call
re-decoded every message entry: **713 ms** on the 45 MB session, on the UI loop,
on every assistant message end, for anyone with `showCacheMissNotices: true`
(which the reporting user had). The running state is 52 ns per notice and adds
167 ms to loading that session, where a full scan of the seeded entries is
504 ms. `CollectCacheMisses` keeps the full scan for transcript rebuilds (resume,
compaction, reload), which still costs ~500 ms on that session.
`TestCacheMissForMatchesTheFullScan` is the differential guard: the incremental
notice must equal `DetectCacheMiss` on the session's entries for every assistant
message, including after a compaction and a branch summary (which reset the
scan). `scanAssistantRequest` reads only the role, usage, model and timestamp
for the state; decoding the whole message (thinking, tool arguments) was what
made seeding cost 500 ms.

**Large-session rendering.** `RenderSessionItems` (`transcript.go`) renders a
session eagerly below 400 items; above the threshold it collects the items into
a collector container and attaches only the trailing window (120 components),
materializing the rest 64 per loop beat via `RunWiring.OnBeat` →
`MaterializeDeferred` (`Container.InsertChildAt`). Each chunk's render caches
are warmed off the UI loop by `TranscriptRenderer.PrerenderQueue`
(`internal/offloop`): a large entry's markdown lex + styling is the last long
beat, so `tui.Preparer.Prepare` renders the chunk ahead of the attach and the
loop only inserts an already-warmed chunk (a width change discards a chunk
warmed for the old width; nil queue keeps the synchronous path). A 30 MB /
15k-entry session paints its first frame in ~20 ms instead of ~400 ms.
Resizing a huge session
still costs ~350–430 ms per new width because the layout renders the whole
attached scroll content at the content width (`ScrollContentLines` calls the
component render) — this matches upstream `layout.ts` and is deliberately not
staggered (a partial-width render would show stale text). A manual `/compact`
on that session spent ~20 s and ~325 GB of allocations in `PrepareCompaction`
before the summarization request was even built, because the port re-derived
the context projection inside the loop over it (`BuildContextEntries` once per
entry); it now builds the projection once (~105 ms at 15.5k entries), pinned by
`TestPrepareCompactionProjectsTheContextOnce`. The other projection was on the
request path: `cacheContextIsCurrent` (the cache-warmer currency check) built
the whole `SessionManager` context — ~190 ms on that session — on **every**
model request, and `BuildSessionContext` held the session mutex while it ran,
so the UI thread (the footer re-reads the session on each status change) parked
behind it and the TUI froze for ~200 ms right after every tool result, which is
how it was reported ("running bash freezes the TUI"). The check now compares
cheap `SessionManager.ContextSignature` values (branch-cache lookup + entry
scan) and the projection snapshots the entries and projects outside the lock;
see `TestCacheContextIsCurrentDoesNotProjectTheSession` (37 MB → 0 allocated per
call) and `TestBuildSessionContextDoesNotHoldTheSessionLock`. The remaining
frame cost was the bash preview: counting a command's output (for the
"… (N earlier lines" hint) went through `tui.WrapTextWithAnsi` per line, ~1 ms
and ~8000 allocations per 2000-line output, and a rebuilt transcript (compaction,
`/reload`, resume) starts with every attached bash component cold, so the first
frame after a rebuild stacked that into a 100+ ms frame (caught by a SIGQUIT
dump inside `bashPreviewComponent.Render` → `visualLineCount`). Plain ASCII
lines now count in one allocation-free pass (`plainWrappedLineCount`), pinned to
the generic wrapper by `TestPlainWrappedLineCountMatchesTheGenericWrapper` and
capped by `TestBashPreviewCountIsAllocationFree`. The **expanded** bash output
had the same O(output²) shape: streaming recreated a `tui.Text` over the whole
output every chunk and re-wrapped it (measured 468 ms per chunk at 20k lines).
`bashExpandedComponent` styles and wraps each complete line once and re-wraps
only the current partial line (`TestBashExpandedComponentMatchesReference`
checks every prefix against the full-output reference;
`TestBashExpandedStreamingIsLinear` bounds it). Two general width fast paths
follow from the same profile: `isInTable` returns early for a rune below the
first range, `graphemeWidth` returns 1 for printable ASCII, and `VisibleWidth`
re-checks ASCII after stripping ANSI (styled tool output is ASCII underneath).
Together the expanded 20k-line stream went 468 ms → 35 ms per frame, and the
collapsed default stays sub-millisecond. **The remaining frame cost is the
change-detection walk, not the post-processing.** A paint already only fills the
terminal height (`RenderLayoutFrame` allocates `height` lines and `paintBox`
fills those), and a warm walk is all cache hits (Markdown reports 100% via a
`sync/atomic` probe), but the renderer re-walks every mounted component to find
what changed, so a warm frame is O(components). `BenchmarkTranscriptFrameScaling`
pins the curve: ~50 us at 1k components, ~150 us at ~4k, ~1.4 ms at 16k.

A warm-frame fast path removes most of that constant. `Container.Render` first
calls `allChildrenReusable`: it reads each child's revision once into a per-frame
scratch slice and records whether the child can serve its cached lines. If every
child can, the container returns the cached frame without snapshotting the child
list, rebuilding the mouse layout, or re-flattening. The walk below reuses the
revisions and reuse flags that scan already read, so a frame never dispatches
`RenderVersion` twice for one child. `Spacer` now reports a revision (bumped by
`SetLines`/`Invalidate`), so the spacer between every message is reused like any
other versioned child instead of re-rendering. `Container.RenderWalks` is the
test seam, and `TestContainerWarmFrameSkipsTheChildWalk`,
`TestContainerWarmFrameKeepsTheMouseLayout` and
`TestContainerReadsEachChildVersionOncePerWarmFrame` pin the behavior. That took
the 8,000-pair warm frame from ~2.0 ms to ~1.4 ms (2,000: ~260 us -> ~150 us),
with no streaming regression (`BenchmarkStreamingAssistantDelta` is unchanged).
The remaining cost is the memory-bound revision reads (one pointer chase per
child per frame); the next optimization is a global render revision bumped
whenever a version does, so an unchanged frame is O(1). The open question is
making that invalidation discipline complete, since components currently change
state (e.g. `Text.SetText`) without telling the parent.

**The flatten, not the walk, is what makes a streaming frame stall.** Reproduced
on the live 61 MB session (25.4k attached children): a warm frame is 11 ms, but
streaming a message made frames **100–190 ms** (52 `render` stall records across
the log, all in the width/wrap/tokenize leaves, keystroke latency entirely
`read`). `Container.Render` re-concatenated **every** child's lines into a fresh
slice whenever any child changed; during streaming the last child changes every
frame, so the whole transcript was re-flattened (and re-allocated) 60 times a
second. It now finds the first changed child, keeps the unchanged prefix, and
rewrites only the suffix **in place** (growing geometrically), which took the
worst streaming frame 122 ms → 23 ms. Because the returned slice can now change
contents while keeping its backing array, slice identity is no longer a valid
change signal; `Container` carries a `version` bumped on every rebuild and
exposes `RenderVersion`, and `firstChangedChild`/`zoneMarkedLines` compare it
(the `MouseRegion` and `ScrollView` pass-throughs and the message/tool components
that return a container's lines forward the revision). `TestContainerRenderReusesPrefixOnTailChange`
and `TestContainerRenderDetectsInPlaceChildChange` pin both halves. The
remaining cost is the per-child revision read described above.

`AppendCompaction` had the
same shape: its entry records the projected system message, and the port
resolved that from `buildSessionContextLocked` **while holding the session
mutex** — 247 ms on a 45 MB session, so every UI read waited. It now projects
before taking the lock (upstream is single-threaded and its parsed-object
entries make the same projection cheap), and `getSessionContextSettings`
resolves the model/thinking level by scanning the path backwards instead of
decoding every message entry to find the last assistant, which cut a
post-compaction projection from 210 ms to 56 ms. Pinned by
`TestAppendCompactionDoesNotHoldTheSessionLock` and
`TestProjectedSettingsResolveLastWriteWins`. Measured end-to-end (real binary in
a pty, scripted model, the 45 MB / 19.4k-entry session resumed, bash tool call,
both TUI modes, silent and output-streaming commands, with and without
auto-compaction in the turn): the worst frame gap during the run is 82–83 ms,
which is the spinner's own ~80 ms animation interval, with keystroke echo at
5–6 ms; before these fixes the same run showed 200–300 ms lock-blocked stalls
and a 121 ms frame after a transcript rebuild.

## Frame cost

The measurements below and in the session-performance discussion above were
collected at successive optimization stages. Later changes supersede earlier
bottlenecks and proposed next steps; these figures are historical observations,
not current test-duration or performance guarantees.

The full layout pass (`RenderLayoutFrame`) is cheap enough to run on every
requested frame: a scroll view over 500 wrapped text lines measures ~19us and
~1.8 KB per pass, and a 50-child nested box tree ~14us
(`BenchmarkRenderLayoutFrame*` in `tui/layout_bench_test.go`). Component render
results are cached, so a frame re-measures and re-paints rather than
re-rendering the content; the markdown and box caches are the ones that keep
large messages off the loop's critical path. The benchmarks are the guard
against a future per-frame cost that scales with content.

One per-frame cost did scale with content: the animation walk. A paint drops the
cached walk (D164), so the walk runs on every paint, and it used to tick
(`Invalidate`) every animator it found. `ToolExecutionComponent.Invalidate`
rebuilds its whole output (`updateDisplay` re-creates and re-wraps every result
`Text`), so scrolling a session with a running tool re-wrapped every tool's full
output on every frame: a 97 MB session measured 100–143 ms `render` phases per
wheel event (measured by the since-removed stall log). The walk now ticks each animator only once per
its own delay (`Renderer.animationTicks`, `nextAnimationForTicked`), matching
upstream's `setInterval(context.invalidate)` cadence; the same scroll measures
10–26 ms, once per second. `TestAnimationWalkTicksAnAnimatorOncePerDelay` pins
it.

The per-frame walk is bounded the same way. The transcript's chat container sets
`SkipUnchangedChildren`, so a child that reports an unchanged render revision
(every message and tool component bumps it on mutation, `Container.MarkDirty`)
is not re-rendered on a scroll-only frame. A 500-tool chat measures 93 µs →
11 µs per warm frame (8.2×, `BenchmarkChatWarmRender`). The skip also made
`firstChangedChild` compare child identity, closing a latent stale-line bug when
a child is replaced in place at the same revision.

The once-per-second tick of a running tool is narrow (D190): the animation walk
calls `AnimationTicker.AnimationTick` when a component implements it, so
`ToolExecutionComponent` bumps its revision without rebuilding its display. The
elapsed label still updates, because `Box.matchCache` checks a versioned child's
revision (a child `Container` rewrites its suffix in place) and `Box.Render`
re-applies the background only from the first changed line; the result `Text`'s
wrap cache survives (`BenchmarkToolAnimationTick`: 2.15 ms → 0.005 ms (0 allocs)
for a 2000-line result, 139 µs → 0.7 µs for a bash preview). The walk also
collects only the animators for its prune set, not every mounted component; a
map entry per component was 40% of the walk. `Box.applyBg` measures the line's
width once (it used to reach `ApplyBackgroundToLine`, which measured the padded
line again).

The animation walk also caches its animator list: the walk visits every mounted
component and a paint invalidates the scan (D164), so it used to traverse the
whole tree on every paint. `Renderer.NextAnimation` reuses the list until
`AnimationTreeRevision` changes (bumped by `Container.AddChild`/`Clear`, not by
a tick) or a 1 s box expires. The width cache holds 32768 styled lines
instead of 512, because a large boxed transcript evicted its own lines and
re-stripped them on every `applyBg` (`VisibleWidth` → `StripTerminalSequences`).
A child Container reports where its last Render started rewriting
(`Container.ChangedFrom`), so a Box re-applies the background only from that
line after an in-place suffix rebuild. Together the scroll-window CPU samples
fell from ~1.27 s to ~0.88 s over 20 s on the 97 MB session
(`TestRendererCachesTheAnimatorWalk` and
`TestBoxReusesPrefixForAnInPlaceContainerChange` pin the changes).

## Syntax highlighting

Syntax highlighting is built in (D231, superseding D217 and D227). Upstream
renders Markdown code blocks and read/write previews with highlight.js and maps
the `hljs-*` scopes to `syntax*` theme tokens (`renderHighlightedHtml` plus
`buildCliHighlightTheme`). `highlight` is a from-scratch port of highlight.js
10.7.3, and `coding/interactive/codehighlight.go` ports the scope mapping and
the scope walk: a registered language gets per-token colors, an unregistered
one keeps upstream's no-valid-language fallback (one `mdCodeBlock` color per
line).

The reference builds highlight.js's HTML and then re-parses it to find the
scopes. The port's `highlight` package already returns the same scope tree, so
the TUI walks the tree directly and skips the HTML round-trip; the two are
equivalent because the HTML walk was exactly escape-then-unescape. On the
10.9 KB `internal/highlight/testdata/sample_big.go`, `highlightCode` measures 1.86 ms
(5.86 MB/s) against the reference's 3.20 ms (3.42 MB/s) in the same session, and
against the retired Chroma measurement (~0.5 MB/s, D227). The scope sequence and
the scope-to-token mapping match upstream; only the theme palette differs
(D154). The per-fragment formatter lookup is a switch, so the wire-in adds no
lock and no off-loop path.

## Full-lexer allocation overhead (historical)

This section describes the retired tui lexer (`MdToken`) before the `markdown`
package replaced it; the numbers and the batched text-token design are kept for
the record, and the tests that guarded them moved to the new package or were
deleted with the model.

`Markdown.Render` lexed the complete changed source, following
`packages/tui/src/components/markdown.ts`; no append-only parser or new token
cache is introduced. The Go lexer reduces allocation overhead by batching
plain-text nodes into parse-owned, nonmoving chunks for documents of at least
4 KiB. Chunks grow from one slot to at most 16, are never pooled across parses,
and keep returned token pointers independent and valid. Longer markup spans
reserve a small inline pointer slice rather than repeatedly growing one;
short labels and plain prose retain minimal allocations. List-item parsing now
carries its temporary marker-stripped source in the item's existing `Text`
field until final whitespace processing, removing two private scratch fields
from every token. On amd64 this reduces `MdToken` from 304 to 280 bytes without
changing its exported fields or final values.

On Linux/amd64 (Intel N150, Go 1.27.1), a 7,900-byte, 100-paragraph lexer fixture
falls from 1,909 allocations to 1,050. Three 100-update streaming benchmark runs
with a roughly 35-43 KB message fall from about 9,334 allocations and 2.10 MB
per update to 5,443 allocations and 1.95 MB. Timing remains about 2.6-2.9 ms per
update, so this is primarily an allocation improvement, not an end-to-end
latency claim. A synchronous roughly 1 MB assistant render drops from about
112 MB allocated to 108 MB in paired three-iteration measurements; worker
admission, rendering cadence, and terminal output policies are unchanged.

Lexer/render upstream goldens, parse ownership across garbage collection,
and an allocation-budget regression test cover the change. A temporary
before/after comparison of 1,768 streaming prefixes (the golden corpus plus a
large mixed-markup document) produced identical token trees and rendered lines.

## The markdown package

`github.com/dat267/pier/internal/markdown` is the markdown engine the TUI uses, and it
holds **one** renderer. `tui/markdown.go` is only the component wrapper
(transform, the per-width incremental line cache, off-loop preparation and the
padding/background fill); `LexMarkdown`, the `MdToken` model, and the old
hand-written lexer and renderer that lived in
`tui/markdownparser.go`/`markdowninline.go` are gone. `internal/markdown/render.go` walks
the `Block`/`Inline` model and emits upstream's styled lines exactly, so
`tui/testdata/md_render_golden.txt` (upstream's `Markdown` component output) and
`tui/testdata/md_golden.txt` (the Marked token projection) pass unchanged. It is
driven by the narrow `Style` interface (`NopStyle` for tests and measurement)
plus a `Terminal` of injected primitives (word wrap, visible width, background
fill, image-line detection, OSC 8), which `tui` implements with its own
functions, so neither package depends on the other's renderer internals.
`internal/markdown/layout.go` is the layout layer: the standalone `RenderSource`/`Render`
entry points, the default `PlainTerminal`, and the ANSI-SGR/East-Asian-width
wrap primitives. The earlier plan of a second, `Style`-driven layout tree was
dropped when the two converged; there is one traversal, not two.

Stages: a hand-written block lexer (no regexp), an inline parser, and the
styled line renderer. `Document.Update` adds incremental mode: it keeps the
stable block prefix and re-lexes only the trailing run an append can affect.

On the `tui/testdata/md_corpus.json` corpus the package matches Marked 18.0.11
token for token, 79/79 cases, through a differential harness. On an Intel N150
(Go 1.27.1, width 80), against Marked 18.0.11 in node:

| fixture | lexer vs `marked.lexer` | full render vs `marked.parse` |
|---|---|---|
| prose 3.96 KB | 390 MB/s vs 40.8 | 52.2 MB/s vs 36.7 (1.4x) |
| prose 49.5 KB | 419 MB/s vs 41.2 | 66.8 MB/s vs 41.7 (1.6x) |
| mixed 1.14 KB | 33.3 MB/s | 16.3 MB/s vs 5.04 (3.2x) |
| doc 64 KB | 35.7 MB/s vs 9.15 | 16.0 MB/s vs 8.44 (1.9x) |
| doc 1 MB | 41.4 MB/s vs 8.11 | 18.8 MB/s vs 7.30 (2.6x) |

The full-render column is the single renderer through `RenderSource` (the
standalone `PlainTerminal`, no TUI padding); the component adds the TUI's own
wrap and padding and measures 340 us for the 4 KB prose fixture and 154 us for
the 1.1 KB mixed one. It is slower than the earlier standalone layout renderer
(58/71/31/27/28 MB/s) because it now does the full parity work: next-block
spacing, box tables, style-prefix tracking and the code-block indent.


Marked's applicable optimizations were ported rather than its code: a
markup-free inline source returns one text run sharing the source (its inline
text tokenizer consumes to the next special character and merges runs), a
paragraph is a source slice instead of a builder copy, and block and line
slices are preallocated. These took prose lexing from 112 to 348 MB/s and 23
to 3 allocations per 3.96 KB. The inline LaTeX tokenizer follows upstream's
`tokenizeInlineLatex`: its closed-span validity checks (a span whose text ends
with whitespace, is followed by a digit, is an identifier followed by a word, or
contains a backtick stays literal text) and its pending span, an unterminated
`$…` whose tail looks like math, or an unterminated `\(`, that renders its raw
source instead of going through the LaTeX renderer. Mid-line `$$`/`\[` stay
literal text rather than splitting the paragraph (D228).
Incremental streaming of 500 growing paragraphs
is 2.27 ms / 3.8 MB versus 196 ms / 312 MB re-lexing each prefix, and is
value-identical to `Lex` at every prefix (byte-by-byte, random-chunk,
retroactive-construct and fuzz tests).

Wired into the component (`tui/markdown.go`), the tui fixtures render faster
end to end than the old lexer/renderer: `benchProse` (4 KB) 384 to 307 us and
`benchMixed` (1.1 KB) 226 to 143 us, with allocations 236 KB/2328 to 216 KB/2312
and 132 KB/1597 to 110 KB/1386 (Intel N150, width 80, `Invalidate` each round).
The remaining cost is the same styling and wrapping pass, not lexing.

The component lexes incrementally too: `tui/markdown.go` keeps the block keys
and final lines before the trailing open run and calls `markdown.LexFrom` for
the tail, with `markdown.FirstUnstable` the boundary rule `Document.Update`
shares. Only keys, lines and two offsets are retained between renders, never a
block tree. The block keys are width-independent, so the block-line reuse is
guarded on the cached render's width as well: a render at a new width re-renders
every block. Without that guard a resize reused old-width lines (the keys still
matched), and the terminal truncated the transcript instead of re-wrapping it.
Streaming 200 growing paragraphs costs 8.3 ms / 13.7 MB / 23k
allocations, against 244.5 ms / 166 MB / 1.73M rebuilding the component on every
append; the full re-lexes alone were 35.7 ms. `LexFrom` is byte-identical to
`Lex` from every stable boundary, and chunked streaming output is checked
against a fresh render of the final source.

The two options it replaces are slower on every fixture. glamour v1.0.0
(`NewTermRenderer`, word wrap 80) runs 0.21-4.45 MB/s with its default style
(15-135x slower) and 1.27-4.45 MB/s with syntax highlighting off (`notty`,
15-16x slower); it is also 15-40x heavier in allocations and pulls in 27
modules. chroma v2 (`quick.Highlight`, terminal256/monokai) tokenizes at
0.41-0.43 MB/s, while the renderer draws the same fenced code at
138-262 MB/s. Measured in a throwaway module under `/tmp`; pier's `go.mod` is
unchanged.

## Prose rendering and the word wrapper

Prose rendering is bounded by the word wrapper, not the markdown package. A CPU
profile of a 49.5 KB prose render put `tui.WrapTextWithAnsi` at 79.8% cum and
`markdown.Lex` at 2.8%; the lexer is already ~9x upstream's (419 MB/s against
`marked.lexer`'s 45). The wrapper port had dropped upstream's printable-ASCII
fast path (`isPrintableAscii(chunk)` then iterate the string and skip the CJK
break test), so every ASCII byte went through `segmentGraphemes` (a `[]string`
with one entry per byte), a `[]rune` conversion and an `isCJKBreakRune`
`sort.Search` over the CJK ranges. It now slices word/space runs directly: for a
wholly printable-ASCII line without touching the tokenizer state, and for each
printable-ASCII run between escapes.

Wrapping the same 98-byte prose line falls from 7.85 us / 56 allocations to
3.47 us / 18, and a 49.5 KB prose render from 4.53 ms to 2.37 ms (2,654 KB /
28,545 allocations to 1,586 KB / 9,545), against pi's 3.66 ms on the same
fixture. The same chunk search and `breakLongWord` also spun forever on a line
containing a malformed ESC (a lone `\x1b` in the text); both now skip escapes
`ExtractANSICode` rejects, which is what upstream does.

## Stdin paste buffering

`StdinBuffer` used to concatenate and rescan the entire paste prefix for every
stdin chunk. A 65,654-byte paste arriving in 128-byte reads allocated 18.2 MB;
`strings.Builder` now grows the accumulator amortized, and terminator search
resumes from the suffix that can straddle the next read. The same fixture
allocates 285 KB in 85 µs on Linux/amd64 (Intel N150, 100 iterations).
`TestStdinBufferChunkedPasteAllocationIsLinear` checks the content, the
terminator split across a read boundary, and an 8x-output allocation ceiling.
A complete paste already present in one input string bypasses the builder and
stays a substring of that input; `TestStdinBufferSingleChunkPasteDoesNotCopyInput`
measures 0 bytes allocated within `ProcessString` for a 65,536-byte payload.

## Editor paste fast path

`Editor.handlePasteLocked` normally sees valid, printable text. It now bypasses
CSI-u replacement when there is no matching sequence and returns clean UTF-8
without rebuilding it in the control-character filter. Inputs with CSI-u or
filtered controls still take the existing replacement/filter path. The existing
1 MB paste benchmark falls from 12.60 ms / 9.74 MB / 82 allocations to 3.54 ms /
2.40 MB / 46 allocations per operation (Linux/amd64, Intel N150, five runs);
`TestEditorCleanLargePasteDoesNotCopyFilteredText` pins the allocation budget,
and `TestEditorPasteFiltersControlBytesAndDecodesCSIu` guards the exceptional
path. Bracketed pastes also slice off the common leading start marker instead of
replacing it in the full input; the fallback still removes an embedded first
occurrence. `TestEditorBracketedLargePasteAvoidsCopyingInput` measures 6.8 KB
allocated for a 270 KB paste, down from 294 KB, and the 1 MB benchmark improves
from 3.54 ms / 2.40 MB to 2.95 ms / 1.35 MB in five-iteration runs on the same machine.

## ASCII fuzzy matching

Autocomplete calls `MatchFuzzy` for each candidate. The common ASCII path now
compares lowercased bytes on demand instead of lowercasing and allocating rune
and UTF-16 slices per query/text pair. Matching and non-matching tests measured
4 and 5 allocations before, then 0 after; the alpha-numeric swap case fell
from 9 to 0. The swapped query is matched through a virtual byte rotation, and
the upstream fuzzy score goldens still pass. Non-ASCII inputs retain the
UTF-16 implementation.

## Model autocomplete snapshots

The `/model` argument provider used to read scoped models three times per
completion. It now takes one snapshot to build an immutable search-item cache;
`AgentSession.ScopedModelsRevision` lets later keystrokes reuse it without
copying the scoped slice; cache misses get the slice and revision in one
lock-consistent snapshot. A cold empty-prefix request maps the source directly;
the test fixture's 4,096-model allocation fell from 471,936 to 209,792 bytes. A
warm empty-prefix request reuses the cache and falls from 393,280 to 196,672
bytes. A non-empty cache hit allocates 328 bytes, down from 262,408 without
caching. Tests cover the allocation budgets and cache refresh on model changes.

## Editor backward word movement

Ctrl+Left over a long word-like segment used to materialize a `[]rune` and
byte-offset table in `WordSegments`, then another rune slice to find the last
ASCII punctuation. `WordSegments` now decodes the source in place and returns
valid segments as source substrings; backward movement records punctuation byte
positions directly and counts the in-range segment prefix without copying it.
The 65,541-byte editor path fell from 876,608 allocated bytes to 64; skipping
8192 punctuation segments no longer allocates the 262 KB prefix copy. Allocation
tests also check the byte cursor after a multibyte suffix.

## Proxy stream delta assembly

`agent/proxy.go` rebuilds cumulative assistant text and thinking blocks from
proxy deltas. Repeated string concatenation copied every previous prefix. A
64 KiB fixture made of 512 128-byte deltas allocated about 18.1 MB for either
content type; per-block `strings.Builder` buffers reduce the direct text-path
benchmark to 356 KB and 144 µs on Linux/amd64 (Intel N150, five iterations).
The byte-budget tests cap both paths at 512 KiB and verify the assembled value.
The tool-call JSON parser and event payloads are unchanged.

## Mistral SSE buffering

`consumeMistralChatStream` used to append every read chunk to an event string and
rescan the full prefix for a delimiter. A 65,536-byte unterminated event read in
128-byte chunks allocated 22.38 MB. It now reuses the read buffer, accumulates
with `bytes.Buffer`, and searches only the suffix that can contain a split event
boundary. The same fixture allocates 253 KB. Tests cover the allocation bound
and a delimiter split across reads. Mistral text and thinking deltas also use
per-block builders. The Go string sanitizer is a no-op because Go strings cannot
retain unpaired UTF-16 surrogates (D2). The 64 KiB/512-delta fixture fell from
18.66 MB to 428 KB.

## Pi-messages SSE buffering

`readPiMessagesEvents` had the same full-prefix string concatenation and scan as
the Mistral reader. A 65,536-byte event arriving in 128-byte reads allocated
22.34 MB. It now reuses a read buffer, normalizes CRLF across chunk boundaries,
and searches from the possible split delimiter suffix. The fixture allocates
209 KB, and a regression test verifies CRLF event boundaries split across reads.

`piMessagesConverter` also used `+=` on cumulative text and thinking blocks. It
now keeps a builder per content index and drops it at the block's end; a 64 KiB
text-delta fixture fell from 18.07 MB to 298 KB, and the thinking fixture uses
302 KB.

## Responses stream delta assembly

`ProcessResponsesStream` used to concatenate every text and reasoning delta onto
the full block prefix. A 65,536-byte text block made from 512 128-byte deltas
allocated 19.4 MB. Per-output-slot builders now append deltas amortized; the same
fixture allocates 1.60 MB while preserving the cumulative message and upstream
score/event goldens.

## Anthropic stream delta assembly

Anthropic text and thinking blocks now append through per-block builders instead
of copying the accumulated prefix for each SSE delta. The 65,536-byte text
fixture with 512 chunks fell from 19.21 MB to 1.42 MB allocated.

## Google stream delta assembly

The Google generative AI stream now uses a per-block builder for text and
thinking deltas rather than copying each accumulated prefix. A 65,536-byte text
block made of 512 128-byte events fell from 19.11 MB to 1.30 MB allocated.

## OpenAI Completions stream delta assembly

The OpenAI Completions stream now appends text and reasoning deltas through
per-block builders. A 65,536-byte text block made of 512 events fell from
18.98 MB to 1.19 MB allocated.

## Session history (high level)

The repository was built as a long port: the `ai`/`agent`/`coding` cores first,
then the `pi-tui` library (renderer, layout, terminals, keys, markdown,
components), then the interactive coding-agent mode (theme, every component,
and the mode method groups), each round verified against upstream Node goldens
and committed locally with a `docs/PORTING.md` update. The CLI
(the root `main.go` over the `cmd` package, originally `cmd/pi`) and its
composition layer (`app.go`) came last,
followed by five deadlock fixes: D136 (editor submit), D137 (model selector),
D138 (scroll) and D139 (exit) were found by driving the real binary in a PTY,
and D156 (login) by reading the dispatch path, with a test that fails instead of
hanging when the regression returns. The
executable was then renamed from `pi` to `gpi` and finally to `pier` (the display name follows the
invoked file name).

D192 later retired the real-binary D136–D139 mutex-deadlock watchdog flows.
Their functional coverage now lives in `coding/interactive/ptyflow_test.go`
(in-process despite the filename), while `internal/uiblock` enforces the
blocking-work invariant statically. Real-binary PTY tests still cover other
flows, and `TestMutexBlockedDetectorHasTeeth` retains the deliberate deadlock
helper. For a current hang, an optional `SIGQUIT` dump is a useful diagnostic,
but the cause need not be a retired UI-state mutex.

## Startup cost on a cold filesystem

Startup was profiled on the 100 MiB session resume (`-c`) and on a fresh
interactive session, warm and with the page cache evicted (`sync` then
`posix_fadvise(POSIX_FADV_DONTNEED)`), and from a remote Cloud Shell whose home
directory is a network-backed filesystem. Cold `-c` went from 516 ms to ~390 ms
end to end (session load 312 ms, most of it the single 100 MiB read), and plain
`pier` is 35 ms warm, 60 ms with a cold binary and config on a local disk.
A reported Cloud Shell run (`PI_TIMING=1`) showed the same session-independent
shape: `TOTAL` 449 ms cold with `initTheme` 151 ms and `createAgentSession`
201 ms, then 33 ms on the immediate second run. The cost there is per-file
latency on the remote filesystem, not any one large file.

- **Resume discovery** reads a bounded head instead of each candidate file:
  `FindMostRecentSession` stats and orders candidates by mtime, then reads a
  4 KiB-buffered, 1 MiB-bounded header (`readSessionHeader`), stopping at the
  first cwd match. The 100 MiB directory went 323 ms cold / 62 ms warm to ~1 ms.
- **Session listing** extracts metadata without decoding payloads:
  `scanListingMessage` reads only role, timestamp and the user/assistant text
  blocks and skips thinking, tool-call and tool-result bodies, falling back to
  `ai.UnmarshalMessage` for shapes it does not fully understand (D225). Listing
  the 105 MiB directory went 2.0 s to 0.30 s.
- **Entry load** uses `bytes.IndexByte` to find string ends in `skipJSONString`
  (counting a preceding backslash run only when the quote may be escaped) and
  scans files of 4 MiB or more on one worker per MiB, capped at `GOMAXPROCS`,
  appending in file order. Loading the 100 MiB session went 130 ms to ~50 ms
  warm.
- **Resource discovery** is concurrent. `CreateAgentSession` ran the four
  independent discovery passes in sequence (context files, skills, prompt
  templates, system-prompt files); they now run in parallel because each is a
  chain of small reads whose wall time on a high-latency filesystem is the sum
  of the chains. `loadSkillsFromDir` scans its tree on a bounded pool
  (`skillScanConcurrency`) and replays ReadDir order depth-first, so skills and
  diagnostics order are unchanged, and the separate `os.Stat` before each
  `os.ReadDir` is gone. `TestLoadSkillsFromDirScansInReadDirOrder`,
  `TestLoadSkillsFromDirRootSkillShortCircuits`,
  `TestLoadSkillsFromDirConcurrentWalksAgree` and
  `TestCreateAgentSessionLoadsAllResourcePasses` pin the behavior.

What remains on a cold start is the one large session read on resume (a
persisted index would remove it, at the cost of invalidation) and, on a remote
filesystem, `initTheme`'s settings and theme reads, which the resource
concurrency does not overlap. The port prints these phases with `PI_TIMING=1`.
