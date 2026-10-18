# Divergences

Representative rationales for deliberate differences from pi: Go runtime
constraints, corrected upstream defects, and project choices. D IDs also appear
in code comments; not every ID has a full entry here, since closed, retired,
withdrawn, historical and removed-code rows are pruned. The range is
**D1–D241**. Entries are ordered numerically; overlapping rows refer to the
owning explanation.

Related documents: [port status](PORTING.md), [architecture](architecture.md),
[lock inventory](locks.md), and [terminal-output audit](terminal-output.md).

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

## D90

Narrow runtime interfaces provide test seams across the wirings.

## D105/D106

The ES `Proxy` renderer reference becomes an explicit forwarder. Clipboard
copy/read operations are injected; the native (Bun FFI) clipboard has no Go
counterpart. Text copy/read, clipboard file-path read and clipboard image read
use the platform commands (`clipboard.ts`, `clipboard-image.ts`):
wl-paste/xclip under Wayland/X11, the WSL `Set-Clipboard` PowerShell fallback
and the Windows Terminal headless OSC 52 path for copy, PowerShell
`Get-Clipboard` for Windows, and `pngpaste` on macOS. Clipboard file paths and
images are pasted as text, so the `@file` attachment path handles them.
Upstream reads file paths only from the native clipboard; the URI-list and
PowerShell forms are port-local substitutes. App-owned clipboard lifetime and
admission are covered by D200/D201.

## D133

**Narrowed: edit preview matches the pin; extension renderer definitions stay out.**

Tool renderers still resolve to built-ins, not extension definitions (D41). The
synchronous-preview gap is closed: `computeEditsPreview` runs on a worker and
applies through the owner post, matching the asynchronous preview in v1.2.0
`core/tools/renderers/edit.ts`. `TestEditPreviewAppliesThroughOwnerPost` covers
that handoff.

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

## D143

**One UI owner loop replaces JavaScript's single-threaded event loop.**

The interactive loop selects over session events, submissions, work completions,
and `ctx.Done()`. Blocking work runs off-loop; streaming partials coalesce
latest-wins. Input/signals, selector callbacks, and lifecycle callbacks also
apply on that loop (stages 3–4). Upstream awaits prompts on the JS event loop.
D144/D145 define input/render ownership; D196/D197 define queue ordering and
fairness. [architecture.md](architecture.md) holds the full design.

## D144

**Render requests coalesce on caller-driven ticks.**

The interactive loop paints once per drain instead of arming a throttled render
timer. Timer mode was later removed for standalone pickers and library
renderers too; callers now drive every renderer.

## D145

**Input, resize, and signals are producer messages.**

The stdin reader, SIGWINCH watcher, and signal handlers only enqueue. The owner
loop dispatches input, paints on resize, and handles shutdown; upstream delivers
these on one JS thread. See D143 for the loop; decoding is consumer-owned.

## D149

**The footer provider retains its cross-goroutine lock.**

`FooterDataProvider.mu` (`coding/footerdata.go`) protects cwd, git paths/branch,
statuses, and listeners shared by footer rendering and its 500 ms git-HEAD
watcher. Retirement would require posting poll results to the UI loop and
delivering listeners outside the lock, a `coding/` change outside the UI
refactor. The watcher reads git state and calls `Refresh`. See
[locks.md](locks.md) for the authoritative inventory.

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

## D171

**OS socket failures are retryable.**

`packages/ai/src/utils/retry.ts` matches fetch/HTTP/websocket texts such as
`socket hang up`, `connection refused`, `connection lost`, `reset before headers`,
and `timed out`. Pier also retries `software caused connection abort`
(ECONNABORTED, Termux/Windows), `connection reset by peer` (ECONNRESET),
`broken pipe` (EPIPE), and `unexpected EOF`. Quota/billing checks still run
first, so a usage-limit error mentioning a reset is not retried.

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
from 139 µs to 0.0009 ms. D238 sets the shell tick; D207/D208 refine
pending-frame clocks. References: `packages/coding-agent/src/modes/interactive/components/tool-execution.ts`
and `packages/coding-agent/src/core/tools/renderers/bash.ts`.

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
postpone that retry. Input, flush deadlines, events, and cancellation continue.

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
Print mode flushes on ordinary and signal returns; persistence failures report
on stderr and cause exit 1 on ordinary returns, while signal returns retain
129/143. This preserves JSON stdout. Formats remain unchanged. Tests cover sync/queued
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

