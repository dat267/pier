# Divergences

Representative rationales for deliberate differences from pi: Go runtime
constraints, corrected upstream defects, and project choices. D IDs also appear
in code comments; not every ID has a full entry here. The range is **D1–D221**.
Entries are ordered numerically. Closed, retired, and withdrawn rows keep their
IDs for historical references; overlapping rows refer to the owning explanation.

Related documents: [port status](PORTING.md), [architecture](architecture.md),
[lock inventory](locks.md), and [terminal-output audit](terminal-output.md).

## D11

**Explicit delta drafts instead of JavaScript Proxies.**

`chord/delta/tracker.go` preserves upstream's tracker lifecycle: value/revision,
`beginChange`, prepare/adopt, `prepareReplace`, staleness, abort, and no-op
adoption. Go callers mutate an explicit draft; the same diff engine materializes
its batch at settlement. Upstream calls batches "exact but not canonical", so
operation tuples may differ while the resulting value matches. `Op`/`WireOp`
are structs rather than heterogeneous tuples.

## D30

**Closed: timing flags are captured at startup.**

`coding/timings.go` captures `PI_TIMING` at package initialization, like v1.2.0
`core/timings.ts`. `SetTimingsEnabled` remains a test override; nil restores the
captured value. `TestTimingsStartupFlag` checks enabled and disabled starts after
changing the environment.

## D41

**Extension mechanics and the package manager are out of scope.**

Extension discovery, the runner, package/tools managers, extension resource
selectors, and extension-supplied prompt templates are absent. Seams use
function values or return nil. Context files, skills, SYSTEM.md and
APPEND_SYSTEM.md are ported.

`/reload` follows `AgentSession.reload` for supported resources: settings and
queue modes, `ai.ResetAPIProviders`, resource files, and a system prompt rebuilt
from active tool names. It also reapplies keybindings, implicit project trust,
the UI, and themes (`applyReloadedSettings`, `ApplyFromSettings`, `SetTheme`).
The built-in tool registry is not rebuilt: its tools capture no
settings-dependent state, and bash reads shell settings per call.

User-visible notices name only supported work. The reload notice omits
"extensions"; trust text names only the `.pi` settings/resources actually gated.
The package-update card and `pi config` resource-manager TUI are removed: both
require installed package resources. The latter was 1,315 lines reachable only
from its golden test. The extension selector's tools-expanded hook is absent.
No install-telemetry ping is sent because there is no backend; fresh-install
version recording remains for the changelog.

The release check runs off-loop and reports on-loop. Its former module-proxy
feed and `go install …@latest` card were superseded by D174. Offline behavior
matches upstream: `cmd.applyOfflineMode` folds `--offline` and truthy
`PI_OFFLINE` into `PI_OFFLINE` and `PI_SKIP_VERSION_CHECK` process-wide.

## D44/D47

Go uses function fields instead of overridable methods; listeners are compared
by code pointer.

## D51

`StdinBuffer` uses callbacks instead of `EventEmitter`.

## D71

**Closed: autocomplete debounces, aborts and runs off the editor.**

`tui.Editor` follows v1.2.0 `components/editor.ts`:

- Non-forced requests for a trigger token wait
  `ATTACHMENT_AUTOCOMPLETE_DEBOUNCE_MS` (20 ms, `getAutocompleteDebounceMs`);
  forced and explicit-Tab requests run immediately.
- The provider call runs on its own goroutine with a cancelable context
  (upstream's `AbortController`); the result is posted back through the editor
  host's `Post`, which the `editorHostAdapter` forwards to the renderer, and is
  applied on the owner loop.
- Superseded requests abort their context and their late results are dropped by
  the request-generation and text/cursor snapshot check
  (`isAutocompleteRequestCurrent`).

A host without the `EditorPoster` seam (test doubles, a nil host) keeps the
synchronous provider call, so editor state stays on one goroutine there.

Tests: `TestEditorAutocompleteRunsOffLoop`,
`TestEditorAutocompleteDebouncesTriggerTyping`,
`TestEditorAutocompleteDebounceDecision`,
`TestEditorAutocompleteDropsStaleResults`.

## D83

**Obsolete: `DisableAutoRender` is a compatibility no-op.**

Every renderer is caller-driven since D146. The method remains for tests and
the API surface.

## D90

Narrow runtime interfaces provide test seams across the wirings.

## D105/D106

The ES `Proxy` renderer reference becomes an explicit forwarder. Clipboard
copy/read operations are injected; native clipboard is out of scope. App-owned
clipboard lifetime and admission are covered by D200/D201.

## D121/D123/D135

**Historical cross-goroutine mutexes.**

Model refresh, startup input/telemetry, lifecycle flags, and the footer watcher
were made mutex-safe. D123's input lock retired in stage 1 in favor of a buffered
submission channel; the remaining UI-state locks retired in stages 3–4. See
D146–D149 and [locks.md](locks.md) for current ownership and retained locks.

## D132

**Closed: branch summarization matches v1.2.0.**

Upstream `core/agent-session.ts.isCompacting` includes branch summarization and
`abortBranchSummary` cancels it. Pier already matches both;
`TestNavigateTreeAbortBranchSummary` covers cancellation. This row was stale.

## D133

**Narrowed: edit preview matches the pin; extension renderer definitions stay out.**

Tool renderers still resolve to built-ins, not extension definitions (D41). The
synchronous-preview gap is closed: `computeEditsPreview` runs on a worker and
applies through the owner post, matching the asynchronous preview in v1.2.0
`core/tools/renderers/edit.ts`. `TestEditPreviewAppliesThroughOwnerPost` covers
that handoff.

## D136

Historical fix: editor `OnSubmit` ran outside the editor lock. The UI lock is
now retired (D146); watchdog coverage is described in D192.

## D137

Historical fix: model-selector callbacks ran outside the state mutex. The UI
mutex is now retired (D146); see D192 for replacement coverage.

## D138

Terminal input is delivered outside the terminal lock. UI-state retirement and
retained terminal I/O locks are covered by D147 and [locks.md](locks.md); see
D192 for watchdog history.

## D139

Historical fix: stdin-buffer callbacks ran outside its lock. The buffer is now
consumer-owned and lock-free (D147); see D192 for replacement coverage.

## D140

The user's hyper and commandcode provider extensions are compiled in as
`providers.ExtensionProviders()`, not loaded from `~/.pi/agent/extensions`
(extension loading is out of scope, D41).

## D141

**A cached, single-line footer replaces upstream's two-line footer.**

The user's footer-extension format is built in:
`3.5%/1M · <statuses> · model · cwd` on one dim line. Upstream walks every
session entry per frame; with raw JSON in Go that required `json.Unmarshal`
per message, measured at 445ms per frame on a 10k-entry session. Pier caches
the footer and invalidates it on session events. `FormatTokens` and
`FormatCwdForFooter` retain upstream behavior. `app.UI` uses the D105 forwarder
so `/tui` and exit-replay renderer swaps reach every holder.

## D142

**Closed: compaction uses the stock summarizer only.**

The user's compaction extension (`~/.pi/agent/extensions/compaction`) is no
longer built in. `coding.Compact` follows v1.2.0
`core/compaction/compaction.ts compact`: the stock prompts, the retry-wrapped
summarizer, the caller's auth headers, and the stock ~13k text budget. The
extension-only pieces (32k no-reasoning call, pi-better-compact prompts,
file-list stripping/regeneration, `PI_COMPACT_MODEL`, opencode routing headers)
were removed, matching upstream with extension mechanics out of scope (D41).

Tests: `TestCompactUsesStockSummarizer` (stock prompt, no `HARD BUDGET`, and
auth headers reaching the request).

## D143

**One UI owner loop replaces JavaScript's single-threaded event loop.**

The interactive loop selects over session events, submissions, work completions,
and `ctx.Done()`. Blocking work runs off-loop; streaming partials coalesce
latest-wins. Input/signals, selector callbacks, and lifecycle callbacks also
apply on that loop (stages 3–4). Upstream awaits prompts on the JS event loop.
D145–D147 define input/render ownership; D196/D197 define queue ordering and
fairness. [architecture.md](architecture.md) holds the full design.

## D144

**Render requests coalesce on caller-driven ticks.**

The interactive loop paints once per drain instead of arming a throttled render
timer. D146 later removed timer mode for standalone pickers and library
renderers too; callers now drive every renderer.

## D145

**Input, resize, and signals are producer messages.**

The stdin reader, SIGWINCH watcher, and signal handlers only enqueue. The owner
loop dispatches input, paints on resize, and handles shutdown; upstream delivers
these on one JS thread. See D143 for the loop and D147 for decoding.

## D146

**Retired: UI-state mutexes and the renderer's internal timer.**

Input and painting share one owner goroutine everywhere, including the
standalone `-r` picker. `Renderer.mu`, `renderMu`, `Container.mu`, `Editor.mu`,
`AltScreen.mu`, and `ScrollView.mu` are deleted. Render requests coalesce on
the tick channel; cross-goroutine callbacks use `Post`, queued under `postMu`.
The ScrollView hide timer became an animation-driven lazy deadline. Off-loop
test readers use `Post` snapshot helpers. [locks.md](locks.md) lists all retired
and retained locks; D192 records replacement watchdog coverage.

## D147

**Retired: decoder and terminal UI-state locks.**

The input consumer owns `StdinBuffer`. The interactive loop feeds raw chunks
through `ProcessTerminal.FeedInput` and drives `PendingTimeout`/`FlushExpired`,
with no internal mutex or timer. Keyboard-protocol negotiation is loop-owned.
`ProcessTerminal.writeMu` remains for write/raw-mode serialization, including
OSC 9;4 progress keepalives and shutdown restoration, not UI state. The legacy
non-raw reader remains for library consumers without a UI loop. The complete
I/O/handoff lock inventory is in [locks.md](locks.md).

## D148

**Closed: the model-catalog refresh registry is lock-free.**

Atomic copy-on-write maps provide insert-if-absent and unpublish-if-matching;
refresh outcomes, waiter counts, and cancellation flags are atomic. Publication
cannot overwrite an entry added after a load, and a waiter slot is claimed only
after confirming publication. These rules close two races found during retirement.

## D149

**The footer provider retains its cross-goroutine lock.**

`FooterDataProvider.mu` (`coding/footerdata.go`) protects cwd, git paths/branch,
statuses, and listeners shared by footer rendering and its 500 ms git-HEAD
watcher. Retirement would require posting poll results to the UI loop and
delivering listeners outside the lock, a `coding/` change outside the UI
refactor. The watcher reads git state and calls `Refresh`. See
[locks.md](locks.md) for the authoritative inventory.

## D150

**Closed: resource diagnostics, source labels and listings match v1.2.0.**

Skills, Context, Prompts, ctrl+o expansion, and skill/prompt/theme diagnostics
follow v1.2.0 `interactive-mode.ts.showLoadedResources`. Warnings remain visible
when startup listings are quiet. Prompt/theme names deduplicate first-wins and
report collision paths; prompt reads and theme discovery retain warnings.
Creation/reload propagate prompt diagnostics. Theme diagnostics are published
per source generation and read without owner-loop file I/O. Discovered themes
carry user/project/temporary `SourceInfo`, with trusted project discovery before
explicit paths and after user defaults, matching `core/resource-loader.ts`. The
port previously rendered a `[Themes]` listing; v1.2.0 has none, so it was removed
(`addLoadedSection` covers only Context, Skills, Prompts and out-of-scope
Extensions). Diagnostic paths use upstream's source labels (`user`, `project`,
`path (temp)`, package source) through `getShortPath`, with parent lookup like
`findSourceInfoForPath`. `coding.LoadSkills` already resolves paths and
deduplicates real paths/names. Extensions and package-source discovery remain
out of scope (D41/D140).

