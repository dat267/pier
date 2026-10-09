# Divergences

Numbered **D-rows**: every place this implementation deliberately differs from
the pi reference. Some rows exist because the reference relies on a JS or Node
behaviour with no direct Go equivalent, or because a reference defect is fixed
here; others are choices of this project's own. D-row numbers live in code
comments at the point of divergence; this file is the log, and it is
representative: the rows below carry a written-up rationale, while the rest live
only as the code comment that introduced them. The range is **D1–D216**.
- D216 — fullscreen keybindings follow upstream v1.1.0 (`packages/tui/src/keybindings.ts`,
  commit `6100fe5a8`) while the port remains pinned to v1.0.2: `Home`/`End` move
  the editor cursor, and `Ctrl+Home`/`Ctrl+End` move the transcript. This row is
  temporary and should be retired when the reference pin advances.
- D188 — the durable execution environment (`env/index.ts`, `env/node.ts`)
  returns failures as Go errors (`*FileError`, `*ExecutionError`, the upstream
  codes preserved) where the reference returns a `Result` value; the `Result`
  helpers exist for callers that need the upstream shape. Filesystem paths,
  `~`/file-URL resolution, the error-code mapping and the tracked temp
  directory/file cleanup match the reference. The shell half of
  `env/node.ts` is ported too (shell resolution with the Windows Git Bash
  candidates and the `sh -c` fallback, direct argv execution, timeout
  validation, the spill rule, process-tree kill on timeout/cancellation, and
  stream-tagged `onOutput`); stdout and stderr use separate reader goroutines
  multiplexed through a bounded Go channel, so cross-stream ordering follows
  goroutine send order rather than a shared OS pipe's byte order. Callback
  delivery is synchronous on that consumer, not Node's per-stream flow control.
- D187 — the attached replicated state (`chord/services/attachedstate.go`, the
  attachment half of upstream `services/state.ts`) delivers callbacks
  synchronously instead of through upstream's per-subscription asynchronous
  queue (a 100-delivery pending window with newest-wins overflow). The
  observable rules the durable session depends on are kept: snapshot capture
  is atomic, buffered frames drain in commit order, a cursor gap reports an
  error and disposes the broken attachment, source values and op batches are
  republished by reference, and a listener that subscribes during a
  publication hydrates after the current drain at the then-current sequence,
  which hides the updates its hydration covers (upstream's late-hydration
  rule).