Because the wait moved off the loop, a timed-out query's snapshot and a late OSC
11 reply can apply in either order. `querySystemColors` folds every report into
the stored colors (upstream `applyTerminalColors` merges field by field), so an
empty timeout result cannot clobber a background that arrived after the timeout.
Before the merge, a timeout applied after the standalone listener's reply zeroed
the background and the system theme lost the terminal's real appearance.
Tests: `TestStaleSystemColorSnapshotDoesNotClobberLateBackground`.

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

**A malformed finished tool call's `argumentsError` carries the Go decoder
message, not V8's.**

Upstream v1.3.0 (`parseToolCallArguments`, `ai/utils/json-parse.ts`) sets
`argumentsError` from the `JSON.parse` (or repair) `SyntaxError` message when a
finished tool call's complete arguments text is not a JSON object. The port
produces the same shape (`<message>`, a blank line, `Received arguments:`, the
raw text), the empty `{}` arguments and the `Invalid arguments for tool "x":`
validation failure, but the leading message is `encoding/json`'s, which differs
from V8's wording (`invalid character ...` vs `Unexpected token ...`). The
model-facing content (the raw text that failed) is preserved.

Tests: `TestParseToolCallArguments`, `TestValidateToolArgumentsReportsMalformedArguments`
(`ai/jsonparse_test.go`, `ai/validation_test.go`) and
`TestStreamOpenAICompletionsMalformedFinalArguments`
(`ai/openai_completions_test.go`).

## D223

**The user goal extension is re-ported as a builtin internal package.**

`~/.pi/agent/extensions/goal` is an installed user extension, not stock pi, and
the port has no extension host (D41). An earlier builtin was removed, then
re-ported as tree code on the user's request: `internal/goal` holds the state, machine, command parser and
renderers, `coding.GoalController` is the host glue, and `coding/interactive`
renders the transcript and tool cards.

The port follows the current extension sources, which are newer than the
removed builtin: `agent_before_settle` (continuation with a hidden round
message) replaces `agent_settled`/`retry_due`, `goal_replace` and the
`version` field exist, and each admitted round folds its token/cost usage.

Because the feature is native, the integration does not keep the extension's
host-object shape. Instead of an injected host and a callback sink bound after
construction, the session owns its controller: `SessionConfig.Goal` makes
`NewAgentSession` build `GoalController` against an unexported `goalSink`
adapter, register the tools, and replay the branch. `AgentSession.Goal` is a
plain field (like `CacheWarmer`), the `agent_end` hook is called from
`handleAgentEvent` before listeners see the event, and the tool-exposure policy
is the pure `goal.DesiredToolNames` next to the state machine. UI notices
produced during construction (the restored-goal notice) are buffered and
delivered by `SetUIHooks`, so the interactive mode sets its callbacks once and
`/reload` calls `Start("reload")` without re-reading the branch by hand.

The two tools (`get_goal`/`set_goal`, D234) are registered in the session
tool registry and join the default active set; unlike the extension, an explicit `--tools` allowlist still
filters them out, because `SetActiveToolsByName` owns pier's allowlist
semantics. The `registerMessageRenderer` for hidden `pi-goal-event` messages is
not ported (those messages are `display:false`, so the transcript never draws
them); the `pi-goal` and `pi-goal-turn` entry renderers cover the visible
cards.

Tests: `internal/goal`'s state/machine/command/render suites,
`coding/goalcontroller_test.go` (effect application, replay, tool exposure,
continuation/error/abort outcomes) and
`coding/interactive/goalrender_test.go` plus `toolrenderer_goal_test.go`.

## D224

**Reconcile a pending goal round's hidden prompt after compaction.**