Tests: `TestPromptTemplateCollisionsReachSession`, `TestUnreadablePromptDiagnostic`,
`TestLoadedPromptConflictsFollowsQuietStartup`, `TestLoadedThemeWarnings`,
`TestThemeCollisionsKeepFirstSource`, `TestDiscoveredThemeScopes`, and
`TestLoadedResourcesHasNoThemesSection`.

## D151

**`defaultsync` applies the settings model at session start and reload.**

`coding/defaultsync.go`, wired through `coding/sdk.go` and
`coding/agent_session_reload.go`, ports the user's extension. Upstream restores
a session's model indefinitely; Pier reapplies `defaultProvider`/`defaultModel`
on creation and `/reload`, as the extension did on `session_start`. Manual
switches last only for the current session; there is no persisted claim, command,
or opt-out except clearing the default. The full catalog model is applied, not
a bare reference lacking footer limits (`?/0`).

Two differences from the extension:

- Its provider-auth snapshot arrives asynchronously, so it polls every 150 ms
  for up to 4 s. Pier's `queueAvailabilityRefresh` is inline, so one attempt
  suffices. Failure texts remain `no configured auth` and
  `no configured auth, or not in the catalog`.
- An explicit `--model`/`--provider` (`CreateAgentSessionOptions.Model` non-nil)
  suspends synchronization for the session, including reloads.

Startup diagnostics report a switch informationally and an unavailable default
as a warning.

## D152

**Portable Unix socket publication.**

Upstream atomically hard-links a bound socket without overwriting another
process's path; cleanup tracks its device/inode. Android denies `link(2)` to
app domains, so Pier's `publishSocket` tries hard link, then
`renameat2(RENAME_NOREPLACE)`, then plain rename. A link failure with `EEXIST`
returns immediately. The plain-rename fallback stays atomic but can replace a
path created in the intervening race window.

Implementation: `server/unix.go`, `server/publish_linux.go`, and
`server/publish_other.go`. `renameNoReplace` uses `//go:build linux` (also
Android); elsewhere it returns `errors.ErrUnsupported` before plain rename.

## D153

**Closed: supported CLI flags and initial images reach startup and reload.**

Implementation: `cmd/main.go`, `coding/clidiagnostics.go`, `cliinitial.go`,
`listmodels.go`, `paths.go`, and `sessionresourceload.go`. Fifteen flags plus two
non-flag inputs had no consumers. Current rules follow upstream except the
explicit omissions below:

- Diagnostics run after parsing, before `--version`; any error exits 1. Unknown
  long flags enter `UnknownFlags` for extensions and are not errors; unknown
  short flags report `Unknown option: -z`. Invalid `--thinking` and blank
  `--name` are errors.
- `@file` text precedes the first positional prompt; remaining prompts queue
  behind it (`buildInitialMessage`). Images travel only with that first prompt:
  `ProcessFileArguments`, `InitialPrompt.Images`, then interactive
  `RunOptions.InitialImages` or headless `PrintModeOptions.InitialImages`, and
  `PromptOptions.Images`. `TestPrintModeInitialImagesOnlyAttachToFirstPrompt`
  covers text and JSON modes against v1.2.0 `modes/print-mode.ts`.
- `--skill`, `--prompt-template`, and `--theme` paths resolve against process
  cwd (`ResolveCLIPaths`/`IsLocalPath`, upstream `resolveCliPaths`). Explicit
  paths survive their corresponding `--no-*`; discovery refusal does not
  refuse named resources. `--no-skills`, `--no-prompt-templates`,
  `--no-context-files`, and `--no-themes` suppress settings paths/discovery.
  `LoadPromptTemplates` now has a caller, enabling `/template`, prompt commands,
  and the startup Prompts section.
- These resource switches persist across `/reload`. `reloadResources` no longer
  restores suppressed resources or drops explicit paths.
- Themes resolve by declared name across discovered sources, not by an old
  `<dir>/<name>.json` fallback that bypassed `--no-themes`.
- `--models` overrides enabled-model settings, warns for unmatched patterns,
  and seeds new sessions: use the default if in scope, otherwise the first
  scoped model (`buildSessionOptions`). A pattern's `model:thinking-level`
  applies unless pinned separately. `--api-key` pins the resolved model
  provider's credential; `--name` writes `session_info`.
- `--approve`/`--no-approve` decide run trust. `--export` and `--list-models`
  complete before the TUI.
- `-e`/`--extension` enters `Extensions` but reports "this build loads no
  extensions" (D41); a missing value errors. `-ne`/`--no-extensions` enters
  `NoExtensions` silently because none load. Neither appears in `--help`, and
  tool descriptions omit extension tools.

## D154

**Pier has its own dark/light palette.**

`coding/interactive/piertheme.go` registers overrides under upstream names
`dark` and `light`. `ResolveThemeSetting`, `ParseAutoThemeSetting`, and
`GetDefaultTheme` still select by terminal appearance. Compared with upstream
`dark.json`/`light.json`:

- Decorative fills (`selectedBg`, `searchMatchBg`) are unset (`\x1b[49m`) so
  transparent/blurred terminal backgrounds show through. Primary text uses
  terminal foreground. Lists mark selection with an accent-colored `→ `.
- Signal fills remain: `toolPendingBg`, `toolSuccessBg`, `toolErrorBg`,
  `userMessageBg`, and `customMessageBg`. They distinguish tool states, user
  blocks, and custom/compaction frames; failed and successful tools otherwise
  had identical output. HTML export uses the same signal tokens.
- Amber replaces teal for the wordmark, borders, selection, and list bullets.
  User panels are warm (`#393630` dark, `#f4eee1` light), not upstream's
  `#343541` blue-gray or `#e8e8e8` gray.

Fills remain distinct after `rgbTo256`: low-spread dark colors use the gray
ramp, hues use the cube. `#393630` maps to 237 versus pending 235; a subtler dark
collapsed onto the same index. `piertheme_test.go` pins the palette.

Embedded upstream palettes remain for parity goldens (which clear the registry)
and library callers without the installer. Both `loadThemeJSON` and `loadTheme`
prefer the registry, avoiding shadowed-theme/export mismatches. `Theme` retains
its source document so registered themes without disk files can export to HTML.
Install follows capability setup because escapes are baked at construction.
D193 covers system fallback panels; D219 covers adaptive system accents.

## D155

**Wiring audit: absent collaborators must not silently disable features.**

Function-field seams were often nil-checked without being assigned. Fixed:
queue `ShowStatus`/`ShowError`/`ShowWarning` (including ctrl+t feedback),
`HandlerWiring.HandleBashCommand` (`!command`), `/new`, `HandleCloneCommand`
and selector `RuntimeFork` (`coding.ForkSessionAtEntry`, `App.forkAtEntry`),
`/import` (`ImportFromJSONL`), `OnExternalEditor`, auth `ScheduleTimer`,
`OnLabelChange`, and `ApplyFullscreenScrollbarSetting`.