- D11 — upstream's chord delta is a JavaScript Proxy draft that records
  operations as the caller mutates it; Go has no proxies, so the port's
  tracker (`chord/delta/tracker.go`) keeps the upstream lifecycle (tracker
  value/revision, beginChange, prepare, adopt, prepareReplace, staleness and
  abort rules, no-op adoption) but hands out an explicit mutable draft and
  materializes its batch with the same diff engine when it settles. Upstream
  documents that a batch is "exact but not canonical", so the op tuples may
  differ while the resulting value matches. The ops/wire tuples themselves are
  structs rather than heterogeneous tuples (the same row covers the delta
  package's Op/WireOp shapes).

- D30 — startup timings read `PI_TIMING` **per call** instead of once at module
  load (upstream reads the flag when the timing module is first imported), so a
  test can toggle the flag and observe the output without the process having to
  restart. `SetTimingsEnabled` is the same override in test form.
- D41 — extension mechanics are out of scope (extension discovery in the
  resource loader, the extension runner, package/tools managers); seams are
  function values or return nil. The resource loader's non-extension pieces are
  ported: context files, skills, and the SYSTEM.md / APPEND_SYSTEM.md prompt
  files. `/reload` follows upstream `AgentSession.reload` for them — settings
  re-read and queue modes, `ai.ResetAPIProviders`, the resource files, then the
  system prompt rebuilt from the active tool names — and the mode's keybindings,
  implicit project trust and UI re-application. Not reloaded: the extension
  runner and package manager (out of scope), the tool registry (built-in tools
  capture no settings-dependent state; the bash tool reads the shell settings
  per call), and extension-supplied prompt templates (the port does not load
  them at boot either). Theme files are re-read — `applyReloadedSettings` calls
  `ApplyFromSettings`, whose `SetTheme` reloads the named theme from disk — so
  the `/reload` notice speaks of keybindings, skills, prompts, themes and
  context files. It deliberately drops upstream's leading "extensions": nothing
  in the port implements them (D41), so the notice would announce work that
  never happens. The same rule covers the other extension-flavoured user-visible
  text: the update card names the module's install path instead of `<app>
  update` (there is no package manager and no update command), the trust row's
  description says "when no saved trust decision decides project trust", the
  trust prompt and the untrusted-project warning name only the `.pi` settings and
  resources this port actually gates (not installing packages or running
  extensions), and
  the package-update card is gone: its only caller upstream asks the package
  manager about the extension packages it installed, which this port has no
  equivalent of. The install-telemetry ping is not sent either: the port has no
  telemetry backend, so the fresh-install version recording stays (it drives the
  changelog) while the reporting seam went with the endpoint, and the extension
  selector's tools-expanded hook went with the extension UI. The `pi config`
  resource-manager TUI went for the same reason:
  it exists to enable and disable package resources, and with no package manager
  there is nothing for it to list — it was 1,315 lines reachable only from its own
  golden test.  The update card is reachable: the release check runs at startup off the UI
  loop and reports on it, and it asks **this module's** release feed — the Go
  module proxy path for the module, which is what the card's `go install …@latest`
  line resolves — rather than pi's own feed, whose versions are pi's. A tag keeps
  its `v` prefix (Go module versions do), so the semver subset accepts the one
  leading `v` npm's `valid` accepts. Upstream's feed also carries release notes
  and a package name, and the card rendered them; the proxy carries a version
  only, so the card carries a version only. Nothing is reported for an unstamped
  build: its version is `0.0.0`, and a proxy pseudo-version of `0.0.0` sorts
  older than `0.0.0`. Offline, nothing is checked: upstream folds `--offline`
  and a truthy `PI_OFFLINE` into `PI_OFFLINE` and `PI_SKIP_VERSION_CHECK` for
  the whole process, and `cmd` does the same (`applyOfflineMode`).
- D133 — tool renderers always resolve to the built-in set (no extension
  definitions); `computeEditsPreview` is synchronous.
- D140 — the user's provider extensions (hyper, commandcode) are compiled in as
  `providers.ExtensionProviders()` instead of being loaded from
  `~/.pi/agent/extensions` (extension loading is out of scope).
- D141 — the builtin footer renders the user's footer-extension format (one dim
  line `3.5%/1M · <statuses> · model · cwd`) instead of upstream's two-line
  footer: upstream's render walks every session entry per frame, which in Go
  means a json.Unmarshal per message per frame (O(session size); measured 445ms
  per frame on a 10k-entry session). The footer render is cached and invalidated
  on every session event; FormatTokens/FormatCwdForFooter keep upstream
  behavior. app.UI is the TuiReference forwarder (D105) so the /tui and
  exit-replay renderer swaps reach every holder.
- D142 — the compaction summarizer follows the user's compaction extension
  (~/.pi/agent/extensions/compaction) instead of stock: a 32k no-reasoning
  call with the pi-better-compact structured prompts, the previous summary's
  file lists stripped before it is fed back, regenerated file lists capped to
  the most recent 40 per list, the PI_COMPACT_MODEL override and opencode
  routing headers. On any failure or an unusable summary it falls back to the
  stock summarizer.
- D44/D47 — Go uses function fields instead of overridable methods; listeners
  are compared by code pointer.
- D51 — `StdinBuffer` uses callbacks instead of `EventEmitter`.
- D71 — the editor requests autocomplete synchronously (upstream debounces).
- D83 — `DisableAutoRender` test seam; obsolete since D146 (every renderer is
  caller-driven, so it is a no-op kept for the test/API surface).
- D90 — narrow runtime interfaces for testability across the wirings.
- D105/D106 — the ES `Proxy` renderer reference becomes an explicit forwarder;
  clipboard copying and reading are injected (native clipboard out of scope).
- D121/D123/D135 — cross-goroutine state made mutex-safe (model refresh,
  startup input/telemetry, lifecycle flags, footer watcher). D123's input lock
  is retired in stage 1 (the submission handoff is a buffered channel); the
  others retire in stages 3-4.
- D132 — branch summarization is tracked as compaction and abortable.
- D146 — **retired**: the renderer's internal timer is gone (D146 removed the
  timer-mode render path outright) and the standalone `-r` session picker runs
  its own loop (raw input → dispatch → paint on one goroutine), so input
  dispatch and painting share the owner goroutine everywhere.
  `Renderer.mu`, `renderMu`, `Container.mu`, `Editor.mu`, `AltScreen.mu` and
  `ScrollView.mu` are deleted; render requests coalesce onto the tick channel
  and callbacks cross goroutines only through `Post` (queue under `postMu`).
  The ScrollView's transient-scrollbar hide timer became a lazy deadline
  driven by the animation walk. Off-loop readers in tests go through
  `Post`-based snapshot helpers.
- D147 — **retired** (the decoder lock and the terminal's UI-state mutex are
  gone): `StdinBuffer` is owned by the input consumer — the interactive UI
  loop feeds raw stdin chunks through `ProcessTerminal.FeedInput` and drives
  force-flushes via `PendingTimeout`/`FlushExpired` (no mutex, no internal
  timer); the keyboard-protocol negotiation state is loop-owned too. The one
  retained lock is `ProcessTerminal.writeMu`: pure write serialization between
  the OSC 9;4 progress keepalive goroutine, loop-side writers, and the
  shutdown path's raw-mode restore — I/O serialization, not UI state. The
  legacy (non-raw) reader path remains for library consumers whose Terminal
  is not driven by a UI loop.
- D148 — **closed**: the model-catalog refresh registry is lock-free (atomic
  copy-on-write map with insert-if-absent/unpublish-if-matching, atomic per
  refresh outcome, waiter count and canceled flag). Two races were fixed on the
  way: publishing must not overwrite an entry that appeared after the load, and
  a waiter slot is only claimed once the entry is confirmed published.
- D155 — **the plumbing-to-nothing inventory** (the port's wiring structs
  inject their collaborators as func fields, and several call sites nil-check a
  seam and skip — so a seam nothing assigns is a feature that silently does
  nothing). **Fixed**: the queue controller's `ShowStatus`/`ShowError`/
  `ShowWarning` (every status it raised was dropped, so ctrl+t toggled with no
  feedback), `HandlerWiring.HandleBashCommand` (`!command`), `/new`,
  `HandleCloneCommand` + the selector's `RuntimeFork` (one runtime fork,
  `coding.ForkSessionAtEntry` + `App.forkAtEntry`), `/import`
  (`ImportFromJSONL`), `OnExternalEditor`, the auth `ScheduleTimer`,
  `OnLabelChange` and `ApplyFullscreenScrollbarSetting`. **The dialog seams are
  now wired too**: `ShowExtensionConfirm` and `PromptForMissingCwd` are
  **callback-based** (`onAnswer`/`onCwd`) rather than value-returning, because
  the selector slot's `Show` mounts a component and returns immediately while the
  seams returned the answer — upstream's equivalents are awaits, so the
  synchronous shape was a mis-port. That is what had left `/import`'s
  "Replace current session?" prompt and the missing-cwd prompt of `/import` and
  the resume flow unreachable; both flows are now callback chains matching
  upstream's sequential awaits, and the dialogs themselves are app-level helpers
  over the selector slot, built on the extension selector component that already
  existed (title, description, options, timeout). The retry also had to key on
  the typed `coding.MissingSessionCwdError`: the text check it used before
  (`strings.Contains(err, "cwd")`) could never match upstream's wording, which
  says "working directory". **Closed**: `ClearStatusContainerIfIdle`
  is implemented (upstream's `!enabled && !activeStatusIndicator &&
  statusContainer.clear()`, shared with the reload path instead of copied), and
  the last two are not work — `RebindSession` is **redundant**: upstream's
  runtime calls a mode-provided rebind after replacing a session, and the port
  does that inline in `applySessionReplacement` (swapping the session and every
  wiring's `SessionInfo`) with the initial binding done at composition, so an
  init-time hook has nothing left to do; `OnPartialEventApplied` is a **test
  seam**, labelled as one where it is declared. The login dialog's select step
  (`ShowAuthSelect`) is implemented — the Amazon Bedrock flow asks one, so the
  login could not get past its first prompt — as a list inside the login dialog,
  which already owns its input routing, so no focus switch was needed; and
  `OnPromptShown` is not dead but an explicitly-labelled test seam (nil simply
  means no hook), like the `Now`/`ScheduleTimer`/`DeleteSession` overrides. **Deliberately off** (out of
  scope): the HTTP dispatcher, the package manager, highlight languages (D74, and
  the user chose to keep the flat fallback), tmux, the process-level seams and
  extension mechanics (D41) — with them `ShowExtensionSelector`, the extension
  resource selector, which has no port counterpart.
  **Not dead, despite looking like the rest** — check whether the nil branch
  skips the work or falls back before treating an unassigned seam as a bug:
  `SessionSelectorOptions.DeleteSession` and `CopyActiveSelection` are overrides
  with working defaults (the deleter and the AltScreen's own selection copy), and
  `MaybeSaveTrust` is a field nothing references at all while the feature it
  names is implemented and called from the reload path. A source-scanning test
  that flags these mechanically was written and dropped as not worth its keep
  (AST heuristics plus a maintained allowlist, which drifts); this row is the
  record instead.
- D154 — **the port ships its own theme palette**.
  `coding/interactive/piertheme.go` defines two themes — one for a dark terminal
  background, one for a light one — and the CLI installs them at startup under
  the upstream names (`dark`, `light`), so the whole settings/terminal-detection
  path (`ResolveThemeSetting`, `ParseAutoThemeSetting`, `GetDefaultTheme`) is
  unchanged and still picks the variant from the terminal. Three deliberate
  departures from upstream's `dark.json`/`light.json`: **the decorative
  background tokens are left unset** (the selected list row, search matches,
  custom messages), which renders as the terminal's default background
  (`\x1b[49m`) so the theme never paints over a transparent or blurred terminal
  (primary text is the terminal's own foreground for the same reason); **the
  signal fills are kept** (`toolPendingBg` / `toolSuccessBg` / `toolErrorBg`, a
  neutral panel while a call runs and the success/error hues as a tint of it,
  plus `userMessageBg`), because they are not decoration. The tool three are the
  *only* signal upstream has that a tool call failed
  (`components/tool-execution.ts` picks between them and fills the whole block;
  the port does the same in `toolexecution.go`), so leaving them unset made a
  failed `bash` call byte-identical to a successful one; the user fill is the
  only thing that marks a block as yours, since the assistant message has no
  fill and both are otherwise plain markdown in the same box. All four are also
  painted by the HTML export (`.tool-execution.success/error` and the user
  block, via `--toolSuccessBg` and friends), which came out blank as well. Their
  values are the port's own, not upstream's: a user message is a warm panel tied
  to the accent (`#393630` dark, `#f4eee1` light) where upstream's is a cool
  blue-gray (`#343541`) or plain grey (`#e8e8e8`); and **the accent is amber
  rather than upstream's teal** — it carries the wordmark, borders, selection
  and list bullets, so which build is running is obvious at a glance. Selection
  stays legible without a fill because every list marks the current row with an
  accent-coloured `→ ` prefix. The fills are chosen to stay distinct after the
  256-colour conversion (`rgbTo256` sends a low-spread dark to the grey ramp and
  pushes a hue into the cube: the warm `#393630` lands on grey 237, one ramp
  step from `toolPendingBg`'s 235, and a subtler dark would have collapsed onto
  the same index), so the states remain distinguishable on a terminal without
  truecolor — and `piertheme_test.go` pins all of it. The embedded upstream
  palettes are kept: they are upstream's reference palette, they are what the
  **upstream-parity test corpus renders with** (those tests clear the theme
  registry first, so they are unaffected by the install), and they remain the
  fallback for library consumers that never call the installer. Two fixes fell
  out of this: `loadThemeJSON` checked the built-ins *before* the registry while
  `loadTheme` checked the registry first, so a theme shadowing `dark` rendered
  as the override but resolved its export and resolved-colour tokens from the
  built-in — both now prefer the registry — and `Theme` carries its source
  document, so a registered theme that has no file on disk can still be exported
  to HTML. The install must follow the truecolor/style capability switch, since
  a theme bakes its 256-colour or truecolor escapes at creation time.

- D153 — **the CLI's startup flags are wired** rather than merely parsed
  (`cmd/main.go`, with the pure parts in `coding/clidiagnostics.go`,
  `coding/cliinitial.go`, `coding/listmodels.go`, `coding/paths.go` and
  `coding/sessionresourceload.go`). Fifteen documented flags plus two
  non-flag inputs were read by nothing: an unknown single-dash option, a bad
  `--thinking` value or a blank `--name` was accepted silently,
  `pier @notes.txt "explain"` sent no file at all, and `--use-theme` left the
  configured theme in place.
  - **Diagnostics** are reported right after parsing — before `--version`, as
    upstream does — and an error among them exits 1.
  - **`@file`** text is folded into the session's first message ahead of the
    first positional message, with the rest queued behind it, matching upstream
    `buildInitialMessage`. `@file` **images** ride that same first message as
    `ImageContent` attachments (`ProcessFileArguments` → `InitialPrompt.Images` →
    `RunOptions.InitialImages` → the first prompt's `PromptOptions.Images`), the
    way upstream attaches them, and only that prompt carries them. *Remaining
    gap*: print mode's prompt path is text-only, so an image there is still
    reported and dropped rather than sent.
  - **`--skill`, `--prompt-template`, `--theme`** paths are resolved against the
    working directory (`ResolveCLIPaths`/`IsLocalPath`, a port of upstream
    `resolveCliPaths`), and each **survives its own `--no-*`**: refusing
    discovery is not refusing what was named, which is upstream's asymmetry in
    its resource loader. `--no-skills`, `--no-prompt-templates` and
    `--no-context-files` suppress the settings' paths and discovery;
    `--no-themes` drops the discovered set (the agent's `themes/` directory and
    the settings' theme paths) likewise. Prompt templates now reach the session
    for the first time — `LoadPromptTemplates` existed with **no caller**, so
    `/template` expansion, the prompt commands and the `[Prompts]` startup
    section were all inert.
  - **All of those switches survive `/reload`**: they are kept on the session and
    re-applied by `reloadResources`, which previously rebuilt the resource set
    from the settings alone — so a reload used to undo `--no-skills`, resurrect
    the settings' skill paths and drop an explicit `--skill`.
  - **Theme discovery is now the only lookup path**: a theme resolves by its
    *declared* name across the sources, as upstream's loader does, rather than by
    its file name under one directory. The old `<dir>/<name>.json` fallback is
    gone because it ignored the discovery switch, which made `--no-themes` leak —
    the theme vanished from the list but still loaded by name.
  - **`--models`** sets the model cycle scope, overriding the settings' enabled
    models, and reports a pattern that matched nothing at startup rather than
    silently scoping nothing. A scope also **seeds the model**, which it did not
    before: with nothing named and a new session, the settings' default is used
    when the scope contains it and the first scoped model otherwise (upstream
    `buildSessionOptions`), including the thinking level a
    `model:thinking-level` pattern carries unless one is pinned. **`--api-key`** pins the credential on the provider
    of the model the session resolved. **`--name`** records a `session_info`
    entry (blank is an error). **`--approve`/`--no-approve`** settle project trust
    for the run. **`--export`** and **`--list-models`** are implemented and exit
    before the TUI.
  **`-e`/`--extension`** is parsed into `Extensions` and reported as ignored
  ("this build loads no extensions"), because extension mechanics stay out of
  scope (D41) and silently accepting a path that does nothing is worse than
  saying so; a missing value is an error like the neighbouring flags.
  **`-ne`/`--no-extensions`** is parsed into `NoExtensions` and says nothing: it
  asks for fewer extensions, and there are none. Neither is in `--help` any more,
  and the tool descriptions no longer mention extension tools.
  Upstream's unknown-flag split is matched exactly, and tested: an unknown
  **long** flag is recorded in `UnknownFlags` for extensions to consume and is
  not an error (upstream's `args.js` does the same), while an unknown **short**
  flag is an `Unknown option: -z` error diagnostic.
- D152 — the Unix socket **publish is portable** (`server/unix.go`,
  `server/publish_linux.go`, `server/publish_other.go`). Upstream publishes a
  bound socket with a hard link, which is atomic and refuses to overwrite — so a
  socket another process created at the path in the meantime is never clobbered,
  and the surviving inode is the one whose device/inode the cleanup path
  recorded. **Android denies `link(2)` to the app domain outright**, so every
  bind failed there and the whole `server/unix` test suite failed with it.
  `publishSocket` now tries the link, then `renameat2(RENAME_NOREPLACE)` —
  atomic, and still refusing to overwrite — and only then a plain rename, which
  keeps atomicity but may replace a path that appeared in the window. An
  occupied destination is never replaced on any path: a link that fails
  `EEXIST` returns immediately rather than falling through. `renameNoReplace`
  is `//go:build linux` (Android satisfies the `linux` tag); elsewhere it
  reports `errors.ErrUnsupported` and the plain rename is the fallback.
- D151 — **defaultsync builtin** (`coding/defaultsync.go`, wired in
  `coding/sdk.go` and `coding/agent_session_reload.go`). A port of the user's
  defaultsync extension: pi scopes the model to the session, so a session that
  once picked a model keeps it across resumes and the settings default
  (`defaultProvider`/`defaultModel`) never reaches it again. Every session
  start — creation and `/reload` — now moves the session onto the settings
  default, which is what the extension did on `session_start`. A manual switch
  therefore lasts for the current session only; there is no persisted
  per-session claim, no command, and no opt-out short of clearing the default.
  The full catalog model object is applied, never a bare ref (a ref without its
  limits reaches the footer as `?/0`). **Two deliberate differences from the
  extension**: (1) it polls (150 ms over a 4 s budget) because pi's
  provider-auth snapshot lands asynchronously, whereas this port's
  `queueAvailabilityRefresh` runs inline, so one attempt is equivalent and the
  same two outcomes — `no configured auth`, or `no configured auth, or not in
  the catalog` — are reported verbatim; (2) an explicit `--model`/`--provider`
  choice (a non-nil `CreateAgentSessionOptions.Model`) suspends the sync for
  that session, including across reloads, because overruling an explicit
  command-line choice would be a surprise the extension never had to consider.
  This diverges from upstream's session-scoped model restoration by design;
  the notice is surfaced with the startup diagnostics (informational on a
  switch, a warning when the default cannot be applied).
- D150 — the startup "loaded resources" area ports the **Skills**, **Context**
  and **Prompts** sections of upstream `showLoadedResources` (collapsed name
  list plus the expanded project/user/path scope groups), the skill
  warnings/collisions block (`[Skill conflicts]`), and the ctrl+o expand
  toggle now drives the header and section expandables (`SetToolsExpanded`).
  Themes and Extensions sections are not rendered (the Go loader exposes no
  custom-theme SourceInfo and extension mechanics are out of scope, D41/D140).
  `coding.LoadSkills` now matches upstream's loader: paths resolve
  (`~`/relative, `ResolvePath`), skills dedupe by canonical real path, and
  name collisions keep the first and record a `collision` diagnostic — the
  prior port appended both the default agent-dir skills and a settings path
  pointing at the same directory, duplicating every skill.
- D149 — `FooterDataProvider.mu` (`coding/footerdata.go`) is the one lock the
  refactor leaves in place, by design. It guards the provider's cwd, git paths,
  branch, extension statuses and listener registry, which its own 500 ms
  **watcher goroutine** (git HEAD polling) shares with the UI loop's footer
  render. Closing it needs the same treatment as the accepted
  `SessionManager` fix plus a decision on the watcher's delivery: the poll
  result would have to be posted to the UI loop (like the theme watcher now is)
  and the listener fan-out delivered outside the lock. That is a `coding/`
  change outside this refactor's scope, so it is documented rather than
  retired; the provider is otherwise loop-idiomatic (its polling goroutine only
  reads git state and calls `Refresh`).
- D145 — terminal input, resize and process signals reach the UI loop as
  channel messages from pure producers (the stdin reader, the SIGWINCH
  watcher, the signal handlers); the loop dispatches input, paints on resize
  and runs the shutdown work on its own goroutine, where upstream delivers
  these on the single JS thread.
- D144 — the interactive renderer runs in caller-driven tick mode (the run
  loop coalesces render requests and paints once per drain) instead of
  arming its own throttled render timer; the standalone session picker and
  library users keep the timer.
- D143 — the interactive run loop is an explicit `select` over producer
  channels (session events, submissions, work completion, `ctx.Done()`) with
  blocking work off-loop and latest-wins coalescing for streaming partials,
  where upstream is single-threaded and awaits the prompt; the stage-3 to
  stage-4 stages move the remaining producers (input/signals, selector and
  lifecycle callbacks) onto the same loop.
- D136 — editor `OnSubmit` runs outside the editor lock.
- D137 — model-selector callbacks run outside the state mutex.
- D138 — terminal input is delivered outside the terminal lock.
- D139 — stdin-buffer callbacks are emitted outside the buffer lock.
- D160 — **the runtime follows the session's directory, and trust is resolved
  there**. Upstream builds a whole runtime per cwd: `main.ts`'s `createRuntime`
  factory takes `sessionManager.getCwd()`, resolves that directory's project
  trust (`resolveProjectTrusted`, its per-cwd cache `projectTrustByCwd`, and —
  only for the initial runtime — the interactive prompt), and builds a settings
  manager, resource loader and session for it. Every session replacement goes
  through it again, so resuming or switching into a session from another
  directory runs in *that* project: its settings, skills, prompt files, context
  files, system prompt and trust. This port keeps one settings manager per
  process, so:
  - **at boot the runtime cwd is the session's**, not the process's: `cmd`
    resolves trust, builds the runtime settings manager and creates the session
    with `sessions.GetCwd()`, which matters exactly when a resumed session
    belongs to another directory — the port used to boot such a session with the
    starting directory's settings and trust. CLI path flags stay
    process-relative, as upstream's `resolveCliPaths(cwd, …)` does.
  - **a switch resets the view the way a replacement does** (upstream
    `rebindCurrentSession` → `renderCurrentSessionState`): the loaded-resources
    and pending-message containers, the compaction queue, the streaming component
    and the pending tools are dropped, and the transcript is re-rendered through
    `renderInitialMessages` — not the reload's `rebuildChatFromMessages`, which
    skips the initial render's trust warning, its footer update and the editor
    history it repopulates. `StartupWiring.RenderCurrentSessionState` had no
    production caller before this.
  - **a switch re-points the settings manager** (`SettingsManager.RebindProject`)
    at the new cwd after resolving its trust with `hasUI` false, so an undecided
    project stays untrusted rather than being asked mid-session (upstream reports
    `hasUI: isInitialRuntime && mode === "interactive"`). Re-pointing rather than
    rebuilding is why no holder of the manager can go stale — a switch that
    rebuilt it would have to reach every wiring that cached it. The run's own
    overrides are re-applied (`--use-theme`), and the settings-derived UI state
    is re-applied (`applySettingsDependentUI`, upstream's `applyRuntimeSettings`
    from `rebindCurrentSession`).
  The trust answer is remembered per project for the run (upstream's
  `projectTrustByCwd`), seeded with the boot answer and filled by each switch, so
  a project resolved once is neither re-read nor re-asked. Theme sources follow
  the same boundary: a project's `.pi/themes` is discovered only for a trusted
  project, and the sources are installed again once trust is decided, because the
  install that the `-r` picker needs happens before any project resource is
  readable.
  What remains is D41's: there are no per-cwd extension services, and the model
  runtime is one process-wide instance. That costs nothing: `models.json` lives
  in the agent dir on both sides (upstream resolves the agent dir once and its
  cwd-bound services carry it over, so switching sessions never changes which
  file is read), and the port re-reads it on `/model` rather than on switch. Everything else about the project does follow: settings,
  skills, prompt templates, context files, system prompt, themes and trust.
- D159 — **the port's session-wide accounting is non-blocking**. Upstream
  computes the `/session` panel (`handleSessionCommand`: the session statistics,
  the cache-waste totals and the usage cost breakdown) inline on its UI loop, and
  re-walks `sessionManager.getEntries()` for the transcript rebuild's cache-miss
  notices. Both are fine upstream because entries hold parsed objects; this port
  stores raw JSON, so each scan re-parsed the session, and the UI loop is one
  goroutine ("dispatch, then render and route input"), which turned a panel into
  a freeze. Measured on a 19k-entry, 45 MB session: typing immediately after
  `/session` became visible only after 0.857s, and the rebuild's cache-miss scan
  cost 563ms cold — on `/reload`, a session switch, `/tree`, compaction end and
  startup.
  Two changes:
  - **the panel is built off the UI loop and posted back to it.** Dispatch
    returns at once, and the finished panel is appended on the next pump. What
    the formatting reads is mutex-guarded session state (SettingsManager,
    SessionManager, the agent's copied state, the projection), never the
    component tree; with no UI to post to (headless wiring) it still computes
    inline, as upstream does. The same session now echoes the keystroke after
    0.038s and the panel appears 0.69s later in the background.
  - **the session-wide accounting is folded, not re-walked.** The manager already
    read every entry's `role`/`usage`/`provider`/`model`/`timestamp` as entries
    arrived, to seed the running scan state that makes a live cache-miss notice
    O(1). That same pass now records what the statistics, the usage cost
    breakdown and the cache-miss scan need, and they account those records with
    `detectCacheMissFor`/the same aggregation the reference functions use — so the
    folded numbers cannot drift — decoding a message only for the misses that are
    actually counted (the transcript keys its notices by the memoized message
    pointer). The rebuild's scan went from 563ms to 30ms on that session, and the
    full scan is still there for callers without a session.
  Everything the panel needs is folded, so the panel itself is fast too: the same
  pass records each message's `role`, `usage`, `provider`, `model`,
  `responseModel`, `timestamp` and — for an assistant message only — the block
  types of its content array, which is what makes the tool-call count exact: the
  role and the array are read, the payloads are skipped, so the pass stays a scan
  rather than a decode. The statistics and the usage cost breakdown are then
  folds over those records. On that session the panel's scans went from 629ms to
  31ms, and the panel is visible 0.080s after the keystroke, against 0.857s for
  the freeze it replaced. `content` stays raw JSON in the facts because a user
  message keeps a string there — decoding it as a block list failed the whole
  record.
  Costs this moved rather than removed: reading the content array raises a load
  of that session from 507ms to 610ms (before the TUI exists at startup, but on
  the UI loop for `/reload` and a session switch), and the reference entry-list
  functions stay for callers without a session.
  The three gaps this D-row recorded are closed: the fold attributes
  `type === "usage"` entries (cache-warming spend) to their `provider/model`, as
  upstream's breakdown and statistics both do; the synthetic context messages
  are stamped from `entry.timestamp` instead of projection time; and a
  compaction's recorded system message is read back and emitted before its
  summary.
- D158 — **the session picker has a stacked layout for narrow terminals**.
  Upstream renders the resume selector — `/resume` in the app and `pier -r`
  (`components/session-selector.ts`) — as a single-line header (title on the
  left, scope/name/sort pinned right) plus one line per session with the
  message count, the age, and optionally the cwd or path in a right-hand column,
  and truncates each of those lines to the width. That is fine on a desktop
  terminal and unusable on a phone: this port is developed on Termux, where a
  portrait terminal is 40-55 columns, and there the header shows the title and
  at most one control, the hints end in `…` before the keys that do the work,
  and the metadata column leaves roughly twenty columns for the session's
  message. Below `mobileTerminalWidth` (60) the picker stacks instead: the title
  keeps its line and the controls move to the next one, the two hint lines are
  wrapped at their separators rather than truncated, and each session becomes
  two lines — the message at the full width, the metadata indented beneath it.
  A screenful therefore holds half as many sessions (`maxVisible/2`, which also
  becomes the page-up/page-down step), and the list's own windowing and paging
  follow the same rule. Above 60 columns nothing changes: upstream's layout is
  reproduced byte for byte, which is why the upstream golden corpus passes
  unchanged there — and why its one 44-column case (`narrow`) is now the single
  corpus render the port deliberately does not reproduce, skipped in
  `sessionselector_test.go` with a pointer here.
- D157 — **path completion treats a trailing `.` or `..` as a directory, not as
  part of the name to split on**. `getFileSuggestions` expands `~` first and then
  splits the result into directory + file prefix, and both `path.join` upstream
  and `filepath.Join` here *clean* the path — so expanding `~/.` collapses the
  `.`, `filepath.Dir` walks up to home's parent and `filepath.Base` hands back
  home's own name. Typing `~/.` and pressing Tab listed `/home` and offered
  `~/dat/` as the single suggestion, which the editor then applied. **Fixed**: the
  raw prefix is split before expanding, a trailing `.` keeps its role as the
  filter (a shell completes `~/.` with dotfiles only) while `..` clears the
  filter and stays in the suggestion. Upstream has the same bug, so the fix is a
  divergence, guarded by TestFileCompletionUnderHomeFiltersInsideHome.
- D156 — **the login flow runs off the UI loop** (the fifth deadlock, and the
  first found by reading rather than by driving the binary in a PTY).
  `ShowLoginDialog`/`ShowApiKeyLoginDialog` called `LoginProvider` inline, and
  the flow waits on the dialog for its answer — `ShowAuthPrompt` selects over
  the dialog's input channel — so the loop sat inside the flow, waiting for a
  keystroke only the loop could deliver. Every provider whose login prompts
  hung: OAuth device flows, API-key entry, Bedrock's method/profile selects.
  The dispatch is on the loop, the same reason the neighbouring
  `HandleBashCommand` needed its off-loop fix. **Fixed**: `AuthWiring.startLogin`
  (shared by the OAuth and API-key dialogs, which differed only in their message
  prefixes) runs the flow on its own goroutine and posts its continuation —
  editor restore, error report, `CompleteProviderAuthentication` — and
  `LoginDialogComponent` takes a `post` func so its own `Show*` mutations are
  marshaled onto the loop. The prompt channel is still created synchronously and
  the browser opener stays on the flow's goroutine (exec can block); with a nil
  `post` the mutations apply inline, which is how the dialog tests drive it.
  Upstream awaits the flow inline, which its single-threaded runtime can afford.
  `TestApiKeyLoginRunsOffTheUILoop` models production's single loop goroutine —
  dispatch, then render and route input — and fails ("the login flow is running
  on the UI loop") instead of hanging when the dispatch blocks.

- D161 — **the port sweeps its own temp output files**. Full tool output over the
  truncation limits is written to `<tmpdir>/pi-bash-<id>.log` — the shared
  accumulator's `pi-output` and the PowerShell tool's `pi-powershell` prefixes
  likewise — and handed to the model as "[Output truncated. Full output: <path>]".
  Upstream never deletes those files, leaving them to the OS (systemd-tmpfiles, a
  reboot). On a host where `/tmp` is tmpfs that scratch is RAM, and one 174 MB
  command sits there for days: measured on this project's host, 230 files /
  442 MB after three days, none of it read again. The port sweeps its own files,
  oldest first, down to a 256 MB budget, at the moment a temp file is created —
  exactly when the pile grows — and never touches the file being written, another
  tool's log, or a directory whose name happens to match. `TestSweepTempOutputFiles`
  pins that, including the oldest-first order.

- D162 — **print mode is entered only by an explicit flag**. Upstream's `main.ts`
  auto-enters print mode when stdin or stdout is not a TTY, so `echo hi | pi`
  runs headless without `-p`. The port requires `-p` or `--mode json`; without
  one of those it runs the interactive mode (or fails to open a terminal), so a
  non-TTY invocation is never silently answered by a single-shot run. Everything
  else in print mode follows `print-mode.ts`: the `-p` text mode prints the final
  assistant message's text, JSON mode streams the header followed by one line per
  session event with the cumulative `partial` stripped from `message_update`, and
  a prompt failure or an error/aborted stop reason goes to stderr with exit
  code 1. The JSON header line carries the session file's own `"type":"session"`
  discriminator (marshaled via `MarshalFileEntry`), matching upstream's
  `JSON.stringify(sessionManager.getHeader())`. `@file` images are warned and
  ignored (D153); extensions and the runtime-rebind plumbing upstream carries
  are out of scope (D41).

- D163 — **renderer low-bandwidth mode**. Retained as port history: this mode
  was removed for upstream parity. SSH rendering now uses upstream's padded
  frame output; SSH Escape timeout and wheel-scroll behavior remain aligned
  with pi's `packages/tui/src/terminal.ts` and `wheel-scroll.ts`.

- D164 — **input paints only when the dispatch asked for one**. The interactive
  loop's input arms used to call `paint()` for every raw stdin chunk. Fullscreen
  mode enables `?1003h` (any-motion mouse tracking), so the terminal reports
  every pointer movement — one event per pixel — and one frame is O(the whole
  transcript): on a large session the loop spent its entire budget on full
  repaints, and mouse movement (and every keystroke queued behind it) was
  unusable. The arms now drain every raw chunk already queued, then paint once,
  and only if the dispatch queued a render request (`paintIfRequested` consumes
  the coalesced tick that `RequestRender` left behind, which also stops the 16 ms
  frame timer from repainting the same request). Measured on the port: 60 mouse
  moves plus one keystroke cost 2–3 paints, down from 63. Input that asks for no
  paint is therefore not painted; the renderer's contract is that components
  call `RequestRender` and every keyboard path signals one in
  `HandleTerminalInput`, so only events that changed nothing (a bare move, a key
  release, a terminal reply) skip a frame. Two supporting changes: the loop
  caches the renderer's animation walk between paints (the walk visits every
  mounted component and the loop asked for it once per input event), dropped by
  the paint that can change a component's animation state; and `VisibleWidth`
  short-circuits printable-ASCII text after stripping sequences, because a
  styled line never reached the plain-ASCII fast path and instead allocated a
  grapheme slice and range-searched twice per character (5.0 µs → 1.0 µs, 1104 →
  324 B/op on a styled tool-output line). Tests:
  `TestMouseMotionBurstDoesNotRepaint`, `TestAnimationScanCacheIsDroppedByAPaint`,
  `TestAltScreenMouseMoveRequestsNoRender`, `TestVisibleWidthStyledTextMatchesPlainText`.
- D165 — **the terminal background probe is listener-driven, not a blocking
  query**. Upstream detects the terminal's light/dark preference by querying it
  synchronously at theme-apply time (`queryTerminalBackgroundColor` waits on a
  channel, `setInterval`-free). The port cannot: the UI loop is the only
  dispatcher of terminal replies (`Renderer.HandleTerminalInput`), so a query
  called on the loop can never receive its own reply and always times out, and a
  query written before the pty is in raw mode is echoed back into the input
  stream (this is what froze the TUI over SSH when the feature was first
  attempted). The port instead writes a single OSC 11 query
  (`Renderer.RequestTerminalBackgroundColor`, `ESC ] 11 ; ? BEL`) after the
  terminal is started and returns immediately; the reply is consumed in
  `HandleTerminalInput`, classified by luminance (`GetThemeForRgbColor`), and
  applied by a `OnTerminalBackgroundColorChange` listener on the loop. The
  `CSI ? 2031` color-scheme notification toggle is likewise deferred: a request
  before `Start` only flips the flag and `Start` replays it, so the mode sequence
  is never echoed. `app.theme.ProbeTerminalBackground` is invoked from
  `RunWiring.OnStarted` (after `UI.Start`, i.e. raw mode + live reader) and
  re-armed on a renderer swap (`RebindTUI`). A terminal that does not answer
  leaves the `COLORFGBG`/fallback theme in place; nothing blocks on the reply.

- D166 — **the global theme is a stable handle, mirroring upstream's `theme`
  Proxy**. Upstream exports `theme` as a `Proxy` that reads
  `globalThis[THEME_KEY]` on every property access, so a component that calls
  `theme.fg(...)` at render always sees the current theme and a switch needs no
  rebuild (`onThemeChange` only invalidates and refreshes the editor border).
  The port returned the current concrete `*Theme` from `ActiveTheme()`;
  components captured that pointer at construction, so a switch left the
  transcript with the old colors and the first fix attempt added an ad-hoc
  `App.rebuildForTheme`. `ActiveTheme()` now returns one stable `*Theme` handle;
  `Fg`/`Bg`/`GetFgAnsi`/`GetBgAnsi`/`ColorMode` forward to `CurrentTheme()` on
  every call (`Theme.concrete`), factories such as `GetMarkdownTheme` close over
  the handle, and `Renderer.Invalidate()` clears the render caches so the next
  paint re-resolves. The header and other components that bake `theme.fg(...)`
  into a `Text` at construction stay baked in both implementations, so
  `onChanged` remains upstream's `updateEditorBorderColor`. Tests:
  `TestThemeHandleResolvesTheCurrentTheme`,
  `TestMarkdownThemeRecolorsAfterASwitch`.

- D167 — **a slice keeps ANSI order across its start boundary**. Upstream's
  `sliceWithWidth` (`utils.ts`) writes a code met inside the range as it is met,
  but buffers the codes from before the range and flushes them when the first
  text is written. A code that applies at the range's first column therefore
  lands ahead of the prefix it belongs after. A selection ending exactly where an
  inline-code span's reset sits is the case that shows it: the span renders
  without its backticks, so a double-click on the word inside it ends on the
  reset, the tail slice wrote the reset before the colour it cancels, and the
  rest of the line kept the span's colour — the remaining text turned yellow.
  The port flushes the carried prefix before writing an in-range code. The golden
  corpus is unchanged (it never covers a slice starting on a reset), and
  `tui/sliceansiorder_test.go` pins the order.

- D168 — **the Escape handler does not wait for the session to go idle**.
  `AgentSession.Abort` cancels the turn and then waits for idle; upstream's
  `session.abort()` is awaited the same way, but awaiting a promise in JavaScript
  yields to the event loop, so the window keeps painting and keeps taking input.
  The port's `waitForSessionIdle` parks the goroutine instead, and the Escape
  handler runs on the UI loop: pressing Escape while a turn streamed froze every
  later keystroke for as long as the turn took to unwind. Measured on a live
  session — `slow UI phase "raw-input" 188ms` with the loop parked in
  `waitForSessionIdle` (`HandleEscape` → `RestoreQueuedMessagesToEditor` →
  `Abort`) — while the neighbouring keystrokes showed `read=8.8ms write=91ms`.
  `AbortAsync` is the loop-safe half: it cancels the retry, the bash run and the
  agent run, and returns. `Abort` keeps its wait for the two callers that need
  the session settled before they continue — session replacement
  (`sessionswitch.go`, which disposes the session next) and tree navigation
  (`interactivemode_selectors.go`, which reads the tree next) — so those two stay
  deliberately blocking. The queue restore already happens before the abort, so
  nothing on the loop needs the turn to have unwound. Tests:
  `TestEscapeHandlerDoesNotWaitForIdle`.

- D169 — **a mouse report that arrives split is still a mouse report**. The input
  buffer holds an incomplete escape sequence for 50 ms, and a lone ESC for 10 ms,
  then force-flushes it — that is how the Escape key itself is delivered. A
  terminal that writes a report in two pieces across that deadline therefore
  delivers `\x1b` as an Escape key and the rest as a plain chunk, and the buffer
  shreds a plain chunk into one sequence per rune: the prompt received
  `[<65;59;42M` as typed text (wheel-down at column 59, row 42). Seen on Windows,
  where `ReadConsole` returns as soon as any byte is available, so a busy loop can
  leave the two pieces more than 10 ms apart. Upstream requires the ESC
  (`/^\x1b\[<.../`), so its tail is typed the same way. The port remembers what a
  flush released while it could still be a report head — a lone ESC, or a
  truncated `\x1b[<...` — and re-joins the next chunk when that chunk completes a
  report, so the wheel still scrolls and nothing reaches the prompt; any other
  chunk clears the memory instead of holding a user's typing back. The alt screen
  additionally accepts a report without its ESC and consumes a truncated
  fragment of one. Residual: a report split again mid-tail still arrives as text,
  and the Escape key it was split from has already been dispatched. Tests:
  `TestSplitMouseReportAcrossTheDeadline`,
  `TestOrphanedMouseReportIsStillDispatched`,
  `TestTruncatedMouseReportIsConsumed`.

- D170 — **the startup header drops upstream's "Pi can explain its own features"
  hint**. Upstream's onboarding block ends with `Pi can explain its own features and
  look up its docs. Ask it how to use or extend Pi.` (`interactive-mode.ts:997`),
  used in both the compact and the expanded header. The port omits that line by
  request: the product it ships is pier, and the hint advertises the capability
  under the upstream name. The key-binding lists and the ctrl+o pointer above it are
  unchanged.

- D171 — **OS-level socket aborts are retryable**. Upstream's retryable list
  (`packages/ai/src/utils/retry.ts`) covers fetch, HTTP and websocket phrasings —
  `socket hang up`, `connection refused`, `connection lost`, `reset before
  headers`, `timed out` — but not the strings the OS produces when a socket dies
  under an open request: `software caused connection abort` (ECONNABORTED, what
  Termux and Windows emit), `connection reset by peer` (ECONNRESET), `broken pipe`
  (EPIPE) and `unexpected EOF`. A request that hit one failed with no retry at
  all. The port matches those four; the quota/billing guard is still evaluated
  first, so a usage-limit error that also mentions a reset is not retried.

- D173 — **one plain label while compacting**. Upstream's
  `CompactionStatusIndicator` (`components/status-indicator.ts:84`) branches on
  the reason: `Compacting context... ${cancelHint}` for a manual `/compact`, and
  `${reason === "overflow" ? "Context overflow detected, " : ""}Auto-compacting...
  ${cancelHint}` for one that the threshold or an overflow triggered, with
  `cancelHint` the bound `app.interrupt` key. The port shows `Compacting...` for
  every reason and drops the hint, so the spinner line reads the same whether the
  compaction was asked for or automatic. The `reason` argument is still plumbed
  through the constructor so its shape matches upstream's; no caller changed.
  The rendered frame is pinned by the `m status compaction` row of
  `coding/interactive/testdata/message_golden.txt`.

- D174 — **the update check reads GitHub releases, and `pier update` installs the
  release asset.** Upstream's card says `<app> update`, and that command runs its
  package manager (updating pi and its packages); the port has no package manager
  (D41), so the check reads `GitHub's `/releases/latest` redirect` and
  `pier update` installs the `pier-<goos>-<goarch>` asset attached by
  release.yml, verifying it against the `.sha256` beside it before
  replacing the running binary (on Windows the running image is moved aside first,
  which upstream calls its self-update quarantine). Three consequences worth
  recording: the module proxy is no longer consulted, so nothing depends on the
  port being published to proxy.golang.org, and the card names `pier update` and
  links this module's release page instead of naming `go install` and pi.dev's
  changelog; a build whose version cannot be ordered against a tag — the commit
  hash `just install` stamps, or the unstamped 0.0.0 default — gets no card at
  all, because upstream's `isNewerPackageVersion` falls back to string inequality
  when a side is not semver, which the port can reach and upstream cannot (its
  current version is always package.json's), so every source build would read as
  out of date forever; and a release asset arrives without its executable bit,
  which the update has to set.

- D175 — **the android build gets a DNS resolver fallback.** Every build here is
  CGO_ENABLED=0, so `net`'s pure-Go resolver reads `/etc/resolv.conf`; Android has
  no such file, because `/etc` is a symlink to the read-only `/system/etc`. The
  resolver then falls back to the loopback defaults, `127.0.0.1:53` and
  `[::1]:53`, and every lookup fails with "connection refused" — so an android
  release asset could not resolve anything on the device it exists for, while the
  same source built by Termux's patched Go could, since Termux keeps its
  nameservers in `$PREFIX/etc/resolv.conf`. On android, when the stock config is
  missing, the port reads `$PREFIX/etc/resolv.conf` (or `PIER_RESOLV_CONF`) and
  installs a `net.Resolver` that dials those nameservers, rotating between them.
  Upstream has no equivalent: Node resolves through the platform's libc, which
  never consults resolv.conf.
  The API is deliberately not used: an unauthenticated client gets 60 requests an
  hour per IP and then a 403, and a mobile IP shares that budget with everyone
  behind it, so `pier update` failed on a device that had never called the API —
  measured as `x-ratelimit-remaining: 0` against `4991/5000` for an authenticated
  client. The redirect is not rate-limited, and the asset names are known, so no
  listing is needed either.

- D176 — **the system prompt omits upstream's documentation index.** Upstream's
  `buildSystemPromptSections` emits a `docs` section that points the model at the
  pi README, `docs/` and `examples/` and routes feature questions to specific
  files (extensions.md, themes.md, skills.md, prompt-templates.md, tui.md,
  keybindings.md, sdk.md, custom-provider.md, models.md, packages.md,
  environment-variables.md). A pier install ships none of that tree: its `docs/`
  holds PORTING.md, DIVERGENCES.md, architecture.md and locks.md, and there is no
  `examples/`, so the section only routed the model to files that are not there.
  The port drops the section (`coding/systemprompt.go`); the reference is
  otherwise unchanged, including the preamble that still names pi as the harness
  the port follows.

- D177: **native filesystem sandbox removed to restore stock pi parity.**
  The port previously added read-only, workspace-write, and full-access modes,
  a Linux Landlock launcher, write/edit path guards, `/permissions`, footer
  mode codes, and a sandbox system-prompt section. All are now removed.
  Upstream `core/tools/{bash,edit,write}.ts` executes tools with the process's
  ordinary OS permissions and has no built-in sandbox. `coding/sdk.go` now
  assembles tools the same way on every platform, with no backend probe or
  read-only fallback. External confinement, including restrictions inherited
  from a parent process, remains outside this feature. The row number is
  retained as removal history; it no longer describes an active divergence.

- D178 — **a killed shell command cannot be held open by a descendant that
  outlived it.** Upstream's abort and timeout kill the process tree by pid
  (`taskkill /F /T` on win32) and then wait for the child's stdio streams to
  close. A descendant the command left behind — a backgrounded server, a
  `start /b`, a `setsid` — inherits the output pipe and keeps it open, so the
  wait outlives the kill: the abort does nothing visible until the last writer
  exits, even though the tree it killed is already gone. The port closes the
  command's output readers as part of the kill, so the drain ends on the kill
  rather than on the last writer, and it drains through `waitForPipeDrain` (the
  grace window the sandbox launcher already used, a port of upstream's
  `waitForChildProcess`). On Windows the tree is additionally held in a job
  object, because `taskkill /T` walks a live parent-child tree and cannot reach
  a process whose parent has already exited, while `TerminateJobObject` still
  reaches it; the guard falls back to taskkill where the assignment fails. The
  job is closed without terminating on a normal completion, so a process the
  command deliberately left running survives, as upstream leaves it. Measured on
  the case that started this: an abort that used to return 9s after cancelling,
  when the detached writer finished, now returns in under 0.1s.

- D179 — **`tui.ParseColor` takes only a string, and capabilities stay
  injected.** Upstream's `parseColor(string | number)` returns a tagged `Color`,
  and `detectCapabilitiesFromEnvironment()` derives truecolor from `COLORTERM`
  and a `TERM` suffix of `-direct` (567469096). The port keeps palette indices in
  the theme document's `ColorValue` (`IsIndex`/`Index`) and parses only the string
  forms, and it keeps capabilities injectable (D69), so
  `themeboot.EnableCapabilities` sets truecolor unconditionally.
  `tui.TerminalDetectsTrueColor` mirrors the upstream environment rule (including
  `-direct`) for a caller that wants it, but it is not yet wired to the boot.
  The theme controller likewise does not re-query the terminal's default colors
  when the light/dark scheme changes (upstream `queryTerminalDefaultColors`):
  the port's combined OSC 10/11/4 burst shares the OSC 11 reply with the
  renderer's background probe, and re-running it from the scheme listener
  swallows the reply that the probe routes
  (`TestBackgroundProbeRoundTripThroughRenderer`).

## D180. No line flattening in render caches (upstream 54c19a252)

Upstream's `flattenLines` runs over the cached render output of `Markdown`,
`Text` and `Box`: V8 represents string concatenations as ropes, and a cache
that keeps the concatenated parts retains a tree per line instead of a flat
string. A long assistant message kept about a fifth of the heap this way.

Go strings are immutable flat buffers; `strings.Builder` output is already a
single buffer, so there is no rope representation to flatten and the port has
no `flattenLines` equivalent.

The token-cache half of the same commit IS ported: `Markdown` holds its parsed
token list in a `weak.Pointer` (`tui/markdown.go`), so a transcript message's
tokens are collected when nothing needs them and the next render re-parses.

## D181 — resource contents arm by key presence

Upstream distinguishes a text resource from a blob resource by key presence
("text" in resource), which matters for an empty text payload. The Go
`ResourceContents` records `TextArm` on decode and marshals the text member
whenever it is active. (pi/packages/mcp/src/protocol/content.ts)

## D182 — mcp stdio exit hook watches signals

Upstream kills live MCP stdio process groups in a `process.once("exit")`
hook. Go libraries cannot hook `os.Exit`, so `mcp.installExitHook` watches
SIGTERM/SIGINT/SIGHUP, kills the live groups, restores the default action,
and re-raises. (pi/packages/mcp/src/transports/stdio.ts)

## D183 — streamable-HTTP retryable errors

Upstream treats fetch `TypeError`s as network failures and retries. Go's
net/http reports failures as typed errors, so the transport retries any
non-`McpHttpError`. (pi/packages/mcp/src/transports/streamable-http.ts)

## D185. MCP integration without the extension system

Upstream's MCP support is an extension: `core/mcp-servers.ts` holds the config
shape and a registry that extensions register servers into, and
`extensions/mcp/` connects them and exposes their tools through the extension
tool model (exposure, namespaces, codemode, `tool_search`, resource tools, the
`/mcp` sign-in and status UI, and `auth.json`-backed OAuth credentials). The
port has no extension mechanics (AGENTS.md), so it ports the config, tool
conversion and connection halves and wires them directly:

- the agent dir's `mcp.json` plus a trusted project's `.pi/mcp.json` are read at
  boot and the enabled servers connect before the session is created;
- `direct`-exposure tools are appended to the built-in registry through
  `CreateAgentSessionOptions.ExtraTools` and follow the same `--tools` /
  `defaultTools` name selection; `codemode`, `deferred` and `hidden` tools are
  registered but never exposed (no codemode, no `tool_search`);
- OAuth credentials live in a per-process store instead of `auth.json` (the
  `/mcp` sign-in that would persist them is unported), and resource reads
  (`read_mcp_resource`) are not ported, so resource links do not name it;
- the tool definition carries no upstream renderers, output schema or
  annotations (the interactive layer's generic tool rendering draws MCP calls),
  and an `isError` result becomes a Go error whose text is the converted
  content (agent.AgentToolResult has no error flag);
- a tool list changed after boot reaches `DirectTools`, but a running session
  keeps the tools it was created with until it reloads (upstream registers into
  the live registry).

(pi/packages/coding-agent/src/extensions/mcp, core/mcp-servers.ts)

## D189 (withdrawn). Virtual models: the provider wrapper

The virtual-model provider wrapper is ported, but the seam lives in `ai`
(`ai.WrapProviderWithVirtualModels`) rather than in `coding`, because a Go
provider's model accessor and stream dispatch are package-private; the
observable behavior (a virtual model hides a physical model with its id,
filtering keeps virtual models, and an unrouted stream fails) matches
upstream's `withVirtualModels`. Withdrawn as a divergence.

(pi/packages/coding-agent/src/core/virtual-models.ts)

## D190. A running tool's animation tick does not rebuild its display

Upstream's `ToolExecutionComponent.invalidate()` is `super.invalidate()` +
`updateDisplay()`, and the shell renderer's 1s
`setInterval(() => context.invalidate())` calls it
(`core/tools/renderers/bash.ts`). The port keeps `invalidate()` as upstream,
but the animation walk ticks a component through `tui.AnimationTicker` when it
implements it: `ToolExecutionComponent.AnimationTick` bumps the tool's revision
only (`Container.BumpRevision`, which keeps the cache). The clock-driven elapsed label still updates,
because `Box.matchCache` now checks a versioned child's revision (a child
`Container` rebuilds its suffix in place, so comparing the line slices cannot
see the change) and `Box.Render` re-applies the background from the first
changed line. `bashPreviewComponent.Invalidate` also keeps its incremental line
count.

Upstream's rebuild re-wrapped every running tool's full output once a second:
2.15 ms per tick for a 2000-line result and 139 µs for a bash preview, against
0.07 ms and 0.0009 ms with the narrow tick.

(pi/packages/coding-agent/src/modes/interactive/components/tool-execution.ts,
pi/packages/coding-agent/src/core/tools/renderers/bash.ts)

## D191. The goal extension is removed (was: the goal round as an invisible custom message)

The goal extension (upstream `~/.pi/agent/extensions/goal`, the Go port's
`goal/` package + `GoalController` builtin) was removed from the port. It was
upstream an installed user extension, not part of the tree, and the port's
commitment to byte-parity stops where upstream relies on the extension host
(D41/D133). Until v0.0.14 the port carried it as a built-in; the unwiring makes
the port behave like a stock pi install without the extension installed.

Historical, for the record: while the builtin existed, a goal round was
delivered as an invisible custom message — `SessionGoalSink.SendGoalMessage`
encoded the round as an `ai.CustomMessage` with role `"custom"` and the
`{customType, content, display: false, details}` envelope, queued it with
`AgentSession.FollowUp` (which started the turn at settle), and `ConvertToLlm`
turned it into a model-visible user message while the transcript's
`decodeCustomMessage` skipped it. Session files with `pi-goal` entries still
replay (custom entries render as generic cards; the goal-specific renderers are
gone).

## D192. The D136-D139 mutex-deadlock watchdog flows are retired

Upstream is single-threaded, so it has no equivalent of the PTY watchdog
suite. The port grew one because its UI state was mutex-guarded and five
concrete bugs (D136-D139) were a goroutine parked on a `sync.Mutex` while the
UI loop waited on it: editor submit, the model selector, terminal/screen
scroll, and the stdin buffer exit. Each flow drove the real binary through a
pseudo-terminal, sent `SIGQUIT`, and failed when the stack dump showed a
goroutine in `sync.runtime_SemacquireMutex`.

The stage-4 refactor retired every UI mutex (`docs/locks.md`; the retained
locks in `tui/` and `coding/interactive/` are handoff and logging locks that
never guard UI state), so the class is structurally unreachable: no shared
lock protects the loop's data, and therefore no flow can park the loop behind
one. `internal/uiblock` proves the stronger, wider property statically — no
blocking call at all (syscalls, network, exec, channel ops, lock acquisition,
sleeps, JSON) is reachable from the UI-loop roots — in one module-wide SSA
pass. The five PTY flows were therefore deleted and their *functional*
behavior moved to `coding/interactive/ptyflow_test.go`, which runs on the app's
own loop without a pty, a binary spawn or `SIGQUIT`: submit reaches the
session, `/quit` requests a shutdown, `/model` opens and disposes the selector,
and the transcript scrolls and paints. `TestMutexBlockedDetectorHasTeeth` is
kept so the dump parser cannot rot silently.

Measured: the five flows plus their harness cost about 11.5 s of
`coding/interactive` (27 s total); the four replacements cost 0.49 s for all of
them.

Deliberately not changed: `internal/uiblock` still spends about 6 s on its one
module-wide SSA load (`packages.Load` 3.7 s, `prog.Build` 1.4 s). Narrowing its
scope or caching the SSA across runs would weaken or obscure the check, so the
cost stays. The four replacements also keep their `testing.Short()` skips
consistent with the remaining PTY tests.

(pi/packages/coding-agent/src/modes/interactive/interactive-mode.ts; the port's
own defect class, D136-D139)

## D193. The system theme's no-color tier keeps the port's signal fills

Upstream's `indexedColors` is the system theme's tier for a terminal that
reports nothing (no OSC 10/11/4 reply): it leaves every panel transparent,
because indices 0-15 with the terminal's own background fit any theme. The
consequence is that a running, failed and successful tool call render
identically, and a user message is indistinguishable from body text. That is
the exact signal `filledBackgroundColors` exists to keep (`piertheme.go`): the
port's own palette paints `toolPendingBg`/`toolSuccessBg`/`toolErrorBg` and
`userMessageBg`, because for a failed call the fill is the only cue.

The port therefore fills those panels in the system theme's no-color tier with
the port's own signal colors for the detected appearance
(`pierSignalPanelHex`): the three tool states, `userMessageBg`, and
`customMessageBg` (the compaction-summary/goal-card/custom-entry frame).
Decoration (`selectedBg`, `searchMatchBg`) stays transparent, matching
`transparentBackgroundColors`. The generator itself stays byte-parity with
upstream: the fallback is applied when the theme is assembled
(`buildSystemTheme`), so `GenerateSystemThemeColors` and the system-theme
golden are unchanged.

(pi/packages/coding-agent/src/modes/interactive/theme/system-theme.ts,
`indexedColors`; the port's `filledBackgroundColors` guarantee)

## D194. The shell elapsed label ticks every 0.1s

Upstream's bash renderer formats the elapsed duration with `toFixed(1)` (0.1s
resolution) and repaints it with `setInterval(context.invalidate, 1000)`, so
the running label moves about once a second. The port keeps the tenths display
(`formatDuration` is `%.1fs` below a minute, matching `toFixed(1)`) but arms a
running shell tool's animation frame at `shellElapsedTick` (100ms), so the
label advances one tenth at a time.
`ToolExecutionComponent.AnimationFrame` picks that tick only for `bash` and
`powershell` (`isShellTool`); every other running tool keeps the upstream 1s
frame, because a repaint of those buys nothing. The tool-level frame is the one
that matters: the animation walk reaches the tool as a direct chat child, and
its `AnimationTick` bumps the revision the label needs, so the nested
`shellElapsedComponent`'s 100ms frame alone would paint without unfreezing the
line.

Cost: a running shell call repaints the loop at 10fps. The label renders one
line, and the tool's `AnimationTick` is the narrow bump (D190), but the whole
screen still paints per frame.

## D195. The MCP connect is asynchronous and the tools attach to the live session

Upstream's MCP extension (packages/coding-agent/src/extensions/mcp/index.ts)
starts every configured connection at boot without blocking the boot, waits
once at `before_agent_start` for the servers whose tools are declared to the
model (`hasDirectTools`: the configured exposures contain "direct"), races
that wait against `DEFAULT_STARTUP_WAIT_MS` (10s), and reports "MCP servers
are still connecting; their tools become available once connected." when the
cap expires. Its tools register into the live registry as they connect.

The port matches the design: `NewMcpManagerAsync` starts the connections and
returns; the session gained a once-per-session `BeforeFirstTurn` hook (fired
at the first `runAgentPrompt`, the `before_agent_start` equivalent) and
`AttachExtraTools` registers and activates the late tools. Print mode waits
in the same hook and reports to stderr; interactive mode reports through the
UI status line. The 1.0.2-era synchronous boot (which cost a full TLS
handshake before the TUI came up) is gone; the connecting status line (the
"Connecting to MCP server "name"..." diagnostic) stays.

`/reload` reaches MCP too: the CLI-owned manager is exchanged for one built
from the re-read config (`ReloadMCPExchange` closes the old connections and
`NewMcpManagerAsync` restarts them; `AttachSession` re-installs the live
wiring), matching upstream session_shutdown (reason "reload") ->
session_start (reason "reload") -> reconnect. The manager re-reads
`~/.pi/agent/mcp.json` and the trusted project's file on every reload.

Divergences from upstream:
- Connection failures surface at the first turn's wait completion (print
  stderr, interactive warning) instead of upstream's startup report; the
  config errors still surface at boot. After `/reload` they surface in the
  reload's error report (the status line names "MCP servers" among the
  reloaded items), mirroring the startup report.
- No `mcp_servers` system-prompt section: upstream lists the servers whose
  tools are not declared there, which only matters for codemode/deferred
  exposure, and the port does not expose those (D185).
- `WaitForDirectTools` polls connection state (20ms) instead of awaiting the
  ready promises; equivalent observation, no shared future.

## D196. Assistant partials are scoped to their producing stream

The split session-event channels (D143) do not preserve ordering between
lossless lifecycle events and coalesced streaming updates. If assistant A's
partial remains queued while A's end and assistant B's start are applied, that
partial must not replace B's content or create A's tool components in B's
stream.

The UI queue stamps assistant events with a monotonically increasing generation
at each assistant start. Both run-loop event-application paths reject message
updates whose generation differs from the applied assistant start. A partial
that overtakes its own start is discarded too; the lossless message end carries
the complete content. Generations belong only to the internal queue envelope,
not session events, persisted JSON, or provider payloads. Direct dispatcher
callers retain the ordered-event contract.

`TestQueuedMessageUpdateCannotOverwriteTheNextMessage` queues A's delayed
partial before A's end and B's start, drains the split channels, and checks the
rendered transcript and pending tools. It also checks that B's own subsequent
partial still renders.

## D197. Pre-paint event drains yield after a bounded, fair batch

The explicit UI loop (D143) previously drained lossless events until that
channel was empty, then partial updates, before painting. A producer can keep
refilling a bounded channel while the loop consumes it, so channel capacity
alone did not bound the drain. Sustained lossless traffic also prevented
partials from receiving service inside it.

Each pre-paint drain now applies at most 64 events. Each round attempts one
lossless event and one partial, spending unused capacity on whichever class
remains ready. FIFO order within each channel is unchanged; lossless events
are not dropped. Remaining queued events wake subsequent main-select
iterations, so painting, input, cancellation and watchdog beats can progress.
This bounds event count, not the execution time of an individual handler.

Tests refill channels synchronously from the loop-side application hooks,
which guarantees sustained traffic without scheduler-dependent timing.
`TestDrainReadyEventsYieldsUnderContinuousRefill` checks the batch bound and
complete eventual lossless delivery; `TestDrainReadyEventsDoesNotStarvePartials`
checks service for both ready classes. `TestRunLoopInputPaintsDuringContinuousEvents`
checks a keystroke's paint, a subsequent cancellation keystroke and advancing
loop beats while both channels remain ready.

## D198. The interactive loop defers new paints when output backs up

The async terminal writer keeps a paused console off the UI loop, but generating
frames indefinitely can grow its FIFO. The interactive schedule now samples
`ProcessTerminal.PendingWriteBytes`, an atomic snapshot of queued and in-flight
committed output. Unlike `WriteBacklog`, this does not acquire the terminal
handoff mutex on the UI loop. Uncommitted frame bytes are excluded.

At 256 KiB or more, the schedule defers new paints. Once paused, it waits until
output falls to 128 KiB or less before resuming. A pending paint is retained on
the existing paint timer, retried at 16 ms intervals even without another render
request. Repeated requests cannot keep pushing an outstanding retry into the
future. Input dispatch, input-flush deadlines, session events and cancellation
continue while painting is deferred. Input-latency tagging occurs only when
the deferred input paint is actually generated.

Already generated differential frames and durable terminal writes are never
dropped or reordered. The thresholds are scheduling watermarks, not hard byte
caps: one large frame can overshoot, and direct writes can still grow the FIFO.
The policy applies to the interactive run-loop schedule, not standalone startup
screens, direct renderer clients, mandatory persistence or shutdown flushing.
Terminals without the optional lock-free snapshot retain existing behavior.

`TestLoopScheduleDefersPaintsUntilOutputDrains` exercises all scheduled paint
paths, watermark hysteresis, retry wake-ups and deferred input tagging under
`testing/synctest`. `TestRunLoopWithBlockedConsole` uses a real `ProcessTerminal`
writing into a pipe without a reader: repeated input/render requests do not grow
the committed backlog, events and cancellation remain responsive, and releasing
the reader delivers all committed output followed by one up-to-date frame
without another request. The terminal backlog test also checks the atomic
snapshot through queued, in-flight, open-frame and drained states.

## D199. Shutdown cancels optional queues but drains accepted saves

This is a Go-only lifecycle policy for `internal/offloop`, not a change to
session serialization or provider requests. `Group.Queue` remains mandatory:
accepted work drains in FIFO order with no cancellation deadline. Session and
settings queues retain that classification. `Group.OptionalQueue` instead
rejects new work, cancels its context and discards waiting tasks on shutdown;
it does not wait for a running worker that ignores cancellation. `StopAll`
begins shutdown on every queue before waiting for mandatory drains, so a
blocked save cannot postpone optional cancellation.

The interactive app classifies theme loads, transcript pre-rendering and
keyboard clipboard reads as optional. Paste coalescing is now per app rather
than process-wide. Both `StopMode` and `Close` stop the owned queues; `StopMode`
also retains explicit flushes for settings/session collaborators outside the
group. Theme loads check cancellation after their blocking read, before
publishing global state. Theme watcher lifetimes follow their queue context.
Theme switches, previews and paste inserts check cancellation again inside
already-posted UI callbacks. Transcript warms check between components and do
not publish a ready chunk or request a render after cancellation.

Cancellation does not forcibly terminate a goroutine, filesystem operation,
component preparation or legacy clipboard provider. Such work can finish later,
but its canceled result is ignored. Clipboard subprocess helpers retain their
existing timeouts. Process-wide clipboard-copy queues and detached work are not
covered by this app-owned policy. Explicit `Flush`/`FlushAll` still wait for
running optional work. Mandatory persistence and terminal flushing can still
wait indefinitely; no blanket shutdown timeout or save abandonment is added.

`TestGroupShutdownCancelsOptionalWorkButDrainsSaves` uses `testing/synctest` to
hold a save and a cancellation-ignoring optional worker: optional cancellation
arrives before the save is released, saves finish in order, shutdown returns
without releasing the optional worker, and queued/late optional tasks never
run. Interactive tests cover real `StopMode` and `Close` queue ownership,
late theme loads and watcher reloads, already-posted switch/preview completions,
late keyboard paste results and canceled transcript warm publication.

## D200. Mode-owned optional producers and a bounded terminal-exit grace

The interactive CLI now extends D199's queue policy to clipboard copies and
explicit detached work. `/copy`, fullscreen selections and OAuth URL copies
share the app's optional clipboard FIFO rather than the legacy process-wide
queue. Platform clipboard helpers receive their queue context; cancellation
stops command fallback and never falls through to OSC 52 after observing a
canceled context. Keyboard and Windows right-click reads use the same owned,
coalesced paste queue. Right-click focus checks and insertion occur on the
owner loop, not on the clipboard worker.

Detached bash/compaction work inherits both the mode lifetime and the active
run context. Stopping or closing the app cancels the mode lifetime before
mandatory drains. The stable UI forwarding reference gates every posted
completion both at submission and execution, including posts already queued
when shutdown begins. Existing standalone clipboard/component APIs retain
their legacy defaults; their callers may opt into the new context-aware helpers.
Cancellation cannot forcibly terminate arbitrary goroutines or console syscalls.

After accepted session/settings saves have drained, graceful permanent
interactive terminal teardown opens one **2 s** output grace period. Fullscreen-to-regular
replay, terminal protocol restoration, post-stop hooks and the optional resume
hint all share that deadline. Temporary renderer stops, editor handoffs and
ordinary library terminal stops remain lossless and unbounded unless the caller
explicitly starts a shutdown grace. `App.Close` uses the same permanent terminal
policy when run-context cancellation bypasses `Lifecycle.Shutdown`.

On timeout, flush waits return without dropping or reordering the committed
FIFO in memory. The CLI skips the resume hint and exits; undelivered terminal
output may therefore be lost at process exit, including display/protocol cleanup
sequences; OS raw-mode restoration is still attempted. On successful delivery, the hint
is queued through the same terminal writer and remaining budget, never written
directly to stdout. Finalization closes output admission and tells the writer
to drain retained bytes and exit if the console recovers. It does not kill or
close the process's stdout. Accepted persistence still has no timeout, and
session serialization and provider payloads are unchanged.

Virtual-time tests assert the exact 2 s boundary, shared deadline, FIFO retention,
late output rejection and writer finalization. Lifecycle tests assert hint
suppression, bounded hint routing, finalization before Exit and the canceled-run
Close path. Mode tests cover clipboard copy cancellation, selection/OAuth copy
ownership, asynchronous right-click reads, detached-work cancellation and
rejection of already-posted results. Clipboard helper tests reject pre-canceled
operations before command launch or OSC 52 fallback.

## D201. Optional-work admission is bounded during normal operation

Shutdown cancellation alone does not bound a live mode's retained work. The
interactive app now applies per-domain admission policies, separate from D198's
paint watermarks and D200's exit grace:

- Clipboard copies retain at most four accepted requests in total (one running
  and three waiting). `Queue.TryGoContext` tests running plus pending count under
  the existing handoff lock and rejects excess requests without waiting. Accepted
  distinct requests remain FIFO. `/copy` and fullscreen selection callers receive
  a false/busy result rather than claiming success; OAuth URL callbacks receive
  the same busy error through their UI-marshaling seam.
- Theme settings switches and previews share one fixed newest-pending key.
  `Queue.GoLatestContext` replaces its pending callback in O(1), so at most one
  load runs and one successor waits. Running work is not canceled or reordered;
  intermediate pending previews/settings applications are deliberately superseded.
  The newest selection still determines the final theme. Theme JSON and saved
  settings formats are unchanged.
- Explicit detached bash, compaction and reload work has four parallel slots
  and no pending list. Excess requests are rejected with visible feedback.
  Slots are reclaimed when workers return, even on cancellation; a worker that
  ignores cancellation continues to occupy its slot. Rejection warnings have
  at most one pending UI notification, preventing a burst from creating one
  warning per rejected worker. The reload admission seam returns a boolean so
  rejection restores its temporary loading screen and editor focus.

Clipboard/paste reads already coalesce overlapping requests, and transcript
pre-rendering already admits one chunk at a time; those policies are unchanged.
These are task-count bounds, not byte caps or global limits on arbitrary SDK
queue users, UI posts, model-turn queues or direct terminal writes. Mandatory
session/settings writes keep unbounded, lossless admission and FIFO draining.
No committed differential terminal frames are evicted during normal operation.

Blocked-worker tests reproduce the pre-change clipboard backlog of 64 waiting
copies plus one running copy, and 65 waiting theme requests plus one running
load. After admission changes, the same bursts retain three waiting copies plus
one running copy, and one pending theme successor plus one running load.
Queue tests assert FIFO, capacity reuse, latest replacement and shutdown
rejection. Interactive tests assert busy feedback, coalesced rejection warnings,
reload restoration, final theme selection and real owner-loop input painting
while a clipboard daemon is blocked.

## D202. Optional terminal output waits before admission

The direct-output audit is in [terminal-output.md](terminal-output.md). At
256 KiB of committed output, title and active-progress refreshes retain one
newest-uncommitted hint each; the writer admits them after recovery to 128 KiB.
Progress clear remains essential and invalidates delayed active hints. Permanent
shutdown drops uncommitted metadata, never committed differential frames.
Interactive OSC 52 fallback now uses the stable terminal FIFO through a
cancellation-aware off-loop API, rather than raw stdout. Whole packets wait for
256 KiB capacity and a closed renderer frame; oversize packets are rejected.
Accepted frames and packets remain FIFO. Essential protocol/lifecycle output,
generic SDK writes and standalone APIs retain their admission behavior, so this
is not a global byte cap. A single oversized title can overshoot. Regressions
observe blocked-writer backlog, cancellation, atomic recovery, stale progress
suppression, oversized-title progress and app-owned clipboard routing.

## D203. Transcript resize cache warming yields between chunks

Measurements of individual owner-loop phases found the largest fixture cost in
transcript width changes: 500 user/assistant pairs took 35.53 ms and allocated
10.48 MB per resize (10 iterations). The CPU/allocation profiles identified the
full Container child-render walk, Markdown lexing/wrapping and GC, not queue
handoff or console I/O. A roughly 1 MB paste took 17.30 ms; a roughly 1 MB tool
update took 18.26 ms in the initial single-iteration probe.

The interactive document now wraps its chat container in an owner-loop-only
resize document. After a complete frame exists, a width change warms at most
64 direct transcript children per Render call and requests another paint while
work remains. The previous complete flattened frame stays visible while caches
warm; partial new-width transcript frames are not published. Ancestor render
cache skipping is disabled while a resize generation is incomplete. Width or
child-identity changes restart the warm snapshot, and publication renders the
current children so newer content is not replaced by an old prepared snapshot.
No worker mutates attached components; no UI mutex or goroutine is added.

The same benchmark's first resize chunk now takes 1.45 ms and allocates 0.59 MB
(10 iterations on Linux/amd64). Remaining chunks are completed outside its timed
region, so this is a per-call latency improvement, not a reduction of total
resize work. Benchmarks remain opt-in and are not ordinary test timing assertions.
Regression tests assert the 64-child bound, continuation through cached parents,
composition in the app, ordered final frames and superseded width/child snapshots.

This is a cooperative bound on the number of width-cold children per Render,
not a universal wall-time bound on a loop beat. One large child, initial cold
mount, paste processing, streaming updates, flattening and non-cacheable custom
components remain separate work. Layout may call Render multiple times during
a paint. The previous transcript can be briefly stale during a resize; terminal
output continues to use differential frames and D198/D202 backpressure. Saved
JSON, provider payloads, standalone container behavior and component ownership
are unchanged.

## D204. Large built-in assistant Markdown renders detached snapshots

A single cold assistant fixture (16,000 heading/paragraph pairs, about 1 MB)
rendered in 296.63 ms and allocated 114.11 MB (Linux/amd64, three iterations).
CPU profiling located the cost in Markdown rendering/token keys and GC. The
D203 child-count budget cannot yield inside this one component. Plain-text tool
updates remain a distinct workload, not silently covered by this Markdown fix.

The interactive app opts built-in assistant text/thinking Markdown into immutable
snapshot preparation at 64 KiB of source or more. Render submits a fresh private
Markdown instance to an app-owned optional queue and returns the previous complete
lines, or `Preparing message...` for a cold message. The queue admits at most four
jobs total (one running, three pending); rejected admission requests a later paint
instead of blocking or retaining more queued work. The snapshot copies text,
width, padding, captured theme/style and transform configuration, never live render
caches. The worker's completion is posted through the mode-owned UI reference.

Text changes, width requests (including a return to a cached width), and explicit
style/theme invalidation supersede older preparation generations. Only a matching
completion installs prepared caches on the owner. Ancestor caches are dirtied on
publication so skipped live assistant subtrees cannot hide the new frame. Mode
shutdown cancels the queue, discards waiting tasks and rejects late UI posts.
An in-progress Markdown parse is not forcibly interrupted; it operates on its
private snapshot and its canceled result is ignored. No UI-state mutex is added.

Detached session `Prepare` retains synchronous warming, avoiding recursive task
submission from prerender workers. Standalone Markdown and user-supplied assistant
transformers retain their synchronous contract unless explicitly opted in with a
captured-safe backend. Saved JSON/provider messages are unchanged. Old complete
lines can briefly show during content, width or theme changes; an empty complete
frame is also retained correctly rather than resurrecting earlier content.

Owner admission for the same assistant fixture measured 0.024 ms and 3,704 bytes
(three iterations). This benchmark uses an admission-only sink, so it measures
owner-side dispatch, not end-to-end latency or reduced background render work.
Worker parsing/allocations, global GC pressure, final container flattening, huge
plain-text tool results and custom callback cost remain separate limits. Tests
observe byte-identical completed Markdown, blocked-worker input service, bounded
owned admission, live-message composition, cached-width/content/theme stale-result
rejection, empty-frame retention and synchronous detached warming.

## D205. Large built-in tool results prepare private snapshots

Reference: `packages/coding-agent/src/core/tools/renderers/{read,bash}.ts`
formats expanded read output and both shell display modes synchronously. D204
moved large assistant Markdown off the owner loop, but one large tool result
could still monopolize it in sanitizing, styling and ANSI-aware wrapping.

Live and replayed built-in read (expanded), bash and powershell results now opt
into the same optional preparation backend at 64 KiB of text content. Assistant
Markdown and tool results share four total admitted jobs, not four per domain.
Workers build private components from captured content blocks, expansion/partial
state, a concrete theme, key hint and copied truncation/full-output metadata.
Strings remain immutable; content slices and metadata pointers are copied.
Persisted JSON metadata of at most 16 KiB is copied, not decoded on the owner.
Shell elapsed state stays on-owner and keeps ticking while output prepares.

The owner retains previous complete result lines, or a cold
`Preparing tool output...` label. Result updates, width changes (including a
return to a cached width), expansion changes, theme invalidation and backend
changes obsolete outstanding generations. Only matching completions apply
and request a render through the mode-lifetime-gated UI post. Rejected admission
retains no work and retries on later renders. Detached `Prepare` remains
synchronous, with no nested submissions. Custom callbacks, image-bearing
results, unknown metadata objects and oversized JSON metadata keep their
synchronous contracts. Raw fallback rendering and other built-in tool types
are unchanged. Saved session/provider JSON is unchanged.

Linux/amd64, three iterations of a 1,050,000-byte expanded plain-text fixture:
read cold work measured 60.31 ms and 28,389,757 bytes, versus 0.077 ms and 21,216
bytes for owner admission; bash measured 59.00 ms and 23,180,128 bytes, versus
0.034 ms and 5,592 bytes for admission. Admission uses a sink and includes
component construction and UpdateResult; it is not an end-to-end latency claim.
A cold CPU profile shows ANSI tracking/wrapping, concatenation and allocation/GC
cost. That work still runs in the background; final container padding/flattening,
GC pressure and unsupported/custom render paths remain limits. Tests observe
byte-identical completed output, live/replayed composition, input service with
a blocked queue, stale-generation rejection, copied result values, custom
callback ownership, persisted metadata and synchronous detached warming.

## D206. Tool workers finish the Box background and padding pass

Reference: `packages/tui/src/components/box.ts` applies horizontal/vertical
padding and backgrounds after rendering its children. D205 moved tool result
formatting and wrapping off-owner, but completion still triggered another full
Box pass on-owner. A 1,050,000-byte expanded output fixture spent 15.90 ms
(read) or 14.65 ms (bash) and about 10.63 MB/90,000 allocations in that owner
publication pass alone.

Eligible D205 tools with built-in call headers and the default Box shell now
capture immutable header lines, the concrete background theme and elapsed-label
text on-owner before submitting work. The same private worker formats the result
and finishes a detached Box frame. A generation-matching completion installs the
raw result and adopts the prepared Box cache, rebinding cache/mouse metadata to
the original owner children. No attached component, callback or elapsed clock
runs on the worker. This shares the existing four-job admission and canceled
UI-post guards; there is no new queue or UI-state lock. Standalone Box rendering
and detached synchronous tool warming are unchanged. Custom call renderers and
self-render shells do not use the prepared Box handoff.

`Box.AdoptPreparedFrame` is an explicit owner-only handoff: its caller must check
the generation and ensure captured child lines match current children at the
prepared width. Padding, child count and background sampling must match. The
private snapshot must not be mutated after handoff. Later owner changes still
invalidate the cache normally, and original mouse callbacks/coordinates remain.
If the shell clock advanced during work, only its trailing rows and bottom
padding are repainted; timer state stays on-owner.

Linux/amd64, three iterations of the same fixture after the change: owner
publication measured 1.47 ms (read) and 0.994 ms (bash), with 1,024,312 bytes and
8 allocations each. The benchmark excludes fixture construction and worker work
but includes completion application and the first owner render. These are not
end-to-end latency measurements: padding work moved, not vanished. Final
container flattening, large headers, retained-output repaints after invalidation
and global GC pressure remain limits. Tests observe identical full frames,
no full owner background pass after completion, stale-result rejection, timer
updates, custom-header ownership, geometry/background rejection and original
mouse targets after adoption.

## D207. Pending tool updates retain the completed Box frame

Reference: `packages/coding-agent/src/modes/interactive/components/tool-execution.ts`
rebuilds the tool shell after result/argument/theme changes, and
`packages/tui/src/components/box.ts` pads and backgrounds the resulting lines.
D205 retained complete raw output while D206 prepared replacement Box frames,
but an invalidation still sent the retained raw output through another full
owner-side Box pass. A 1,050,000-byte fixture measured 14.83 ms (read) or
15.88 ms (bash), about 10.15 MB and 90,000 allocations, for a pending update's
invalidation/admission/render alone.

Eligible D206 tools now retain the actual completed tool frame, including its
header, padding and background, until matching replacement work completes.
Result updates, target width changes and theme invalidation admit new private
work but do not repaint the old large result on-owner. Prepared-frame metadata
and displayed-frame metadata are separate: a completion that has not yet been
painted cannot replace the metadata of the frame actually retained. A cold tool
still uses the pending label; synchronous fallback/collapse/empty paths retain
their existing behavior. Custom headers and self-render shells do not opt in.

Elapsed clocks remain live. Only the two clock rows are styled with the retained
frame's concrete background and original width; the slice is copied only when
those rows change. `ChangedFrom` describes the frame actually returned, allowing
parent Boxes to reuse the unchanged prefix. Pending tools report no stable
render version, so a parent that skips unchanged children still admits/retries
work, including after busy rejection. Mouse dispatch uses the displayed width
and previous layout, not the pending target width; resize clicks cannot trigger
an attached full-Box render. Original result click callbacks still act on current
owner state. No new worker, queue, lock or saved/wire JSON change is introduced.
D204/D205's shared four-job admission, generation checks and teardown guards
remain in force.

Linux/amd64, three iterations of the same fixture after the change: read pending
owner work measured 0.061 ms, 1,997 bytes and 48 allocations; bash measured
0.039 ms, 1,800 bytes and 43 allocations. Initial preparation and worker execution
are excluded; these are not end-to-end latency claims. The old header/theme/width
can remain visible until work completes. Large headers still render on-owner;
clock slice copies, final container flattening and global GC remain limits.
Tests observe complete-frame retention without attached background calls for
content/width/theme changes, matching completed replacements, live clocks through
parent caches, admission retry in skipping parents, stale rejection and resize
mouse dispatch without a full attached repaint.

## D208. Accepted pending clock updates reuse a versioned frame

Reference: `packages/coding-agent/src/core/tools/renderers/bash.ts` updates a
trailing elapsed label while output streams. D207 avoided full retained-output
repainting, but copied the complete retained line slice whenever that label
changed because all pending tools reported no stable render version. The
unversioned parent-cache contract treats a reused slice as unchanged, so those
copies were required to keep the clock visible.

Only unadmitted/rejected work now reports no stable revision. Accepted pending
work reports the owner's container revision; each changed retained clock tail
bumps that revision and records its first changed row. The owner can mutate those
two retained rows in place, with version-aware parents detecting the change and
Boxes reusing the unchanged backgrounded prefix. No private worker snapshot is
mutated. Busy/unadmitted clock updates still copy the line slice and remain
unversioned, so skipping parents retry admission and cannot miss clock changes.
Completion and source updates keep the existing invalidation/generation protocol.
No new queue, lock, worker or saved/wire JSON change is introduced.

Linux/amd64, 20 iterations over a 1,050,000-byte output fixture with accepted
replacement work blocked: a clock tick fell from 1.055 ms, 484,070 bytes and
17 allocations to 0.002456 ms, 684 bytes and 16 allocations. This benchmark
isolates the tool's tick/render, not parent flattening, worker work or end-to-end
input latency. Large headers, busy clock copies, final container flattening and
global GC remain limits. Tests observe reused backing storage with a changed
render revision, live clocks in skipping parents, busy admission retries,
parent Box tail updates, stale-result rejection and displayed resize geometry.

## D209. First-child Container changes reuse flattened owner storage

Reference: `packages/tui/src/tui.ts`'s `Container.render` builds a fresh flattened
line array each time. The port already reused storage when a later child changed,
but a changed first child used a separate scratch flatten followed by a fresh
allocation and another full copy. Nested containers with a single transcript/tool
child therefore still allocated a content-sized array after every clock update.

The owner now rewrites the changed suffix in existing Container storage for all
child indices, including zero. Capacity grows geometrically when necessary;
initial rendering, explicit invalidation and structural cache drops still rebuild
normally. The redundant full-frame scratch array is removed. Render revisions
and `ChangedFrom` continue to signal in-place changes to parent Containers and
Boxes. Shrinking a frame clears removed string slots so reusable backing storage
does not keep dropped output alive. All mutations remain on-owner; no new worker,
queue, lock or saved/wire JSON change is introduced.

Linux/amd64, 20 iterations changing the first child of a 30,000-line warm
Container: flattening fell from 1.406 ms, 540,672 bytes and one allocation to
0.0265 ms, zero bytes and zero allocations. The child renderer is an allocation-free
fixture; this measures flattening alone, not Markdown, worker work, terminal
painting or end-to-end input latency. Copy work is still linear in the changed
suffix. Cache invalidation, capacity growth, large headers, busy clock copies and
global GC remain limits. Tests observe reused first-child storage, zero warm
change allocations, removed-reference clearing, output/growth/width correctness
and version-aware nested propagation.

## D210. Session persistence failures are reported, not swallowed

Reference: `packages/coding-agent/src/core/session-manager.ts` writes with
synchronous filesystem calls (`writeFileSync`, `appendFileSync`, `openSync`),
so a permission error, a missing directory or a failed close throws at the
caller. The Go port moved session writes onto the `internal/offloop` worker so
the UI owner loop never blocks on disk (D199), but the moved helpers returned
nothing: `rewriteFile` and `writeSessionLine` discarded their `os` errors,
`FlushWrites` only proved the queue drained, and a failed initial write still
set `flushed = true`, so later appends could produce a headerless or truncated
file while reporting success. A missing serializer branch even dropped entries
silently from an otherwise successful rewrite.

The port now records every persistence failure (rewrite, append, marshal,
mkdir) in a `SessionWriteError` buffer with its path and operation.
`FlushWrites` waits for accepted work and then returns the unresolved failures
as an error, so queue completion and successful persistence are distinct;
`DrainWriteErrors` hands out and clears the diagnostics without waiting on disk.
`WriteErrorsReady` is a coalesced, non-blocking wakeup so an owner loop can
report failures that occur while it is idle. A failed write marks the session's
write state uninitialized, so the next save rewrites the complete accepted
prefix rather than appending to a file that never opened; each queued append
captures its own immutable prefix view (the entry slice is append-only), so a
recovery rewrite cannot duplicate entries queued behind it. New sessions and
branches replace the write state, so a worker completing an old generation
cannot apply its result to a replacement session.

Consumers report on the owner loop: the interactive run loop selects on the
wakeup channel and shows a chat error, graceful teardown drains writes and
writes any unresolved failure through D200's terminal output grace rather than
direct stdout, and print mode flushes on every ordinary return and exits
nonzero on a persistence failure (stderr keeps JSON stdout clean). No saved
session JSON, settings JSON or wire payload changes. Tests observe failed
initial writes and appends (sync and queued), full-prefix recovery with entry
truncation invariants, per-task prefix recovery against duplicate appends,
coalesced notification, idle owner-loop reporting, graceful-shutdown
diagnostics and print-mode exit codes.

## D211. A provider rebuild publishes the whole set, not an empty one

Reference: `packages/coding-agent/src/core/model-runtime.ts` `rebuildProviders`
calls `this.models.clearProviders()` and then `this.recomposeProvider(id)` for
every id, each of which publishes one provider (`setProvider`/`deleteProvider`).
Between the clear and the last add, a reader of the provider set sees a provider
that exists as missing, and nothing is locked across the rebuild, so the window is
reachable from any goroutine.

That is not theoretical: while a rebuild ran, the session's pre-prompt auth check
asked `Models.CheckAuth` for the provider and got "provider not composed", which
`CheckAuth` reports as "no auth configured" rather than an error. The prompt path
turns that into `FormatNoAPIKeyFoundMessage`, so a configured provider produced
"No API key found for anthropic." at the first prompt. It surfaced as a flaky
`TestSubmitFromEditorReachesSession` failure (30 s deadline, then the chat error)
and was reproduced deterministically under CPU load: 7 failures in 200 runs, with
the composition check printing "provider not composed for anthropic" five times.
The port departs from the reference by building the composed providers first
(`composeProvider`, which does not publish) and installing the complete set in one
step (`ai.Models.ReplaceProviders`, a single locked map swap that supersedes
in-flight refreshes for the replaced ids). The reference's one-provider path is
unchanged: `recomposeProvider` still publishes a single provider for the callers
that change only one.

`TestRebuildProvidersPublishesTheWholeSet` runs rebuilds in a loop while another
goroutine reads the provider set and fails if any provider disappears; it fails on
the reference's shape at iteration ~1650 of 2000.

## D212. Lead context with initial system message

Reference: `packages/durable/src/harness/context.ts` at v1.0.2 leaves the first
system message after initial user input. Generation commits that input before it
prepares and appends the baseline system entry. Providers treat only a leading
system message as the initial prompt and tool set, so the pinned ordering can
change prompt behavior and invalidate prompt caching.

Follow v1.1.0 (`leadWithSystem`): after tool-result ordering, move the first
non-user message to the front only when it is a system message. Keep later system
messages and persisted entries in place; `ContextView.Contributions` also retains
committed order. `TestDeriveContextLeadsWithInitialSystemMessage` covers initial
user messages, a later assistant and system message, and unchanged contribution
order.

## D213. SQLite ordered scans leave first-page ID bounds open

Reference: `packages/durable/src/storage/sqlite/storage.ts` `scanSql` uses
`id < Number.MAX_SAFE_INTEGER` for a descending scan without a cursor. This
omits the valid maximum safe ID, while memory scans include it, so SQLite's first
page can disagree with memory.

The Go SQLite scans omit cursor predicates on first pages and add strict `>` or
`<` predicates only when continuing a cursor. `TestConformanceScansIntegerBoundaryIDsInBothOrders`
asserts the maximum safe ID survives ascending and descending scans across
backends.

## D214. Go separates shell strings from argv commands

Reference: `packages/durable/src/env/index.ts` gives `Shell.exec` a string-or-argv
union. The Go `Shell` keeps `Exec(string, ...)` for shell scripts and adds
`ExecArgv([]string, ...)` for direct execution, instead of erasing this choice
with `any`. `TestOSShellExecArgvDoesNotParseArguments` verifies arguments reach
program without shell parsing.

## D215. OS filesystem watches use polling on every platform

Reference: `packages/durable/src/env/node-watch.ts` prefers native file-system
watchers and polls only on unreliable file systems. Go's standard library has no
portable watcher, so `OSFileSystem.Watch` polls snapshots every 2 seconds on all
platforms and reports mode `polling`. Persistent changes are reported, but a
change undone between scans may be missed. Directory growth beyond watch budget
ends watcher with an error. `TestOSFileSystemWatchReportsRecursiveChanges` and
`TestOSFileSystemWatchStopsWhenTreeExceedsDirectoryBudget` cover coverage and
resource limits (upstream `a84510819`).