The upstream goal extension queues one `pi-goal-event` round message at
`agent_before_settle` (`~/.pi/agent/extensions/goal/index.ts`). A compaction
between tool calls can summarize or remove that message while the round is
still active. The machine retains its pending turn, but the next model request
may no longer have the round instructions. `GoalController.roundPromptIfMissing`
checks the post-compaction agent messages before each next assistant request
and restores the hidden round message only when no `pi-goal-event` round
matches the active goal id and turn. Round-event details carry the current
`goalId`, so a prior goal's round 1 cannot suppress a different goal's round 1.
Paused, completed, and unarmed goals do not inject a reminder. Matching the
custom event type, goal id, and turn avoids duplicates.

This Go-port-local repair borrows the per-request reminder reconciliation
pattern in OpenCode's Plan plugin (`packages/core/src/plugin/plan.ts`, commit
`37e3912d8ea5814593b8591bdc85f91cb69949e2`); the upstream goal extension does
not reconcile round prompts this way.

Tests: `TestGoalRoundPromptSurvivesCompactionWithinRound`
(`coding/goal_compaction_test.go`) and
`TestGoalRoundReminderDoesNotReusePreviousGoalsTurn`
(`coding/goalcontroller_test.go`).

## D225

**Session listing scans message metadata instead of decoding every message.**