Dialogs use callback chains, not value-returning seams: `ShowExtensionConfirm`
and `PromptForMissingCwd` deliver via `onAnswer`/`onCwd`, since selector `Show`
mounts and returns while upstream awaits answers. This enables `/import`'s
"Replace current session?" and import/resume missing-cwd prompts. App helpers
reuse the extension selector's title/description/options/timeout component.
Retries key on `coding.MissingSessionCwdError`, not `strings.Contains(err, "cwd")`,
which missed upstream's "working directory" wording. Bedrock's `ShowAuthSelect`
is a list inside the login dialog, preserving its focus/input routing (D156).

Closed or intentional seams:

- `ClearStatusContainerIfIdle` implements upstream's
  `!enabled && !activeStatusIndicator && statusContainer.clear()` and shares
  reload logic.
- `RebindSession` is redundant: composition binds initially;
  `applySessionReplacement` swaps the session and every wiring's `SessionInfo`.
- `OnPartialEventApplied` and `OnPromptShown` are labeled test hooks, like
  `Now`/`ScheduleTimer`/`DeleteSession` overrides.
- HTTP dispatch, package management, tmux, process-level seams, extension
  mechanics, and `ShowExtensionSelector` remain out of scope (D41).
- `SessionSelectorOptions.DeleteSession` and `CopyActiveSelection` have working
  defaults (deleter and AltScreen selection copy). Unreferenced `MaybeSaveTrust`
  does not mean trust saving is absent; reload implements it separately.

An AST scan with a maintained allowlist was dropped because it confused
fallbacks/test hooks with dead wiring; this row records the audit instead.

## D156

**Login waits off the UI loop.**

Inline `ShowLoginDialog`/`ShowApiKeyLoginDialog` called `LoginProvider`, which
waited on `ShowAuthPrompt` input that only the blocked UI loop could deliver.
OAuth device flows, API-key entry, and Bedrock method/profile selects hung.
Upstream's await yields; Go's wait does not.

`AuthWiring.startLogin` runs the shared flow on a goroutine, then posts editor
restoration, error reporting, and `CompleteProviderAuthentication`.
`LoginDialogComponent` marshals `Show*` mutations with `post`; prompt channels
are created synchronously and browser exec stays on the flow goroutine. Nil
`post` applies inline for tests. This was the fifth deadlock, found by source
review rather than PTY driving. `TestApiKeyLoginRunsOffTheUILoop` dispatches,
renders, and routes input on one loop, failing with
"the login flow is running on the UI loop" rather than hanging.

## D157

**Home-path completion splits raw input before cleaning.**

Upstream `getFileSuggestions` expands `~` before splitting directory/filter;
`path.join`/`filepath.Join` clean `~/.` to home, then split home into its parent
and name. Tab consequently listed `/home` and offered `~/dat/`. Pier splits
first: trailing `.` filters dotfiles inside home, while `..` clears the filter
and remains in the suggestion. Upstream has the same bug.
`TestFileCompletionUnderHomeFiltersInsideHome` guards the fix.

## D158

**The session picker stacks below 60 columns.**

Upstream `components/session-selector.ts` uses a one-line title/controls header
and right-hand metadata columns. On 40–55-column Termux terminals, controls and
hints truncate and messages get roughly twenty columns. Below
`mobileTerminalWidth` (60), Pier gives title/controls separate lines, wraps hint
separators, and gives each session two lines: full-width message, then indented
metadata. Visibility and page-up/page-down use `maxVisible/2`.

At 60 columns and above, upstream output is unchanged. Its 44-column `narrow`
golden is the sole deliberate exception, skipped in `sessionselector_test.go`
with this row cited. This applies to both `/resume` and `pier -r`.

## D159

**Session-wide accounting is folded and displayed off-loop.**

Upstream `handleSessionCommand` and transcript cache-miss rebuilding scan parsed
entries inline. Pier stores raw JSON, so repeated decoding froze its one UI
loop. On a 19k-entry, 45 MB session, `/session` delayed typing by 0.857s and
cache-miss rebuilding cost 563ms cold (startup, reload/switch/tree/compaction).

The existing entry scan now records `role`, `usage`, `provider`, `model`,
`responseModel`, `timestamp`, and assistant content block types. Payloads remain
undecoded; `content` stays raw because user messages may hold strings rather
than block lists. Statistics, cost breakdowns, and misses fold these facts using
`detectCacheMissFor` and reference aggregation. Only counted misses decode
messages; transcript notices use memoized message pointers. Live notices are
O(1); full entry-list functions remain for callers without a session.

The panel worker reads mutex-guarded settings/session state, copied agent state,
and the projection, never components, then posts the panel. Headless wiring
computes inline. Measurements on that session:

- Off-loop dispatch alone reduced typing delay to 0.038s; panel completion
  followed 0.69s later.
- Folded cache-miss scan: 563ms to 30ms. Panel scans: 629ms to 31ms; panel visible
  after 0.080s instead of 0.857s.
- Load cost moved from 507ms to 610ms due to content-type scanning. Startup pays
  before the TUI; reload/switch still pay on-loop.

Three recorded gaps are closed: `type === "usage"` cache-warming spend is
attributed to provider/model; synthetic messages use `entry.timestamp`, not
projection time; recorded compaction system messages precede summaries.

## D160

**One settings manager follows session cwd and trust.**

Upstream `main.ts.createRuntime` rebuilds cwd-bound settings/resources/session
on replacement using `sessionManager.getCwd()`, `resolveProjectTrusted`, and
`projectTrustByCwd`. Pier reuses one manager:

- Boot resolves trust, builds settings, and creates the session using
  `sessions.GetCwd()`, not process cwd. CLI resource paths remain
  process-relative (`resolveCliPaths`).
- Replacement clears loaded resources, pending messages, compaction queue,
  streaming state, and pending tools. `StartupWiring.RenderCurrentSessionState`
  uses `renderInitialMessages`, not reload's `rebuildChatFromMessages`, restoring
  trust warnings, footer, and editor history as upstream
  `rebindCurrentSession`/`renderCurrentSessionState` do.
- `SettingsManager.RebindProject` repoints all holders after resolving trust
  with `hasUI` false: undecided projects remain untrusted, with no mid-session
  prompt. Run overrides (`--use-theme`) and `applySettingsDependentUI` reapply,
  matching upstream `applyRuntimeSettings`.

Trust answers cache per project for the run, seeded at boot. Project
`.pi/themes` loads only when trusted; theme sources reinstall after the initial
trust decision because the `-r` picker needs themes before project resources
are readable.

Extensions remain absent (D41). The model runtime remains process-wide:
`models.json` is agent-dir scoped on both sides, so switches do not change its
path; Pier rereads it on `/model`, not on switch. Settings, skills, templates,
context, system prompt, themes, and trust otherwise follow the session project.

## D161

**Tool scratch logs are swept to a 256 MB budget.**

Upstream leaves truncated output files to the OS. On this host, one 174 MB
command persisted for days; after three days, 230 files occupied 442 MB of
scratch RAM on tmpfs. Pier sweeps oldest-first when creating a log, down to
256 MB, covering `pi-bash-<id>.log`, `pi-output`, and `pi-powershell` prefixes.
The active file, unrelated tool logs, and matching directory names are never
touched. The model still receives "[Output truncated. Full output: <path>]".
`TestSweepTempOutputFiles` checks exclusions and oldest-first order.

## D162

**Closed: non-TTY stdin or stdout selects print mode.**

`cmd.resolvePrintMode` follows v1.2.0 `main.ts.resolveAppMode`: either non-TTY
stream selects text print mode, while `-p` and `--mode json` remain explicit
options. RPC stays rejected, never converted to print mode.
`TestResolvePrintMode` checks precedence and both streams;
`TestNonTTYStartsPrintMode` runs piped startup without a renderer.

Text prints the final assistant text; JSON emits the tagged session header and
events, stripping cumulative `partial` from `message_update`. Failures report
on stderr with exit 1. Initial images now attach in both modes (D153).
Extensions/runtime rebind remain out of scope (D41).

## D163

**Historical: low-bandwidth renderer mode was removed for parity.**

SSH uses upstream padded frames. Escape timeout and wheel scrolling remain
aligned with `packages/tui/src/terminal.ts` and `wheel-scroll.ts`.

## D164

**Input paints only when requested.**

Fullscreen `?1003h` reports every mouse motion. Previously every stdin chunk
triggered an O(transcript) paint, starving queued keystrokes. The loop now drains
queued chunks and paints once only when `paintIfRequested` consumes a coalesced
`RequestRender` tick; the 16 ms timer cannot repaint that same request. A burst
of 60 moves plus one key costs 2–3 paints, not 63. Keyboard input requests
renders; unchanged moves, key releases, and terminal replies do not.

The animation walk caches between paints and invalidates after a paint that
could change animation state. `VisibleWidth` also fast-paths printable ASCII
after stripping sequences, avoiding grapheme allocation/search for styled text:
5.0 µs to 1.0 µs and 1104 to 324 B/op on a tool-output line.

Tests: `TestMouseMotionBurstDoesNotRepaint`,
`TestAnimationScanCacheIsDroppedByAPaint`,
`TestAltScreenMouseMoveRequestsNoRender`, and
`TestVisibleWidthStyledTextMatchesPlainText`.

## D165

**The listener-driven OSC 11 probe starts after raw mode.**

Upstream v1.2.0 `theme-controller.ts` queries combined OSC 10/11/4 colors and
repeats after appearance reports. Pier also sends nonblocking
`Renderer.RequestTerminalBackgroundColor` (`ESC ] 11 ; ? BEL`) for an independent
background listener. Sending before raw mode caused echoed queries and an SSH
freeze. `RunWiring.OnStarted` probes after `UI.Start`, and `RebindTUI` rearms it.
`CSI ? 2031` notification requests before `Start` only set a flag; `Start`
replays the toggle.

`HandleTerminalInput` delivers OSC 11 to both the combined query and the
listener. A background-only late reply can therefore update an auto theme
without the combined query's final DA1 sentinel. No reply leaves the
environment/system fallback. D218 covers off-loop combined-query waits.

Tests: `TestBackgroundListenerSeesLateReplyDuringColorQuery`,
`TestSystemColorQueryDoesNotHideStartupBackgroundReply`, and
`TestBackgroundProbeRoundTripThroughRenderer`.

## D166

**A stable theme handle replaces the upstream Proxy.**

Upstream's `theme` Proxy reads `globalThis[THEME_KEY]` per access. Pier's
`ActiveTheme()` now returns a stable `*Theme`: `Fg`, `Bg`, `GetFgAnsi`,
`GetBgAnsi`, and `ColorMode` forward through `Theme.concrete` to `CurrentTheme`.
`GetMarkdownTheme` factories capture that handle, and `Renderer.Invalidate`
clears caches after a switch. This replaces stale captured concrete themes and
the attempted `App.rebuildForTheme` workaround.

Construction-time `theme.fg` strings, such as header Text, remain baked on both
sides; `onChanged` still follows `updateEditorBorderColor`. Tests:
`TestThemeHandleResolvesTheCurrentTheme`, `TestMarkdownThemeRecolorsAfterASwitch`.

## D167

**ANSI slicing preserves codes at the start boundary.**

Upstream `utils.ts.sliceWithWidth` buffers pre-range codes but immediately emits
in-range codes. If a slice starts on an inline-code reset, the reset precedes
the carried color it should cancel, turning following text yellow after a
selection/double-click. Pier flushes the carried prefix before an in-range code.
Existing goldens lack this case and remain unchanged;
`tui/sliceansiorder_test.go` checks the ordering.

## D168

**Escape cancels without waiting for idle.**

Upstream's awaited `session.abort()` yields; Go's `AgentSession.Abort` blocks in
`waitForSessionIdle`. Calling it on Escape froze later input until the turn
unwound. A live trace measured `slow UI phase "raw-input" 188ms` in
`HandleEscape`, `RestoreQueuedMessagesToEditor`, and `Abort`, with neighboring
keystrokes at `read=8.8ms write=91ms`.

`AbortAsync` cancels retry, bash, and agent runs and returns immediately. Queue
restoration already precedes cancellation. `Abort` retains its wait where
settlement is required: session replacement (`sessionswitch.go`, before
disposal) and tree navigation (`interactivemode_selectors.go`, before reading
the tree). `TestEscapeHandlerDoesNotWaitForIdle` covers the loop-safe path.

## D169

**Mouse reports reassemble after escape deadlines.**

The buffer flushes incomplete sequences after 50 ms and lone ESC after 10 ms.
Reports split across a deadline can dispatch Escape, then type a rune-split tail
such as `[<65;59;42M` (wheel-down at column 59, row 42). Windows `ReadConsole`
and delayed Android reads exposed this; upstream's ESC-required regex has the
same failure.

Pier remembers a flushed potential head (ESC, `\x1b[`, or truncated
`\x1b[<...`) and rejoins valid continuations across multiple reads, including
bursts with subsequent reports or keys. Unrelated input clears the head. AltScreen
also accepts ESC-less reports and consumes truncated fragments. An Android
`books` replay reproduced `64;11;24M` leaking into the old editor; the fixed
binary consumed the burst and stayed in alternate screen. An Escape already
dispatched cannot be undone.

Tests: `TestSplitMouseReportAcrossTheDeadline`,
`TestExpiredMouseHeadRejoinsWheelBurst`,
`TestExpiredMouseTailReassemblesAcrossReads`,
`TestExpiredMouseReportRejoinsAtEveryBoundary`,
`TestOrphanedMouseReportIsStillDispatched`, and
`TestTruncatedMouseReportIsConsumed`.

## D170

**Closed: both header variants already include the upstream hint.**

`BuildStartupHeader` includes
`Pi can explain its own features and look up its docs. Ask it how to use or extend Pi.`
like v1.2.0 `interactive-mode.ts`. `TestRunStartupHeader` checks compact and
expanded variants. The omission recorded here was stale.

## D171

**OS socket failures are retryable.**

`packages/ai/src/utils/retry.ts` matches fetch/HTTP/websocket texts such as
`socket hang up`, `connection refused`, `connection lost`, `reset before headers`,
and `timed out`. Pier also retries `software caused connection abort`
(ECONNABORTED, Termux/Windows), `connection reset by peer` (ECONNRESET),
`broken pipe` (EPIPE), and `unexpected EOF`. Quota/billing checks still run
first, so a usage-limit error mentioning a reset is not retried.

## D173

**Closed: compaction labels already match v1.2.0.**

`NewCompactionStatusIndicator` shows `Compacting context...` for manual work,
`Auto-compacting...` for threshold work, and
`Context overflow detected, Auto-compacting...` for overflow, each with the
bound `app.interrupt` cancel hint. `TestCompactionStatusLabels` checks these
against `components/status-indicator.ts`. The plain-label description was stale.

## D174

**Updates use GitHub releases and verified release assets.**

Upstream `<app> update` runs its package manager (absent here, D41). Pier reads
GitHub's `/releases/latest` redirect, then `pier update` installs the release.yml
asset `pier-<goos>-<goarch>` after checking its sibling `.sha256`. Windows moves
the running image aside first (upstream's self-update quarantine); downloaded
assets also need their executable bit set.

The card names `pier update` and this module's release page, not `go install`
or pi.dev's changelog. No module-proxy publication is required. Unorderable
source versions (the commit hash from `just install` or unstamped `0.0.0`)
produce no card: upstream `isNewerPackageVersion` falls back to inequality for
non-semver, which would mark these builds outdated forever.

The GitHub API is deliberately avoided: unauthenticated clients share 60
requests/hour/IP, then receive 403. A mobile update observed
`x-ratelimit-remaining: 0` versus `4991/5000` authenticated. The redirect is not
rate-limited, and known asset names need no API listing. Android DNS fallback
is a separate issue (D175).

## D175

**Android gets a pure-Go DNS fallback.**

Releases use `CGO_ENABLED=0`. Go's resolver reads `/etc/resolv.conf`, absent on
Android (`/etc` points to read-only `/system/etc`), then tries `127.0.0.1:53` and
`[::1]:53`, failing with "connection refused". Termux's patched Go works because
it uses `$PREFIX/etc/resolv.conf`.

When stock config is missing, Android boot reads that Termux file or
`PIER_RESOLV_CONF` and installs `net.Resolver` rotating among its nameservers.
Node uses platform libc rather than resolv.conf, so upstream needs no equivalent.

## D176

**The system prompt omits pi's documentation index.**

Upstream `buildSystemPromptSections` emits a `docs` section pointing at pi's
README/docs/examples and routes questions to extensions.md, themes.md,
skills.md, prompt-templates.md, tui.md, keybindings.md, sdk.md,
custom-provider.md, models.md, packages.md, and environment-variables.md.
Pier ships port/architecture notes, not that tree or `examples/`, so
`coding/systemprompt.go` omits the section rather than naming nonexistent files.
The rest, including the preamble naming pi as the reference harness, is unchanged.

## D177

**Historical: native filesystem sandbox removed for stock pi parity.**

Read-only/workspace-write/full-access modes, Linux Landlock launcher, write/edit
path guards, `/permissions`, footer codes, and the sandbox prompt section are
removed. Upstream `core/tools/{bash,edit,write}.ts` uses ordinary OS permissions
without a built-in sandbox; `coding/sdk.go` now does so on every platform, with
no backend probe or read-only fallback. External/parent-process confinement
remains outside this feature. This ID records removal, not an active divergence.

## D178

**Shell cancellation cannot wait forever on surviving descendants' pipes.**

Upstream kills by process-tree pid (`taskkill /F /T` on Windows), then waits for
stdio to close. Background servers, `start /b`, or `setsid` descendants may keep
inherited output pipes open beyond the killed tree. Pier closes output readers
on kill and bounds draining with `waitForPipeDrain`, the existing grace helper
based on upstream `waitForChildProcess`.

Windows additionally uses a job object: `TerminateJobObject` reaches children
whose parent exited, unlike `taskkill /T`; assignment failure falls back to
taskkill. Normal completion closes the job without termination so intentional
background processes survive. The reproducer fell from 9s after cancellation
to under 0.1s.

## D179

**Closed: color parsing and truecolor boot detection follow v1.2.0.**

`tui.ParseColor(any)` accepts strings and numeric ANSI indices like upstream
`parseColor(string | number)`. Theme documents retain `ColorValue`
(`IsIndex`/`Index`) for JSON shape. `ThemeBoot.EnableCapabilities` follows
`detectCapabilitiesFromEnvironment`, then applies `PI_TRUE_COLOR`, then
`terminal.trueColor`. The injectable seam (D69) remains without forcing truecolor.

Tests: `TestParseColorAcceptsNumericPaletteIndex`, `TestTerminalDetectsTrueColor`,
`TestThemeBootDetectsTrueColorFromEnvironment`, `TestThemeBootPITrueColorOverride`,
and `TestThemeBootTrueColorOverride`.

Appearance notifications cover `system` and light/dark pairs. Selection uses
reported background, then appearance, then `COLORFGBG`, matching
`detectTerminalTheme`. `applyFromSettings` and `applyTerminalColorSchemeChange`
refresh combined colors. OSC 11 listener fan-out and its regressions live in
D165; off-loop waits live in D218. Additional tests:
`TestAutoThemeSettingQueriesCurrentTerminalColors` and
`TestSystemThemeRefreshesTerminalColorsOnAppearanceChange`.

## D180

**Go render caches need no V8 rope flattening.**

Upstream `54c19a252` adds `flattenLines` to Markdown/Text/Box caches: V8 string
concatenation retains rope trees, which held about a fifth of a long assistant
message's heap. Go strings and `strings.Builder` output are already flat.
The same commit's weak token cache is ported: `tui/markdown.go` uses
`weak.Pointer`; collected tokens reparse on the next render.

## D181

**Resource text/blob arms track key presence.**

Upstream `packages/mcp/src/protocol/content.ts` checks `"text" in resource`,
including empty text. `ResourceContents.TextArm` records that choice on decode
and marshals the text member whenever active.

## D182

**MCP stdio cleanup hooks signals, not `os.Exit`.**

Upstream `packages/mcp/src/transports/stdio.ts` kills process groups in
`process.once("exit")`. Go libraries cannot hook `os.Exit`; `mcp.installExitHook`
watches SIGTERM/SIGINT/SIGHUP, kills live groups, restores the default action,
and re-raises.

## D183

**HTTP retry classification uses Go errors.**

Upstream `packages/mcp/src/transports/streamable-http.ts` retries fetch
`TypeError` network failures. Go's net/http uses typed errors, so the transport
retries non-`McpHttpError` failures.

## D185

**MCP is wired directly without extension mechanics.**

Upstream `core/mcp-servers.ts` plus `extensions/mcp/` use extension registration
for exposure, namespaces, codemode, `tool_search`, resources, `/mcp` UI, and
`auth.json` OAuth. Pier wires config, conversion, and connections directly:

- Agent-dir `mcp.json` and trusted project `.pi/mcp.json` configure servers.
  Connections and live-tool attachment follow D195 (superseding synchronous
  boot and reload-only tool updates).
- `direct` tools enter `CreateAgentSessionOptions.ExtraTools` and built-in
  `--tools`/`defaultTools` selection. `codemode`, `deferred`, and `hidden` tools
  register but are never exposed; there is no codemode or `tool_search`.
- OAuth uses a per-process credential store, not `auth.json`; `/mcp` sign-in
  persistence is unported. `read_mcp_resource` is absent, so resource links do
  not advertise it.
- Definitions omit upstream renderers, output schema, and annotations;
  interactive MCP calls use generic tool rendering. `isError` becomes a Go
  error containing converted content because `agent.AgentToolResult` has no
  error flag.

Reference: `packages/coding-agent/src/extensions/mcp` and `core/mcp-servers.ts`.

## D187

**Attached-state callbacks are synchronous.**

`chord/services/attachedstate.go` ports the attachment half of `services/state.ts`
without upstream's per-subscription async queue (100 pending deliveries,
newest-wins overflow). Required durable rules remain: atomic snapshots,
commit-order buffered drains, cursor-gap errors disposing broken attachments,
source/op republication by reference, and subscriptions made during publication
hydrating after the current drain at its then-current sequence. This last rule
hides updates already covered by late hydration.