`buildSessionInfo` (the session picker's per-session summary) walks each stored
message for its role, timestamp and user/assistant text. The typed decoder
materializes every block, including thinking, tool-call arguments and
tool-result bodies that the listing never reads; on a 100 MiB session that was
almost the whole cost. The Go port scans the raw message instead, taking the
fields it needs and skipping the rest in place, with the typed decoder as the
fallback for any shape the scanner does not fully understand
(`coding/session_manager.go`'s `scanListingMessage`, the same contract as the
lazy loader's `scanSessionLine`).

The scanner validates structure only where it reads values, so for a session
whose message JSON the typed decoder would reject (a malformed block field, an
invalid number), the listing may still report that message's metadata where
upstream's `parseSessionEntryLine` would drop the entry. Valid sessions list
identically; `TestScanListingMessageMatchesTypedDecode` pins the agreement for
the shapes both accept.

Measured on a 100 MiB session (30,314 messages): `buildSessionInfo` 1.7s to
0.3s, and a 105 MiB session directory lists in 0.3s instead of 2.0s.

## D226

**A custom message that lands between a tool call and its result is deferred.**

The goal controller appends its lifecycle note (`pi-goal-event`, `display:false`)
while the `set_goal` tool executes, so the session order is assistant tool
call, goal `custom` entry, goal `custom_message`, tool result. `ConvertToLlm`
turned that note into a user message, and the OpenAI-compatible transform treats
a user message as interrupting the tool flow: it synthetically answered the call
and left the real result behind a user message, so opencode-go rejected the
request with `Messages with role 'tool' must be a response to a preceding
message with 'tool_calls'` (request reproduced from the 100 MiB session: one
orphan tool result).

`ConvertToLlm` now holds converted custom messages while an assistant message's
tool calls are unanswered and emits them after the results, mirroring the
transform's existing transparency for system messages. Genuine user messages
are untouched (they still interrupt tool flow through the transform). The note
stays in context, only after the result. This is Go-port-only: upstream has no
goal extension to interleave.

Any custom role is held, not just the goal event; the ordering rule is about
the tool pair, and the roles are out-of-band notes rather than turns.

## D228

**Mid-line `$$`/`\[` are literal text, not a paragraph split.**

Upstream's block LaTeX extension implements `start(source)` as
`/(?:^|\n) {0,3}(?:\$\$|\\\[)/`, and Marked reads a returned index as "the
preceding paragraph ends here". So `a $$x^2$$ b` parses as a paragraph `a`
followed by a pending `latexBlock` whose raw is `" $$x^2$$ b"`, and renders as
three lines with the raw display math; `a \[x^2\] b` splits the same way. The
same hook makes the whole parse quadratic: `start` scans the remaining source
once per block. Plain `marked.lexer` is linear (25.6 to 25.2 MB/s from 32 KB to
256 KB on paragraphs-only); adding only that `start` hook drops it to 5.7 to
0.9 MB/s, and pi's component render measures 2.36 ms at 16 KB against 302 ms at
256 KB.

pier treats `$$`/`\[` as block LaTeX only at a line start (after at most three
spaces) and as literal text otherwise, so `a $$x^2$$ b` stays one paragraph.
Only this mid-line case differs; line-start block math, inline `$…$` (including
upstream's closed-span validity checks in `internal/markdown/inline.go`), pending spans
and `\(…\)` all match upstream. Reproducing pi here would import the quadratic
`start` seam for an artifact no user asks for.

## D229

**A skip container serves its cached frame from an aggregate content revision.**

Upstream `packages/tui/src/tui.ts` re-renders the composed document every frame;
the fullscreen transcript's scroll child is the whole chat container, so a wheel
frame walked every mounted message (an interface assertion plus a `RenderVersion`
read per child) to decide reuse. On a long session that walk was the frame's
scaling term (47% of a 3000-pair expanded-thinking frame with the scrollbar
hidden, and it grew linearly with the mounted children).

Pier keeps a package-level `contentRevision` counter. Every content mutation
bumps it (`Container.dropRenderCache`/`BumpRevision`, the `Text`/`Spacer`/
`Markdown`/`Box` invalidations, `resizeDocument`'s multi-frame warm). A
`SkipUnchangedChildren` container caches the revision at the **start** of the
frame and returns its cached lines while that revision still matches, reading no
child revision at all. Caching the start revision (not the post-render one) is
what makes a child that bumps while rendering keep invalidating until it settles.
The invariant extends the existing `SkipUnchangedChildren` contract — every
child already had to bump on a change — to a global counter, so an unrelated
mutation costs one extra walk and never a stale frame. The cooperative resize
warm (`resizeDocument`, D203) calls the exported `tui.BumpContentRevision` so an
ancestor keeps re-rendering it across frames.

Linux/amd64, expanded thinking, wheel-style frame with the transient scrollbar:
500 pairs 304 µs/173 KB/865 allocs and 1500 pairs 478 µs fell to 238 µs and
~260 µs, so the frame is now flat in transcript size instead of growing with it.
This measures the render, not the terminal write. Tests pin the zero-revision-read
warm frame, descendant-mutation invalidation, the resize warm continuation and
the existing per-child walk-once behavior.

## D230

**The scrollbar repaints a cell without re-segmenting the line.**

Upstream `packages/tui/src/layout.ts` replaces a cell by slicing the line three
times and rebuilding the string. Pier's `SegmentGraphemes` built a `[]string`
with one header per character for ASCII, so a visible scrollbar row allocated
four such slices per frame (`GetGraphemeCellRange` plus three `SliceByColumn`
calls); `segmentGraphemes` was 59.6% of a scroll frame's allocated bytes.

`scanGraphemes` visits clusters without allocating, and `GetGraphemeCellRange`,
`SliceWithWidth` and `GetOsc8LinkAtColumn` use it. The scrollbar cell bytes are
unchanged (a golden table pins wide glyphs, tabs, ANSI and OSC 8), and
`replaceScrollbarCell` fell from 13,156 to 857 bytes/call. This measures one
cell replacement, not the whole frame.

## D231

**Syntax highlighting is built in.**

Syntax highlighting was removed earlier because Chroma v3 ran at ~0.5 MB/s on
the UI loop and made code fences a render stall. The `highlight` package is now a
from-scratch port of highlight.js 10.7.3 (the v1.3.0 reference engine), all 191
`lib/index.js` languages are registered, and `TestGoldenHighlight` matches the
reference HTML byte-for-byte on every fixture.

`coding/interactive/codehighlight.go` ports the reference's
`renderHighlightedHtml` and `buildCliHighlightTheme`: it walks the port's scope
tree, tracks the open scopes, resolves a scope by exact name then
`.`-prefix then `-`-prefix, colors each line with the innermost scope that has a
formatter (an empty line stays empty), and decodes the entities highlight.js
emits. A registered language gets per-token colors; any other language keeps
upstream's no-valid-language fallback, one `mdCodeBlock` color per line. The
scope-to-token map is upstream's (`keyword`->`syntaxKeyword`,
`built_in`->`syntaxType`, `literal`/`number`->`syntaxNumber`,
`regexp`/`string`->`syntaxString`, `subst`->`text`,
`comment`/`doctag`->`syntaxComment`, `meta`->`muted`,
`function`/`title`->`syntaxFunction`, `class`/`type`->`syntaxType`,
`tag`->`syntaxPunctuation`, `name`->`syntaxKeyword`,
`attr`/`variable`/`params`->`syntaxVariable`, `operator`->`syntaxOperator`,
`punctuation`->`syntaxPunctuation`, `emphasis`/`strong`/`link`->
italic/bold/underline, `addition`/`deletion`->
`toolDiffAdded`/`toolDiffRemoved`), so the ANSI differs from upstream only by the
theme palette (D154).

The reference builds highlight.js's HTML and then re-parses it; the port's
`highlight` package already returns the same scope tree, so `renderHighlightedNodes`
walks the tree directly and skips the HTML round-trip (equivalent, since the
HTML walk was exactly escape-then-unescape). On the 10.9 KB
`internal/highlight/testdata/sample_big.go`, the wired path measures 1.86 ms (5.86 MB/s)
against the reference's JS wired path at 3.20 ms (3.42 MB/s) in the same
session, so that stall does not return, and the port has no third-party
dependency:
`github.com/alecthomas/chroma/v3` and its `dlclark/regexp2` indirect dependency
stay removed, keeping the binary and startup gains that removal produced.

One divergence remains from the reference loader: pi registers 21 languages
eagerly and imports the rest lazily, so an uncommon language can render plain
until `loadAllHighlightLanguages` resolves. Pier registers all 191 at startup,
so an uncommon language is always colored. The token output is identical once
loaded.

## D232

**The agent may clear its own goal.**

The reference goal extension keeps `clear` human-only: its `update_goal` accepts
only `complete` and `blocked`, and `/goal clear` is the only way to tombstone a
goal. A model that judges the current goal irrelevant or superseded can only
complete it (claiming an outcome it did not reach) or block it (after three
rounds), and `create_goal` refuses while an unfinished goal exists ("A goal
already exists. Clear it first."), so it cannot start the corrected goal in the
same turn.

Pier exposes `clear` to the agent through its `set_goal` tool (D234), which
tombstones the goal through the same `OpClear` path as the human command and
stops goal work; the reply says so. Because the goal is gone, `set_goal` with a
new objective succeeds immediately after, so the agent can replace a stale goal
with the right one in one turn.

`goalClear` now also disarms and clears `pendingTurn`. The goal loop already
stopped because the view is nil, so the only change is a consistent snapshot
(the reference leaves `armed` set after a clear).

## D233

**`/goal set` overwrites a live goal without confirmation.**

The reference `/goal set` over an unfinished goal asks the human to confirm
("Replace goal?") before it tombstones the old goal and creates the new one; a
declined confirmation leaves the goal untouched. Pier overwrites directly: the
same `goal_replace` path runs (tombstone, create, queue the first round) with no
prompt, and the command replies "Goal replaced." The goal state machine and its
replay are unchanged.

The confirmation hook that existed only for this prompt is removed;
`GoalController.SetUIHooks` now installs the notify callback alone.

## D234

**The goal tools are `get_goal` and `set_goal`.**

The reference exposes three tools: `get_goal` (read), `create_goal` (create;
refuses a live goal), and `update_goal` (complete/blocked/clear, each guarded by
the exact `goal_id` and `revision` from `get_goal`). A model that wants to change
direction must clear first (D232), and every completion copies a ref back from
`get_goal`.

Pier collapses the write path into one tool. `set_goal` takes `objective` to
start the goal or replace a live one, and `status` (`complete`/`blocked`/`clear`;
`blocked_reason` for blocked) to end it. The exact id/revision guard is gone: the
tool acts on the current goal, so the model never has to copy a ref back, and a
replaced goal is tombstoned through the same `goal_replace` path so replay still
validates every step. The agent's objective path uses a create-or-replace
`goal_agent_set` machine event that does not queue a round (the run is active, so
`agent_before_settle` reserves the next one), unlike the human `/goal set`, which
queues the first round.

`get_goal` is the only other tool. `DesiredToolNames` keeps `set_goal` always
active and `get_goal` only while a goal is pursued.

## D235

**Stdio RPC has a bounded extension-free first milestone.**

Reference: pi v1.3.0 `packages/coding-agent/src/modes/rpc/{rpc-mode,rpc-types,jsonl}.ts`.
`coding/rpcmode.go` implements prompt/steer/follow_up/abort/clear_queue,
state/messages, model/thinking selection, queue modes, session introspection
(stats/entries/tree/fork messages/last assistant text), naming, `get_commands`,
compaction and retry controls, bash execution, and the session-replacement
commands (`new_session`, `switch_session`, `fork`, `clone`). It uses the
existing JSON event shaper, preserves optional correlation ids and response key
order, and keeps auth preflight off the command owner. Successful prompt
acceptance reports `started`/`queued`, after the run flag is installed,
independently of completion. The old boolean preflight callback remains
available to Go callers.

Deliberate boundaries and Go-specific behavior:

- Four pending prompt preflights may wait in addition to an accepted turn.
  Admission stays in input order; excess requests get a correlated failure,
  rather than growing an unbounded goroutine queue.
- Untrusted command shapes and required fields are checked explicitly. A valid
  id/type survives a field error; invalid envelopes and JSON syntax errors use
  `command: "parse"`. Parser error text is Go's, not JavaScript's engine text.
- A single unbuffered byte handoff serializes records and backpressures
  producers. Cancellation releases the owner and prompt workers even when an
  OS read/write cannot be interrupted. At most one input read and one output
  write may remain blocked until the OS releases them or the process exits;
  these pumps never mutate session state. In-flight output may be lost at
  shutdown, but accepted session saves drain without a deadline.
- RPC never opens the `--resume` picker or the cross-project session-fork
  confirmation. It rejects those paths with guidance to use noninteractive
  flags, and session-id warnings go to stderr, not protocol stdout. `@file`
  rejection follows upstream; images arrive through command payloads.
- `bash` and `compact` run off the read loop so a later `abort`/`abort_bash`
  can cancel them; the reference fires every input line without awaiting, which
  this port narrows to those two long-running commands. `abort` also cancels a
  manual compaction and branch summary (upstream `abort()` does).
- Session replacement runs through `RPCModeOptions.ReplaceSession`. The runner
  unsubscribes and disposes the old session after settling its workers, then
  builds and subscribes the replacement; with no factory the commands fail
  explicitly. `cmd` supplies the factory (upstream `createRuntime`): the new
  session shares the model runtime, settings and tool selection, a
  cross-project switch resolves that project's trust without a prompt and
  rebinds the one settings manager (D160), and the MCP first-turn hook is
  re-pointed at the new session. `export_html` renders through an injected
  `RPCModeOptions.ExportToHtml` seam because the exporter lives in
  `coding/interactive` and `coding` cannot import it; `cmd` supplies
  `interactive.ExportSessionToHTML` with the settings theme. Extensions/UI
  remain deferred; this does not wire the independent framed-CBOR server into
  the CLI. The typed subprocess client is ported separately in
  `coding/rpcclient` (spawn, id-correlated requests, `OnEvent`, wait/collect
  helpers, SIGTERM stop with a one-second kill escalation).
- `SessionManager.CreateBranchedSession` held the manager lock and then called
  the locking `GetBranch`, deadlocking every call. No prior caller exercised
  it; the RPC fork/clone path does, so it now uses the locked `branchLocked`
  helper (regression test `TestCreateBranchedSessionBranchesInPlace`).
- `get_last_assistant_text` omits `text` when there is no text. The reference
  runtime does the same (`JSON.stringify` drops the `undefined` value) even
  though its declared response type says `text: string | null`.
- `get_commands` lists prompt templates and then skills, each with `sourceInfo`.
  It never emits extension commands, which are out of scope.
- `set_auto_retry` writes the persisted retry setting
  (`settingsManager.setRetryEnabled`), the value the run loop reads, rather
  than the in-memory display toggle.

`cmd/rpcmode.go` unregisters and joins its signal waiter and returns 143/129
through the normal host cleanup path. The shared session queue-display lists
are mutex-protected across commands and agent events; listeners run outside
the lock. Queued user messages retain empty texts, image blocks and creation
timestamps. `AbortBash` now passes the tracked child context to execution,
matching upstream instead of cancelling an unused controller.

Tests: `coding/rpcmode_test.go` covers wire errors/correlation, LF/CRLF/Unicode
and large records, acceptance/events, queued templates/images/empty texts,
abort during auth and generation, admission limits, model/thinking/queue
selection, output failure/backpressure, and EOF accepted-save draining.
`coding/rpcmode_session_test.go` covers the session introspection, compaction,
retry and bash commands and the new field validation.
`coding/rpcmode_replace_test.go` covers new_session, switch_session, fork
(before/at), clone and the unavailable-factory errors.
`cmd/rpcmode_test.go` exercises CLI boot, noninteractive startup boundaries,
stdout cleanliness, signal unwinding, and an end-to-end `new_session` through
the real binary. `TestSessionAbortBashReachesExecution`
reproduces and guards the shell cancellation fix. See `docs/rpc.md` for usage
and the complete supported subset.

## D236

**The unreachable reference packages are removed from the tree.**

Reference: pi v1.3.0 `packages/{chord,client,protocol,server,telemetry,durable}`.
The Go port had a counterpart for each, but none is reachable from the built
CLI: `go list -deps github.com/dat267/pier` never pulls in `chord`,
`chord/{delta,services,facets}`, `client`, `protocol`, `server`,
`server/testing`, `telemetry` or `durable`. The CLI's only consumers are the
interactive, print and RPC modes over `ai`/`agent`/`coding`/`tui`/`mcp`; the
chord transport belongs to the durable remote-execution host, and upstream's
v1.3.0 typed-host rewrite already replaced those packages (`docs/PORTING.md`
records that). `telemetry` is ported but imported by nothing: upstream `ai`
only carries an optional `telemetryContext`, which the port does not set.

The packages and their tests are removed from `main` and preserved on the
`unwired-packages` branch, which points at the pre-removal commit. `go mod
tidy` drops the dependencies they alone used (`github.com/sgtdi/fswatcher`,
`modernc.org/sqlite`). Later D-rows and `docs/PORTING.md` text that describe
those packages are historical, not a description of the current tree.

Reintroducing any of them is a feature decision, not a port fix: wire it into a
runtime path first, then move it back from the branch.

## D237

**A fresh session persists its initial tool declaration, so a resumed transcript
resolves its loadout.**

Reference: pi v1.3.0 `core/agent-session.ts` `_restoreToolsFromTranscript`
(6f2f164ab) and `agent/agent.ts` `createMutableAgentState`. The agent seeds the
initial system message (prompt plus the active tools in `toolsAdded`) into
state, but the loop only emits that message when `declareToolChanges` finds a
change. On the first turn the seeded declaration already matches the executable
set, so nothing is emitted and the transcript holds only the later deltas (the
goal and MCP tools). `_restoreToolsFromTranscript` then replays the transcript's
declarations against an empty base, and because `_usesDefaultTools` restores
even when the caller passes tools, a default-tools session resumes with no
built-in tools, or with only the later additions.

Reproduced in the port before the fix: construct a session on a fresh
`SessionManager`, run one prompt, then construct a second session from the same
store. The second session's active tools were `[]` without the goal controller
and `[set_goal]` with it; the reference's transcript can omit the declaration
for the same reason.

The port appends the seeded initial declaration to the store once, when the
session has no prior entries and no caller-supplied initial messages, so the
transcript is self-contained and the restore resolves the recorded loadout. A
session already in the broken state (its transcript never declared the
built-ins) stays broken; resuming it with an explicit `--tools` list takes the
non-default-tools path, skips the restore, and recovers it.
## D238

The shell elapsed label ticks at 100ms instead of upstream's 1000ms.
`core/tools/render-utils.ts` arms `setInterval(() => context.invalidate(), 1000)`
while a shell call runs, but its label formats with `toFixed(1)` (tenths), so
the running clock is displayed with a resolution its redraw period cannot show:
`Elapsed 2.0s` jumps to `3.0s` a whole second at a time. The port's
`shellElapsedTick` is 100ms, so the display moves by .1s and the tenths
resolution is real. Non-shell running tools keep the upstream 1s tick, and the
final `Took` value is unaffected (it reads the recorded duration). This departs
from upstream's 1000 ms tick for the shell label only; the cost is ten small
differential paints a second while a shell command runs.
## D239

**Every pause transition emits a context-only goal message.**

Reference: the installed goal extension's `machine.ts` (the extension source,
D223). It wraps up a completed or blocked goal with a `<goal_complete>` or
`<goal_blocked>` message, but a pause only appends the durable pause entry
(reload and a budget stop also notify the human). `agent_end` abort,
`agent_before_settle` abort/error and `/goal pause` therefore left the model
with no record that goal work had stopped, so its next turn still saw the
objective as a live task.

The port emits a `<goal_paused>` custom message (`pi-goal-event`, display false,
triggerTurn false, details `kind: "paused"`) on every transition to paused:
the round-end abort/error/budget stops, `/goal pause`, and the reload pause. It
carries the stop code and reason, the objective as reference only, and the
`/goal resume` hint. It stays context-only and never starts a run; reload and
the budget stop keep their notify. `DisplayBody` also strips
`<goal_paused>` so the dormant paused event card renders correctly if an event
renderer is ever wired.
## D240

**Proxy resolution uses Go's transport, not upstream's node-http-proxy module.**

Reference: `ai/utils/node-http-proxy.ts` (`getProxyForUrl` /
`resolveHttpProxyUrlForTarget`, the `no_proxy` parser, the per-provider `env`
lookup) and its two callers. The port uses `http.DefaultClient`, whose transport
resolves `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` and their lowercase forms through
`net/http.ProxyFromEnvironment` (`coding/httpdispatcher.go` seeds the two
uppercase forms from the configured proxy setting). Two upstream behaviors are
therefore absent:

- A provider-scoped `options.env` proxy (`resolveHttpProxyUrlForTarget(url,
  options.env)`) is ignored; only the process environment is read.
- Upstream's own `no_proxy` parser (bracketed hosts, `*.`/leading-dot/`*`
  suffixes, a per-entry port) is replaced by Go's `httpproxy` semantics, which
  also accept CIDR entries and `*`.

The two upstream call sites do not change the outcome: the Codex one feeds the
WebSocket transport, which is out of scope (D28), and the Bedrock one only
forces HTTP/1 and installs proxy agents when the AWS SDK would otherwise use
HTTP/2, while the port's client is already HTTP/1.1 over the shared transport.
## D241

**Headless stdout has no takeover; diagnostics are written to stderr.**

Reference: `core/output-guard.ts` (`takeOverStdout`, `writeRawStdout`,
`waitForRawStdoutBackpressure`, `flushRawStdout`), used by `main.ts`,
`modes/print-mode.ts` and `modes/rpc-mode.ts`. After takeover, upstream
redirects every stray `process.stdout.write` to stderr and serializes protocol
output through a raw tail that retries `ENOBUFS`/`EAGAIN`. The port does not
patch `os.Stdout`: a Go `os.File` write is blocking, so `coding/printmode.go`'s
`printStdout` mutex plus that blocking write already provide output ordering and
backpressure, and the diagnostics upstream would redirect are written to stderr
directly (`cmd/main.go`, `coding.FormatCLIDiagnostic`). `restoreStdout` has no
counterpart.