## D188

**Durable environment failures use Go errors.**

`env/index.ts`/`env/node.ts` return `Result`; Pier returns `*FileError` and
`*ExecutionError` with upstream codes and retains Result helpers. Paths,
`~`/file-URL resolution, error mapping, and tracked temp cleanup match.

Shell support includes Windows Git Bash candidates, `sh -c` fallback, direct
argv execution, timeout validation, spill rules, process-tree cancellation,
and stream-tagged `onOutput`. Separate stdout/stderr reader goroutines feed a
bounded channel, so cross-stream ordering follows send order, not a shared
pipe's bytes. Callbacks run synchronously on its consumer rather than Node
per-stream flow control. D214 documents the string/argv API split.

## D189

**Withdrawn: virtual models match upstream behavior.**

`ai.WrapProviderWithVirtualModels` lives in `ai`, not `coding`, because model
access and dispatch are package-private. It matches `withVirtualModels`: a
virtual model hides a physical model with its ID, survives filtering, and an
unrouted stream fails. Package placement is not a behavioral divergence.
Reference: `packages/coding-agent/src/core/virtual-models.ts`.

## D190

**Animation ticks do not rebuild full tool output.**

Upstream `ToolExecutionComponent.invalidate()` calls `super.invalidate()` and
`updateDisplay()`; bash's 1s `context.invalidate` interval rewraps all output.
Pier preserves explicit invalidation but uses `tui.AnimationTicker`:
`ToolExecutionComponent.AnimationTick` only calls `Container.BumpRevision`,
retaining caches. `Box.matchCache` checks versioned-child revisions (in-place
suffix changes cannot be detected by slice comparison); `Box.Render` backgrounds
from the first changed line. `bashPreviewComponent.Invalidate` also retains
incremental line counts.

For a 2000-line result, tick cost fell from 2.15 ms to 0.07 ms; bash preview
from 139 µs to 0.0009 ms. D194 now matches upstream tick frequency; D207/D208 refine
pending-frame clocks. References: `packages/coding-agent/src/modes/interactive/components/tool-execution.ts`
and `packages/coding-agent/src/core/tools/renderers/bash.ts`.

## D191

**Historical: the user goal extension was removed.**

The built-in `goal/` package and `GoalController` existed until v0.0.14, but
`~/.pi/agent/extensions/goal` was a user extension, not stock pi. Removal restores
stock behavior at the D41/D133 extension boundary.

Historically `SessionGoalSink.SendGoalMessage` encoded an invisible
`ai.CustomMessage` (`role: "custom"`, `{customType, content, display: false, details}`)
and queued `AgentSession.FollowUp` to start on settlement. `ConvertToLlm` made
it model-visible user content; transcript `decodeCustomMessage` skipped it.
Existing `pi-goal` entries still replay as generic cards; goal renderers are gone.

## D192

**Retired: UI-mutex deadlock PTY watchdog flows.**

Upstream's single JS thread has no equivalent. Five port watchdog flows for
D136–D139 drove submit/model/scroll/buffer-exit interactions through a real PTY,
sent `SIGQUIT`, and rejected dumps containing `sync.runtime_SemacquireMutex`.
The UI-state locks are now retired (D146/D147, [locks.md](locks.md)); core locks
and other blocking work still need enforcement.

`internal/uiblock` performs a module-wide SSA check for blocking syscalls,
network/exec, channel operations, locks, sleeps, and JSON reachable from UI roots.
Functional flows moved to in-process `coding/interactive/ptyflow_test.go`:
submit reaches the session, `/quit` requests shutdown, `/model` opens/disposes,
and transcript scrolls/paints. `TestMutexBlockedDetectorHasTeeth` remains to
validate the dump parser. The four replacements retain `testing.Short()` skips.

The old five flows cost about 11.5 s within a 27 s package run; replacements
cost 0.49 s combined. SSA still costs about 6 s (`packages.Load` 3.7 s,
`prog.Build` 1.4 s); narrowing/caching it would weaken or obscure the check.
Reference: `packages/coding-agent/src/modes/interactive/interactive-mode.ts`;
this defect class is port-local.

## D193

**System fallback keeps Pier's signal fills.**

Upstream `system-theme.ts.indexedColors` leaves all panels transparent when no
OSC 10/11/4 colors arrive. Pier applies D154's `pierSignalPanelHex` for the
detected appearance to `toolPendingBg`, `toolSuccessBg`, `toolErrorBg`,
`userMessageBg`, and `customMessageBg`. `selectedBg`/`searchMatchBg` remain
transparent (`transparentBackgroundColors`). The overlay runs in
`buildSystemTheme`, not `GenerateSystemThemeColors`, so generator goldens
remain upstream-parity.

Reference: `packages/coding-agent/src/modes/interactive/theme/system-theme.ts`.

## D194

**Closed: shell elapsed labels refresh every 1000 ms.**

`bash`/`powershell` use a 1s `shellElapsedTick`, matching v1.2.0
`core/tools/render-utils.ts.toolTimerText`. Tenths display resolution and
recorded final durations remain unchanged. Tool-level animation still drives
silent calls through cached parents, and D190's efficient revision bump remains.
`TestShellElapsedDuration` checks the interval; elapsed-label regressions cover
live clocks and final `Took` values.

## D195

**MCP connects asynchronously and attaches tools to live sessions.**

Upstream `packages/coding-agent/src/extensions/mcp/index.ts` starts connections
at boot, then waits once at `before_agent_start` for `hasDirectTools` servers.
`DEFAULT_STARTUP_WAIT_MS` caps that at 10s; expiry reports
"MCP servers are still connecting; their tools become available once connected."
Tools register live as connections finish.

Pier follows this through `NewMcpManagerAsync`, session `BeforeFirstTurn`
(first `runAgentPrompt`), and `AttachExtraTools`. Print warnings go to stderr;
interactive warnings go to UI status. This replaces 1.0.2 synchronous boot/TLS
waiting; the `Connecting to MCP server "name"...` diagnostic remains.

`ReloadMCPExchange` closes old connections, rereads agent-dir/trusted-project
`mcp.json`, starts `NewMcpManagerAsync`, and `AttachSession` restores live wiring.
This matches upstream shutdown/start reasons "reload" and reconnect.

Remaining differences:

- Connection failures surface at first-turn wait completion rather than startup;
  config errors still report at boot. Reload failures join its error report,
  whose status names "MCP servers" among reloaded items.
- No `mcp_servers` prompt section: it describes non-direct exposures Pier does
  not expose (D185).
- `WaitForDirectTools` polls every 20ms instead of awaiting ready promises.

## D196

**Assistant partials carry queue-local stream generations.**

D143's lossless lifecycle and latest-wins partial channels do not preserve
cross-channel order. A delayed partial from assistant A must not overwrite B
or create A's tools after A's end and B's start.

Each assistant start stamps a monotonically increasing generation. Both
run-loop application paths reject updates not matching the applied start,
including partials overtaking their own start; lossless end events carry full
content. Generations exist only in the queue envelope, not session events,
persisted JSON, or provider payloads. Direct dispatchers still require ordered
events. `TestQueuedMessageUpdateCannotOverwriteTheNextMessage` checks rendered
content/pending tools for A's delayed partial and B's valid next partial.

## D197

**Pre-paint drains apply at most 64 events with fair service.**

Channel capacity did not bound a drain continually refilled by producers, and
lossless traffic could starve partials. Each round now attempts one lossless
event and one partial, giving unused capacity to whichever remains ready.
FIFO within each channel and all lossless deliveries are preserved. Remaining
events wake the main select, allowing paint/input/cancellation/watchdog beats.
The bound is event count, not individual handler time.

Tests refill synchronously through loop-side hooks, avoiding scheduling luck:
`TestDrainReadyEventsYieldsUnderContinuousRefill` checks bounds/eventual delivery;
`TestDrainReadyEventsDoesNotStarvePartials` checks both classes;
`TestRunLoopInputPaintsDuringContinuousEvents` checks input paint, a subsequent
cancel key, and advancing loop beats.

## D198

**Paint scheduling uses output-backlog hysteresis.**

`ProcessTerminal.PendingWriteBytes` gives an atomic snapshot of queued/in-flight
committed output, excluding open frames; unlike `WriteBacklog`, it takes no
terminal handoff mutex on the UI loop. At 256 KiB or more, new paints pause;
they resume at 128 KiB or less. The existing timer retains the pending paint
and retries every 16 ms without needing another request. New requests cannot
postpone that retry. Input, flush deadlines, events, and cancellation continue;
input-latency tagging waits until its paint is generated.

Committed differential frames/writes are never dropped or reordered. Watermarks
are not hard caps: a large frame or direct writes can overshoot. This applies
only to the interactive schedule, not standalone screens, direct renderer
clients, persistence, or shutdown flushing. Terminals without the optional
lock-free snapshot retain old scheduling.

`TestLoopScheduleDefersPaintsUntilOutputDrains` uses `testing/synctest` for all
paint paths, hysteresis, retry wakeups, and input tagging.
`TestRunLoopWithBlockedConsole` verifies bounded repeated-paint growth,
responsive events/cancellation, and FIFO delivery followed by a current frame
when a real pipe writer recovers. Backlog tests cover queued, in-flight,
open-frame, and drained snapshots. D202 covers optional direct-output admission.

## D199

**Shutdown cancels optional queues but drains accepted saves.**

`internal/offloop.Group.Queue` is mandatory: accepted session/settings tasks
drain FIFO without a cancellation deadline. `Group.OptionalQueue` rejects new
work, cancels context, drops waiting tasks, and does not wait for a running
worker ignoring cancellation. `StopAll` starts every shutdown before awaiting
mandatory drains, so a blocked save cannot delay optional cancellation.
Explicit `Flush`/`FlushAll` still wait for running optional work.

The app classifies theme loads, transcript pre-rendering, and keyboard clipboard
reads as optional; paste coalescing is per app. `StopMode` and `Close` stop owned
queues, with `StopMode` retaining explicit collaborator flushes outside the group.
Theme reads check cancellation before publishing; watcher lifetime follows queue
context. Posted theme switches/previews/pastes check again on execution.
Transcript warming checks between children and cannot publish or request render
after cancellation.

Cancellation does not kill arbitrary goroutines, reads, preparation, or legacy
clipboard providers; late results are ignored and clipboard subprocess timeouts
remain. This original policy did not cover process-wide copy queues/detached
work; D200 extends ownership and bounds permanent terminal exit. Session/provider
serialization is unchanged; accepted saves still have no timeout.

`TestGroupShutdownCancelsOptionalWorkButDrainsSaves` uses `testing/synctest` to
hold a save and cancellation-ignoring worker: cancellation precedes save release,
saves remain ordered, shutdown need not release the optional worker, and
queued/late tasks never run. App tests cover StopMode/Close, late reads/watchers,
posted switch/preview callbacks, paste, and canceled transcript publication.

## D200

**Mode-owned optional producers and a shared 2 s terminal-exit grace.**

D199's cancellation rules now include clipboard copies and explicit detached
work. `/copy`, fullscreen selection, and OAuth URL copies share the app's
optional FIFO instead of a process-global queue. Platform helpers receive its
context; cancellation stops fallback commands and cannot fall through to OSC 52.
Keyboard/Windows right-click reads share coalesced paste ownership; right-click
focus/insertion apply on-owner.

Detached bash/compaction inherits mode lifetime and active run context. Stop/Close
cancels that lifetime before mandatory drains. The stable UI forwarder gates
posts at both submission and execution, including already-queued callbacks.
Standalone clipboard/component APIs keep legacy defaults or may opt into
context-aware helpers. Arbitrary goroutines and console syscalls cannot be
forcibly terminated.

After accepted saves drain, permanent interactive teardown shares **2 s** across
fullscreen replay, protocol restoration, post-stop hooks, and resume hint.
`App.Close` uses this policy when run cancellation bypasses `Lifecycle.Shutdown`.
Temporary stops, editor handoffs, and ordinary library stops remain lossless and
unbounded unless a caller explicitly starts a grace period.

Timeout ends flush waits without dropping/reordering the in-memory FIFO; CLI
exit can lose undelivered display/protocol output, never accepted saves. Raw-mode
restoration is still attempted. The resume hint uses the same FIFO and remaining
budget, or is skipped on timeout. Finalization closes admission and tells the
writer to drain/exit if the console recovers; it neither kills nor closes stdout.
Serialization/provider payloads are unchanged.

Virtual-time tests cover the exact 2 s/shared deadline, FIFO retention, late
rejection, and finalization. Lifecycle tests cover hint suppression/routing,
finalization before Exit, and canceled-run Close. Mode/helper tests cover copy
ownership/cancellation, right-click async reads, detached cancellation,
already-posted rejection, and pre-canceled commands/OSC 52 fallback.

## D201

**Optional admission has per-domain task-count limits.**

These live-mode limits complement D198's paint watermarks and D200's exit grace:

- Copies: four total (one running, three waiting). `Queue.TryGoContext` counts
  under its handoff lock and rejects without waiting; accepted copies stay
  FIFO. `/copy`/selection return busy/false, and OAuth callbacks report the same
  busy error through UI marshaling.
- Theme switches/previews: one running and one newest-pending load.
  `Queue.GoLatestContext` replaces a fixed pending key in O(1). It does not
  cancel/reorder running work; intermediate pending choices are superseded.
- Detached bash/compaction/reload: four parallel slots, no pending list.
  Excess work reports visibly; slots release on worker return, so ignoring
  cancellation still occupies a slot. Rejection warnings coalesce to at most
  one pending UI notification. Reload's boolean admission result restores its
  temporary loading screen/focus on rejection.

Overlapping paste reads and single-chunk transcript warming already coalesce.
These are not byte caps or global SDK/UI-post/model-turn/direct-output limits.
Mandatory saves retain unbounded, lossless FIFO admission/draining; committed
terminal frames are not evicted. Settings/theme JSON is unchanged.

Blocked-worker tests reduce 64 waiting copies plus one running to three waiting
plus one running, and 65 waiting themes plus one running to one successor plus
one running. Tests also cover FIFO/capacity reuse/latest replacement/shutdown,
busy feedback, warning coalescing, reload restoration, final theme choice, and
owner input painting with a blocked clipboard daemon.

## D202

**Optional terminal output waits before FIFO admission.**

At 256 KiB committed output, title/active-progress updates keep one newest
uncommitted hint each until recovery to 128 KiB. Essential progress-clear
invalidates delayed active hints. Permanent shutdown discards only uncommitted
metadata, never committed frames.

Interactive OSC 52 fallback uses the stable FIFO through a cancellation-aware
off-loop API, not raw stdout. Whole packets wait for 256 KiB capacity and a closed
renderer frame; oversized packets reject. Accepted packets/frames stay FIFO.
Essential protocol/lifecycle output, SDK writes, and standalone APIs retain their
admission, so this is not a global cap; one oversized title can overshoot.
[terminal-output.md](terminal-output.md) owns the direct-output inventory.
Tests cover blocked backlog, cancellation, atomic recovery, stale progress,
oversized-title progress, and app clipboard routing.

## D203

**Transcript resize warming yields every 64 direct children.**

Profiles identified the full Container walk, Markdown lex/wrap, and GC as the
resize cost, not queues/I/O. A 500-pair fixture took 35.53 ms and 10.48 MB per
resize (10 iterations); initial single probes of roughly 1 MB paste/tool updates
took 17.30 ms/18.26 ms.

An owner-only resize document wraps chat. After a complete frame exists, width
changes warm at most 64 direct children per Render and request continuation.
The previous complete flattened frame stays visible, never a partial new-width
frame. Ancestor cache skipping disables while incomplete. Width/child-identity
changes restart the snapshot; publication renders current children so stale
prepared content cannot replace newer state. No workers mutate attached children.

First-chunk cost fell to 1.45 ms and 0.59 MB (Linux/amd64, 10 iterations).
Remaining chunks are outside timing: this reduces per-call latency, not total
work. Opt-in benchmarks do not become ordinary timing assertions. Tests check
chunk bounds, cached-parent continuation, app composition, ordered frames, and
superseded width/child snapshots.

The bound counts children, not time; one large child, initial mount, paste,
streaming, flattening, or custom renderers can still dominate. Layout can call
Render repeatedly per paint. Frames may briefly be stale on resize. Standalone
containers, ownership, JSON/provider payloads, and D198/D202 output policy remain
unchanged.

## D204

**Large built-in assistant Markdown prepares private snapshots.**

A 16,000-pair heading/paragraph fixture (about 1 MB) took 296.63 ms and
114.11 MB cold (Linux/amd64, three iterations), dominated by Markdown/token keys
and GC. D203 cannot yield within one large child.

At 64 KiB source or more, built-in assistant text/thinking submits a private
Markdown snapshot to the app's optional backend and displays prior complete
lines or `Preparing message...`. It admits four jobs total (one running, three
pending); rejection retains no queued work and requests a later paint. D205
shares this budget with tool results.

Snapshots capture text, width, padding, concrete theme/style, and transform
configuration, never live caches. Owner-posted completion installs caches only
for matching content/width/style generations, including returns to cached
widths, and dirties ancestors. Teardown uses D199/D200 cancellation/post gates;
a running parse may finish privately but cannot publish canceled results.
Empty complete frames remain valid rather than resurrecting old content.

Detached session `Prepare` stays synchronous to avoid recursive submissions.
Standalone Markdown and custom assistant transformers stay synchronous unless
explicitly opted into a captured-safe backend. No UI-state lock or JSON/provider
change is added; old complete lines can briefly remain during changes.

Admission-only cost measured 0.024 ms and 3,704 bytes (three iterations), not
end-to-end latency or reduced worker work. Worker allocations/GC, final
flattening, plain-text tools, and custom callbacks remain limits. Tests cover
identical completed Markdown, blocked-worker input, bounded admission, live
composition, stale cached-width/content/theme rejection, empty-frame retention,
and synchronous detached warming.

## D205

**Large built-in tool results share the snapshot backend.**

Upstream `packages/coding-agent/src/core/tools/renderers/{read,bash}.ts` formats
expanded read and both shell modes synchronously. Live/replayed expanded read,
bash, and powershell now opt into D204's shared four-job budget at 64 KiB text.
Workers capture content, expansion/partial state, concrete theme, key hint,
truncation/full-output metadata, and copied slices/pointers. Persisted JSON
metadata up to 16 KiB is copied, not decoded on-owner. Elapsed clocks remain
owner-owned and live.

Display retains prior complete results or `Preparing tool output...`.
Content/width (including cached returns), expansion, theme, and backend changes
obsolete generations; only matching owner-posted completions apply. Rejected
admission retries without retaining work. Detached `Prepare` remains synchronous
with no nested submissions. Custom callbacks, images, unknown metadata objects,
JSON metadata over 16 KiB, raw fallback, and other tool types stay synchronous.
D204's teardown guards and unchanged JSON/provider formats apply.

Linux/amd64, three iterations of a 1,050,000-byte expanded fixture:

- Read cold: 60.31 ms/28,389,757 bytes; admission: 0.077 ms/21,216 bytes.
- Bash cold: 59.00 ms/23,180,128 bytes; admission: 0.034 ms/5,592 bytes.

Admission uses a sink, including construction/UpdateResult, not end-to-end
latency. ANSI wrap/tracking, concatenation, allocations/GC still run on workers;
final padding/flattening and unsupported paths remained owner limits (D206).
Tests cover identical output, live/replay composition, blocked-queue input,
stale generations, copied values, custom callback ownership, persisted metadata,
and synchronous detached warming.

## D206

**Eligible tool workers also finish Box padding/backgrounds.**

Upstream `packages/tui/src/components/box.ts` styles/pads after child rendering.
D205 left this full pass on-owner: a 1,050,000-byte fixture's publication cost
15.90 ms read or 14.65 ms bash, about 10.63 MB/90,000 allocations.

Built-in headers/default shells capture header lines, concrete background, and
elapsed text on-owner. The same worker finishes a detached Box frame;
generation-matching completion installs results and adopts its cache, rebinding
metadata to original owner children. Attached components, callbacks, and clocks
never run on workers. Custom call headers and self-render shells do not opt in;
standalone Box/detached synchronous warming remain unchanged. D204/D205's
budget and lifetime guards apply without a new queue or lock.

`Box.AdoptPreparedFrame` is owner-only: generation, current child lines at width,
padding, child count, and background sampling must match. The handed-off snapshot
must remain immutable. Later changes invalidate normally; mouse callbacks and
coordinates remain original. If time advanced during preparation, only trailing
clock rows/bottom padding repaint.

Linux/amd64, three iterations: owner publication fell to 1.47 ms read/0.994 ms
bash, each 1,024,312 bytes/8 allocations. Timing excludes fixture/worker
work, includes apply/first owner render, and is not end-to-end latency. Padding
moved rather than disappeared; flattening, headers, retained-frame invalidation
(D207), and GC remain limits. Tests cover identical frames, no full owner pass,
stale rejection, timers, custom headers, geometry/background rejection, and
original mouse targets.

## D207

**Pending updates retain the completed tool Box frame.**

Upstream `tool-execution.ts` rebuilds shells and Box restyles lines. D205/D206
still repainted retained raw output after invalidation: a 1,050,000-byte fixture
cost 14.83 ms read/15.88 ms bash, about 10.15 MB/90,000 allocations for pending
invalidation/admission/render.

Eligible tools now retain complete header/padding/background frames during
replacement preparation. Prepared metadata is separate from displayed metadata:
an unpainted completion cannot replace retained-frame geometry. Cold tools use
the pending label; synchronous fallback/collapse/empty paths, custom headers,
and self-render shells keep existing contracts. Content/width/theme changes
admit work without repainting old large output on-owner.

Only two clock rows restyle with the retained frame's concrete background/width.
`ChangedFrom` describes the returned frame for parent prefix reuse. Mouse dispatch
uses displayed width/layout, not pending width, avoiding full attached renders
on resize clicks; original result callbacks still use current owner state.
Originally all pending tools were unversioned and copied slices when clocks
changed; D208 now versions accepted pending work while keeping busy work
retryable. The D204/D205 budget, generation checks, teardown guards, and unchanged
JSON apply.

Linux/amd64, three iterations: read pending work fell to 0.061 ms/1,997 bytes/
48 allocations; bash to 0.039 ms/1,800 bytes/43 allocations. This excludes initial
preparation/workers, not an end-to-end claim. Old header/theme/width may remain
visible; large headers, busy clock copies, flattening, and GC remain limits.
Tests cover retained frames without attached background passes, replacements,
live cached-parent clocks, busy retries, stale rejection, and resize mouse routing.

## D208

**Accepted pending clocks mutate retained rows with owner revisions.**

Upstream `packages/coding-agent/src/core/tools/renderers/bash.ts` updates a
trailing elapsed label. D207 copied the entire pending frame for every clock
change because unversioned parent caches compare slices. Now only
unadmitted/rejected work is unversioned. Accepted pending work reports owner
Container revisions and its first changed row, allowing two-row mutation and
parent Box prefix reuse without modifying private worker snapshots.

Busy/unadmitted clocks still copy slices so skipping parents retry admission
and see ticks. Completion/source changes keep existing invalidation/generation
rules. No queue, lock, worker, or JSON/provider changes.

Linux/amd64, 20 iterations with blocked accepted replacement over 1,050,000 bytes:
a tick fell from 1.055 ms/484,070 bytes/17 allocations to
0.002456 ms/684 bytes/16 allocations. This isolates tick/render, not parents,
workers, or input latency. Headers, busy copies, flattening, and GC remain limits.
Tests cover backing reuse/revision changes, cached-parent clocks, retries,
Box tail updates, stale rejection, and displayed resize geometry.

## D209

**Container reuses flattened storage even for first-child changes.**

Upstream `packages/tui/src/tui.ts` allocates a flattened line array per render.
Pier reused later-child suffixes but first-child changes built scratch storage,
allocated, and copied again, penalizing nested single-transcript/tool containers.
All child indices now rewrite the changed suffix in existing owner storage.
Capacity grows geometrically; initial render, invalidation, and structural cache
drops still rebuild. Shrink clears removed string references. Revisions and
`ChangedFrom` propagate in-place changes through Containers/Boxes. No additional
queue, worker, lock, or JSON/provider change is introduced.

Linux/amd64, 20 iterations with an allocation-free first child in 30,000 warm
lines: flattening fell from 1.406 ms/540,672 bytes/one allocation to
0.0265 ms/zero bytes/zero allocations. This measures flattening only, not
Markdown, workers, terminal paint, or input latency. Changed-suffix copies remain
linear; invalidation/growth, headers, busy copies, and GC remain limits. Tests
cover reuse, zero warm allocations, cleared references, output/growth/width,
and nested version propagation.

## D210

**Persistence failure is distinct from queue completion.**

Upstream `core/session-manager.ts` synchronous writes throw on filesystem errors.
Pier's off-loop `rewriteFile`/`writeSessionLine` formerly discarded errors;
`FlushWrites` only confirmed draining, failed initial writes set `flushed`, and
missing serializer cases silently dropped entries. Appends could therefore
report success with a headerless/truncated file.

`SessionWriteError` now records rewrite/append/marshal/mkdir failures with path
and operation. `FlushWrites` drains accepted work and returns unresolved errors;
`DrainWriteErrors` clears diagnostics without disk waits; `WriteErrorsReady`
coalesces nonblocking wakeups for idle UI reporting. Failed writes mark state
uninitialized, forcing the next save to rewrite the full accepted prefix.
Each append captures an immutable prefix of the append-only entry slice so
recovery cannot duplicate later queued entries. New sessions/branches replace
write state, preventing old worker generations from publishing into replacements.

The UI selects on wakeups and reports chat errors. Teardown drains saves then
routes unresolved diagnostics through D200's output grace, never direct stdout.
Print mode flushes on every ordinary return and exits nonzero on failure;
stderr preserves JSON stdout. Formats remain unchanged. Tests cover sync/queued
initial and append failures, prefix/truncation recovery, per-task duplicate
prevention, coalesced wakeups, idle reporting, teardown, and print exit codes.

## D211

**Provider rebuilds publish atomically.**

Upstream `core/model-runtime.ts.rebuildProviders` clears providers then publishes
one at a time through `recomposeProvider`. Go readers could observe a missing
configured provider during that window. Pre-prompt `Models.CheckAuth` interpreted
"provider not composed" as "no auth configured", producing
"No API key found for anthropic." through `FormatNoAPIKeyFoundMessage`.
`TestSubmitFromEditorReachesSession` then hit its 30 s deadline. Under CPU load,
200 runs produced seven failures and five "provider not composed for anthropic"
diagnostics.

Pier builds without publishing (`composeProvider`), then swaps the whole map
under one lock (`ai.Models.ReplaceProviders`), superseding in-flight refreshes
for replaced IDs. Single-provider `recomposeProvider` remains unchanged.
`TestRebuildProvidersPublishesTheWholeSet` concurrently rebuilds/reads and failed
on the old shape around iteration 1650 of 2000.

## D212

**Closed: initial system-message ordering matches the pin.**

v1.0.2 placed initial system context after user input, affecting prompts/cache.
v1.1.0's `leadWithSystem` is implemented and retained in v1.2.0.
`TestDeriveContextLeadsWithInitialSystemMessage` checks initial users, later
assistant/system messages, and unchanged contribution order. ID retained for
historical references.

## D213

**SQLite first-page scans have no artificial ID cursor.**

Upstream `packages/durable/src/storage/sqlite/storage.ts.scanSql` uses
`id < Number.MAX_SAFE_INTEGER` for descending scans without a cursor, omitting
that valid ID unlike memory scans. Pier adds strict `>`/`<` predicates only for
cursor continuations. `TestConformanceScansIntegerBoundaryIDsInBothOrders`
checks maximum-safe-ID inclusion in both orders across backends.

## D214

**Shell scripts and direct argv have separate Go methods.**

Upstream `packages/durable/src/env/index.ts` uses a string-or-argv union.
`Shell.Exec(string, ...)` and `ExecArgv([]string, ...)` preserve the choice without
`any`. `TestOSShellExecArgvDoesNotParseArguments` checks unparsed argument delivery.

## D215

**Native file notifications have a snapshot-polling fallback.**

Upstream `packages/durable/src/env/node-watch.ts` prefers native watchers and
polls on Windows/unreliable filesystems. Pier's `github.com/sgtdi/fswatcher`
triggers snapshot diffs, with a 2-second safety scan for missed/filtered events;
Windows/Android are polling-only. Snapshots preserve recursion, exclusions,
coalescing, and directory budget. Transient changes undone before notification
or scan can still be missed. `TestOSFileSystemWatchReportsRecursiveChanges`
checks platform modes; `TestOSFileSystemWatchStopsWhenTreeExceedsDirectoryBudget`
checks the limit.

## D216

**Closed: fullscreen keys match the pin.**

v1.1.0's `packages/tui/src/keybindings.ts` mapping (`6100fe5a8`), retained in
v1.2.0, uses `Home`/`End` for the editor and `Ctrl+Home`/`Ctrl+End` for the
transcript. ID retained for historical references.

## D217

**Chroma replaces highlight.js.**

Upstream `theme.ts.highlightCode` uses `cli-highlight` with syntax theme tokens.
Pier uses Go-native Chroma v3.0.0-alpha.5 for Markdown code blocks and read/write
previews, mapping categories to `syntax*`. Unknown languages/lexer failures use
plain `mdCodeBlock` styling. Lexer/token boundaries differ, so ANSI is not
byte-identical. `TestHighlightCodeAppliesTokenColorsForKnownLanguage` checks
colors and preserved source. Reference:
`packages/coding-agent/src/modes/interactive/theme/theme.ts`.

## D218

**The combined system-color query waits off the owner loop.**

Upstream `theme-controller.ts.queryTerminalColors` and `interactive-mode.ts`
await `this.themeController.waitForTerminalColors()` before building the header;
JS yields to dispatch replies. Blocking Pier's owner prevented dispatch and
always consumed the full 100 ms `TERMINAL_QUERY_TIMEOUT_MS` before first paint
(measured ~150 ms regardless of terminal).

`BeginTerminalColors`/`TerminalColorWait` in `tui/terminalqueries.go` and
`tui/render.go` register/write on-owner, wait on a goroutine, and apply through
`Marshal`; late replies apply through `onLateReply`. Pier paints the fallback
before colors settle, then repaints, unlike upstream's single initial paint.
First-frame median improved from ~155 ms to ~53 ms. D165 covers raw-mode timing
and OSC 11 listener fan-out.

Tests: `TestProbeSystemColorsDoesNotBlockTheCaller`
(`coding/interactive/interactive_test.go`) and
`TestRendererBeginTerminalColorsWaitsOffLoop` (`tui/terminalcolors_test.go`).
Reference: `packages/coding-agent/src/modes/interactive/theme/theme-controller.ts`.

## D219

**The adaptive system theme uses Pier amber.**

Upstream `packages/coding-agent/src/modes/interactive/theme/system-theme.ts`
generates colors from terminal background/palette. Pier keeps generation and
appearance selection, then shifts `accent`, `borderAccent`, `customMessageLabel`,
`mdCode`, and `mdListBullet` to amber in OKLCH, preserving generated chroma and
adjusting lightness only to retain contrast. Without RGB background it uses ANSI
index 3, supplied by the terminal palette. D154's dark/light overrides already
use amber; this extends identity to default `system`.
`TestSystemThemeAccentFollowsTerminalAppearance` checks hue, contrast on light/dark
backgrounds, and indexed fallback.

## D220

**The interactive create-time model catalog refresh is offline; the background
pass performs it.**

Upstream `packages/coding-agent/src/core/sdk.ts` refreshes the model catalogs at
create (`refreshOnCreate` defaults to true). When the persisted
`models-store.json` is older than four hours
(`RemoteCatalogRefreshIntervalMS`), that makes the first launch after a break
wait on one HTTP request per provider, bounded by the 15 s
`ModelRefreshTimeoutMS`. Pier keeps the create-time refresh for print mode and
`--list-models`, which resolve a model before any background pass, but the
interactive mode runs create with `AllowModelNetwork: false`: it restores the
persisted overlay and availability snapshot from `models-store.json` without a
request, and the `RefreshModelCatalogs` pass that `interactive.Run` already
schedules after startup performs the network refresh. Startup for an
interactive session after the freshness window drops from the request time to
the store-load time (measured ~260-310 ms to ~25 ms here), and the catalog
still updates a moment later through the same runtime.

Tests: `TestCatalogRefreshAllowedAtCreate` (`cmd/catalogrefresh_test.go`) and
`TestCreateModelRuntimeOfflineCreateLoadsTheStoreWithoutNetwork`
(`coding/remotecatalog_test.go`).

## D221

**The last terminal colors are cached, so the first system-theme paint does not
flash.**

D218 keeps startup fast by painting before the OSC color query settles and
repainting when the terminal's reply arrives. With `theme: system` the first
frame is generated from no reported colors, so a light terminal flashes the dark
fallback (or the reverse) until the reply. Pier now writes the settled
`TerminalColors` to `<agentDir>/terminal-colors.json` off the owner loop (the
off-loop query wait goroutine) and seeds the system theme from it at boot
(`cmd`, before the first `InitTheme`), so the first frame matches the previous
session's terminal; the live query still revalidates and overwrites the cache.
Upstream (`theme-controller.ts`) instead awaits `waitForTerminalColors()` before
building the header, which the port's single loop cannot do without delaying the
first frame (D218). A terminal that changed appearance between sessions still
recolors on the reply.

Tests: `TestTerminalColorsCacheRoundTrip` and
`TestThemeControllerPersistsTerminalColors`
(`coding/interactive/terminalcolorscache_test.go`).
