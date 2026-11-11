# Deliberate divergences from pi (D1–D445)

Each D-row records a deliberate difference from pi: a Go runtime constraint, a
corrected upstream defect, or a project choice. Rows are in numeric order and
usually have:

- a bold one-line summary;
- `Reference:` the upstream source, with the revision it was compared against;
- what pier does instead, and why;
- a `Test:` line naming the tests that reproduce the difference.

Code comments cite rows by number (`// D149: ...`), and every number a comment
cites has a row here: the comments that cited numbers from before this file
existed now name the row that owns the explanation, or state the difference
inline without a number. Rows are pruned when their divergence is resolved, and
the pruning commit removes the citations with them; the history is in `git log`.
A row that overlaps another refers to the one that owns the explanation. The
runtime design, stdio RPC contract and lock inventory are in
[ARCHITECTURE.md](ARCHITECTURE.md).

### D41

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

### D44/D47

**Go uses function fields instead of overridable methods; listeners are compared
by code pointer.**

### D49

**An astral-plane character is one `StdinBuffer` sequence, not two surrogates.**

Reference: pi v1.5.0 (f9a26365) `packages/tui/src/stdin-buffer.ts`
`extractCompleteSequences` takes one UTF-16 code unit per step
(`sequences.push(remaining[0])`), so a character outside the BMP reaches the
listener as two lone-surrogate strings. Go strings cannot hold an unpaired
surrogate, so the port iterates runes and emits the whole character once; the
same reason makes `runeCount` count runes where upstream tests
`sequence.length === 1`, which only feeds the pending Kitty printable-codepoint
dedupe.

A byte-level differential run of 1500 inputs (escape sequences, bracketed
pastes, terminal replies, CJK and Latin text, randomly split across chunks and
decoded through Node's `StringDecoder` the way `stdin.setEncoding("utf8")` does)
matched the pinned buffer on every input without an astral character; the astral
split is the only difference it found.

Tests: `TestProcessTerminalReassemblesUTF8Input` and
`TestStdinBufferPreservesUnicodeEscapeCharacters`.

### D50

**Go stdin reads cannot be paused like Node's stdin stream.**

`ProcessTerminal.Stop` gates input delivery without joining a blocking console
read. If that read completes while still stopped, it discards its bytes and
relinquishes reader ownership. A later `Start` reopens input with a fresh buffer
and handler, reusing an in-flight reader rather than competing for the same fd.
The reader belongs to the input file, not to one terminal: every
`ProcessTerminal` on that file shares it, and a read goes to whichever terminal
is started when it returns, as Node's single `process.stdin` stream delivers to
the attached listener. Without that, the startup trust prompt or the `--resume`
picker left its read in flight next to the app's new reader. That read then won
the app's first input, the terminal-color replies, and discarded it, so the
system theme never got the terminal's colors. The shared reader's mutex
serializes admission and exit only; reads, parsing and callbacks run outside
it. The raw buffer stays on the UI owner, while library-mode buffers
are atomically published to the one reader. Restarting inside an input callback
is supported. A blocked read may remain until input/EOF arrives; this is not
Node's synchronous `process.stdin.pause()` behavior.

Reference: pi v1.4.0 (`4a080252`), `packages/tui/src/terminal.ts` `start`/`stop`.
Scenario: a renderer-mode switch or suspend/resume stops and starts the same
terminal; new input must work, with no second concurrent stdin reader. CR-053
repairs the old permanent closed flag and restart reader admission.

Test: `TestStdinHandoffBetweenTerminalsDeliversToTheStartedOne`
(`tui/terminal_handoff_test.go`).

### D51

**`StdinBuffer` uses callbacks instead of `EventEmitter`.**

### D75

**Custom-theme watching polls file metadata instead of receiving filesystem events.**

Pinned pi v1.4.0 `packages/coding-agent/src/modes/interactive/theme/theme.ts`
watches the custom-theme directory and debounces a matching change for 100 ms.
`coding/interactive/theme.go` polls the active file every 250 ms and reloads only
when its modification time advances. An ordinary edit can therefore take an
extra poll interval to appear; a replacement that preserves or backdates the
mtime can be missed. The watcher keeps the last valid theme through temporary
missing or invalid file states. This standard-library implementation avoids a
filesystem-watcher dependency.

### D90

**Narrow runtime interfaces provide test seams across the wirings.**

### D105/D106

**The renderer `Proxy` becomes an explicit forwarder, and the clipboard uses
platform commands.**

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

### D133

**Tool renderers stay built-in; the edit preview runs asynchronously as upstream
does.**

Tool renderers still resolve to built-ins, not extension definitions (D41). The
synchronous-preview gap is closed: `computeEditsPreview` runs on a worker and
applies through the owner post, matching the asynchronous preview in v1.2.0
`core/tools/renderers/edit.ts`. `TestEditPreviewAppliesThroughOwnerPost` covers
that handoff.

### D140

**The hyper and commandcode provider extensions are compiled in.**

The user's hyper and commandcode provider extensions are compiled in as
`providers.ExtensionProviders()`, not loaded from `~/.pi/agent/extensions`
(extension loading is out of scope, D41).

### D141

**A cached, single-line footer replaces upstream's two-line footer.**

The user's footer-extension format is built in:
`3.5%/1M · <statuses> · model · cwd` on one dim line. Upstream walks every
session entry per frame; with raw JSON in Go that required `json.Unmarshal`
per message, measured at 445ms per frame on a 10k-entry session. Pier caches
the footer and invalidates it on session events. `FormatTokens` and
`FormatCwdForFooter` retain upstream behavior. `app.UI` uses the D105 forwarder
so `/tui` and exit-replay renderer swaps reach every holder.

### D143

**One UI owner loop replaces JavaScript's single-threaded event loop.**

The interactive loop selects over session events, submissions, work completions,
and `ctx.Done()`. Blocking work runs off-loop; streaming partials coalesce
latest-wins. Input/signals, selector callbacks, and lifecycle callbacks also
apply on that loop (ownership refactor stages 3–4). Upstream awaits prompts on
the JS event loop.
D144/D145 define input/render ownership; D196/D197 define queue ordering and
fairness. Session-selector loader posts recheck their captured load context
when the owner executes them, so cancellation or refresh also invalidates
already-queued progress/completions
(`TestSessionSelectorRejectsPostedResultsAfterCancel`). Status-expiry timers
also post to that owner; their cancellation check runs inside the posted
callback (`TestSessionSelectorStatusTimerAppliesOnOwner`). The full design is
in [Runtime structure](ARCHITECTURE.md#runtime-structure).

### D144

**Render requests coalesce on caller-driven ticks.**

The interactive loop paints once per drain instead of arming a throttled render
timer. Timer mode was later removed for standalone pickers and library
renderers too; callers now drive every renderer.

### D145

**Input, resize, and signals are producer messages.**

The stdin reader, SIGWINCH watcher, and signal handlers only enqueue. The owner
loop dispatches input, paints on resize, and handles shutdown; upstream delivers
these on one JS thread. See D143 for the loop; decoding is consumer-owned.
Startup screens close their input handoff before stopping the terminal, so an
already-admitted sender parked on a full queue can return. Before this, the sole
stdin reader stayed trapped in the retired callback and input stopped working
once the next screen reused it. `TestStartupLoopReleasesBlockedInputProducer`
fills the startup queue behind a controlled initial paint and confirms producer
release on settlement (v1.5.0 `f9a26365` cli/startup-ui.ts ownership handoff).

### D149

**The footer provider retains its cross-goroutine lock.**

`FooterDataProvider.mu` (`coding/footerdata.go`) protects cwd, git paths/branch,
statuses, and listeners shared by footer rendering and its 500 ms git-HEAD
watcher, which reads git state and calls `Refresh`. Retiring it would require
posting poll results to the UI loop and delivering listeners outside the lock,
a `coding/` change outside the UI refactor. The
[lock inventory](ARCHITECTURE.md#lock-inventory) is authoritative.

### D151

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

### D154

**Pier has its own dark/light palette.**

`coding/interactive/themes/dark.json` and `light.json` are the single palette
source for the CLI, library callers and HTML export. `ResolveThemeSetting`,
`ParseAutoThemeSetting`, and `GetDefaultTheme` still select by terminal
appearance. Pier deliberately does not track pi v1.4.0's okhsl palette redesign.
Compared with upstream `theme/{dark,light}.json`:

- Decorative fills (`selectedBg`, `searchMatchBg`) are unset (`\x1b[49m`) so
  transparent/blurred terminal backgrounds show through. Primary text uses
  terminal foreground. Lists mark selection with an accent-colored `→ `.
- Signal fills remain: `toolPendingBg`, `toolSuccessBg`, `toolErrorBg`,
  `userMessageBg`, and `customMessageBg`. They distinguish tool states, user
  blocks, and custom/compaction frames; failed and successful tools otherwise
  had identical output. HTML export uses the same signal tokens.
- Amber identifies the wordmark, borders, selection, and list bullets.
  User panels are warm (`#393630` dark, `#efdcc0` light), rather than the
  original upstream snapshot's `#343541` blue-gray or `#e8e8e8` gray. D377
  retuned these values for measured contrast without changing the hues.

Fills remain distinct after `rgbTo256`: low-spread dark colors use the gray
ramp, hues use the cube. `#393630` maps to 237 versus pending 235; a subtler dark
collapsed onto the same index. `piertheme_test.go` pins the palette.

The former embedded upstream snapshot and redundant `InstallPierTheme` override
are gone. No caller needs to install the palette; clearing the registry exposes
pier's built-ins, not a second color scheme. Both `loadThemeJSON` and `loadTheme`
still prefer caller-registered themes, and `Theme` retains their source documents
so memory-only themes can export. Capability setup must precede theme loading
because escapes are baked at construction. D193's no-color system panels read
these same JSON files; D219 covers adaptive system accents.

Fifteen interactive golden families are reproducibly generated using read-only
upstream rendering code with pier's JSON data, rather than pinning an unused
upstream palette. See `coding/interactive/testdata/README.md` for commands,
source revisions and the explicitly retained legacy non-color fixture contracts.
Reference: pi v1.4.0 (`4a080252`), `modes/interactive/theme/theme.ts` and
`theme/{dark,light}.json`. Scenario: resolve `dark` without a registry, or export
it from a library caller; colors must agree with the CLI's amber palette.

### D155

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

### D156

**Login waits off the UI loop.**

Inline `ShowLoginDialog`/`ShowApiKeyLoginDialog` called `LoginProvider`, which
waited on `ShowAuthPrompt` input that only the blocked UI loop could deliver.
OAuth device flows, API-key entry, and Bedrock method/profile selects hung.
Upstream's await yields; Go's wait does not.

`AuthWiring.startLogin` reports blocked status on-owner, runs the shared flow
on a goroutine, then posts status clearing, editor restoration, error reporting,
and `CompleteProviderAuthentication`.
`LoginDialogComponent` marshals `Show*` mutations with `post`; prompt channels
are created synchronously and the automatic sign-in URL launch stays on the
flow goroutine. The device-code prompt's Enter action (open the verification
page and copy the code, pi v1.5.0 login-dialog.ts) runs from input handling
like upstream, consuming the action once. A successfully started launcher is
waited for asynchronously so Unix child processes are reaped without blocking
login (`TestOpenBrowserReapsExitedLauncher`,
`TestDeviceCodeConfirmOpensAndCopies`).
Nil `post` applies inline for tests. This was the fifth deadlock, found by source
review rather than PTY driving. `TestApiKeyLoginRunsOffTheUILoop` dispatches,
renders, and routes input on one loop, failing with
"the login flow is running on the UI loop" rather than hanging.

The Radius login menu likewise uses the owner-loop animation scheduler instead
of `radius-login-selector.ts`'s 50ms interval: the selected row shimmers on idle
frames without a timer mutating attached UI state. Its auth-method selector
shows the upstream intro and posts responses back to the waiting login worker.
The post-login MCP offer reads/writes global config on an app-owned mandatory
queue, posts the dialog/result on the owner loop, and reloads only after a
consented write succeeds; shutdown drains accepted writes.
Tests: `TestRadiusLoginMenuShimmersOnlyWhileSelected`,
`TestRadiusLoginSelectIntro`, `TestRadiusMcpOfferMatchesUpstream`.

### D157

**Home-path completion splits raw input before cleaning.**

Upstream `getFileSuggestions` expands `~` before splitting directory and
filter. `path.join` (like `filepath.Join`) cleans `~/.` to the home directory,
which then splits into its parent and name, so Tab on `~/.` lists `/home` and
offers `~/dat/`. Pier splits the raw input first: a trailing `.` filters
dotfiles inside home, while `..` clears the filter and stays in the suggestion.
`TestFileCompletionUnderHomeFiltersInsideHome` guards the fix.

### D158

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

### D159

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

Cache-warming usage entries also seed the scan's previous request, as upstream
`scan` does: a `cache_warm` entry with non-zero prompt tokens becomes the
branch's latest request (its prompt, provider/model and timestamp,
`reportedCache: true`), so a following total miss is counted instead of being
suppressed by the older assistant turn's missing cache activity. Other usage
kinds leave the scan alone. Both the running state and the free-function scan
share that rule, and `TestCacheStatsCacheWarmParity` pins it against fixtures
generated from the pin (D273 covers the unparseable-timestamp idle gap).

### D160

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
  matching upstream `applyRuntimeSettings`. Accepted saves retain their
  submission's storage and diagnostic path; an old project's completion cannot
  clear the new project's dirty retry state.

Trust answers cache per project for the run, seeded at boot. Project
`.pi/themes` loads only when trusted; theme sources reinstall after the initial
trust decision because the `-r` picker needs themes before project resources
are readable. Trust-store read, parse and lock failures stop resolution before
any default or prompt. `ProjectTrustStore.Get`/`GetEntry` preserve their nullable
signatures but panic with the store error, matching upstream's throwing getters;
`ReadEntry` provides the error-returning form used by resolution and UI callers.
UI failures are reported without saving or claiming a successful trust change.

Extensions remain absent (D41). The model runtime remains process-wide:
`models.json` is agent-dir scoped on both sides, so switches do not change its
path; Pier rereads it on `/model`, not on switch. Settings, skills, templates,
context, system prompt, themes, and trust otherwise follow the session project.

### D161

**Tool scratch logs are swept to a 256 MB budget.**

Upstream leaves truncated output files to the OS. On the development host, one
174 MB command persisted for days; after three days, 230 files occupied 442 MB
of scratch RAM on tmpfs. Pier sweeps oldest-first when creating a log, down to
256 MB, covering `pi-bash-<id>.log`, `pi-output`, and `pi-powershell` prefixes.
The file being created and other output files still open by this process,
unrelated tool logs, and matching directory names are never touched. Closed
files remain eligible, and so do files another pier process holds open, since
the active-file registry is per process. The model still receives "[Output
truncated. Full output: <path>]". `TestSweepTempOutputFiles` checks exclusions
and oldest-first order.

The sweep stops at the tool-output prefixes on purpose. MCP output files
(`pi-mcp-<hex>.txt` and binary resource dumps, written by `SaveMcpToTempFile`
through the same `utils/output-files.ts` convention) are left to the OS exactly
as upstream leaves them: they are not swept, are not part of the budget, and a
sweep triggered by another tool skips them. Sweeping them would delete files
the model was told it could read back, so a growing MCP scratch directory on
tmpfs is accepted, as in the reference (CR-072).

### D164

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

### D165

**The listener-driven OSC 11 probe starts after raw mode.**

Upstream v1.2.0 `theme-controller.ts` queries combined OSC 10/11/4 colors and
repeats after appearance reports. Pier also sends nonblocking
`Renderer.RequestTerminalBackgroundColor` (`ESC ] 11 ; ? BEL`) for an independent
background listener. Sending before raw mode caused echoed queries and an SSH
freeze. `RunWiring.OnStarted` probes after `UI.Start`, and `RebindTUI` rearms it.
`CSI ? 2031` notification requests before `Start` only set a flag; `Start`
replays the toggle. Like pinned v1.5.0 (`f9a26365`) `TuiBase.start/stop`
in `packages/tui/src/tui.ts`, restarting the same renderer clears query-stop
state and re-enables the desired notifications. Stop disables their wire mode
without forgetting that setting, so suspend/external-editor resumes keep
appearance tracking live.

`HandleTerminalInput` delivers OSC 11 to both the combined query and the
listener. A background-only late reply can therefore update an auto theme
without the combined query's final DA1 sentinel. No reply leaves the
environment/system fallback. D218 covers off-loop combined-query waits.

Tests: `TestBackgroundListenerSeesLateReplyDuringColorQuery`,
`TestSystemColorQueryDoesNotHideStartupBackgroundReply`,
`TestBackgroundProbeRoundTripThroughRenderer`,
`TestRendererColorQueryWorksAfterRestart`, and
`TestRendererRestoresColorSchemeNotificationsAfterRestart`.

### D166

**A stable theme handle replaces the upstream Proxy.**

Upstream's `theme` Proxy reads `globalThis[THEME_KEY]` per access. Pier's
`ActiveTheme()` returns a stable `*Theme`: `Fg`, `Bg`, `GetFgAnsi`,
`GetBgAnsi`, and `ColorMode` forward through `Theme.concrete` to `CurrentTheme`.
`GetMarkdownTheme` factories capture that handle, and `Renderer.Invalidate`
clears caches after a switch. This replaces components capturing stale
concrete themes and the attempted `App.rebuildForTheme` workaround.

Construction-time `theme.fg` strings, such as header Text, remain baked on both
sides; `onChanged` still follows `updateEditorBorderColor`.

Tests: `TestThemeHandleResolvesTheCurrentTheme`,
`TestMarkdownThemeRecolorsAfterASwitch`.

### D167

**ANSI slicing preserves codes at the start boundary.**

Upstream `utils.ts.sliceWithWidth` buffers pre-range codes but immediately emits
in-range codes. If a slice starts on an inline-code reset, the reset precedes
the carried color it should cancel, turning following text yellow after a
selection/double-click. Pier flushes the carried prefix before an in-range code.
Existing goldens lack this case and remain unchanged;
`tui/sliceansiorder_test.go` checks the ordering.

### D168

**Escape cancels without waiting for idle.**

Upstream's awaited `session.abort()` yields; Go's `AgentSession.Abort` blocks in
`waitForSessionIdle`. Calling it on Escape froze later input until the turn
unwound. A live trace measured `slow UI phase "raw-input" 188ms` in
`HandleEscape`, `RestoreQueuedMessagesToEditor`, and `Abort`, with neighboring
keystrokes at `read=8.8ms write=91ms`.

Escape calls `AbortAsync`, which cancels the retry, compaction, agent run and
a running `!` command (it belongs to the turn, D246) and returns immediately.
`Abort`, like upstream's `abort()`, stops the turn but leaves a user bash
command running (`TestAbortLeavesAUserBashCommandRunning`). Queue restoration
already precedes cancellation. `Abort` retains its wait where
settlement is required: session replacement (`sessionswitch.go`, before
disposal) and tree navigation (`interactivemode_selectors.go`, before reading
the tree). `TestEscapeHandlerDoesNotWaitForIdle` covers the loop-safe path.

### D169

**Mouse reports reassemble after escape deadlines.**

The buffer flushes incomplete sequences after 50 ms and lone ESC after 10 ms.
Reports split across a deadline can dispatch Escape, then type a rune-split tail
such as `[<65;59;42M` (wheel-down at column 59, row 42). Windows `ReadConsole`
and delayed Android reads exposed this; upstream's ESC-required regex has the
same failure.

Pier remembers a flushed potential head (ESC, `\x1b[`, or truncated
`\x1b[<...`) and rejoins valid continuations across multiple reads, including
bursts with subsequent reports or keys. Unrelated input clears the head, and a
rejoined head is dropped again once the buffer can no longer become a report, so
typing `[200~` or `[A` after Escape stays text; a lone `[` that times out is
released as text too. AltScreen
also accepts ESC-less reports and consumes truncated fragments. An Android
`books` replay reproduced `64;11;24M` leaking into the old editor; the fixed
binary consumed the burst and stayed in alternate screen. An Escape already
dispatched cannot be undone.

Tests: `TestSplitMouseReportAcrossTheDeadline`,
`TestExpiredMouseHeadRejoinsWheelBurst`,
`TestExpiredMouseTailReassemblesAcrossReads`,
`TestExpiredMouseReportRejoinsAtEveryBoundary`,
`TestExpiredEscapeHeadReleasesNonMouseTail`,
`TestOrphanedMouseReportIsStillDispatched`, and
`TestTruncatedMouseReportIsConsumed`.

### D171

**OS socket failures are retryable.**

`packages/ai/src/utils/retry.ts` matches fetch/HTTP/websocket texts such as
`socket hang up`, `connection refused`, `connection lost`, `reset before headers`,
and `timed out`. Pier also retries `software caused connection abort`
(ECONNABORTED, Termux/Windows), `connection reset by peer` (ECONNRESET),
`broken pipe` (EPIPE), `unexpected EOF`, and Go's HTTP/2 peer-reset wording
(`stream error: stream ID ...; PROTOCOL_ERROR; received from peer`, plus
`http2: server sent GOAWAY`). Quota/billing checks still run first, so a
usage-limit error mentioning a reset is not retried.

### D174

**Updates use GitHub releases and verified release assets.**

Upstream `<app> update` runs its package manager (absent here, D41). Pier reads
GitHub's `/releases/latest` redirect, then `pier update` installs the release.yml
asset `pier-<goos>-<goarch>` after checking its sibling `.sha256`. Windows moves
the running image aside first (upstream's self-update quarantine); downloaded
assets also need their executable bit set.

The card names `pier update` and this module's release page, not `go install`
or pi.dev's changelog. v1.5.0's global `selfUpdateCommand` setting and
`PI_SELF_UPDATE_COMMAND` override, which run a user-supplied package-manager
command instead, are not read: pier's update path is always the verified release
download above. No module-proxy publication is required. Unorderable
source versions (the commit hash from `just install` or unstamped `0.0.0`)
produce no card: upstream `isNewerPackageVersion` falls back to inequality for
non-semver, which would mark these builds outdated forever.

The GitHub API is deliberately avoided: unauthenticated clients share 60
requests/hour/IP, then receive 403. A mobile update observed
`x-ratelimit-remaining: 0` versus `4991/5000` authenticated. The redirect is not
rate-limited, and known asset names need no API listing. Android DNS fallback
is a separate issue (D175).

### D175

**Android gets a pure-Go DNS fallback.**

Releases use `CGO_ENABLED=0`. Go's resolver reads `/etc/resolv.conf`, absent on
Android (`/etc` points to read-only `/system/etc`), then tries `127.0.0.1:53` and
`[::1]:53`, failing with "connection refused". Termux's patched Go works because
it uses `$PREFIX/etc/resolv.conf`.

When stock config is missing, Android boot reads that Termux file or
`PIER_RESOLV_CONF` and installs `net.Resolver` rotating among its nameservers.
Node uses platform libc rather than resolv.conf, so upstream needs no equivalent.

### D176

**The system prompt omits pi's documentation index.**

Upstream `buildSystemPromptSections` emits a `docs` section pointing at pi's
README/docs/examples and routes questions to extensions.md, themes.md,
skills.md, prompt-templates.md, tui.md, keybindings.md, sdk.md,
custom-provider.md, models.md, packages.md, and environment-variables.md.
Pier ships port/architecture notes, not that tree or `examples/`, so
`coding/systemprompt.go` omits the section rather than naming nonexistent files.
The rest, including the preamble naming pi as the reference harness, is unchanged.

### D178

**Shell cancellation cannot wait forever on surviving descendants' pipes.**

Upstream kills by process-tree pid (`taskkill /F /T` on Windows), then waits for
stdio to close. Background servers, `start /b`, or `setsid` descendants may keep
inherited output pipes open beyond the killed tree. Both shell entry points
close output readers on cancellation and bound post-exit draining with
`waitForPipeDrain`. They wait for the direct child to exit before starting the
100 ms idle grace, matching `packages/coding-agent/src/utils/child-process.ts`
and preserving descendant output written shortly after shell exit. A kill gate
serializes timeout and abort kills with that wait (`coding/bashexecutor.go`):
once the child is reaped, a late timer cannot signal the group (whose pid can be
reused) or turn a completed command into a timeout.

Windows additionally uses a job object: `TerminateJobObject` reaches children
whose parent exited, unlike `taskkill /T`; assignment failure falls back to
taskkill. Normal completion closes the job without termination so intentional
background processes survive. The reproducer fell from 9s after cancellation
to under 0.1s.

### D180

**Go render caches need no V8 rope flattening.**

Upstream `54c19a252` adds `flattenLines` to Markdown/Text/Box caches: V8 string
concatenation retains rope trees, which held about a fifth of a long assistant
message's heap. Go strings and `strings.Builder` output are already flat.
The same commit's weak token cache is ported: `tui/markdown.go` uses
`weak.Pointer`; collected tokens reparse on the next render.

### D181

**Resource text/blob arms track key presence.**

Upstream `packages/mcp/src/protocol/content.ts` checks `"text" in resource`,
including empty text. `ResourceContents.TextArm` records that choice on decode
and marshals the text member whenever active.

### D182

**MCP stdio cleanup hooks signals, not `os.Exit`.**

Upstream `packages/mcp/src/transports/stdio.ts` kills process groups in
`process.once("exit")`. Go libraries cannot hook `os.Exit`; `mcp.installExitHook`
watches SIGTERM/SIGINT/SIGHUP, kills live groups, restores the default action,
and re-raises.

### D183

**HTTP retry classification uses Go errors.**

Upstream `packages/mcp/src/transports/streamable-http.ts` retries fetch
`TypeError` network failures. Go's net/http uses typed errors, so the transport
retries non-`McpHttpError` failures.

### D185

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
  `codemode` stays the default exposure so an `mcp.json` means the same as in
  pi, which makes a server added without `"exposure": "direct"` contribute no
  tools. Upstream's `ensureDiscoveryActive` warns when such tools have no tool
  to reach them; pier warns under the same condition at startup, `/reload` and
  session replacement, naming the servers and the `"exposure": "direct"` fix
  instead of upstream's generic text, since no setting activates the missing
  tools. Tests: `TestMcpUnreachableToolsWarningFollowsConfiguredExposures`,
  `TestStartupMCPMessagesAppendTheUnreachableWarning` and
  `TestReloadMCPWarnsAboutUnreachableTools`.
- OAuth uses a per-process credential store, not `auth.json`; `/mcp` sign-in
  persistence is unported. Accordingly, `oauth.clientRegistration: "cimd"` is
  not carried through config/runtime: upstream's Client ID Metadata Document
  selection is wired by `extensions/mcp/oauth.ts` for the unported sign-in flow.
  `read_mcp_resource` is absent, so resource links do not advertise it.
- Radius OAuth login offers the upstream global provider-auth MCP setup.
  It preserves unrelated config and indentation, replaces MCP OAuth with
  `auth.provider` only after consent, and reloads the MCP manager afterward.
- Definitions omit upstream renderers, output schema, and annotations;
  interactive MCP calls use generic tool rendering. `isError` stays a flagged
  `agent.AgentToolResult`, preserving converted content and details through the
  agent loop; it is not converted to a Go error.

Reference: `packages/coding-agent/src/extensions/mcp` and `core/mcp-servers.ts`.

### D190

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
pending-frame clocks. References:
`packages/coding-agent/src/modes/interactive/components/tool-execution.ts` and
`packages/coding-agent/src/core/tools/renderers/bash.ts`.

### D193

**System fallback keeps Pier's signal fills.**

Upstream `system-theme.ts.indexedColors` leaves all panels transparent when no
OSC 10/11/4 colors arrive. Pier reads D154's built-in JSON signal fills for the
detected appearance to `toolPendingBg`, `toolSuccessBg`, `toolErrorBg`,
`userMessageBg`, and `customMessageBg`. `selectedBg`/`searchMatchBg` remain
transparent (`transparentBackgroundColors`). The overlay runs in
`buildSystemTheme`, not `GenerateSystemThemeColors`, so generator goldens
remain upstream-parity.

Reference: `packages/coding-agent/src/modes/interactive/theme/system-theme.ts`.

### D195

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
- `WaitForDirectTools` polls every 20ms instead of awaiting ready promises, so
  it reads a per-attempt `settled` flag rather than the live connection state:
  upstream's `ready` promise settles when an attempt connects or fails and is
  only replaced by a reconnect, so a direct server that dropped after it
  connected must not make the first prompt wait the budget again
  (`TestWaitForDirectToolsReturnsAfterAConnectedServerDrops`,
  `TestWaitForDirectToolsStillWaitsForAnUnsettledServer`).

### D196

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

Session subscriptions also capture a binding generation. The owner rejects
other bindings even if their listener snapshots were admitted before
unsubscribe and publish after a replacement's queue clear. Those callbacks
remain safe to finish without changing the listener registry contract.
`TestSessionReplacementRejectsAnAlreadyAdmittedOldListener` reproduces a late
custom message reaching the replacement transcript before this filtering.

Each subscription also owns an enqueue stop signal. Replacement retires it
before awaiting the outgoing run, releasing a producer parked on the full
lossless channel without dropping current-binding events or changing session
persistence. `TestSessionReplacementSettlesAProducerParkedOnTheFullEventQueue`
reproduces the circular wait when the owner waited for Abort before retiring
its producer.

### D197

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

### D198

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

### D199

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
remain. Clipboard subprocess cancellation closes the parent's reader/writer
pipes as well as killing the direct child, so daemonized descendants cannot
strand a pipe read or input-copy goroutine after timeout. The descendants are
not killed merely for retaining clipboard ownership. This original policy did
not cover process-wide copy queues/detached work; D200 extends ownership and
bounds permanent terminal exit. Session/provider serialization is unchanged;
accepted saves still have no timeout.

`TestGroupShutdownCancelsOptionalWorkButDrainsSaves` uses `testing/synctest` to
hold a save and cancellation-ignoring worker: cancellation precedes save release,
saves remain ordered, shutdown need not release the optional worker, and
queued/late tasks never run. App tests cover StopMode/Close, late reads/watchers,
posted switch/preview callbacks, paste, and canceled transcript publication.

### D200

**Optional producers are mode-owned, and terminal exit shares one 2 s grace.**

D199's cancellation rules extend to clipboard copies and explicit detached
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

### D201

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

### D202

**Optional terminal output waits before FIFO admission.**

At 256 KiB committed output, title/active-progress updates keep one newest
uncommitted hint each until recovery to 128 KiB, and the interactive OSC 52
fallback waits for capacity instead of writing raw stdout. Permanent shutdown
discards only uncommitted metadata, never committed frames. The FIFO and its
waiters are `ProcessTerminal.writesMu` in the
[lock inventory](ARCHITECTURE.md#lock-inventory).

#### Direct terminal output inventory

D198's paint watermarks do not cover direct terminal writes. The direct-write
inventory distinguishes producers rather than imposing a lossy cap on the
terminal's generic FIFO:

- Differential main/fullscreen paints, cursor/screen commands, alternate-screen
  transitions and scrollback replay retain lossless committed FIFO admission.
  Renderer paints remain scheduled through D198; a large frame can overshoot.
- Bracketed paste, keyboard negotiation, color queries/subscriptions, cursor
  restoration, progress clear and graceful-exit hints are essential protocol or
  lifecycle output. They do not wait for optional admission. Queries with reply
  deadlines cannot be delayed behind optional output without changing semantics.
- Window-title updates and active-progress refreshes/keepalives are redundant
  metadata. At 256 KiB of committed queued plus in-flight output, they defer
  before enqueueing. There is one newest-pending slot per kind, not a mutation
  of any committed FIFO entry. The writer admits deferred hints after recovery
  to 128 KiB or less, without requiring another input event. Progress clear
  invalidates pending active hints, and stopped ticker generations cannot
  reintroduce an active indicator. Permanent shutdown discards uncommitted hints.
- Interactive clipboard OSC 52 fallback previously wrote raw stdout from its
  worker, bypassing terminal ordering, backlog measurement and exit policy.
  `CopyTextToClipboardWithOSC52` injects the fallback sink, while retaining the
  platform helper order and existing 100,000-byte encoded-payload limit. The
  app captures its stable terminal sink at construction. ProcessTerminal uses
  `WriteOptionalContext`; custom terminals retain their own Write behavior.

`WriteOptionalContext` is an off-loop API. It waits before accepting a complete
packet until committed bytes plus packet length fit within 256 KiB and no
renderer frame is open. Cancellation, output finalization or permanent shutdown
release waiting producers without dropping accepted output. Packets larger than
256 KiB are rejected rather than split through escape sequences or frames.
Accepted packets retain FIFO ordering. Admission is not a delivery confirmation;
D200 still governs display output remaining at exit. D201 bounds app clipboard
jobs (including waiting producers) to four; this API does not bound arbitrary
SDK callers that spawn their own waiting goroutines.

A title stays one atomic value: an oversized title on an idle terminal is
allowed to overshoot instead of truncating or deadlocking. Deferred metadata
memory is therefore bounded by one title value plus one fixed progress sequence,
not by an absolute title-byte limit. Generic `Write`, standalone clipboard APIs
and standalone fullscreen selection defaults retain compatibility; this is not a
global hard byte cap or an admission rule for arbitrary SDK writes. No saved
session/settings JSON or provider payload changes.

Blocked-writer regression: 64 repeated same-title/progress updates plus 10
virtual keepalive ticks formerly added 1,296 committed bytes behind a 262,144-byte
in-flight prefix. Now they add zero committed bytes while stalled, retain only
newest metadata, and recover without replaying active progress after clear.
Tests also observe waiting/canceled optional admission, responsive essential
writes, atomic frame/FIFO recovery, oversized-title progress, injected clipboard
fallback errors, and app-owned OSC 52 routing. Existing D198 owner-loop
blocked-console input tests and the SSA UI-blocking gate remain active.

### D203

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

### D204

**Large built-in assistant Markdown prepares private snapshots.**

A 16,000-pair heading/paragraph fixture (about 1 MB) took 296.63 ms and
114.11 MB cold (Linux/amd64, three iterations), dominated by Markdown/token keys
and GC. D203 cannot yield within one large child.

At 64 KiB source or more, built-in assistant text/thinking submits a private
Markdown snapshot to the app's optional backend and displays prior complete
lines or `Preparing message...`. It admits four jobs total (one running, three
pending); rejection retains no queued work and requests a later paint. The
app posts ancestor invalidation to the next owner turn: doing it synchronously
during Render let that frame cache the rejected placeholder as unchanged, and
when every accepted job was then superseded, the newest content stayed pending
forever. `TestRejectedMarkdownPreparationRetriesAfterSupersededWork` (v1.5.0
`f9a26365` interactive-mode.ts) drives that full-queue/stale-completion ordering
and confirms retry after capacity returns. D205 shares this budget with tool
results.

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

### D205

**Large built-in tool results share the snapshot backend.**

Upstream `packages/coding-agent/src/core/tools/renderers/{read,bash}.ts` formats
expanded read and both shell modes synchronously. Live/replayed expanded read,
bash, and powershell opt into D204's shared four-job budget at 64 KiB text.
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

### D206

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

### D207

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

### D208

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

### D209

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

### D210

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

The UI selects on wakeups and reports chat errors. v1.5.0's `_appendEntry`
rolls the in-memory entry, leaf and id map back when its synchronous persist
throws; pier cannot apply that after an off-loop failure because later entries
may already reference the failed one. It keeps the accepted prefix in memory
and the next save rewrites that prefix (the failed append marked the state
uninitialized), so the file never gains a child with a missing parent; a failure
with no later append leaves memory ahead until the reported error is handled.

Teardown drains saves, then routes unresolved diagnostics through D200's output
grace, never direct stdout. Print mode flushes on ordinary and signal returns;
persistence failures report on stderr and exit 1 on ordinary returns, while
signal returns keep 129/143, so JSON stdout stays clean. Formats are unchanged.

Tests cover sync/queued initial and append failures, prefix/truncation
recovery, per-task duplicate prevention, coalesced wakeups, idle reporting,
teardown, and print exit codes.

### D211

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

### D218

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

The retained blocking OSC 11 background and color-scheme helpers also use the
existing query handoff mutex for pending requests and timeout/reply settlement.
Scheme waiters are separate from owner-only listener registries; off-loop cleanup
never mutates attached callbacks. Listeners run after unlocking and before the
scheme waiters receive their results. No UI-state lock is added.

Because the first paint legitimately precedes the reply, the system theme keeps
upstream's pending state: the controller marks the terminal colors pending when
it takes over, and `buildSystemTheme` passes `saturation: 0` plus
`appearanceHint: getTerminalTheme()` until `SetSystemTerminalColors` records the
first report. The frames before the reply are terminal-default (grayscale)
rather than ANSI palette colors with a guessed appearance, so the startup look
does not depend on whether the reply beats the paint. The D193 signal panel fills
follow the detected appearance, so they too wait for the first report: choosing
them from the environment fallback made launch show deep-green tool boxes on one
run and the reported bright variant on the next.

Reference: pinned `f9a26365`
`packages/coding-agent/src/modes/interactive/theme/theme-controller.ts` (the
constructor's `markTerminalColorsPending()`) and `theme.ts` `createSystemTheme`.

Tests: `TestProbeSystemColorsDoesNotBlockTheCaller`
(`coding/interactive/interactive_test.go`) and
`TestRendererBeginTerminalColorsWaitsOffLoop` (`tui/terminalcolors_test.go`) for
the off-loop wait; `TestStaleSystemColorSnapshotDoesNotClobberLateBackground`
for the merge; `TestBackgroundQueryTimeoutReplyRace`,
`TestColorSchemeQueryTimeoutReplyRace` and
`TestColorSchemeQueryWaitsForPriorListeners` for the blocking helpers; and
`TestSystemThemeStartsGrayscaleUntilColorsAreReported` for the pending state.

### D219

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

### D220

**Interactive startup skips the create-time catalog refresh; the background
pass does it.**

Upstream `packages/coding-agent/src/core/sdk.ts` refreshes the model catalogs at
create (`refreshOnCreate` defaults to true). When the persisted
`models-store.json` is older than four hours (`RemoteCatalogRefreshIntervalMS`),
that makes the first launch after a break wait on one HTTP request per provider,
bounded by the 15 s `ModelRefreshTimeoutMS`. Pier keeps the create-time refresh
for print mode and `--list-models`, which resolve a model before any background
pass, but the interactive mode runs create with `AllowModelNetwork: false`: it
restores the persisted overlay and availability snapshot from
`models-store.json` without a request, and the `RefreshModelCatalogs` pass that
`interactive.Run` already schedules after startup performs the network refresh.
Startup for an interactive session after the freshness window drops from the
request time to the store-load time (~260-310 ms to ~25 ms on the development
host), and the catalog still updates a moment later through the same runtime.

Tests: `TestCatalogRefreshAllowedAtCreate` (`cmd/catalogrefresh_test.go`) and
`TestCreateModelRuntimeOfflineCreateLoadsTheStoreWithoutNetwork`
(`coding/remotecatalog_test.go`).

### D221

**A malformed finished tool call's `argumentsError` carries the Go decoder
message, not V8's.**

Upstream v1.4.0 (`parseToolCallArguments`, `ai/utils/json-parse.ts`) sets
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

### D223

**The user goal extension is re-ported as a builtin internal package.**

`~/.pi/agent/extensions/goal` is an installed user extension, not stock pi, and
the port has no extension host (D41). An earlier builtin was removed, then
re-ported as tree code at the user's request: `internal/goal` holds the state,
machine, command parser and renderers, `coding.GoalController` is the host
glue, and `coding/interactive` renders the transcript and tool cards.

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
delivered by `SetUIHooks`. The interactive mode installs that posted UI hook
and the idle-round launcher on every session bind, including `/new` and switch,
so replacement controllers do not silently buffer notices. The owner checks
the originating controller before applying a posted notice, rejecting outgoing
notifications even if they were already enqueued. `/reload` calls
`Start("reload")` without re-reading the branch by hand.

Idle round delivery uses the host work scheduler (D143). A queued closure
checks that its goal id, revision and pending turn still identify an armed,
active reservation before starting a run; pausing, clearing, replacing or
pausing/resuming while it waits invalidates it. The session sink also checks
its existing disposed flag before delivering deferred goal work, since disposal
need not change the durable goal reservation
(`TestDisposedSessionDoesNotStartAReservedGoalRound`). The round's run start
re-checks it in the critical section that installs the run, so a round racing
`Dispose` cannot start a run the abort misses
(`TestReservedGoalRoundRacingDisposeLeavesNoUnabortableRun`). A rejected resume leaves
activation unchanged, including for terminal completed goals
(`TestGoalResumeLeavesCompletedGoalDisarmed`). Already-started work retains
its existing cancellation policy. Tool exposure is synchronized before a
trigger-turn message can start an idle model request inline, so the first round
has the active goal's tools rather than waiting until that run returns.
Machine transitions and their durable append effects share the controller lock
(see `coding.GoalController.mu` in the
[lock inventory](ARCHITECTURE.md#lock-inventory)), so concurrent goal tools
cannot persist clear/create entries out of order.

The two tools (`get_goal`/`set_goal`, D234) are registered in the session tool
registry and join the default active set. Unlike the extension, an explicit
`--tools` allowlist still filters them out, because `SetActiveToolsByName` owns
pier's allowlist semantics. The `registerMessageRenderer` for hidden
`pi-goal-event` messages is not ported (those messages are `display:false`, so
the transcript never draws them); the `pi-goal` and `pi-goal-turn` entry
renderers cover the visible cards. Durable entries are published as
`entry_appended` events after the controller unlocks (upstream emits on append),
and entry renderers receive the entry's payload, so a live transcript draws the
goal cards without a reload. Publisher admission is released with a defer, so a
session listener that panics through the tool dispatcher's recovery does not
disable later goal-card publication
(`TestGoalPublisherResumesAfterRecoveredListenerPanic`).

The tool cards are multi-line like the built-in ones: `set_goal` shows the
objective as a wrapped body with a compact created/replaced outcome line, and
`get_goal` shows state, objective, budget usage and any stop reason, with a
click/expand preview for long text (`internal/goal/render.go`). Goal cards leave
one blank line between nonempty sections: title, status, objective, optional
budget/blocker details, and tool outcome (including errors). This applies to
creation, completion, blocked, clear and empty-goal results as well as durable
lifecycle cards. Label-only events and admitted-round entries remain compact.
Handled mouse events mark the custom-entry wrapper dirty, so renderer-local
expand/collapse changes invalidate its revision and repaint in the cached
transcript.

A collapsed goal card is capped by lines, not characters: the same five-line
preview the shell card uses, followed by `... (N more lines, click or expand for
full text)`. One long line is shown whole, and a collapsed preview keeps its
first lines as written instead of flattening them.

A successful objective `set_goal` action shows one tool card, rather than also
showing its durable create and replacement-clear cards. This user-requested
presentation difference matches the result's goal ID to the create card and,
for a replacement, its immediately preceding clear record. The owner retains
these component references only while objective tools are pending, then removes
the redundant cards after success. Replay applies the same rule. Command-driven
changes, errors and incomplete tool calls keep their lifecycle cards. All
clear/create records and append events remain intact for persistence and replay.

Tests: `internal/goal`'s state/machine/command/render suites,
`coding/goalcontroller_test.go` (effect application, replay, tool exposure,
continuation/error/abort outcomes),
`TestSessionReplacementKeepsGoalCommandNoticesVisible` (initial/new/switch),
`TestSessionReplacementRejectsRetiredGoalNotices` (posted/late callbacks), and
`coding/interactive/goalrender_test.go` plus `toolrenderer_goal_test.go`.

### D224

**A pending goal round's hidden prompt is restored after compaction.**

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

### D225

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

The lazy loader's `scanSessionLine` is stricter: its
message member must be strictly valid JSON, or the line falls back to the
typed decoder and is dropped like upstream's `JSON.parse`. It previously used
a bracket counter that accepted invalid escapes, raw control characters,
leading zeros and trailing commas and could turn a corrupt line into a message
shell (`TestLoadEntriesBufferedRejectsInvalidMessageJSON`,
`FuzzSessionScanAcceptsOnlyValidJSON`). Strict scanning costs about 2x the
metadata scan (3 MiB: 2.4 ms to 5.7 ms; still about 600 MB/s). The fast string
reader also rejects invalid UTF-8 so the caller decodes it like `JSON.parse`
(replacement characters); previously a malformed role or id kept its raw bytes
and the listing disagreed with the typed decode
(`TestSessionScanRejectsInvalidUTF8Strings`,
`FuzzScanListingMessageMatchesDecode`).

Measured on a 100 MiB session (30,314 messages): `buildSessionInfo` 1.7s to
0.3s, and a 105 MiB session directory lists in 0.3s instead of 2.0s.

### D226

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

`ConvertToLlm` holds converted custom messages while an assistant message's
tool calls are unanswered and emits them after the results, mirroring the
transform's existing transparency for system messages. Genuine user messages
are untouched (they still interrupt tool flow through the transform). The note
stays in context, only after the result. This is Go-port-only: upstream has no
goal extension to interleave.

Any custom role is held, not just the goal event; the ordering rule is about
the tool pair, and the roles are out-of-band notes rather than turns.

### D228

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

### D229

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

### D230

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

### D231

**Syntax highlighting is built in.**

Syntax highlighting was removed earlier because Chroma v3 ran at ~0.5 MB/s on
the UI loop and made code fences a render stall. The `highlight` package is now a
from-scratch port of highlight.js 10.7.3 (the v1.4.0 reference engine), all 191
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
`highlight` package already returns the same scope tree, so
`renderHighlightedNodes` walks the tree directly and skips the HTML round-trip
(equivalent, since the HTML walk was exactly escape-then-unescape). On the 10.9
KB `internal/highlight/testdata/sample_big.go`, the wired path measures 1.86 ms
(5.86 MB/s) against the reference's JS wired path at 3.20 ms (3.42 MB/s) in the
same session, so the stall does not return. The port uses no third-party
highlighter: `github.com/alecthomas/chroma/v3` and its indirect
`dlclark/regexp2` dependency stay removed, keeping the binary-size and startup
gains of their removal.

Auto-detection follows `core.js highlightAuto`: relevance first, then the base
language of a `supersetOf` pair (Arduino prefers C++), then the reference's
registration order, which `lang_order.go` holds as a generated list because Go
registration order is alphabetical and upstream registers `xml` and `yaml`
early. Manual begins (used where RE2 cannot express a rule) take part in the
same rule ordering: at one position the earlier rule wins, as in the reference's
alternation.

`RegexpMode` collapses the reference's zero-width outer regexp rule and its
inner `regexp` rule into one mode with `relevance: 2`: the port's lookahead guard
replaces the outer rule (RE2 has no lookaround), so both relevance
contributions must land on the guard-started mode. Mode relevance generally
follows `core.js` exactly — an undeclared relevance is 1, a declared 0 zeroes
the mode, and variant expansion inherits the wrapper's relevance — pinned by
`internal/highlight/testdata/relevance` (generated by running the pinned engine).

One divergence remains from the reference loader: pi registers 21 languages
eagerly and imports the rest lazily, so an uncommon language can render plain
until `loadAllHighlightLanguages` resolves. Pier registers all 191 at startup,
so an uncommon language is always colored. The token output is identical once
loaded.

### D232

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

### D233

**`/goal set` overwrites a live goal without confirmation.**

The reference `/goal set` over an unfinished goal asks the human to confirm
("Replace goal?") before it tombstones the old goal and creates the new one; a
declined confirmation leaves the goal untouched. Pier overwrites directly: the
same `goal_replace` path runs (tombstone, create, queue the first round) with no
prompt, and the command replies "Goal replaced." The goal state machine and its
replay are unchanged.

The confirmation hook that existed only for this prompt is removed;
`GoalController.SetUIHooks` now installs the notify callback alone.

### D234

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

`get_goal` is the only other tool. `DesiredToolNames` keeps both active for the
whole session, so a goal transition never changes the request prefix (D418).

All three `set_goal` properties explicitly accept null, while remaining
optional. Reproducer: a caller exposes every property as required without
strict-schema conversion, forcing a non-null `status` alongside a new
`objective`; execution rejects both and retries cannot express the intended
call. Nullable properties let that caller send
`{"objective":"fix it","status":null,"blocked_reason":null}`. The reverse ends a
goal with a null objective. Exactly one nonempty objective or status is still
required; two active values and two null values remain errors. The regression
test exercises both strict and non-strict Responses declarations with all
properties required, normal agent validation, and execution. The upstream
counterpart is the extension's separate create/update tools, not a pi built-in
tool.

### D235

**Stdio RPC is a bounded, extension-free subset.**

Reference: pi v1.4.0 `packages/coding-agent/src/modes/rpc/{rpc-mode,rpc-types,jsonl}.ts`.
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
reproduces and guards the shell cancellation fix.
`TestRPCRejectsFieldTypesItCannotHonor` pins each rejected field type with its
message, the echoed command and an unchanged session;
`TestRPCValidToggleChangesTheReportedState` is its control. See
[Stdio RPC](ARCHITECTURE.md#stdio-rpc) for usage and the complete supported
subset.

### D236

**The unreachable reference packages are removed from the tree.**

Reference: pi v1.4.0 `packages/{chord,durable,telemetry}`; upstream's typed
host had already replaced its `client`, `protocol` and `server` packages before
v1.3.0. The port had counterparts for all of them, but none is reachable from
the built CLI: `go list -deps github.com/dat267/pier` never pulls in `chord`,
`chord/{delta,services,facets}`, `client`, `protocol`, `server`,
`server/testing`, `telemetry` or `durable`. The CLI runs the interactive, print
and RPC modes over `ai`/`agent`/`coding`/`tui`/`mcp`; the chord transport
belongs to the durable remote-execution host, which the CLI does not run.
`telemetry` was ported but imported by nothing: upstream `ai` only carries an
optional `telemetryContext`, which the port does not set.

The packages and their tests are removed from `main` and preserved on the
`unwired-packages` branch, which points at the pre-removal commit.
`go mod tidy` drops the dependencies they alone used
(`github.com/sgtdi/fswatcher`, `modernc.org/sqlite`). Later D-rows that describe
those packages are historical, not a description of the current tree.

Reintroducing any of them is a feature decision, not a port fix: wire it into a
runtime path first, then move it back from the branch.

### D237

**A fresh session persists its initial tool declaration, so a resumed transcript
resolves its loadout.**

Reference: pi v1.4.0 `core/agent-session.ts` `_restoreToolsFromTranscript`
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

v1.4.0 (`bd3c9b823`) resolves the loadout from every system message on the
branch, honors the removals those messages record, and activates extension tools
registered after the branch recorded its loadout. The port replays the same
declarations through `GetCurrentTools`, so removals and pre-compaction
declarations reach the restored set. Extension activation stays out of scope
(D41); MCP tools that register later are attached by the CLI's manager
(`AttachExtraTools`), not by this restore.

### D238

**The shell elapsed label ticks at 100ms instead of upstream's 1000ms.**

`core/tools/render-utils.ts` arms `setInterval(() => context.invalidate(), 1000)`
while a shell call runs, but its label formats with `toFixed(1)` (tenths), so
the running clock is displayed with a resolution its redraw period cannot show:
`Elapsed 2.0s` jumps to `3.0s` a whole second at a time. The port's
`shellElapsedTick` is 100ms, so the display moves by .1s and the tenths
resolution is real. Non-shell running tools keep the upstream 1s tick, and the
final `Took` value is unaffected (it reads the recorded duration). The cost is
ten small differential paints a second while a shell command runs.

### D239

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
renderer is ever wired. D243 extends the notify to every pause.

### D240

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

### D241

**Headless stdout has no takeover; diagnostics are written to stderr.**

Reference: `core/output-guard.ts` (`takeOverStdout`, `writeRawStdout`,
`waitForRawStdoutBackpressure`, `flushRawStdout`), used by `main.ts`,
`modes/print-mode.ts` and `modes/rpc-mode.ts`. After takeover, upstream
redirects every stray `process.stdout.write` to stderr and serializes protocol
output through a raw tail that retries `ENOBUFS`/`EAGAIN`. The port does not
patch `os.Stdout`: a Go `os.File` write is blocking, so `coding/printmode.go`'s
`printStdout` mutex plus that blocking write already provide output ordering and
backpressure, and the diagnostics upstream would redirect are written to stderr
directly (`cmd/main.go`, `coding.FormatCLIDiagnostic`). The `--resume` picker,
which upstream's takeover would also send to stderr, instead draws on stderr for
the explicit headless flags (`--print`, `--mode json`) and on stdout otherwise
(`cmd.sessionPickerOutput`). `restoreStdout` has no counterpart.

### D242

**A goal stays active while the session retries a transient failure.**

Reference: the installed goal extension's `machine.ts` pauses on every
`agent_before_settle` outcome of `error`. Pier checks the session's
`willRetryAfterAgentEnd` result first. When auto-retry is enabled, attempts
remain, and the assistant error is retryable, the goal remains armed and does
not pause or enqueue a duplicate round. The retry continues the existing round;
its successful settle can reserve the next one. A final non-retried error still
pauses with `run-error`. User aborts, exhausted budgets, and reload pauses keep
their existing pause behavior (D239 covers the context-only pause message).

This applies to transient HTTP/2 stream resets as well: D171's classifier
recognizes Go's
`stream error: stream ID ...; PROTOCOL_ERROR; received from peer` and HTTP/2
GOAWAY wording, which upstream's fetch/websocket error list does not contain.

Tests: `TestAgentSessionRetryableFailureRecoveryKeepsGoalActive`,
`TestGoalControllerFinalFailureStillPauses`, and
`TestBeforeSettleRetryingKeepsTheGoalActive`.

### D243

**Every goal pause emits a human-facing notification.**

Reference: the installed goal extension's `machine.ts` (the extension source,
D223). Its `agent_before_settle` abort and error branches, and the `goal_pause`
branch, commit the durable pause entry without a `notify` effect; only the
budget stop and the reload pause notify. A goal that stopped after a run error,
or on a cancellation, therefore went silent in the UI even though the goal had
stopped.

The port emits `Goal paused: <reason>` (level `warning`) from every pause
branch: `agent_end` abort, `agent_before_settle` abort and error, the budget
stop, and the reload pause. `/goal pause` already notifies through the command
reply, so `goalPause` keeps that notice instead of adding a second. The
notification is additive: the durable pause entry and D239's context-only
`<goal_paused>` message are unchanged. A retryable failure that the session
retries never pauses, so it emits no notice (D242).

Tests: `TestAbortedGoalAttemptPauses`, `TestBeforeSettleAbortDurablyPauses`,
`TestBeforeSettleProviderErrorPauses`, and
`TestGoalControllerFinalFailureStillPauses`.

### D244

**`find` keeps leading and trailing whitespace in result paths.**

Reference: `core/tools/find.ts` trims each fd output line
(`rawLine.replace(/\r$/, "").trim()`) before relativizing it. For a file whose
name legitimately begins or ends with a space, that trim yields a path that does
not exist, so the model cannot read what it just found. The port removes only the
trailing carriage return and preserves the rest of the name; the trim is the
upstream defect here. Every other line-normalization rule matches: empty lines
are skipped and the path is still relativized against the search root.

Test: `TestFindToolPreservesWhitespaceInFileNames`.

### D245

**MCP HTTP URLs in WHATWG special-scheme form are stored canonically.**

Reference: `core/mcp-servers.ts` validates with `URL.canParse` and retains the raw
config string. The JavaScript HTTP client later resolves special URLs such as
`http:localhost:8080/mcp` and backslash separators. Go's `net/url` does not
perform that resolution. The port validates with equivalent effective URL
semantics and stores the normalized absolute URL so `net/http` and the MCP
transport reach the same host and path. Ordinary absolute HTTP(S) URLs are
retained unchanged. The WHATWG parser's whitespace preprocessing (leading and
trailing C0 controls/spaces stripped, tab/CR/LF removed anywhere) is applied
before validation and parsing, so a config URL with a stray space or newline
loads as upstream's does; the stored effective URL is the stripped form.

Tests: `TestValidateMcpServerConfigNormalizesSpecialHTTPSchemeURL`,
`TestValidateMcpOAuthNormalizesSpecialLoopbackCallbackURL`, and
`TestValidateMcpServerConfigMessages`.

### D246

**A `!` command renders through the shell tool's frame.**

Reference: `interactive-mode.ts` `handleBashCommand` (the frame goes into
`pendingMessagesContainer` while `session.isStreaming`, and
`flushPendingBashComponents` moves it into the transcript only on a normal
submission) and `components/bash-execution.ts` (its own bordered `$ command`
frame with a 20-line preview, status-line hints and a loader).

Upstream renders a manual command with a component of its own, keeps it in the
footer dock while the model streams, and only shows it in the transcript on the
next submission; the dock sizes to its children, so on a 30-row terminal a
completed preview leaves the transcript one row. The port treats the command as
a manual shell **tool call**: it builds the same `ToolExecutionComponent` a
model `bash` call uses (with `core/tools/renderers/bash.ts`), so the call line,
collapsed preview and hidden-line hint, click-to-expand, partial/error
backgrounds and the Elapsed/Took label are upstream's tool behavior, and the
frame joins the transcript as soon as the command is triggered.

The session entry is unchanged: a `bashExecution` custom message recorded by
`recordBashResult` (deferred to the turn's end while the agent streams), which
`ConvertToLlm` still projects as a user message; D248 restores the frame's
position on replay. A running `!` command belongs to the active turn, so Escape
aborts it with the turn. The call line's prompt is the shell-mode `!` rather
than the tool's `$` (D251). The `!!` form keeps its context exclusion but no
longer has upstream's separate dim styling, which a tool frame has no equivalent
for.

Tests: `TestBashFrameJoinsTheTranscriptWhileTheModelStreams`,
`TestManualBashFrameTicksWhileTheAgentIsIdle` and
`TestEscapeCancelsTheManualCommandWithTheTurn`.

### D247

**A stale callback timeout cannot remove a replacement waiter.**

Reference: `packages/mcp/src/oauth/callback.ts` clears and deletes a state in a
single-threaded JavaScript event loop. Go's `time.AfterFunc` callbacks can run
concurrently with callback handling. A timer that had fired before its old
waiter was removed could otherwise delete a newly registered waiter with the
same state. The port expires a state only when the map still points to that
timer's entry.

Test: `TestStaleCallbackTimeoutDoesNotDeleteReplacementWaiter`.

### D248

**A replayed `!` frame keeps its live transcript position, not its entry
order.**

Reference: `core/agent-session.ts` `recordBashResult` defers the message while
the agent is streaming ("to avoid breaking tool_use/tool_result ordering") and
`_flushPendingBashMessages` appends it when the run ends (`prompt`'s `finally`).
The session therefore stores a command run during a turn after everything the
turn produced, while the live transcript shows the frame when it is triggered
(D246), so a resumed transcript would draw it at the end of the turn.

The port keeps the upstream session order: the entry is still a
`bashExecution` custom message appended at the turn's end, so the file and the
LLM context are unchanged, and `ConvertToLlm` already holds a custom message out
of context when it would split a tool call from its result. The message is
instead stamped with the command's start (`recordBashResult` keeps the record
time for callers that bypass `executeBash`), and the replayed transcript places
the frame before the first entry appended after that moment, which is the
position the live transcript gave it. A frame never moves later than its own
entry, so an entry sharing the start millisecond keeps the recorded order;
entry timestamps that cannot be read never move a frame.

Tests: `TestSessionBashMessageIsStampedAtStart`,
`TestReplayedBashFrameKeepsItsPositionAmongTheTurn` and
`TestReplayedIdleBashFrameStaysInPlace`.

### D249

**A `bashExecution` message persists the command's duration.**

Reference: `core/messages.ts` `BashExecutionMessage` has no execution time, so a
replayed manual command cannot show the shell tool's `Took` label. The port adds
`durationMs` after the upstream keys (omitted when unknown), so a message
written before the field existed, or by upstream, stays byte-identical and
shows no label. `ExecuteBash` records the measured duration with the message
stamped at the command's start (D248).

Tests: `TestSessionBashMessageIsStampedAtStart` and
`TestSessionBashMessageRecordsDuration`.

### D250

**A continued session restores its `!` commands to the editor history.**

Reference: `interactive-mode.ts` `addMessageToChat` adds only user-message text
to the editor history when `populateHistory` is set; its `bashExecution` branch
renders the frame and stops. A reloaded session therefore loses every `!`/`!!`
command from the up-arrow history, even though the live submit added the typed
text. The port restores `!` + the recorded command (`!!` for the excluded form)
while replaying the message. The command was trimmed before execution, so the
restored entry is the normalized form rather than the raw typed text.

Test: `TestTranscriptRestoresBashCommandHistory`.

### D251

**A manual `!` command's call line uses the shell-mode `!` prompt.**

Reference: `components/bash-execution.ts` renders its own `$ command` header, and
`theme.ts` colors the editor border with `bashMode` while the input starts with
`!`. D246 already gives a manual command the shell tool's frame, so the frame
alone no longer tells the two apart. The port swaps the call line's prompt from
the shell tool's `$` to `!` (the sigil the user typed), leaving the rest of the
frame identical and the color unchanged. A manual power-shell command would use
the same `!` prompt; the result renderer, including a custom session renderer,
is unchanged.

Tests: `TestBashCommandFromTheEditor`,
`TestReplayedBashFrameKeepsItsPositionAmongTheTurn` and
`TestReplayedIdleBashFrameStaysInPlace`.

### D252

**Proxy content events reject negative array indices.**

Reference: `packages/agent/src/proxy.ts` assigns
`partial.content[contentIndex]`. A proxy sending
`{"type":"text_start","contentIndex":-1}` creates a JavaScript array property
that is absent from JSON output. The same assignment panicked in the port's
streaming goroutine and could terminate the process. Indexed proxy events return
a protocol error for negative indices; the stream reports its ordinary error
event instead of attempting a Go slice access.

Test: `TestProxyRejectsNegativeContentIndex` covers every indexed event type.

### D253

**Integer JSON tokens retain the port's native integer precision.**

Reference: `core/session-manager.ts` and AI providers serialize JavaScript
numbers with `JSON.stringify`. Go's typed timestamp and token-count fields use
`int64`; rewriting their integer tokens through a double can move a valid Go
value outside its type's range. For example, `9223372036854775807` rounds to
`9223372036854776000`, which cannot be read back into `int64`. The shared JSON
encoder retains integer token precision while normalizing floating-point
spellings and negative zero. A lone surrogate escape (`"\ud800"`) becomes
U+FFFD instead: Go strings cannot hold an unpaired surrogate, while
`JSON.stringify` would preserve the escape, so pier normalizes it rather than
emitting text it cannot represent (a valid pair still becomes the character).
Tool arguments still use JavaScript double semantics through their ordered
value decoder.

Test: `TestMarshalJSONPreservesTypedIntegerRange` observes the round-trip
failure when the encoder normalizes all integer tokens through a double.

### D254

**The settings-backed HTTP idle timeout bounds provider requests.**

Reference: `core/http-dispatcher.ts` sets Undici's `headersTimeout` and
`bodyTimeout` from the global idle setting. `core/sdk.ts` separately sets the
provider request's `timeoutMs` from `retry.provider.timeoutMs`, falling back to
the effective idle timeout only when no provider override exists. OpenAI,
Anthropic, Codex, Mistral, and OpenRouter image APIs pass `timeoutMs` to their
requests (`api/azure-openai-responses.ts` shares the OpenAI SDK); Google,
Vertex, Bedrock, and Pi Messages rely on the dispatcher timeout.

Go's `http.Transport` has no per-request header timeout, and writing
`ResponseHeaderTimeout` on the process-wide `http.DefaultTransport` races with
in-flight requests (D40). The port therefore arms one timer per provider request
(`ai/streamidle.go`), including the single-request OpenRouter image generation.
Dispatcher-only requests use the raw settings delay for both header and body
idle phases, matching Undici's FastTimer. `coding/sdk.go` passes the global
setting separately from the provider SDK `TimeoutMs`; a provider retry override
therefore cannot shorten the body-idle setting. OpenAI, Anthropic, and
OpenRouter image SDK requests also have a Node `setTimeout` deadline around
`fetch` (`pi/node_modules/openai/src/client.ts`,
`pi/node_modules/@anthropic-ai/sdk/src/client.ts`): the port uses the smaller of
that SDK deadline and the global header-idle timeout, then switches to the raw
Undici body-idle delay after headers arrive. In either case every body read
re-arms the timer, so only silence fails the stream and a stream that keeps
producing data outlives the timeout.

The timer counts time blocked in a body read, not wall-clock silence: a callback
that fires while the consumer is processing re-arms instead of cancelling, and
the deadline runs from the last delivered bytes, so a loaded machine, a GC pause
or a slow renderer cannot abort a connection whose bytes already arrived. A nil
or non-positive global idle setting disables only the Undici bound; an SDK
header deadline remains independent.

Close marks the body closed before stopping the timer; resets check that flag
again afterwards. A pending read returning buffered bytes after close, or a late
timer callback, cannot restart a timer that retains the closed response.
`TestIdleGuardDoesNotRearmAfterCloseDuringRead` covers that shutdown race without
sleeping.

Codex keeps upstream's `Codex SSE response headers timed out` wording for the
headers case; its `AbortSignal.timeout` header delay also keeps Node's 1ms clamp
and uint32 range check. Mistral keeps upstream's headers-only header timer and
`Mistral response headers timed out` wording (`ai/mistral_conversations.go`;
upstream's `AbortSignal.timeout` wraps the headers only) while its body idles
under the shared guard. The federation stream (`ai/pi_messages.go`) and the
agent proxy fetch (`agent/proxy.go`) use the global idle timeout. The proxy's
provider `TimeoutMs` remains local and is not serialized, matching
`agent/proxy.ts`. The other idle-timeout messages ("Provider response headers
idle", "Provider stream idle") are the port's own; upstream surfaces undici's
`Headers Timeout Error`/`Body Timeout Error` instead.

The same change removed a port-only defect: the Codex SSE path cancelled the
request context as soon as the headers arrived, truncating every stream to the
events already buffered.

Tests: `TestProviderStreamsAbortWhenResponseHeadersIdle`,
`TestIdleGuardDoesNotFireWhileTheConsumerIsBusy`,
`TestStreamAnthropicAbortsWhenStreamIdles`,
`TestStreamAnthropicKeepsAResponsiveStreamAlive`,
`TestStreamMistralAbortsWhenStreamIdles`,
`TestMistralActiveStreamOutlivesTheIdleTimeout`,
`TestStreamProxyAbortsWhenResponseHeadersIdle`,
`TestStreamCodexResponsesKeepsStreamingPastTheHeadersTimeout`,
`TestOpenRouterImagesAbortsWhenResponseHeadersIdle`,
`TestProviderHeaderTimerClampsLargeDelayToOneMillisecond`,
`TestOpenAICompletionsUsesNodeHeaderTimeoutClamp`,
`TestProviderRequestSeparatesHeaderAndBodyTimerSemantics`,
`TestProviderDispatcherHeaderTimerKeepsLargeFastTimerDelay`,
`TestCodexAbortSignalHeaderTimerClampsLargeDelay`,
`TestCodexAbortSignalRejectsOutOfRangeDelayBeforeFetch`,
`TestCodexRejectsNegativeTimeoutBeforeFetch`,
`TestSDKTimeoutDoesNotReplaceDispatcherBodyIdleTimeout`,
`TestStreamProxyUsesDispatcherIdleTimeoutNotProviderOverride`,
`TestMistralSDKTimeoutDoesNotReplaceDispatcherBodyIdleTimeout`,
`TestDispatcherOnlyTimeoutIgnoresProviderOverride`,
`TestCreateAgentSessionRequestOptions`, `TestHTTPIdleTimeoutReachesTheRequest`,
and `TestGuardIdleBodyPreservesFastTimerDelayAndSaturatesDuration`.

### D255

**Non-streaming auth and config requests carry their own default timeout.**

Reference: `core/http-dispatcher.ts` installs the idle timeout on the process
dispatcher, so every `fetch` in pi inherits a bound, including OAuth token
exchanges, device-code polling, ADC and metadata token discovery, the Radius
gateway config, MCP OAuth discovery and federation token exchange. The port
cannot set a transport timeout per request (D40) and the settings-backed value
only reaches provider streams, so those calls take a default bound when their
context has none: 60s in `ai` (`ai.doBoundedRequest`) and 30s for MCP OAuth
discovery (`oauth.defaultFetch`, matching the 30s MCP request timeout). A caller
deadline is always shorter and wins, and closing the response releases the
timer. Requests that already carried a shorter bound are unchanged:
`postOAuthJSON` 30s, the OpenRouter exchange, GitHub Copilot's 5s token retry,
the OpenRouter catalog's 15s and MCP request timeouts.

Tests: `TestBoundedRequestHonorsTheCallerDeadline`,
`TestGitHubCopilotTokenRefreshIsBounded` and `TestDefaultFetchIsBounded`.

### D256

**A cancellation never reaches the model as Go's context text.**

Reference: `agent/src/agent-loop.ts` reports a call it aborted before starting it
as `createErrorToolResult("Operation aborted")`, provider streams shape their
terminal event with `output.stopReason = signal.aborted ? "aborted" : "error"`,
and `api/models.ts` `applyAuth` calls `signal.throwIfAborted()`, so a cancel
during auth resolution fails `lazyStream` with the abort reason. Upstream tools
take an `AbortSignal` and cannot produce a context error; in Go the cancellation
is a `context.Canceled` (or a `DeadlineExceeded`) value that any tool, hook or
auth step can return, and it used to reach the transcript as "context canceled".

The port normalizes it at three boundaries, all wrapping-aware
(`ai.isContextError`, `agent.isContextError`):

- Tool execution: a canceled run whose tool returns a context error records
  `Operation aborted`, the same result the loop writes for a call it aborted
  before starting it. A tool's own failure keeps its message even when the run is
  canceled around it.
- Lazy stream setup: a setup failure caused by the request context is reported
  with `Request was aborted` while keeping upstream's `error` stop reason
  (`api/lazy.ts` `createSetupErrorMessage` hardcodes `stopReason: "error"`).
  Upstream shows the DOMException text there ("This operation was aborted"); the
  port uses its canonical abort text so one cancellation reads the same wherever
  it lands.
- Provider error normalization and run failure: `NormalizeProviderError` and
  `handleRunFailure` map any wrapping context error to `Request was aborted`, so
  the openai-completions/google/bedrock/responses/codex fail paths and the
  agent's failure message agree. Upstream reaches the same text by throwing
  `Error("Request was aborted")` at each abort check.

Tests: `TestCanceledToolCallReportsOperationAborted`,
`TestToolFailureKeepsItsMessageWhileTheRunIsCanceled`,
`TestModelsStreamSetupFailureOnCancelReportsAbort`,
`TestModelRuntimeStreamCancellationReachesOAuthAuthResolution`,
`TestNormalizeProviderErrorReportsCanceledRequestsAsAborts` and
`TestHandleRunFailureMapsCanceledContextToAbortMessage`.

### D257

**Model and footer-status ordering use locale-neutral collation.**

Reference: pi v1.4.0 `core/model-resolver.ts`, `bestFuzzyMatch`,
`cli/list-models.ts`, and `modes/interactive/components/model-selector.ts`
sort providers and IDs by Node's default `localeCompare`; the builtin footer
(`modes/interactive/components/footer.ts`) sorts extension-status keys the same
way. Pier uses `golang.org/x/text/collate` root collation in model resolution,
CLI listing, the interactive selector, and footer status rendering, rather than
byte order or a host-selected locale. This restores the reported punctuation
and case ordering (`model-2` over `model_1`, `model-Z` over `model-z`) while
keeping ordering deterministic. On a Swedish Node host, `model-ä` ranks after
`model-z`; Pier selects `model-z` regardless of the host locale. Locale and
Unicode collation version differences are not an assertion of byte-identical
ICU behavior.

Tests: `TestFuzzyModelAliasLocaleOrdering`, `TestListModelsTextUsesLocaleOrdering`,
`TestModelSelectorProviderOrderMatchesUpstreamLocale`,
`TestFooterStatusesUseUpstreamLocaleOrdering`.

### D258

**The crash hint names the issue tracker, not `/bug`.**

Reference: `modes/interactive/interactive-mode.ts` points the crash report at
its `/bug` flow, which uploads a bundle to the developers. The port omits the
share and bug-report flows (D41), so a hint that says "run /bug" names a command
that does not exist. The startup notice and the post-crash instructions instead
name the recorded crash log (`GetCrashLogPath`) and
`https://github.com/dat267/pier/issues`, which is what a user can act on. The
crash record itself is unchanged.

Tests: `TestCrashHelpers`, `TestFatalRuntimeError`.

### D259

**MCP sign-in errors do not name a `/mcp` command.**

Reference: `extensions/mcp/runtime.ts`, `signInRequiredMessage`, tells the user
to run `/mcp` when a server has no provider auth. Upstream drives OAuth sign-in
from that command; the port has neither the command nor a sign-in entry point
(D185 keeps the OAuth store per-process). The provider form of the message still
names `/login <provider>`, which exists. The OAuth form points at the
`oauth` block in `mcp.json` instead of at a command that does not exist.

Test: `TestMcpConnectionAsksOAuthServersToSignIn`.

### D261

**An invalid `file://` URL is returned unchanged instead of raising.**

Reference: `utils/paths.ts`, `normalizePath`, calls Node's `fileURLToPath`
unguarded, so a non-local host (`file://host/x`), an encoded slash
(`file:///tmp/a%2Fb`), malformed percent encoding or invalid encoded UTF-8
throws and the caller reports a URL error. `NormalizePath` returns a string and
has no error result, so it returns the input unchanged when `filePathFromURL`
rejects the URL; the caller then fails on a path ("file not found:
file://host/x") instead of on the URL. The follow-up URL-parser audit matched
1,174 generated inputs against Node: dot segments resolve before encoded-slash
validation, invalid encoded UTF-8 is rejected, raw tabs/newlines and trailing C0
whitespace follow WHATWG rules, escaped localhost is recognized, and
drive-letter roots survive parent segments (including the legacy `C|` spelling).

Tests: `TestNormalizePathKeepsAnInvalidFileURL`,
`TestNormalizePathFileURLRemovesEncodedSlashBeforeValidation`,
`TestNormalizePathFileURLRejectsInvalidUTF8`,
`TestNormalizePathFileURLIgnoresParserWhitespace`,
`TestNormalizePathFileURLAcceptsEscapedLocalhost`,
`TestNormalizePathFileURLRecognizesDriveRoots`.

### D262

**An escaped suffix after a wildcard matches literally, unlike minimatch's fast
path.**

Reference: `core/model-resolver.ts` matches scoped-model patterns with
`minimatch(value, pattern, { nocase: true })`. A follow-up comparison of 61,412
generated pattern/value pairs covered brace expansion (comma groups,
`{1..3}`-style sequences, literal `{a}`, inner-first nesting, escapes), path
canonicalization (`..` removes the preceding segment; `.` stays literal), the
`.`/`..` and dotfile segment rules, empty paths, absolute paths, repeated and
trailing separators, and globstars. It left 87 mismatches, all in the
escaped-suffix fast path described below.

Brace expansion caps Cartesian-product allocation and nested alternatives before
growing buffers, retains suffixes after truncating alternatives, ignores discarded
empty alternatives when counting the budget, and honors the reference's nesting
and rewrite limits. Negative padding counts the sign, large sequence values use
JavaScript numbers, escaped class hyphens stay literal, empty invalid ranges
match nothing even when negated, backslash pairs follow
minimatch's conditional brace-expansion pass, and literal-brace and `${...}`
handling follows brace-expansion. Matcher memo tables grow with visited
states instead of reserving the full pattern/value product on a first-character
mismatch. Regression tests measure allocations with modest fixtures.

One difference remains, in minimatch's fast paths (`starDotExtRE` and
`qmarksRE`): for a pattern whose wildcard is followed by an escaped suffix such
as `*\}`, minimatch compares the raw escape text (`endsWith("\\}")`) instead of
the escaped character, so `*\}` does not match `}`. Pier compares the unescaped
character and matches, which is what minimatch's own compiled regex does. The
affected shape is a wildcard followed only by characters outside
`+@!?*[(` with an escape among them; a model-scope pattern has no use for it.

Tests: `TestMatchGlobAgainstMinimatch`,
`TestMatchGlobBraceSequencesAndDotSegments`,
`TestMatchGlobEscapedSuffixFastPath`.

### D263

**Expanded thinking keeps italic across all Markdown text elements.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/modes/interactive/components/assistant-message.ts`
sets `defaultTextStyle.italic`, but
`packages/tui/src/components/markdown.ts` renders headings, code spans, fenced
code and their decorations through separate styles. For example, a thinking
block containing `# Heading` and an inline code span displays those elements
without italic even while its ordinary prose is italic.

The port opts expanded thinking into `MarkdownOptions.ForceItalic`. After
Markdown styling and wrapping, each newly rendered text line inherits italic,
restores it after SGR resets (including combined and empty parameters), and
closes it at the line boundary. Bold, colors, links and syntax highlighting
remain intact. Image escape payloads are not wrapped. This presentation is
cached with the rendered block and captured by detached Markdown preparation;
it does not add a transcript-wide rendering pass. Normal Markdown, answer text,
collapsed thinking, disabled terminal styles and model-facing messages are
unchanged.

Tests: `TestThinkingMarkdownKeepsStyledTextItalic`,
`TestThinkingItalicSurvivesStreamingAndVisibility`,
`TestThinkingItalicRespectsDisabledStyles`,
`TestMarkdownForceItalicRestoresResets`,
`TestMarkdownForceItalicKeepsImagePayloads`, and
`TestMarkdownForceItalicSurvivesDetachedPreparation`.

### D264

**A summarization that runs out of answer room escalates instead of failing the
compaction.**

Reference: `core/compaction/compaction.ts` (`generateSummaryWithUsage`,
`createSummarizationOptions`, `getSummarizationFailure`,
`completeSummarization`, `UPDATE_SUMMARIZATION_PROMPT`).

Upstream v1.3.0 failed the whole compaction on the first `length` stop with
`Summarization failed: generation hit the token cap and the summary is
incomplete`, which left a long session uncompactable. Upstream v1.4.0 addresses
the common cause by sending no `maxTokens` for a summary request
(`6fcb48670`), so the provider sizes the answer from the request: the model's
output limit, reduced to fit the remaining context, as for a normal turn. The
port does the same, and the fixed caps (`0.8 * reserveTokens` for a conversation
summary, `0.5` for a turn prefix, a hardcoded `4096` for a branch summary) are
gone with them.

What remains reachable is a `length` stop where the answer really did fill the
room that was left, or where the thinking tokens took most of it. The port
escalates there instead of giving up:

1. Same input, without the thinking tokens: the retry turns reasoning off, which
   is the only share left to free once the provider sizes the budget.
2. Less input at a time: the messages are split near the middle by estimated
   token weight, the first half is summarized down to a checkpoint, and the
   second half is merged into it through upstream's update prompt. Each chunk's
   answer then has the whole budget for its own request, and a chunk that still
   overflows splits again. The split never falls between a tool call and its
   result: the boundary nearest the weight midpoint is chosen whose left side has
   no outstanding call, so a parallel batch and its results stay in one chunk.
   Only when every boundary sits inside a call/result chain does the weight split
   win, because a separated pair still beats failing the compaction. That
   fallback is always an interior boundary, including when a large final tool
   result alone crosses half the input weight.

One summarization spends at most `maxSummarizationRequests` (10) requests, and
every pass shares one routing id, as one upstream `completeSummarization` call
does across its retries. Usage accumulates across the passes, since the tokens
were really spent. Only a single message that overflows on its own, a model that
overflows every chunk, or a summarization that exhausts that budget still
reports upstream's message, so partial text never becomes a checkpoint. The
split-turn path uses the same escalation, merging through a turn-prefix merge
instruction so its checkpoint keeps the turn-prefix sections
(`## Original Request`, `## Progress So Far`, `## Context Needed to Continue`)
instead of being restyled into the conversation ones.

Tests: `TestSummarizationSendsNoOutputLimit`,
`TestSummarizationRetriesOnceWhenTheTokenCapIsHit`,
`TestSummarizationRetryIsBoundedAndStillFailsLoudly`,
`TestSummarizationDoesNotRetryWithoutALever`,
`TestSummarizationSplitsTheInputWhenTheCapKeepsHitting`,
`TestSummarizationSplitStaysBounded`,
`TestSummarizationSplitKeepsToolCallsWithTheirResults`,
`TestSummarizationSplitSummarizesACallWithItsResult`,
`TestCompactionSplitsResultHeavyToolPairAtLastResort`,
`TestTurnPrefixSummaryMergeKeepsItsOwnSections` and
`TestBranchSummarySendsNoOutputLimit`.

### D265

**Detailed request identity names the actual Go runtime and Go platform.**

Reference: pi v1.4.0 (`4a080252`),
`packages/ai/src/utils/identity.ts` (`adc5fa9f8`, `04a93bad8`). Upstream's
`formatUserAgent` reports Node/Bun and JavaScript platform/architecture names.
For example, a Node request on Linux amd64 identifies itself as
`pi/1.4.0 (linux; node/v<version>; x64)`.

The port keeps the product/version and detailed layout but reports
`runtime.GOOS`, `go/<runtime.Version without the go prefix>` and
`runtime.GOARCH`: `pi/1.4.0 (linux; go/<version>; amd64)` in that scenario.
Windows reports `windows`, not `win32`. This identifies the actual runtime
rather than pretending to run Node. Short mode remains exactly `pi`; custom
mode is verbatim, including an empty value. In the Go options,
`CustomUserAgent` selects a literal even when it equals `short` or `detailed`.

Tests: `TestFormatUserAgent`, `TestLiteralUserAgent`,
`TestRequestIdentityHeaders` and `TestImageRequestIdentityHeaders`.

### D266

**Bedrock identifies the caller without pretending to use the JavaScript SDK.**

Reference: pi v1.4.0 (`4a080252`),
`packages/ai/src/api/bedrock-converse-stream.ts` (`bedrockUserAgent`,
`adc5fa9f8`, `04a93bad8`), plus the reference lockfile's `@aws-sdk/core`
3.977.9 user-agent configuration and middleware.

Upstream gives the AWS SDK a product/version pair in detailed mode, a product
pair in short mode, or a custom string. The SDK escapes these tokens, adds
its own SDK/runtime/feature metadata and emits `User-Agent` and
`x-amz-user-agent`. A detailed request therefore includes the contribution
`pi/1.4.0` after JavaScript SDK metadata.

The port uses a raw HTTP Bedrock adapter, not that SDK. It emits the same
escaped identity contribution as both header values, without inventing
`aws-sdk-js` metadata: detailed mode sends `pi/1.4.0`, short mode `pi`, and
custom mode the SDK-normalized custom value. An explicit empty custom value
suppresses Go's default user agent. Unlike ordinary HTTP identity formatting,
Bedrock custom values are not verbatim: spaces, unsupported punctuation and
non-ASCII UTF-16 units are escaped as the SDK does, and `api/` names are
lowercased.

Headers are installed before SigV4 signing. Caller `User-Agent` strings can
override the default; null caller values are ignored on Bedrock, as upstream's
header middleware specifies. Reserved `x-amz-*` overrides stay ignored.

Tests: `TestBedrockIdentityHeaders` uses 12 cases generated by executing the
pinned `bedrockUserAgent` function and AWS SDK middleware.
`TestBedrockIdentityHeaderOverrides` covers caller overrides, null/empty
values, reserved headers and the signed `x-amz-user-agent` header.

### D267

**Request params keep JavaScript member order; map-backed options need order sidecars.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/api/openai-completions.ts`
`buildParams`, `packages/ai/src/api/openai-responses.ts` `buildParams`, and
`packages/ai/src/api/bedrock-converse-stream.ts` `requestMetadata`.

The port retains sampling-parameter key order when decoding model JSON,
including custom `models.json` definitions, model overrides, and thinking-level
parameters. The OpenAI Completions and Responses builders merge those in
JavaScript first-assignment order. Model serialization also uses these order
sidecars, so file and in-memory model-store clones retain the decoded defaults
and thinking-level override order. Go callers that construct map-backed
sampling parameters directly can provide `SamplingParamsOrder` on `Model` or
`StreamOptions`; without it, unknown map order falls back to sorted keys.

Bedrock request metadata preserves order when options are decoded from JSON.
Go callers constructing `RequestMetadata` maps directly can provide
`RequestMetadataOrder`; absent that sidecar, keys sort. Raw `OnPayload`
replacements still preserve their JSON member order.

Tests: `TestProviderPayloadReplacementsMatchUpstream/openai-*-sampling-order`,
`TestComposeCustomModelPreservesSamplingParamOrder`,
`TestComposeModelOverridePreservesSamplingParamOrder` and
`TestBedrockRequestsMatchSDKWire/request-metadata-order`. Both wire fixtures are
generated by executing pi v1.4.0 and its pinned AWS SDK.
`TestModelsStorePreservesSamplingOrder` (`coding/modelsstore_parity_test.go`)
checks defaults and thinking-level override order through both in-memory stores
and the file store, including the serialized model and captured HTTP request.

### D268

**Bedrock JSON hooks represent native binary values as base64 strings.**

Reference: pi v1.4.0 (`4a080252`),
`packages/ai/src/api/bedrock-converse-stream.ts` `createImageBlock` and
`decodeRedactedContent`, plus its AWS SDK Converse serializer.

Upstream passes native `Uint8Array` values to `onPayload` for image bytes and
redacted reasoning. The Go hook accepts JSON (`json.RawMessage`), not a native
JavaScript object, and represents those values as base64 strings. Reproducer:
an image block with `data: "aW1hZ2U="` produces native bytes at
`messages[0].content[0].image.source.bytes` upstream; the Go hook's member is
`"aW1hZ2U="`. HTTP serialization uses base64 in both implementations, so the
wire body is still byte-compared without normalization.

The Bedrock fixture generator normalizes native binary values only when
recording the hook's JSON view; it never changes the command passed to the SDK
or the captured HTTP body. `TestBedrockRequestsMatchSDKWire/binary-content`
covers image and redacted-reasoning bytes.

### D269

**An unparseable Google error body carries the Go decoder message, not V8's.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/api/google-generative-ai.ts`
catch plus `@google/genai`'s `throwErrorIfNotOK`: a non-2xx response whose
`content-type` includes `application/json` is read with `response.json()`, so a
malformed body throws V8's `SyntaxError` (`Unexpected token 'o', "nope" is not
valid JSON`, `Unexpected end of JSON input`) and that text becomes
`errorMessage`. The port re-parses the body for its
`JSON.parse`/`JSON.stringify`-equivalent message and, when that fails, reports
its decoder's error text instead (`invalid character 'o' in literal null
(expecting 'u')`; an empty body reports `unexpected end of JSON input`). The
shape is preserved: no status prefix is added, and the request still fails with
`stopReason: "error"`. Valid JSON bodies and non-JSON content types match
upstream byte-for-byte. Same class as D221: the port does not emulate
JavaScript engine error messages, so the V8 wording difference is accepted, not
a pending fix.

Tests: `TestProviderPayloadReplacementsMatchUpstream/google-generative-ai-*`
(`ai/provider_payload_replacements_test.go`) and
`TestGoogleAPIErrorMessage` (`ai/google_generative_ai_test.go`).

### D270

**A malformed Bedrock HTTP error body uses Go's status/body text, not the SDK's JSON parse error.**

Reference: pi v1.4.0 (`4a080252`),
`packages/ai/src/api/bedrock-converse-stream.ts` `formatBedrockError`, plus the
pinned `@aws-sdk/core` REST-JSON `handleError` and
`@aws-sdk/client-bedrock-runtime` ConverseStream deserializer. The SDK parses
HTTP error bodies as JSON regardless of content type. For an invalid body such
as `proxy exploded`, V8's `JSON.parse` error and the SDK's deserialization
suffix become the assistant error. Pier reports
`502 Bad Gateway: proxy exploded`. This is user-visible text only; modeled
errors, valid unmodeled JSON errors, empty bodies, and modeled stream exceptions
match the source fixture.

As in D269, the V8 wording is deliberately not emulated.
`ai/testdata/bedrock-errors/golden.json` is generated by executing pi
against stubbed HTTP responses and a modeled stream exception. The Go test pins
the accepted difference at `TestBedrockErrorsMatchPinnedSDK/gateway-text-body`.

### D271

**An empty model catalog is persisted as `[]`, and a legacy `null` is repaired on write.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/models-store.ts` declares
`ModelsStoreEntry.models` as `readonly AnyModel[]`, and
`packages/coding-agent/src/core/remote-catalog-provider.ts` uses
`stored ?? { models: [] }` as the fallback when persisting a missing catalog or
transient failure. A 404/501 refresh retains an existing models array, sets
`lastModified` to zero and removes the ETag; only an entry without a cached body
starts with `[]`. Pier previously cleared that body and persisted
`"models":null`. Both tools share `~/.pi/agent/models-store.json`, so pi's
restoration through `remoteModels(...).filter(...)` could throw on the null
array. `ModelsStoreEntry.MarshalJSON` emits `[]` for a nil catalog, and catalog
refreshes retain the body like upstream.

The departure: upstream `FileModelsStore.write` round-trips the parsed file, so
an untouched provider's legacy `"models":null` would survive a later write. Pier
normalizes every entry to the array shape instead, repairing the corruption it
produced. This repair changes only the null array, not the entry's metadata.

Tests: `TestFileModelsStoreEmptyCatalogIsArray` and
`TestFileModelsStoreWriteRepairsLegacyNullCatalog` (`coding/modelsstore_test.go`),
plus `TestRemoteCatalog404AndError` (`coding/remotecatalog_test.go`) and
`TestRemoteCatalogMissingRetainsCachedBody` (`coding/remotecatalog_parity_test.go`).

### D272

**The in-memory credential store exposes committed snapshots, not live references.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/auth/credential-store.ts`
`read` and `modify`. Upstream returns live credential objects and passes the
stored object into the callback. If a callback changes `current.key` and then
throws, the mutation remains visible; a caller can also mutate a credential
returned by `read` without another `modify` call.

Pier runs callbacks outside the data mutex, with per-provider completion chains
serializing `modify` and `delete`. To avoid races with concurrent and reentrant
readers, callbacks, inputs and returned values own deep credential snapshots,
including API-key environments and OAuth raw extras. Reads during a callback
see the last committed value. Successful in-place edits still commit even if
the callback returns nil. A callback error or cancellation before commit
leaves the stored value unchanged. Cancellation does not forcibly stop an
already-running synchronous callback.

A queued cancellation returns promptly but retains its place behind the active
operation until that operation settles, so later writes cannot overtake it.
Unrelated providers and read/list calls do not wait on a callback. This fixes
the previous reentrant-read deadlock without exposing shared mutable values.

Tests: `TestInMemoryCredentialStoreModifyCanRead`,
`TestInMemoryCredentialStoreCallbackUsesCommittedSnapshot` and
`TestInMemoryCredentialStoreOAuthCallbackUsesSnapshot`
(`ai/authstore_reentrancy_test.go`), plus ordering, cross-provider progress and
cancellation coverage in `ai/authstore_queue_test.go`.

### D273

**An unparseable cache-warming timestamp leaves the idle gap unmeasured.**

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/cache-stats.ts`
`scan`. A `cache_warm` usage entry seeds the previous request with
`timestamp: Date.parse(entry.timestamp)`. `Date.parse` returns `NaN` for a
malformed timestamp, so the next turn's `idleMs` is `Math.max(0, ts - NaN)`,
which is `NaN`. That value is below `CACHE_TTL_MS` and never selects the
"Cache miss after Nm idle" label; the notice still reports the token and cost
part of the miss.

Pier stores `CacheMiss.IdleMs` as an `int64`, which cannot hold `NaN`. When the
entry timestamp does not parse, the previous request keeps the warmed prompt,
model key and `reportedCache`, but its idle gap is reported as zero
(`timestampKnown` is false in `coding/cachestats.go`). The observable behavior
matches: the miss is counted, and the TTL idle label is not chosen. A
timestamp that does parse behaves exactly as upstream.

Test: `warm-invalid-timestamp` in `coding/testdata/cache-stats/golden.json`,
generated from the pinned `cache-stats.ts`; the fixture records upstream's
`idleMs: null` (`NaN`) and `coding/cachestats_parity_test.go` asserts the zero
sentinel plus the unchanged token/cost accounting.

### D274

**Null MCP resource members are normalized instead of rejected.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/extensions/mcp/tools.ts` `blockToContent`. Both
members below are malformed: the MCP schema types `size` as a number and `blob`
as a string.

- A `resource_link` with `"size": null` formats the null in upstream:
  `formatSize(null)` returns `"nullB"`, so the message reads
  `[Resource <uri> "<name>" (nullB)]`. Pier's `ContentBlock.Size` is a Go
  `*int64`, where JSON null and an absent member are both nil, so it omits the
  parenthesized detail. Matching the text would require a raw-member presence
  flag for invalid data, so the omission is deliberate.
- A resource with `"blob": null` satisfies upstream's `"blob" in resource`
  check, and `Buffer.from(null, "base64")` then throws a `TypeError` that
  rejects the whole conversion. Pier decodes null into the empty string and
  treats it as an empty blob, saving a zero-byte temp file with the
  "(unknown type, 0B)"/"(<mime>, 0B) saved to ..." text. The blob arm is
  otherwise key-presence tracked (`protocol.ResourceContents.BlobArm`, D181).

Tests: the MCP tool fixtures (`coding/testdata/mcp-tools/golden.json`)
deliberately exclude both null cases; `TestResourceContentsArmByPresence`
pins key-presence tracking, and `TestMcpToolContentMatchesUpstream` covers the
neither-member resource that shares this code path.

### D275

**MCP OAuth documents keep only members the typed model can hold.**

Reference: pi v1.4.0 (`4a080252`), `packages/mcp/src/oauth/types.ts`. The
reference parsers spread the whole input object and only override the fields
they validate, so a document's unknown members (`revocation_endpoint`,
`x_extra`, `x_custom`), an empty `authorization_servers: []`, and a declared
member with the wrong type (`grant_types: "authorization_code"`,
`token_endpoint_auth_method: 5`) all survive in the result and its structured
clone.

Pier models the documents with structs. `ParseClientInformation` copies
every declared metadata member verbatim (including empty strings) and keeps
`redirect_uris`' `[]` default, so a valid registration response matches the
pin; `ParseProtectedResourceMetadata` and `ParseAuthorizationServerMetadata`
validate every declared field. Members outside that model are dropped: unknown
keys, wrongly typed declared members (the Go field stays nil), and empty
`authorization_servers`/`scopes_supported` arrays (`omitempty`). The extra
members are kept on the metadata structs' `Extra` map but are not marshaled.

No supported path observes them: discovery state lives in the in-memory store
and every consumer reads typed fields. `mcp/oauth/testdata/parse/golden.json`
therefore excludes those cases, with the reasons recorded in its generator.

### D276

**`ResourceURLFromServerURL` keeps the input's port and host case.**

Reference: `packages/mcp/src/oauth/discovery.ts` `resourceUrlFromServerUrl`
returns a WHATWG `URL`, so `https://example.com:443/mcp` serializes as
`https://example.com/mcp` and `HTTPS://EXAMPLE.COM/MCP` lowercases the host.
Pier's helper returns a Go `url.URL` string with the fragment cleared, keeping
the explicit default port and the host case.

The helper's only consumer is `SelectResource`, which compares origins
(lowercased hosts, default ports stripped) and paths, so every match decision
and returned resource string agrees with the pin; only the helper's own output
and a mismatch message built from it can differ. WHATWG whitespace
preprocessing is applied before parsing, so padded inputs behave as upstream.

Tests: `TestOAuthResourceURLMatchesUpstream` and
`TestOAuthSelectResourceMatchesUpstream` replay
`mcp/oauth/testdata/parse/golden.json`, which pins the whitespace cases and the
`selectResource` decisions; the default-port and host-case helper outputs are
excluded there for this reason. `TestResourceURLKeepsDefaultPortAndHostCase`
pins those excluded outputs and that `SelectResource` still matches them.

### D277

**A mistyped settings member falls back to its typed default.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/settings-manager.ts`. The reference parses
`settings.json` into a raw object and reads each member with a `??` default, so
a wrong-typed value survives the load and its getter can return it as-is: with
`"compaction": {"enabled": "yes"}`, `getCompactionEnabled()` returns the string
`"yes"`, and a mistyped `thinkingBudgets.low` is echoed by
`getThinkingBudgets()`. A mistyped project value also shadows the global one
before the getter defaults.

Pier decodes into typed structs. `MigrateSettingsRaw` decodes member by
member (and nested struct members within a member), so a value the field cannot
hold only leaves that member at its zero value and the typed getter applies its
default; unrelated members survive. A project member rejected this way masks the
inherited global member, so the default applies instead of the global value
showing through. The public `MigrateSettingsRaw`/`MigrateSettings` use the same
path.

The remaining differences are deliberate:

- A getter that the reference does not type-check can echo the raw value there
  (`compactionEnabled == "yes"`). Pier's typed getters always return their
  declared type and therefore the default.
- A leaf collection with an invalid element (`"defaultTools": ["read", 5]`, a
  map value of the wrong type) is left at its zero value; the reference keeps
  the raw array/map and lets the getter filter. A present-but-empty object is
  kept (as `{}` would be), so schema round trips and save paths are unchanged.
- A member with an unsupported JSON type is absent from the typed view, but the
  raw file survives on disk: the persist path merges modified members into the
  raw content, and the value is repaired only when the user fixes it.

Tests: `TestSettingsLoadMatchesUpstream` with `coding/testdata/settings-load/`
(global and project scope, mistyped string/number/array/nested values, an
invalid-JSON control and an unknown-member control).

### D278

**A malformed session header id is rejected by listing and loading alike.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/session-manager.ts`. `loadEntriesFromFile`
requires the first parsed entry to be a `session` header whose `id` is a string;
an empty string counts, while a missing, `null` or non-string id discards the
file. `buildSessionInfo` (the resume listing) only checks `type === "session"`,
so the reference *lists* such a file with the invalid id (`undefined`, `null` or
a number) even though it cannot open it — the picker offers a session that fails
to resume.

Pier's typed `SessionHeader` cannot hold a non-string id, so the header line
fails to decode and the listing skips the file. Loading and listing therefore
both reject a header whose id is missing, `null` or non-string, which is
deliberately stricter than the reference's listing. A header with a string id —
including `""` — is accepted by both, matching the reference; the loaders check
the raw member so an empty id is distinguished from an absent one
(`sessionHeaderHasStringID`).

Tests: `TestSessionLoadMatchesUpstream` with `coding/testdata/session-load/`
(valid, empty, missing, null and non-string ids, a non-session first entry, and
malformed/blank first lines; listing is compared where the header id is a
string).

### D279

**Filesystem failures keep Go's error text.**

Reference: pi v1.4.0 (`4a080252`), `core/tools/read.ts` calls Node's
`fs.access` before reading, so a missing file rejects with libuv's message
(`ENOENT: no such file or directory, access '<path>'`), and the write/edit/read
tools surface the same style of errno text through Node's fs promises. The port
returns Go's `os` errors, for example
`stat /path/nope.txt: no such file or directory`.

Emulating Node's errno phrases would mean porting libuv's per-code messages and
per-syscall wording (`access`, `open`, `write`), the same class of
runtime-diagnostic emulation the port declines for V8 JSON errors (D269/D270).
The tool result still names the path and the failure, and providers read it as
an error result, not as data. The write tool's directory case is the same
divergence: Node reports
`EISDIR: illegal operation on a directory, open '<path>'` and the port keeps
Go's os error. Every other part of the read tool's output matches token for
token: a generated corpus compares it over 16 cases (empty files,
offsets and limits including fractional and out-of-range values, blank lines,
CRLF, a BOM, invalid UTF-8, a >50 KB line, >2000 lines with the truncation
notice, and nested paths).

Tests: `TestReadToolMatchesUpstreamTextPath`, `TestReadToolMissingFileError`
(`coding/read_tool_parity_test.go`).

### D280

**An older successful queued settings write does not clear newer dirty values.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/settings-manager.ts` clears the whole
modified-field set after each successful queued task. If another setter runs
while that task is pending, its fields are already in the shared set; an earlier
success can clear them before a later write fails. Subsequent saves then omit
those failed changes.

Pier's queued worker clears only the fields in the completed task's snapshot,
and only if the current value still equals the persisted snapshot. Later fields
and newer values remain dirty for retry. This is a deliberate reliability fix;
synchronous writes still clear under the manager lock.

Test: `TestSettingsRetryRetainsChangesAfterOverlappingQueuedSaveFails`.

### D281

**An over-wide editor grapheme does not recursively wrap itself.**

Reference: pi v1.4.0 (`4a080252`), `packages/tui/src/components/editor.ts`
`wordWrapLine("界", 1)` recurses with identical input until JavaScript throws
`RangeError: Maximum call stack size exceeded`. The corresponding Go path
fatally exhausts its goroutine stack. `Editor.Render(2)` reaches this case
because it reserves one column for the cursor.

Pier only recursively unwraps paste markers, which are multi-grapheme atomic
editing segments. An indivisible wide grapheme stays intact even when wider
than the available layout column, avoiding non-progress recursion and preserving
the pending input. Ordinary wrapping and visual splitting of paste markers are
unchanged.

Test: `TestEditorRendersWideGraphemeInNarrowTerminal` (normal-width control and
isolated narrow render, with a child-only stack limit).

### D282

**A malformed `models-store.json` reads as empty but is never overwritten.**

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/models-store.ts`
parses the file with an uncaught `JSON.parse`, so malformed content makes
`read`, `write` and `delete` throw, and no write rewrites the file. A non-object
top level (`null`, an array, trailing content) likewise throws or, for an array,
drops the entry being written.

Pier's read path keeps its established tolerance: malformed content yields an
empty store, so startup still falls back to the built-in catalogs. Its write and
delete paths reject a file they cannot parse, or whose top level is not an
object, and leave it untouched, so a catalog refresh cannot replace every other
provider's stored entry with the one being written. Reads stay available while
mutations refuse to destroy unreadable content.

Test: `TestFileModelsStoreWriteRejectsMalformedAndNonObjectJSON`
(`coding/modelsstore_test.go`), with the read tolerance pinned by
`TestFileModelsStoreParseErrors`.

### D283

**Settings transactions lock before the first read, even for a missing file.**

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/settings-manager.ts`
`FileSettingsStorage.withLock` invokes the callback before locking when the file
is absent. Two concurrent Go callers can both read absence and overwrite one
another's first update; JavaScript's synchronous callbacks cannot overlap in
one process.

Pier creates the parent directory and acquires the existing file lock before
reading, for both existing and missing settings files. Read-only loads therefore
create missing settings directories (but never a settings file), and may report
parent-directory or lock failures. This avoids replaying callbacks or adding a
second locking protocol and serializes first writers across storage instances
and processes.

Tests: `TestFileSettingsStorageConcurrentFirstWrites`,
`TestSettingsProjectDirectoryCreation`.

### D284

**AI and MCP requests retain Go client hooks and the ten-redirect limit.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/api/openai-responses.ts`
uses the SDK's fetch transport. Pier removes authorization, cookies and proxy
credentials after an origin change and keeps them removed on subsequent hops,
but retains the configured `http.DefaultClient` transport, timeout and redirect
hook, and its existing default ten-hop limit rather than fetch's twenty.
The global client and original request headers are not mutated.

Tests: `TestResponsesRedirectCredentials`, `TestProviderRequestRedirectPolicy`.

MCP's built-in Streamable HTTP client also keeps Go's ten-hop limit. Reference:
v1.5.0 (`f9a26365`) `packages/mcp/src/transports/streamable-http.ts` uses Fetch.
Because net/http copies the initial headers again on each redirect, checking
only the latest origin leaked stripped credentials on an A-to-B-to-B chain.
The hook checks the bounded redirect history and keeps bearer, proxy and cookie
credentials removed for the rest of that request. Separate requests retain
their own credentials, and same-origin chains retain nonsensitive headers.

Tests: `TestStreamableHTTPRedirectChainCannotRestoreCredentials`,
`TestStreamableHTTPRedirectDoesNotForwardAuthorizationAcrossPorts`,
`TestStreamableHTTPRedirectLimitRemainsTen`.

### D285

**Agent stream delivery uses producer-captured read-only partial snapshots.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/utils/event-stream.ts`,
`types.ts` and `packages/agent/src/agent-loop.ts` `streamAssistantResponse`.
Upstream's provider partial is a shared live helper, and the agent spreads its
fields on one JavaScript thread. In Go, the provider and agent run concurrently:
copying the live struct or serializing its content on the agent/UI goroutine
races with the provider's next delta, even before the UI event handoff.

`AssistantMessageEventStream.Push` captures message fields, content slots and
diagnostic slots on the producing goroutine. Published block values and their
text/JSON payloads stay read-only; copying slots does not copy the growing text
prefix. Forwarding streams retain the original capture instead of re-reading the
live helper on a different worker. `NextSnapshot` selects that read-only view,
and the agent uses it for both its message and inner event. The existing `Next`
and serialized event shape retain the upstream shared-live `Partial` contract;
callers that need concurrent read/retention use `NextSnapshot` instead. Producers
must publish through `AssistantMessageEventStream.Push`.

No UI-state lock, callback queue, or provider/session JSON field is added.

Tests: `TestAgentOwnsProxyPartialsDuringStreaming` reproduces mutation of an
emitted event without the race detector, and also covers the race under `-race`;
`TestAssistantStreamSnapshotDoesNotChangeLiveNext` and
`TestAssistantStreamForwardingPreservesProducerSnapshot` preserve the old helper
and forwarded snapshots.

### D286

**The prompt keeps file writes on `edit` and `write` instead of the shell.**

Upstream pi v1.4.0 (`4a080252`) contributes only `Use write only for new files
or complete rewrites` and the edit guidelines, so nothing stops a model from
rewriting files with redirection, `sed`, or an inline script — edits that bypass
the tool's diff preview and byte-exact replacement. Pier adds one rule when a
shell tool and at least one file tool are selected:

- `Do not write or edit files through bash (redirection, heredocs, sed, perl,
  python); use edit or write.`

It names `PowerShell` instead of `bash` when only that shell is selected, names
only the file tools actually available, and is omitted when neither `edit` nor
`write` is present, since the instruction would then point at a missing tool.
This follows jcode, whose system prompt carries the same instruction ("Edit
files with `edit`, `replace`, `apply_patch`, or `write`, not sed, perl, or Python
scripts in bash").

The rule is built in `buildRules`, not in the per-tool contributions, so the
upstream-cited snippet and guideline tables stay byte-comparable with pi.

Tests: `TestSystemPromptForbidsShellFileWrites`,
`TestSystemPromptShellWriteRuleNamesTheAvailableFileTool` and
`TestSystemPromptShellWriteRuleNeedsAFileTool`.

### D287

**Malformed session parent cycles terminate instead of hanging branch walks.**

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/session-manager.ts`
`buildSessionPath` follows `parentId` without cycle detection. A valid JSONL file
with two entries pointing to each other therefore loops indefinitely when
`GetBranch` or a context projection walks from the leaf. Pier stops at the first
repeated entry ID in branch, projection, and branch-summary walks; a cycle is
represented once in root-to-leaf traversal order. This changes only malformed
session graphs and avoids wedging the agent while preserving their readable
entries.

Test: `TestSessionParentCycleDoesNotHang` opens a synthetic session containing
two disconnected parent cycles in a bounded child process and checks branch,
projection-path, and branch-summary traversal, including one acyclic control
in the existing session-manager suite.

### D288

**A settings writer that loses its lock lease does not commit its pending write.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/settings-manager.ts` writes from inside
`FileSettingsStorage.withLock`; the pinned implementation relies on
`proper-lockfile` and has no explicit ownership check before the write. Pier checks
that the acquired lock directory still has the same file identity after the
callback and before committing, preventing a suspended writer from overwriting a
successor after stale-lock reclamation. This is a deliberate concurrency-hardening
divergence. The identity check is not an atomic fencing token, so a lock can still
be replaced in the narrow interval between the check and the write.

Test: `TestSettingsDisplacedWriterCannotCommit` blocks a holder, replaces its
lock directory, gives the replacement lock to a successor, and verifies that the
displaced holder fails without overwriting the original settings or removing the
successor's lock.

### D289

**Session JSONL storage is private to the owning user on POSIX systems.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/session-manager.ts` and
`packages/coding-agent/src/core/session-export.ts` use Node's default 0666/0777
creation modes, filtered by umask;
`packages/coding-agent/src/core/agent-session-runtime.ts` preserves imported
source modes with `copyFileSync`. Pier creates and rewrites session JSONL files
with mode 0600, uses mode 0700 for session directories, hardens existing session
files and known session directories when opened, and does not copy source
permissions to imported sessions. A directory inherited from a session file
opened by explicit path is not a pier session store: sessions derived from that
manager (new, clone, fork, import) keep using it for placement but leave its
mode alone, so cloning a session from a shared or user-owned directory cannot
chmod it (M73). Newly created export parent directories use 0700; explicitly
selected existing export directories retain their permissions. On Windows these
POSIX mode bits do not replace inherited ACLs; this hardening addresses the
reproduced permissive-umask exposure on POSIX.

Tests: `TestSessionManagerPersistsFilesWithPrivateMode`,
`TestOpeningExistingSessionMakesFilePrivate`, `TestForkedSessionUsesPrivateModes`,
`TestSessionManagerUsesPrivateDirectoryMode`, `TestSessionDirectoryUsesPrivateMode`,
`TestSessionExportUsesPrivateDirectoryMode`, `TestSessionExportUsesPrivateFileMode`,
`TestImportedSessionUsesPrivateMode`, `TestPathOpenedSessionReportsDerivedDirectory`,
`TestManagedSessionReportsItsDirectory`,
`TestDerivedSessionDirKeepsParentDirectoryMode`,
and `TestDerivedForkKeepsParentDirectoryMode`.

### D290

**Rendered OSC 0/1/2/52 controls and terminal-title control characters are suppressed.**

Reference: pi v1.4.0 `packages/tui/src/tui.ts` `applyLineResets` and
`packages/tui/src/utils.ts` `normalizeTerminalOutput` pass OSC 52 through;
`packages/tui/src/terminal.ts` `setTitle` wraps the provided title without
removing control characters. Pier removes OSC 0/1/2 title changes and OSC 52
clipboard operations at the final rendered-line
boundary while preserving OSC 8 hyperlinks and supported image sequences; it
also strips Unicode control characters from OSC 0 title text. This is a
deliberate terminal-output security divergence. v1.5.0
`utils.ts replaceControlCharacters` replaces each C0/C1 run with one space
before `setTitle`; pier keeps removing the characters entirely, which is the
stronger boundary and does not leave a placeholder in a title.

Tests: `TestApplyLineResetsStripsUnsafeOSC` covers BEL, ESC-ST, C1 ST,
unterminated controls, image lines and OSC 8;
`TestProcessTerminalSetTitleStripsControlCharacters` verifies the emitted OSC 0
sequence cannot be terminated by title content.

### D291

**Custom export colors are validated before HTML interpolation.**

Reference: pi v1.4.0
`packages/coding-agent/src/modes/interactive/theme/theme-schema.ts` accepts
string color values;
`packages/coding-agent/src/modes/interactive/theme/theme.ts`
`getThemeExportColors` returns custom CSS strings, and
`packages/coding-agent/src/core/export-html/index.ts` inserts them into the
`<style>` element. A color such as `#123456</style><script>…` closes the style
element and executes when the export is opened. Pier validates export values
with its color parser and omits invalid values. This is an intentional
export-security divergence.

Test: `TestGenerateSessionHTMLEscapesCustomExportCSS` reproduces and prevents
an injected script in the generated style element.

### D292

**Exported session HTML files are private on POSIX.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/export-html/index.ts`
uses `writeFileSync` defaults, so new transcript exports honor the process
umask and existing destination modes are preserved. Pier creates and rewrites
export files with mode 0600 to prevent conversation contents from being
readable under permissive umasks. Windows inherited ACLs are not changed.

Test: `TestExportHTMLWritesPrivateFiles` checks both export entry points under
umask 000, including pre-existing 0644 destinations.

### D293

**Debug log files are private on POSIX.**

Reference: pi v1.4.0 `packages/coding-agent/src/modes/interactive/interactive-mode.ts`
creates the debug log with `writeFileSync` defaults after recursively creating
its parent directory. The debug log includes rendered UI lines and agent
context messages. Pier writes the file with mode 0600, including when replacing
an existing file; its parent directory policy is unchanged. Windows inherited
ACLs are not modified.

Test: `TestWriteDebugLogFileUsesPrivateMode` checks new and existing files
under umask 000.

### D294

**MCP configuration files are private on POSIX.**

Reference: pi v1.4.0 `packages/coding-agent/src/extensions/mcp/config.ts`
uses `writeFileSync` defaults, honoring the process umask and preserving
existing modes. MCP configuration may contain authorization headers or other
credentials. Pier creates and rewrites configured files with mode 0600;
the global agent-data directory is hardened to 0700 when settings or global
MCP configuration is accessed (D300). Windows inherited ACLs are not changed.
This is an intentional credential-privacy divergence.

Test: `TestAddMcpServerConfigUsesPrivateFileMode` checks new and existing files
under umask 000.

### D295

**Global settings files are private on POSIX; project settings remain shareable.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/settings-manager.ts`
uses `writeFileSync` defaults for both scopes. Global settings can contain
proxy credentials, so Pier creates and rewrites global `settings.json` with
mode 0600; global settings access hardens the agent-data directory to mode
0700 (D300). Project settings keep the upstream umask-based mode for repository
sharing. Windows inherited ACLs are not changed.

Tests: `TestGlobalSettingsFileUsesPrivateMode` checks new and existing global
files under umask 000; `TestProjectSettingsFileUsesNodeDefaultMode` pins the
unchanged project file behavior.

### D296

**Crash log files are private on POSIX.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/crash-log.ts` uses
`writeFileSync` defaults. Crash records may contain error details, stack paths,
session paths, and cwd. Pier writes new and updated crash logs with mode
0600. Windows inherited ACLs are not changed.

Test: `TestCrashLogUsesPrivateMode` checks new and existing files under umask
000.

### D297

**TUI overwide-line crash logs are private on POSIX.**

Reference: pi v1.4.0 `packages/tui/src/tui-main-screen.ts` writes all rendered
lines to `pi-tui-crash.log` with `writeFileSync` defaults. Rendered output may
contain secrets. Pier creates and rewrites the diagnostic with mode 0600;
Windows inherited ACLs are not changed.

Test: Linux `TestMainScreenCrashLogUsesPrivateMode` checks new and existing
files.

### D298

**Trust decision files are private on POSIX.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/trust-manager.ts` writes
`trust.json` with `writeFileSync` defaults. The file controls whether project
resources are trusted and contains project paths; another local user able to
modify it could silently authorize project resources. Pier creates and rewrites
it with mode 0600; the trust store hardens its agent-data directory to mode
0700 before access (D300). Windows inherited ACLs are not changed.

Test: `TestTrustFileUsesPrivateMode` checks new and existing files under umask
000.

### D299

**Opt-in TUI terminal-write logs are private on POSIX.**

Reference: pi v1.4.0 `packages/tui/src/terminal.ts` appends raw terminal
output to `PI_TUI_WRITE_LOG` with Node defaults. The capture can include
prompts and responses. Pier creates and updates these logs with mode 0600;
Windows inherited ACLs are not changed.

Test: Linux `TestTerminalWriteLogUsesPrivateMode` checks new and existing
files under umask 000.

### D300

**Global agent-data directories are private on POSIX.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/settings-manager.ts`
creates the global settings directory with Node defaults, while
`packages/coding-agent/src/core/auth-storage.ts` requests mode 0700 only when
creating a missing auth directory. Existing directories can stay world-writable;
mode-0600 files inside them can still be replaced by another local user. Pier
creates and hardens the global agent directory to mode 0700 before reading or
writing global settings, auth, MCP, or trust data. The MCP loader and trust
store also harden their explicit agent directory before access. Project `.pi`
directories retain their existing shareable permissions. Windows inherited
ACLs are not changed.

Tests: `TestGlobalSettingsFileUsesPrivateMode`,
`TestAuthStorageHardensExistingParentDirectory`,
`TestLoadMcpConfigHardensExistingAgentDir`, and
`TestTrustFileUsesPrivateMode` check creation or repair under umask 000. Linux
`TestEnsurePrivateDirectoryRejectsBroadPaths` ensures cwd, parent, and root
paths are not accidentally chmodded; Linux `TestEnsurePrivateDirectoryRejectsSymlink`
prevents chmod from following an agent-directory symlink.

### D301

**Custom prompt-section order must be supplied explicitly for Go maps.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/system-prompt.ts` inserts
custom sections through `Object.entries`, preserving JavaScript property order.
Go maps have no insertion order, so Pier accepts `CustomSectionOrder` to preserve
the caller's intended order; custom names omitted from that slice are sorted
lexically for deterministic output. The builder retains first-insertion order
through prompt rendering and session patches: overriding a base section keeps
its slot, while a built-in name absent from the base follows custom order.
Tests reproduce explicit ordering, the fallback, and newly added built-in names.
The `prepareLoadout` extension hook itself remains out of scope under D41.

### D302

**MCP initialization cannot resurrect a closed client.**

Reference: pi v1.4.0 `packages/mcp/src/client.ts` sets `connected` after awaiting
`notifications/initialized` without rechecking closure. If that send has already
dispatched but completes after client close or a transport drop, upstream returns
success and changes `closed` back to `connected`. Pier rechecks `connecting` under
the client mutex before publishing metadata or connected state; a winning close
returns `McpConnectionClosedError` instead. No callback runs under that mutex.

Test: `TestClientConnectDoesNotResurrectClosedConnection` gates send completion
and checks explicit close, peer drop, and an ordinary successful-connect control.

### D303

**Session-search regexes use Go's RE2 syntax, not JavaScript's.**

Reference: pi v1.4.0
`packages/coding-agent/src/modes/interactive/components/session-selector-search.ts`
`parseSearchQuery` compiles `re:` patterns with `new RegExp(pattern, "i")`.
Pier compiles `regexp.Compile("(?i)" + pattern)`: RE2 has no lookaround or
backreferences, so patterns upstream accepts (`(?=...)`, `\1`) are parse
errors and therefore select nothing. Patterns both engines support behave the
same, including case-insensitive leftmost matching and the score
`UTF-16 index × 0.1`. Everything else in the query language (quoted phrases,
unclosed-quote fallback, name filter, relevance ordering) is unchanged; the
differential corpus of 130 queries over 60 sessions matched upstream, error
text aside.

Test: `TestSessionSearchRegexUsesRE2Syntax` pins the rejected constructs and a
matching plain pattern.

### D304

**OAuth expiry timestamps saturate at Go's int64 millisecond bounds.**

Reference: pi v1.4.0 OAuth providers calculate expiry with JavaScript Number
arithmetic (`auth/oauth/anthropic.ts`, `auth/oauth/radius.ts`,
`auth/oauth/xai.ts`, `auth/oauth/kimi-coding.ts`,
`auth/oauth/openai-codex.ts`, `auth/oauth/openai-chatgpt.ts`, and
`auth/oauth/github-copilot.ts`). Go stores `OAuthCredential.Expires` as int64
Unix milliseconds, so finite JS deadlines outside that range cannot be
represented exactly. `ai/oauth_expiry.go` saturates at the nearest bound to
preserve whether the credential is in the future or past rather than wrapping
into the opposite state. Ordinary provider lifetimes retain their exact
millisecond values.

Tests: `TestAnthropicTokenExpiryDoesNotWrapForLargeLifetime`,
`TestAnthropicTokenExpiryKeepsLargeNegativeLifetimeInPast`, and
`TestOAuthExpiryRemainsFutureAcrossTokenParsers` reproduce overflow in the
covered lifetime and absolute-expiry parsers.

### D305

**Provider body-idle delays beyond Go's duration range saturate at MaxInt64.**

Reference: the pinned `core/http-dispatcher.ts` passes finite millisecond values
to Undici FastTimer, whose numeric delay is not limited to Go's int64
nanoseconds. `ai/streamidle.go` saturates only values above about 292 years to
`time.Duration(math.MaxInt64)` instead of overflowing into an immediate timer.
This boundary is not exactly representable in Go; the practical outcome remains
a non-expiring timeout. Ordinary timeout settings remain exact. SDK header
`setTimeout` delays use Node's separate signed-32-bit clamp as described in D254.

Test: `TestGuardIdleBodyPreservesFastTimerDelayAndSaturatesDuration` pins both
the raw delay above Node's timer range and saturation above Go's duration range.

### D306

**Extreme prompt-cache TTLs saturate at Go's int64 millisecond bounds.**

Reference: pi v1.4.0 `core/cache-warmer.ts` multiplies model TTL seconds by
1000 in JavaScript Number space and stops warming when a scheduled time exceeds
its fixed one-hour safety window. Pier's `ModelPromptCache` stores seconds as a
Go `int`, while cache-warmer calculations use int64 milliseconds. If that
product cannot fit, `coding/cachewarmer.go` saturates to the nearest bound so a
far-future lifetime remains outside the warming window and a past lifetime
cannot wrap into a short positive delay. Ordinary TTLs retain exact values.

Test: `TestCacheWarmerStopsWhenTTLMillisecondsWouldOverflow` reproduces a
large finite TTL wrapping into a scheduled near-term refresh.

### D307

**Legacy helpers expose a one-shot terminal color-scheme query.**

Reference: pi v1.4.0 (`4a080252`), `packages/tui/src/tui.ts` exposes color-scheme
report listeners and notification toggles, but no `CSI ? 996 n` query; the
interactive `theme-controller.ts` subscribes to reports and requests terminal
colors. Pier retains `Renderer.QueryTerminalColorScheme` and
`DetectTerminalThemeForAuto` as compatibility helpers that send `CSI ? 996 n`
and expect `CSI ? 997`; no first-party runtime caller uses these helpers. The
active theme controller instead follows the upstream report/OSC-color path.

Tests: `TestRendererTerminalQueries` and `TestTerminalDetection` cover the
compatibility helpers.

### D308

**Nested tool-call execution is absent; its session record is retained.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/nested-tool-calls.ts` and `agent-session.ts`
execute nested tools through the agent pipeline, emit parent-linked events, and
record nested usage on the caller's result. Pier keeps the nested-call data
model and recorder for session compatibility, but has no nested-execution host.
The upstream entrypoints are `ctx.executeTool()` from codemode/extension tools,
which are out of scope in `AGENTS.md`; no first-party Go runtime caller uses the
recorder. This is a deliberate feature omission, not evidence of parity for
nested execution.

Tests: `TestNestedCallRecorder`, `TestNestedCallRecorderLimits`, and
`TestToolResultNestedCallsRoundTrip` cover the retained data-model surface.

### D309

**Programmatic structured tool output and output-schema validation are absent.**

Reference: pi v1.4.0 (`4a080252`), `packages/agent/src/types.ts` and
`agent-loop.ts` carry `structuredContent` through tool results and hooks;
`packages/coding-agent/src/core/tools/bash.ts` emits bounded machine-readable
Bash output using `OutputAccumulator.readFullOutput`. Built-in `read`/`bash`
output schemas and MCP/extension tools use this data for programmatic callers
such as codemode scripts. Pier's `AgentToolResult` and `AfterToolCallResult` do
not carry it, `AgentTool` has no `outputSchema`, and the output accumulator has
no full-output reader. This is an intentional omission of the codemode and
extension consumer surface excluded by `AGENTS.md`; text returned to the model
remains the supported contract. Do not claim parity for programmatic structured
tool results or output-schema validation.

### D310

**The positional output-accumulator constructor treats zero limits as defaults.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/tools/output-accumulator.ts` uses nullish
coalescing (`options.maxLines ?? DEFAULT_MAX_LINES` and the corresponding
`maxBytes` expression), so an explicit zero remains zero. Pier's
`NewOutputAccumulator(maxLines, maxBytes, prefix)` has positional integer
arguments and uses zero to mean “use defaults”; this preserves the established
Go constructor convention used by first-party shell callers, which pass zeros
to request normal limits. No first-party shell path requests a zero-byte or
zero-line display limit. The difference is confined to direct callers of this
Go constructor.

Reproducer: `TestOutputAccumulatorZeroLimitsUseGoDefaults` in
`coding/outputaccumulator_utf8_test.go` records the Go side; the upstream
constructor expression establishes the explicit-zero behavior.

### D311

**Tool-update listeners are serialized per Go tool invocation.**

Reference: pi v1.4.0 (`4a080252`), `packages/agent/src/agent-loop.ts`
invokes `emitToolExecutionUpdate` inline for each `onUpdate`, stores the
returned promises, and awaits them together with `Promise.all`. An asynchronous
upstream listener can therefore overlap later update listeners, although their
invocation order follows the tool's updates. Go's `AgentEventSink` is
synchronous; `toolUpdateDispatcher` drains one invocation's updates serially to
preserve the single-writer callback contract in `AGENTS.md`. This can add
latency when listeners block, but tool duration is captured before the update
drain. This is an intentional concurrency difference, not timing parity.

Reproducer: `TestToolUpdatesAreSerializedInCallOrder` in
`agent/tool_prepare_panic_test.go` covers Go serialization; a pinned Bun probe
with an async upstream listener observed five simultaneous callbacks.

### D312

**Listener subscription changes take effect on the next Go event.**

Reference: pi v1.4.0 (`4a080252`), `packages/agent/src/agent.ts`
`processEvents` iterates its live `Set`; if a listener unsubscribes a later
listener during dispatch, that listener is skipped for the current event (and
newly added listeners can be visited). Go snapshots listeners under `listenerMu`
before releasing the lock and invoking callbacks, so subscription changes apply
to subsequent events only. This is intentional to keep user code
outside the mutex and make concurrent subscribe/unsubscribe safe, per the
single-writer callback ownership rules in `AGENTS.md`.

Reproducer: `TestAgentUnsubscribeDuringDispatchUsesListenerSnapshot` in
`agent/agent_test.go` covers the Go contract; a pinned Bun probe showed the
upstream live-Set case invokes only the first listener.

The same snapshot contract applies to `mcp.Client.handleNotification` versus
`packages/mcp/src/client.ts` `handleNotification`, and to MCP transport events
versus `packages/mcp/src/transports/transport.ts`: an earlier callback can
unsubscribe a later one in upstream's live `Set`, while Go invokes the
already-admitted callback snapshot. This preserves the existing rule that user
callbacks never run while the notification or transport registry mutex is held.

Reproducers: `TestClientNotificationUnsubscribeDuringDispatchUsesSnapshot` in
`mcp/client_notification_dispatch_test.go` and
`TestTransportMessageUnsubscribeDuringDispatchUsesSnapshot` in
`mcp/transport_events_test.go`; pinned Node Set iteration invokes only the first
listener in each case.

### D313

**Context token totals follow JavaScript Number precision within Go's int range.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/utils/estimate.ts`
adds token usage and trailing estimates as JavaScript `Number`s. Go rounds
through `float64` for usage-field sums and context totals, so values above 2^53
match JavaScript's loss of integer precision rather than retaining exact int64
arithmetic. The public Go result uses `int`; values beyond that type's range
saturate at its bounds because the corresponding JavaScript number cannot be
represented. This boundary is limited to implausibly large reported usage.

Reproducers: `TestCalculateContextTokensUsesJSNumberRounding`,
`TestEstimateContextTokensUsesJSNumberAddition`, and
`TestCalculateContextTokensSaturatesBeyondGoInt` in `ai/estimate_test.go`.

### D315

**Read and ls cancellation cannot interrupt synchronous filesystem calls.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/tools/read.ts` and `ls.ts` install abort
listeners while awaiting filesystem operations, rejecting with
`Operation aborted` as soon as the signal fires. Go's `os.Stat`, MIME sniff,
`os.ReadFile`, and directory reads are synchronous and cannot be interrupted by
a context. The read and ls tools check cancellation after filesystem or
image-processing boundaries and before returning, preventing a canceled
operation from later resolving successfully. If a filesystem call is blocked,
however, the caller still waits for that call to return. Running these calls in
detached goroutines would leak work and is not done.

Reproducers: `TestReadToolReturnsAbortWhenCanceledDuringMimeSniff` in
`coding/read_cancel_unix_test.go` uses a FIFO to cancel during MIME detection,
then releases the read and checks that Go returns an abort rather than a late
success. `TestLsToolAbortsWhenCanceledDuringFilesystemWork` in
`coding/ls_cancel_test.go` covers cancellation after the ls entry check.

### D316

**Interactive session sharing and bug reporting are not ported.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/modes/interactive/session-share.ts`, `bug-report.ts`,
`interactive-mode.ts`, and `components/settings-selector.ts`. The port omits
`/share` and `/bug`; it does not upload session transcripts to Radius/GitHub
Gists or collect and deliver bug report bundles. The associated
`sessionShareDestination` setting row is also absent because neither destination
has an effect here. The live-service and sensitive-data consent flows are
outside this port. Local HTML export is a separate supported operation.

### D317

**Superseded by D364: tool validation now ports pi-schema.**

The v1.4.0 TypeBox subset documented here was replaced for v1.5.0. The old
validator ignored `pattern`, `format`, numeric and length bounds, and several
JSON Schema keywords. `TestValidateToolArgumentsEnforcesPattern` and the
pi-schema corpus keep the replacement honest.

### D318

**BuildInitialPrompt preserves the caller's message slice.**

Reference: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/cli/initial-message.ts` shifts the first positional
message from `parsed.messages` in place. Go's exported `BuildInitialPrompt`
returns the remaining messages in `InitialPrompt.Rest` without mutating the
input slice. This preserves caller ownership; the composed prompt and queued
messages are otherwise unchanged.

Reproducer: `TestBuildInitialPromptDoesNotMutateMessages` in
`coding/cliinitial_test.go`.

### D319

**Context estimates use -1 for a missing usage index.**

Reference: pi v1.4.0 (`4a080252`),
`packages/ai/src/utils/estimate.ts` and
`packages/coding-agent/src/core/compaction/compaction.ts` return `null` when
no assistant usage applies. Go exposes `LastUsageIndex` as an `int` and uses
`-1` for that case; valid indices remain unchanged. Callers should test for a
nonnegative index rather than serialize this Go field as the upstream result.

Reproducer: `TestEstimateContextTokensMatchesUpstream` in
`coding/estimate_parity_test.go` maps upstream's null golden value to the Go
`-1` sentinel.

### D320

**Diagnostic error names use Go types when no explicit name is available.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/utils/diagnostics.ts`
reads the JavaScript `Error.name` property. Go errors have no corresponding
field, so `ExtractDiagnosticError` uses an explicit `Namer` when provided and
the dynamic Go type otherwise. This makes diagnostic metadata language-specific
instead of always synthesizing the JavaScript default name `Error`.

Reproducer: `TestDiagnosticErrorUsesGoTypeNameWithoutNamer` in
`ai/diagnostics_test.go`.

### D321

**The shared Go session constructor also applies CLI scoped-model seeding.**

References: pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/sdk.ts` treats `scopedModels` as the model-cycle
scope and resolves an initial model separately; `packages/coding-agent/src/main.ts`
`buildSessionOptions` seeds a new CLI session from an in-scope settings default
or the first scoped model. Pier folds that CLI step into `CreateAgentSession`,
which is shared by the CLI and SDK. CLI behavior therefore matches the combined
upstream path, while a direct Go SDK call with a scope can select a model where
upstream `createAgentSession` alone would not.

Reproducer: `TestScopedModelsSeedTheModel` in `coding/scopedmodel_test.go`.

### D322

**The explicit low-bandwidth render mode is port-local.**

Pi v1.4.0's `packages/tui/src/tui-main-screen.ts` has no low-bandwidth
switch. Pier retains `tui.SetLowBandwidth` for explicit renderer clients: it
can trim trailing padding, omit unnecessary hyperlink resets and skip no-op
alt-screen frames. Interactive startup's `ConfigureLowBandwidth` always leaves
the mode disabled, including over SSH, matching pi's default.

Reproducers: `TestLowBandwidthDefaultsOffEvenOverSSH`,
`TestAltScreenLowBandwidthTrimsTrailingSpaces`,
`TestMainScreenLowBandwidthTrimsTrailingSpaces`, and
`TestAltScreenLowBandwidthSkipsNoOpFrame`.

### D323

**A failed post-compaction prompt restores every queued message exactly once.**

Reference: pi v1.4.0
`packages/coding-agent/src/modes/interactive/interactive-mode.ts`
`flushCompactionQueue`. Upstream registers the prompt rejection handler, then
`await`s the remaining messages into the session queue. The handler's
microtask usually runs after the first `followUp`/`steer` has already queued
that message, so `clearQueue` drops it and the restored copy is the only one;
with more than one remaining message, the later iterations run after the
clear and survive in both the session queue and the restored compaction
queue. Pier restores, returns, and leaves every message in the restored queue
only: the duplication in the multi-message microtask window is a timing
artifact, not observable behavior users can rely on, and it would display,
restore and send the same message twice.

Test: `TestCompactionQueueFailureDoesNotDuplicateQueuedMessages`.

### D324

**OAuth discovery normalizes path dot segments and keeps every candidate on the
issuer's origin.**

Reference: pi v1.4.0 `packages/mcp/src/oauth/discovery.ts`. The reference builds
its well-known URLs from WHATWG `URL.pathname`, which removes `.` and `..`
segments while parsing (including the `%2e` spellings), so an issuer such as
`https://as.example/a/../b` is discovered through
`/.well-known/oauth-authorization-server/b`. Go's `url.URL.EscapedPath` keeps
those segments, so `parseWHATWGURL` now applies the same normalization before
any consumer reads the path (`mcp/oauth/url.go`). That covers the protected
resource candidate, the authorization-server candidates and `SelectResource`'s
path comparison (`TestDiscoverAuthorizationServerMetadataNormalizesDotSegments`,
`TestBuildAuthorizationServerDiscoveryUrlsNormalizesDotSegments`,
`TestSelectResourceComparesNormalizedPaths`).

One reference quirk is intentionally not followed: for an issuer whose path
begins with `//`, the reference resolves its third candidate against the origin
as a protocol-relative path, so `https://h//x` probes host `x`. Pier keeps every
candidate on the issuer's origin; the first two candidates already agree. Test:
the `https://h//x` row in
`TestBuildAuthorizationServerDiscoveryUrlsNormalizesDotSegments`.

### D325

**Streaming JSON previews stop at the Go JSON decoder's nesting limit.**

Reference: pi v1.4.0 `packages/ai/src/utils/json-parse.ts`
`parseStreamingJson` falls back to `partial-json` and catches parser errors as
an empty object. Pier's final tool-argument parser uses Go `encoding/json`,
which rejects nesting deeper than 10,000 levels; before this bound, the
recursive streaming fallback accepted such a prefix and the Anthropic stream
reparsed it after every `input_json_delta`. A single 500 KB, depth-250,000
synthetic provider argument took 15.69 seconds to parse. The partial parser now
applies the Go decoder's 10,000-level bound and returns `{}` for deeper
streaming previews. Completed arguments were already rejected by the final
decoder. This intentionally changes only previews of arguments that cannot
pass final decoding, and avoids unbounded recursion/time on provider input.

Test: `TestParseStreamingJSONRejectsExcessiveNesting`.

### D326

**Federation token expiry saturates at int64 Unix-second bounds.**

Reference: pi v1.4.0 at `4a080252f06caf63a525d7873c5d94d3c23887e0` delegates OIDC
federation to the Anthropic SDK. Its TypeScript source
`node_modules/@anthropic-ai/sdk/src/lib/credentials/oidc-federation.ts`
computes `nowAsSeconds() + expiresIn` with finite JavaScript Number values;
`token-cache.ts` reuses a token whose expiry remains far in the future. Pier
stores this deadline as int64 Unix seconds. The federation conversion retains
ordinary values and fractional truncation, but saturates when the converted
lifetime or addition would exceed int64, preserving whether the token is future
or past instead of wrapping it into the opposite cache state.

Test: `TestFederationTokenCacheLargeExpiryStaysCached`.

### D327

**Federation refresh-threshold arithmetic saturates at int64 Unix-second bounds.**

Reference: the pinned Anthropic SDK source
`node_modules/@anthropic-ai/sdk/src/lib/credentials/token-cache.ts` compares a
cached token's remaining lifetime against the refresh thresholds before reuse.
Pier's `FederationTokenCache.RefreshBackoff` is a configurable int64 seconds
value, and adding an extreme positive backoff to `now` could wrap the threshold
into the past and make an expired token appear fresh. The comparison uses
saturating Unix-second addition, so a backoff that exceeds int64 forces a
refresh.

Test: `TestFederationTokenCacheLargeRefreshBackoffDoesNotReuseExpiredToken`.

### D328

**MCP OAuth token expiry saturates at int64 Unix-millisecond bounds.**

Reference: pi v1.4.0 `packages/mcp/src/oauth/provider.ts` computes
`Date.now() + expires_in * 1000` using JavaScript Numbers. Pier persists
`TokensExpireAt` as int64 Unix milliseconds. `SaveTokens` preserves ordinary
expiry arithmetic but saturates at int64 bounds when a finite lifetime cannot
be represented, keeping far-future and far-past tokens from wrapping across
zero.

Test: `TestMcpOAuthProviderSaturatesLargeTokenExpiry`.

### D329

**Remote-catalog freshness compares against a cutoff to avoid elapsed-time overflow.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/remote-catalog-provider.ts`
compares `Date.now() - checkedAt` against the refresh interval using JavaScript
Numbers. Pier persists `checkedAt` as int64 Unix milliseconds; subtracting an
extreme persisted timestamp can wrap and make stale catalog data appear fresh.
The equivalent strict cutoff comparison (`checkedAt > now - interval`) avoids
the overflow while preserving the interval boundary and future-timestamp behavior.

Test: `TestRemoteCatalogMinimumCheckedAtTriggersRefresh`.

### D330

**Positive MCP server timeouts remain active across int64 conversion bounds.**

Reference: pi v1.4.0 `packages/coding-agent/src/extensions/mcp/runtime.ts`
converts configured seconds to milliseconds, and `packages/mcp/src/client.ts`
passes positive finite values to `setTimeout`. Pier stores request timeouts as
int64 milliseconds. Conversion preserves ordinary truncation, clamps finite
positive sub-millisecond values to 1ms, and saturates finite oversized delays
so the existing JavaScript-timer compatibility clamp handles them. If the
seconds-to-milliseconds product is non-finite, the runtime passes an explicit
zero to preserve the upstream no-timer behavior instead of falling back to a
Go default.

Test: `TestMcpServerTimeoutMsClampsConfiguredSeconds`.

### D331

**Codex error reset estimates avoid int64 timestamp multiplication overflow.**

Reference: pi v1.4.0 `packages/ai/src/api/openai-codex-responses.ts` computes
reset minutes from `resets_at * 1000 - Date.now()` using JavaScript Numbers.
Pier preserves its existing int64 formatting for ordinary timestamps and uses
JavaScript-number arithmetic/stringification only when a positive reset time
exceeds int64 milliseconds, preventing far-future waits from wrapping to zero.

Test: `TestParseCodexErrorResponseLargeResetTimestampDoesNotWrap`.

### D332

**Finite far-future OAuth device-code seconds saturate at the Go `int` bound.**

Reference: pi v1.4.0 `packages/ai/src/auth/oauth/xai.ts` accepts finite positive
`expires_in` values and passes them to the device-code poller. Pier stores
provider expiry and interval seconds as `int`; direct float conversion can wrap
a large finite positive value to a negative number and expire the authorization
immediately. `oauthSecondsToInt` preserves ordinary truncation and saturates
out-of-range values, so a far-future expiry remains future and oversized polling
intervals still pass through the existing JavaScript timer clamp.

Test: `TestParseXaiDeviceCodeSaturatesLargeFiniteExpiry`.

### D333

**Mistral cached prompt tokens clamp before int64 conversion.**

Reference: pi v1.4.0 `packages/ai/src/api/mistral-conversations.ts` computes
`Math.min(promptTokens, Math.max(0, cachedTokens))` after accepting only finite
numbers. Pier stores the cached-token count as int64 and clamps the finite
provider number to the known prompt-token bound before conversion, so huge
values cannot wrap negative and become a false zero. Ordinary in-range
conversion continues to truncate as before.

Test: `TestMistralStreamClampsLargeCachedTokenUsage`.

### D334

**Finite Radius model numbers saturate at int64 representation bounds.**

Reference: pi v1.4.0 `packages/ai/src/providers/radius-config.ts` validates
`contextWindow` and `maxTokens` as numbers and preserves them in the model;
cost tiers likewise carry numeric `inputTokensAbove`. Pier stores these fields
as int64, so conversion saturates out-of-range values rather than wrapping
large positive catalog numbers negative. In-range values preserve truncation.

Test: `TestRadiusGatewayConfigSaturatesLargeNumericFields`.

### D335

**Finite HTTP idle timeout settings saturate at int64 milliseconds.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/http-dispatcher.ts`
accepts finite nonnegative JavaScript numbers and floors them. Pier stores the
setting as int64 milliseconds, so finite values beyond that representation
saturate at MaxInt64 rather than wrapping negative and being treated as a
negative/no-timer setting. Ordinary values retain truncation.

Test: `TestHTTPIdleTimeoutUsesJavaScriptNumberParsing`.

### D336

**Large finite models.json cache lifetimes and cost thresholds saturate to Go integer bounds.**

Reference: pi v1.4.0 `packages/ai/src/providers/model-schema.ts` accepts
positive numeric prompt-cache lifetimes and numeric cost-tier thresholds without
upper bounds; `packages/coding-agent/src/core/provider-composer.ts` carries
these values into models. Pier stores them as `int` seconds or int64 token
thresholds. Out-of-range finite values saturate instead of wrapping negative;
ordinary in-range values preserve truncation.

Test: `TestModelsJSONSaturatesLargePromptCacheAndCostNumbers`.

### D337

**Finite models.json context and max-token limits saturate at int64 bounds.**

Reference: pi v1.4.0 `packages/ai/src/providers/model-schema.ts` accepts numeric
`contextWindow` and `maxTokens`; the provider composer carries definition and
override values into models. Pier stores both as int64, so finite out-of-range
values saturate instead of wrapping negative. Ordinary in-range values preserve
truncation.

Test: `TestModelsJSONSaturatesLargeContextAndMaxTokenLimits`.

### D338

**Oversized finite fullscreen wheel settings saturate before the configured clamp.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/settings-manager.ts`
accepts finite numeric settings and clamps them to 1..100; the pinned
`packages/tui/src/wheel-scroll.ts` accepts finite wheel counts. Pier decodes the
JSON number into an int before SettingsManager applies that bound, so decoding
saturates out-of-range values. This preserves ordinary truncation and lets the
configured clamp produce the intended maximum.

Test: `TestSettingsFullscreenWheelScrollLines`.

### D339

**Skill-tree scanning uses a fixed pool rather than one goroutine per directory.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/skills.ts`
`loadSkillsFromDirInternal` recursively scans child directories synchronously.
Pier retains parallel directory reads but bounds the worker count at four, rather
than creating a goroutine for every discovered directory. The scan map is
assembled by the worker results and replayed depth-first, so skills and
diagnostics keep one deterministic order.

Each directory's entries are sorted by name before the walk. The earlier port
used the filesystem's native order on the belief that `readdirSync` returns it;
Node goes through libuv's `uv_fs_scandir`, which sorts. The difference only
showed in a differential run (M222): the same fixture came back alphabetical
from the reference (`alpha,beta,caps,...`) and in creation order from a raw
`ReadDir` (`alpha,beta,outer,caps,...`). Sorting matches the reference and keeps
the skill list in the prompt stable across filesystems.

Tests: `TestLoadSkillsFromDirBoundsWorkerGoroutines`,
`TestLoadSkillsFromDirVisitsEntriesInNameOrder`,
`TestLoadSkillsFromDirSortsEntriesByName`.

### D340

**Goal cards resolve an unknown phase to a neutral color instead of panicking.**

Goal cards render persisted `pi-goal` entries and `set_goal` tool details, whose
phase is not re-validated before rendering. `Theme.Fg` panics on an unknown
color name, so a session written by a different build (or corrupted data) killed
the TUI on its first frame even though `FoldGoal` already treats such a session
as corrupt and only warns. `internal/goal/render.go` now resolves phase colors
through one total helper that falls back to `muted`; known phases keep their
colors, the blocked-reason line keeps its `warning` fallback, and the get_goal
state line now shows an unknown phase as `muted` instead of `success`.

Tests: `TestGoalEntryRendererRendersUnknownPhase`,
`TestSetGoalResultRendersUnknownPhase`.

### D341

**Successful remote JSON bodies are capped before they are buffered.**

Reference: pi v1.4.0 (4a080252) parses classifier, MCP Streamable HTTP, and MCP
OAuth responses without a byte cap. Those documents are untrusted network input
from a provider, MCP server, or authorization server, and pier buffers them
before validating, so an endpoint could exhaust process memory. Pier reads them
through `internal/boundedio` and fails as a protocol error once the body exceeds
the limit: 1 MiB for classifier, OAuth, Vertex token and Radius gateway
config documents, 8 MiB for an OpenRouter image response, and 16 MiB for the
OpenRouter catalog and MCP JSON-RPC responses (the existing
`MaxMessageBytes` default, shared with the SSE and stdio paths). Error bodies and
SSE frames already had their own caps.

Tests: `TestOpenAIDecisionsRejectsOversizedResponseBody`,
`TestTypeSafeSystemOneRejectsOversizedResponseBody`,
`TestPersonalizeOpenRouterCatalogRejectsOversizedResponseBody`,
`TestVertexMetadataTokenRejectsOversizedResponseBody`,
`TestVertexADCTokenResponseOverTheLimitIsAProtocolError`,
`TestVertexADCTokenResponseCutOffSurfacesTheReadError`,
`TestStreamableHTTPRejectsOversizedJSONResponse`,
`TestDiscoverProtectedResourceMetadataRejectsOversizedBody`,
`TestExchangeAuthorizationCodeRejectsOversizedBody`,
`TestRegisterClientRejectsOversizedBody`,
`TestOAuthDocumentOverTheCapIsAProtocolError`,
`TestOAuthDocumentCutOffSurfacesTheReadError`,
`TestRadiusGatewayConfigOverTheCapIsAProtocolError`,
`TestOpenRouterImagesResponseOverTheCapIsAProtocolError`, plus the
`internal/boundedio` unit controls.

### D342

**Model, provider, and MCP text is stripped of terminal controls before it is
rendered.**

Reference: pi v1.4.0 (4a080252) `packages/coding-agent` renders assistant text,
thinking, and provider error text as-is; `packages/tui/src/tui.ts`
`applyLineResets` also passes CSI controls through, keeping only the D290 OSC
filter. A provider or model that returns `ESC[2J` therefore clears the user's
screen, and cursor/mode controls can do worse. Pier treats that text as data:
`tui.SanitizeUntrustedText` removes every escape sequence (CSI, OSC, DCS, SOS,
PM, APC, and single-character escapes) and every C0 control except newline and
tab (C1 characters are ordinary UTF-8 text and stay, matching the goal
renderers' whitespace parity), and it runs on:

- assistant text and thinking before Markdown rendering, assistant
  abort/error lines, chat errors and warnings, transcript status text,
  compaction error text, and the tree selector's error preview;
- the tool-call fields the cards interpolate: every string and key in the
  decoded arguments (tool names, paths, commands, patterns, file bodies) and
  the fallback/self renderer's tool name, while the tool still executes the raw
  arguments;
- session display text that other clients or models wrote: the tree selector's
  entries and tool-call previews, and compaction/branch summaries;
- goal objectives, blockers, and event bodies in the goal cards;
- model catalog IDs, display names and provider badges in both model selectors
  (including unavailable configured IDs), and the footer's model ID. Catalog
  identity and selection/search values stay raw; only display text is cleaned.
  Reference: v1.5.0 `f9a26365` components/model-selector.ts and
  scoped-models-selector.ts render these fields as-is. Reproducers:
  `TestModelSelectorCatalogFieldsCannotEmitTerminalControls`,
  `TestScopedModelSelectorCatalogFieldsCannotEmitTerminalControls`,
  `TestFooterCatalogIDCannotEmitTerminalControls`.

Renderer-generated styling, hyperlinks, and image escapes are unaffected
because they are applied after sanitizing. Print-mode stdout is deliberately
left verbatim: it is program output that may be piped, not a terminal render.

Tests: `TestAssistantTextCannotEmitTerminalControls`,
`TestAssistantThinkingCannotEmitTerminalControls`,
`TestAssistantErrorMessageCannotEmitTerminalControls`,
`TestChatErrorCannotEmitTerminalControls`,
`TestStatusCannotEmitTerminalControls`,
`TestToolCallArgumentsCannotEmitTerminalControls`,
`TestToolNameCannotEmitTerminalControls`,
`TestTreeToolCallPreviewCannotEmitTerminalControls`,
`TestTreeEntryTextCannotEmitTerminalControls`,
`TestSessionSummaryCannotEmitTerminalControls`,
`TestGoalCardCannotEmitTerminalControls`, and the `tui`
`SanitizeUntrustedText` unit table.

### D343

**ctrl+o expands the visible window synchronously and catches up off-screen.**

Reference: pi v1.4.0 (4a080252) `modes/interactive/interactive-mode.ts`
`setToolsExpanded` calls `setExpanded` on every chat child before the next paint.
Each call rebuilds that card's display and drops its render cache, so the frame
after ctrl+o re-renders the whole transcript: 137 ms measured for 600 bash cards
with 200-line outputs, against 0.23 ms for a cached paint. Pier applies the
expansion to the trailing `toolSweepWindowComponents` (8) cards and queues the
rest oldest-first, draining `toolSweepChunkComponents` (8) per loop beat through
`MaterializeToolChunk`; the status line still reports the new state on the
keypress, and the loop keeps beating while work remains. Off-screen cards keep
their previous rendering until their turn, which only a scroll into them could
observe during the ~1 s catch-up. This mirrors the existing ctrl+t thinking
sweep.

A pending sweep belongs to the transcript it started on. `QueueController`
outlives the chat children, so every transcript replacement (session switch,
branch navigation, compaction rebuild) goes through `StartupWiring.resetChat` or
`EventDispatcher.ResetSweeps` and calls `QueueController.ResetSweeps` before the
children are dropped; otherwise the loop would keep rebuilding discarded cards
and repaint the replacement transcript once per chunk (M77).

Tests: `TestSetToolsExpandedRebuildsOnlyTheVisibleWindow`,
`TestMaterializeToolChunkExpandsEveryDeferredCard`,
`TestMaterializeToolChunkIsBounded`,
`TestSetToolsExpandedRestartsPendingSweep`,
`TestSetToolsExpandedReportsStatusImmediately`,
`TestSetToolsExpandedNoOpKeepsDeferredWork`,
`TestToolCardExpansionReachesDeferredCards`,
`TestChatResetStopsBothSweeps`,
`TestWiringChatResetDropsPendingSweeps`.

### D344

**Goal prompt wrappers neutralize closing tags in the text they wrap.**

Goal objectives and blocker text come from `/goal` and from the model's
`set_goal`, and rounds re-inject the objective inside `<untrusted_objective>` to
tell the model it is data. Text containing `</untrusted_objective>` used to close
that block early, leaving its own following lines outside the wrapper and ahead
of the harness framing lines, so a prompt-injected turn could persist
instructions that read as harness text later. `escapeGoalText` now rewrites every
`</` in the embedded objective, blocker code, and blocker message to `&lt;/` in
`GoalRoundPrompt`, `PauseContext`, and `WrapupContext`; other markup is untouched
so an objective like `fix <b>` still reads normally. Stored entries and display
cards are unchanged.

Test: `TestGoalPromptWrappersCannotBeClosedByTheirContent`.

### D345

**Summarization prompts keep message data inside their tagged blocks.**

Reference: pi v1.4.0 (4a080252) `core/compaction/compaction.ts` and
`core/compaction/branch-summarization.ts` interpolate the serialized conversation
between `<conversation>` tags (and the chained summary between
`<previous-summary>` tags) without escaping. Message content comes from users,
models, and tool results, so text containing `</conversation>` closed the block
early and left its following lines outside it, ahead of the summarization
instructions; a chained summary could do the same and persist the payload across
compactions. Pier now rewrites every `</` in the conversation text and the
previous summary to `&lt;/` through `escapePromptDelimiters`/`conversationBlock`
in both summarizers. Other markup is unchanged, and the serialized conversation
itself still matches upstream byte for byte.

Tests: `TestConversationSummaryPromptCannotBeClosedByItsContent`,
`TestSerializeConversationMatchesUpstream`.

### D346

**Stored summaries are escaped when they are re-injected as context.**

Reference: pi v1.4.0 (4a080252) `core/messages.ts` defines
`COMPACTION_SUMMARY_PREFIX`/`BRANCH_SUMMARY_PREFIX` with `<summary>` tags and
`convertToLlm` concatenates the stored summary between them unescaped. A summary
is model output and can contain `</summary>` itself (the M71 fix stops the
summarizer prompt from being closed, not the model from emitting the text), so
every later request would end the summary block early and leave the trailing text
outside it, reading as harness context for the rest of the session. Pier escapes
`</` in the summary through `escapePromptDelimiters` for both custom roles. The
stored entry bytes are unchanged; only the converted context is escaped.

Test: `TestSummaryContextCannotCloseItsWrapper`.

### D347

**Session status reads reuse a projection cache.**

Reference: pi v1.4.0 `core/agent-session.ts` `getContextUsage` and
`core/session-manager.ts` `getBranch`/`getLatestCompactionEntry` recompute from
the parsed entries on every call, and the interactive footer invalidates on
every session event (each streaming update included). The Go port's
`GetContextUsage` re-projected the whole compaction window and re-estimated its
tokens, and `ProjectedEntries` re-walked it; measured at 40,000 entries:
9.6 ms per `GetContextUsage` and 4.6 ms per `ProjectedEntries`, paid per paint
while streaming. `SessionManager` now caches the resolved window and its token
estimate, plus the newest branch compaction, under the same `(leaf, entry count)`
key as `Projection`; entries are immutable after they are appended and the
slices are handed out with no spare capacity, so the cache cannot be written
through. Measured after: 416 ns per `GetContextUsage` and 30 ns per
`ProjectedEntries`. The cached values are the same the reference computes, which
the existing projection, estimate and compaction tests pin; the caches
themselves are covered by `TestProjectedWindowIsCachedPerVersion` and
`TestLatestCompactionIsCachedPerVersion`, and
`BenchmarkGetContextUsage`/`BenchmarkProjectedEntries`/`BenchmarkLatestCompaction`
keep the cost visible.

### D348

**Ignored single-character illegal runs are batched without changing token output.**

Reference: pi v1.5.0 (`f9a26365`)
`packages/coding-agent/src/utils/syntax-highlight.ts` uses highlight.js 10.7.3.
Its `core.js` `processLexeme` appends each ignored illegal to `modeBuffer`.
On the reproduced 8 KiB JSON input (`0x` repeated 4,096 times), Go's immutable
string concatenation and per-character matching cost about 20 ms/op and 24,700
allocations. Byte-buffer and source-span prototypes reached 4.8 and 3.9–4.6
ms/op but still lost to Node, so neither was retained.

`ignoredIllegalBatch` now coalesces consecutive ignored character-class
illegals into one source slice. A lazy, capture-free regex searches begin/end
rules once, consuming the real preceding rune before each potential token so
`^` and `\b` never see an artificial slice boundary. The ordinary matcher then
resolves rule priority, captures and callbacks at that position. Isolated
illegals, partial-resume scans, manual rules, zero-width and multi-character
illegal patterns retain the old scanner. The batched character count preserves
the port's existing iteration guard, including UTF-16 surrogate units, and
cannot consume a token past that guard. The per-mode `sync.Once` publishes an
immutable matcher, runs no user code and adds no UI ownership or worker handoff.

Same-process paired scanner measurements on an Intel N150 (three 100-iteration
runs, warmed) show 20.07–20.25 ms/op unbatched versus 1.27–1.47 ms/op batched
(medians 20.14 and 1.27), with allocations falling from about 24,700 to 30. Five
warmed pinned Node runs measured 1.88–1.99 ms/op (median 1.92), so the retained
fix beats the reference on the reproduced input. Representative JSON (7,424
bytes), valid JSON, normal JavaScript and Python showed no meaningful regression
in the paired runs. This is a bounded improvement, not a claim that every
language beats Node. Reproduce with `BenchmarkIgnoredIllegalScanner` and `node
internal/highlight/testdata/ignored-illegals/benchmark.mjs`.

Tests: the red-first `TestLongInvalidJSONNumberAllocationBudget` (24,654
allocations before, budget 256), `TestLongInvalidJSONNumberMatchesReference`,
`TestIgnoredIllegalScannerMatchesReference` (454 Node-generated fixtures),
`TestIgnoredIllegalBatchPreservesBuiltinScanner`,
`TestIgnoredIllegalBatchPreservesIterationBudget`, and
`FuzzIgnoredIllegalBatch` cover output, callbacks, ordering, Unicode, nested and
continued sublanguages, fallbacks and the original scanner.

### D349

**Word diffs trim like JavaScript and tokenize without per-token regexes.**

Reference: pi v1.5.0 (`f9a26365`) `components/diff.ts` uses jsdiff 8.0.4
`word.js`. Its `equals` trims JavaScript `\s`, and its
`tokenizeIncludingWhitespace` splits into extended-word runs, whitespace runs
and single other characters, then copies whitespace onto neighboring tokens.
The port trimmed with Go's `strings.TrimSpace`, so a U+FEFF-prefixed word was
removed and re-added instead of compared equal, and it matched a regex for
every token part. The equal function now uses the port's JavaScript whitespace
predicate (`tui.IsWhitespaceRune`, allocation-free), and the tokenizer scans
runes and records source ranges. Tokens may overlap, exactly as jsdiff's
duplicated whitespace does; `join` strips the later copy. The rendered output
is unchanged, pinned by Node-generated diff goldens that now include FEFF, NEL,
NBSP, punctuation, leading whitespace, emoji, accents and a soft hyphen.

Measured on a 2,000-word line with two changed words (warmed, 50 iterations,
five runs): 6.27–6.38 ms and 8,160 objects before; 0.55–0.64 ms and 138 objects
after; pinned jsdiff 0.92–1.00 ms. The allocation regression budget is 1,000
objects (`TestDiffWordTokenizerAllocationBudget`), and
`FuzzWordTokenizeRoundTrip` checks that every split can be joined back to the
source. `BenchmarkDiffWordsLarge` keeps the cost visible.

### D350

**JSON canonicalization scans bytes instead of decoder tokens.**

Reference: pi v1.5.0 `JSON.stringify` produces JavaScript key order, escaping
and number text; `ai.MarshalJSON` canonicalizes Go output to those bytes. The
decoder-based pass (`json.Decoder.Token` recursively, plus a fresh encoder per
string) cost 55 ms and 456k allocations on a 754 KB conversation body, and
`MarshalJSON` took about 90–100 ms end to end. The replacement parses one value
with a byte scanner into a single output buffer, decodes strings with the same
lone-surrogate and invalid-UTF-8 replacement rules, keeps `JSON.stringify`'s
control-character escapes and literal U+2028/U+2029, and reuses
`javascriptJSONNumber`/`appendJSObjectKey` for number text and index-key order.
Objects still buffer their fields, since keys are reordered and deduplicated;
arrays append directly. `jsArrayIndexKey` validates digits before `ParseUint`,
which allocated a syntax error for every ordinary key.

Invalid input falls back to the decoder path, so the standard diagnostic
wording callers report is unchanged (D269). The scanner enforces the decoder's
10,000-level nesting limit; without it, hostile deeply nested input recursed
until the stack failed instead of reporting an error. The scanner's output is
pinned byte-for-byte against that decoder oracle by
`TestCanonicalizeMatchesReference` and `FuzzCanonicalizeMatchesReference` (614k
inputs in a 30-second run), and `TestCanonicalizeRejectsExcessiveNesting` pins
the boundary.

`MarshalJSON` also emits strings, booleans, integers and valid
`json.RawMessage` directly instead of encoding and re-parsing them, which the
nested message marshalers do constantly. Measured on the same body: canonical
pass 55 → 11 ms and 456k → 148k allocations; `MarshalJSON` about 33–36 ms and
288k allocations (benchmarks
`BenchmarkCanonicalizeLargeJSON`, `BenchmarkMarshalConversation`). Content
lists now append their canonical blocks directly instead of round-tripping a
`[]json.RawMessage`, and the common text/thinking/image/tool-call blocks are
emitted field by field rather than encoded and spliced. A nil block pointer
encodes as `null` instead of the previously malformed splice.

A validator first recognizes input that the encoder already produced in
canonical form (no insignificant whitespace, canonical escapes and number text,
index keys first, no duplicates, valid UTF-8) and returns an owned copy without
reparsing; noncanonical or invalid input takes the full parser, so diagnostics
are unchanged. Small objects check duplicate keys in a fixed array; wider ones
switch to a set so the check stays linear on hostile input. The conversation
benchmark is now 11–12 ms and 24k allocations (from 33–38 ms/288k); the
normalized fast path also covers whole request bodies whose raw tool arguments
are already canonical. A 790 KB already-normalized body canonicalizes in 2.5 ms
(`BenchmarkCanonicalizeNormalizedJSON`) versus 11–12 ms through the parser.

M143 is closed on a fair, realistic baseline rather than the original minimal
4k body. `BenchmarkMarshalConversationRealistic` builds user/assistant pairs
with model metadata, usage, cost and timestamps. Node `JSON.stringify` of the
same canonical bytes (verified byte-identical by
`ai/testdata/marshal-conversation/benchmark.mjs`) measures 0.045 ms for 100
messages, 0.223 ms for 500, and 1.97–2.14 ms for 4,000. Pier measures
0.29–0.37, 1.43–1.88, and 13.4–15.2 ms: 6.3–8.4x Node throughout, not the
earlier impression of a pathological gap. The target is to stay within 10x
Node and under 2 ms for a realistic 500-message body; the remaining cost is
Go's reflection encoder plus byte-parity canonicalization, which is accepted
for realistic sizes. `BenchmarkMarshalConversation` keeps the older minimal
4k body visible.

### D351

**A rejected-candidate run stops the dispatch scan after a fixed budget.**

Reference: pi v1.5.0 runs highlight.js 10.7.3, whose sticky alternation rejects
a long run with no matching rule in one engine pass. The Go port's per-byte
dispatch retries an anchored submatcher at each candidate; every retry can scan
the remaining tail before failing, so a rejected run was quadratic. Measured
for `0x` repeated under JavaScript: 8 KiB took 0.40 s and 32 KiB 13.7 s, versus
0.46 s in Node at 32 KiB. After 256 consecutive rejected candidates the port
now returns no match, and the engine appends the remainder through the normal
keyword pass (plain text for the rejected run). The same run now takes 23 ms at
8 KiB and 0.22 s at 32 KiB, faster than the reference. Ordinary inputs find a
match long before the budget; all generated goldens, the corpus, and the paired
JavaScript/JSON/Python benchmarks are unchanged.

The difference is bounded and only affects input past 256 rejected candidate
bytes: rule highlighting resumes only at the next engine scan, while keywords
still apply to the appended text. `TestRejectedCandidateRunStopsAtBudget` pins
the short-run reference behavior and the budget cutoff, and
`BenchmarkHighlightRejectedRun` keeps the cost visible; this is the same
responsiveness trade as the existing iteration cap.

### D352

**The Responses input array is assembled canonically instead of re-marshaled per item.**

Reference: pi v1.5.0 `convertResponsesMessages` builds plain objects and
stringifies once. The Go port built each input item with
`mustMarshalJSON(orderKeys(...))`, whose `orderedMap` marshaled every key and
value through `MarshalJSON`, so a 40,000-message transcript ran 600k
canonicalizations and 7.9M allocations (550 ms, 368 MB) before the final array
was canonicalized again. `orderedMap` now keeps parallel key/value slices and
emits canonical bytes directly (`appendCanonicalJSONString`, integer/raw/nested
map paths); `convertResponsesMessages` appends those ordered maps and the
assistant converter's items, canonicalizing reasoning signatures on the way,
and emits the array once without a second full canonicalization pass.

Measured on the same transcript: 550 ms and 368 MB with 7.9M allocations →
82 ms and 78 MB with 0.79M allocations. Output stays canonical, pinned by
`TestConvertResponsesMessagesOutputIsCanonical`, the existing Responses input
and wire parity tests, and a 500-turn allocation budget; the benchmark is
`BenchmarkConvertResponsesMessages`.

### D353

**Streamed tool arguments render a large preview at most once a frame.**

Reference: pi v1.5.0 `utils/streaming-json.ts` parses each provider append
incrementally and shares completed values between snapshots, so a snapshot
costs the open path. Pier ports the parser's rules exactly (`ai/streaming_json.go`)
but writes the canonical `JSON.stringify` bytes of the root object into an
append-only buffer as it reads. Two cases `JSON.parse` orders differently
from the text (a duplicate key keeps its first position; array-index keys sort
first, ascending) turn that object unordered: its members stay in arrival
order in the buffer, a preview emits them in JS order, and closing the object
rewrites its bytes in canonical order once. A preview is that buffer plus the
closing tail of the open path.
Contiguous `json.RawMessage` previews cannot share bytes (each one ends where
the next continues), so every preview copies the buffer: previews up to 1 KiB
refresh at every delta, larger ones at most once per 16 ms frame, and the
preview is exact once the root object closes. The reference instead spends
per-append copy credit and lets a large open container lag. So that a stream
pausing mid-call still shows every byte received, a toolcall_delta whose
preview was held back carries a refresh handle: when the agent's
`NextSnapshot` sees no event within a frame, it renders the current preview
under the arguments' lock (only if no later text awaits its own event) and
delivers it as a toolcall_delta with an empty delta. JSON and RPC output
therefore carry those extra empty-delta updates during pauses.

When the complete text is one strict JSON object, the buffer already is the
final value, so `streamingArguments.Finish` returns it without the full
`ParseToolCallArguments` parse; any tolerated or malformed input, or a lone
surrogate, takes that parse, which also reports malformed arguments.

Measured in 16-byte deltas including the finish, `BenchmarkStreamingArgumentsLarge`
against the reference's `ToolCallArgumentsStream` (append + finish) under Node
on the same machine: a 100 KiB write took 11.5 ms and 12.1 MB in the previous
byte-doubling schedule, 1.2 ms in the reference, and 0.81 ms and 1.1 MB now; a
200-edit call (19 KB) 7.9 ms, 0.80 ms and 0.30 ms; a 55-byte bash call 19 µs,
3.3 µs and 1.35 µs. Byte-credit schedules were measured first: matching the
reference's freshness copied 30 MB for the 100 KiB write (10 ms), and none that
beat it kept the preview within a few KiB.

Keys out of order (A424) were first spliced into the buffer by copying every
member after the insertion point, and placed by a linear rescan of the frame,
so `BenchmarkStreamingArgumentsKeyShapes` (2,000 members, about 22 KB) took
1.1 ms for named keys, 12.6 ms for ascending index keys and 123 ms for
descending ones, behind the reference's 132, 76 and 106 ms; at 4,000 members
descending took 522 ms against the reference's 255. Placing an in-order key
in O(1) and keeping an out-of-order object unordered brings them to 1.2, 1.2
and 1.4 ms (1.8, 2.4 and 3.2 ms at 4,000), with the named and large-call
benchmarks unchanged.

The OpenAI completions, Responses, Anthropic, Bedrock, Mistral, and
pi-messages adapters and the agent proxy share the stream. Tests:
`TestStreamingArgumentsPreviewIsThrottled` pins the frame schedule and exact
finish; `TestStreamingArgumentsSmallCallUpdatesEveryDelta` pins the per-delta
window; `TestStreamingArgumentsFinishMatchesFullParse` and
`FuzzStreamingArguments` pin `Finish` to the full parse;
`TestStreamingJSONUpstreamCases` carries the reference's test cases;
`TestStreamingJSONOutOfOrderKeysMatchReference` pins out-of-order previews to
the reference's and `TestStreamingArgumentsDescendingIndexKeysStayLinear` keeps
them linear;
`TestProxyToolArgumentsPreviewIsThrottled` covers the proxy;
`TestStreamingArgumentsPauseRefreshesThrottledPreview`,
`TestStreamingArgumentsPauseRefreshWaitsForUndeliveredText` and
`TestEventStreamNextWithinNeverLosesAnEvent` pin the pause refresh;
`BenchmarkStreamingArgumentsLarge` keeps the cost visible.

### D354

**Streaming markdown reuses its block-cache arrays.**

Reference: pi v1.5.0 `components/markdown.ts` keeps the stable token prefix and
final lines across `setText` appends. The port had that cache but rebuilt the
`finals` and `fullKeys` slices and copied every block key on each render, so a
long streaming message did quadratic allocation; it also copied the whole
source through `strings.ReplaceAll` even when it had no tabs. The port now grows
the two cache slices amortized, overwrites only the tail, skips the
tab-normalization copy when no tab is present, and flattens the finals directly
into the result instead of through an intermediate `contentLines`.

Measured on growing paragraph appends (no tabs): 200 appends 11 ms/13.6 MB →
3.4–5.5 ms/3.0 MB; 1,000 appends 169 ms/306 MB → 45–52 ms/36 MB; 2,000 appends
682 ms/1.22 GB → 163 ms/124 MB. The remaining growth is the returned line slice,
which callers receive whole. `TestMarkdownStreamRenderReusesCacheSlices` pins
the backing arrays; `BenchmarkMarkdownStreamAppendLong` keeps the scale
visible.

### D355

**Bedrock text and reasoning stream through builders.**

Reference: pi v1.5.0 `api/bedrock-converse-stream.ts` accumulates text with
JavaScript string concatenation; V8 represents the growing value as a rope, so
each delta is cheap even though every partial event exposes the full text.
`block.text += delta` in the port copied the whole prefix per delta (Bedrock
sends small content deltas), and the partial snapshot then copied it again.
The port keeps `strings.Builder` values on the block and uses `String()` for
snapshots:
appending is amortized and later appends never change bytes already covered by
an earlier snapshot's length.

Measured for 100 KiB of text in 16-byte deltas: 65.4 ms and 348 MB allocated
before, 0.46 ms and 0.77 MB after (224 MB/s; allocations are mostly the content
slice rebuilt for each partial). `BenchmarkBedrockTextStream` keeps this
visible; the existing Bedrock stream parity tests pin the emitted snapshots.

### D356

**A markdown transform does not disable incremental lexing.**

Reference: pi v1.5.0 `components/markdown.ts` applies `options.transform` before
its token cache and reuses rendered blocks by token, so a transform is not
grounds for discarding the previous parse. The port's `canReuseLex` rejected
every transformed source, and the assistant path always installs a transform;
each streaming delta therefore re-lexed and re-rendered the whole message. The
port keeps the transform and lets the existing prefix check decide: if the new
transformed source starts with the cached transformed source, the cached block
window is still valid. Non-monotonic rewrites fail the prefix check and re-lex.

Measured on 1,000 growing paragraph appends with the production-style identity
transform: 2.47 s and 1.63 GB allocated before, 37–41 ms and 36 MB after.
`TestMarkdownTransformStreamingReusesStablePrefix` pins the tail-only render;
`TestMarkdownTransformStreamingMatchesFreshRender` checks identity, prefixing,
replacement, and last-line-uppercasing transforms at every prefix;
`FuzzMarkdownTransformIncrementalMatchesFull` covers arbitrary sources;
`BenchmarkMarkdownStreamAppendTransform` keeps the scale visible. When no
transformer is installed, the assistant leaves `Transform` nil instead of
building an identity closure on every update (100 KiB stream: 83k → 77k
allocations), which is behaviorally identical.

### D357

**Streaming assistant text caches its clean sanitization prefix.**

Reference: pi v1.5.0 passes assistant text to the renderer without terminal
sanitization; pier treats remote text as data (D342). The sanitizer's
`HasTerminalControl` scanned the whole message on every streaming update, which
made a 100 KiB reply arriving in 16-byte deltas take 226 ms. A per-block
`StreamingSanitizer` fixes that: when the new text starts with the previous text
and the appended suffix has no control bytes, reuse the clean-prefix result;
controls, rewrites, or a previously dirty text fall back to the full scan. The
sanitized output is identical.

`TestStreamingSanitizerMatchesFullSanitize` and
`FuzzStreamingSanitizerMatchesFull` check every prefix against the full scan;
`BenchmarkAssistantStream100KB` measures the end-to-end update plus paint, now
9.2 ms with the same allocations.

### D358

**A single thinking block skips the join copy.**

Reference: pi v1.5.0 `assistant-message.ts` joins thinking runs with
`blocks.join("\n\n")`; JavaScript returns the single element without building
a new string. The port's `joinThinkingBlocks` always wrote the block into a
`strings.Builder`, copying the whole thinking text on every streaming delta. It
now returns the sole block directly.

Measured end-to-end with 100 KiB of thinking in 16-byte deltas: 148 ms and
359 MB allocated before, 10.3 ms and 11 MB after.
`TestJoinThinkingBlocksSingleBlockDoesNotCopy` pins zero allocations;
`BenchmarkAssistantThinking100KB` keeps the scale visible.

### D359

**Canonicalization rejects trailing non-whitespace.**

Reference: pi v1.5.0 parses provider and persisted bodies with `JSON.parse`,
which rejects text after the first value. The byte scanner and its
`json.Decoder` fallback both stopped after the first value, so `1 2`,
`{} trailing` and `truex` were silently truncated (the `json.RawMessage`
marshal path already guarded with `json.Valid`). After parsing one value the
scanner now allows only trailing whitespace; the fallback decodes once with
`UseNumber` (JSON.parse accepts values beyond float64, so `1e1000` must still
become `null`) and requires a second decode to return EOF, reporting the
offending character.

`TestCanonicalizeRejectsTrailingText` pins the rejection, trailing whitespace
acceptance, and `1e1000`; the differential fuzz oracle now shares the strict
behavior.

### D360

**Ordered JSON object decoding rejects trailing data.**

Reference: pi v1.5.0 parses provider/tool JSON fragments with `JSON.parse`,
which rejects text after the object. `decodeOrderedJSONFields` (shared by
OpenAI, Bedrock, Mistral, Google, constrained-sampling and system-one paths)
stopped after the last member, so `{"a":1}]` was accepted and the suffix
ignored. The decoder now requires the closing brace and then EOF, allowing only
trailing whitespace.

`TestDecodeOrderedJSONFieldsRejectsTrailing` covers trailing text, a second
object, a stray delimiter, and trailing whitespace; existing provider goldens
remain byte-identical.

### D361

**Tool-argument updates skip unchanged raw bytes.**

Reference: pi v1.5.0 streams tool arguments through `ToolCallArgumentsStream`;
the UI still receives a delta event for every provider chunk. The port's
`UpdateArgs` marked the display pending unconditionally, so each render
re-decoded and rebuilt the full argument preview even when the throttled
preview bytes had not changed. It now compares `json.RawMessage`/`[]byte`
arguments and returns before invalidating when they are identical.

A repeated 1 MiB write argument rendered 100 times: 6.53 s and 7.56 GB allocated
before, 86 ms and 70 MB after. The equality check preserves every changed
update; `TestUpdateArgsSkipsUnchangedRawMessage` covers both paths and
`BenchmarkToolArgsRepeat` keeps the scale visible.

### D362

**Assistant streaming reuses its block-component map.**

Every assistant update built a fresh `map[assistantBlockKey]tui.Component` for
the current block set, even though the previous map was only read to reuse
Markdown instances. The port now alternates between two maps and calls `clear`
on the older one; the old map's components stay referenced for one generation so
lookups remain valid, then are released. No output changes.

Measured on the 100 KiB stream: 77k → 64k allocations and 9–10 ms → 6.0–6.7 ms;
thinking: 102k → 90k allocations and 10.7–13 ms → 9.1–9.6 ms.
`TestAssistantBlockComponentMapsAreReused` pins the alternation;
`BenchmarkAssistantStream100KB` and `BenchmarkAssistantThinking100KB` keep the
scale visible.

### D363

**Each lone low-surrogate escape becomes its own replacement character.**

Reference: JavaScript `JSON.parse` keeps every UTF-16 code unit, so
`"\udc00\udc00"` is two lone low surrogates; Go strings cannot represent
them, and D253 replaces each with U+FFFD. The canonical scanner treated any
surrogate as the high half of a pair, so two consecutive low escapes collapsed
into one U+FFFD. Only values in `D800–DBFF` may start a pair; a low surrogate is
emitted as U+FFFD and scanning continues.

`TestMarshalJSONReplacesLoneSurrogateEscapes` covers consecutive low/lows and
low/high; the canonical differential fuzz preserves two replacements, and
`TestParseToolCallArgumentsMatchesReference` normalizes the reference's golden
the same way for tool arguments.

### D364

**Tool validation ports pi-schema.**

Reference: pi v1.5.0 (`f9a26365`), `packages/ai/src/utils/validation.ts` and
`packages/schema/src/{compiler,checks,coerce,explain,resolver}.ts`. Tool
schemas are parsed into ordered nodes, local `$ref`/`$id`/anchors are resolved,
all three coercion passes run (optional-null removal, TypeBox conversions,
primitive coercion), and failures use pi-schema messages and dotted paths.
The corpus in `ai/testdata/validation-pi/` holds the 146 serializable upstream
probes, including recursive refs, pattern/format/length bounds, dependencies,
composed schemas, `unevaluatedProperties`, and the huge-enum error cutoff.

`unevaluatedProperties` and `unevaluatedItems` use the same annotation merge as
the reference: prefix/items/contains, allOf/anyOf/oneOf, if/then/else,
dependent schemas and refs mark what they evaluated. Every standard format in
`packages/schema/src/formats` is checked. `$dynamicRef` resolves same-resource
anchors and pointers like `$ref`; full `$id`-resource dynamic-scope selection is
not ported. External `$ref` documents stay unresolved because
`validation.ts` passes no `schemas` map, which matches the reference.

Deliberate boundary: Go's RE2 lacks lookbehind and backreferences, so a valid
ECMAScript pattern using them is left unconstrained. A malformed pattern such as
`[` still fails schema compilation instead of being ignored.

Tests: `TestValidateToolArgumentsPiSchemaCorpus` replays the corpus;
`TestValidateToolArgumentsLeavesRE2UnsupportedPatternsUnconstrained` pins the
lookbehind and backreference boundary (each pattern rejects the value under
Node's `u`-flag RegExp) against a supported control pattern, and
`TestValidateToolArgumentsCompilesPiSchema` the malformed-pattern failure.

### D365

**The v1.5.0 model catalog includes GPT-6.1 Terra.**

Reference: pi v1.5.0 `f9a26365`, `packages/ai/catalog/generate.ts` and
`catalog/providers/openai*.ts`. The checked-in catalog had been generated
before Terra was added to the catalog source. Regenerating upstream data with
`node catalog/generate.ts --strict --data-only` and then running
`scripts/gen_catalog.py` adds Terra for OpenAI, Azure
OpenAI Responses, Codex, Bedrock, OpenRouter and gateway providers. The OpenAI
entry uses the official $1/$5 pricing with a 272k context window; Codex uses a
500k window. `TestCatalogIncludesGPT61Terra` pins the three first-party
surfaces. The image and classifier catalogs were regenerated from the same
data-only output; `TestImageCatalogCarriesTheV150Refresh` pins the refreshed
OpenRouter image entry and the existing flux model. The generator commands in
`AGENTS.md` and the scripts now name `catalog/generate.ts`.

### D366

**Finite Mistral usage saturates at the int64 representation boundary.**

Reference: pi v1.5.0 (`f9a26365`),
`packages/ai/src/api/mistral-conversations.ts` `consumeChatStream` preserves
provider token counts as JavaScript Numbers. Pier stores them as int64, so
finite out-of-range prompt, completion and total counts saturate instead of
wrapping negative or losing input usage. This extends the representation rule
in D313/D334 to this parser. Ordinary in-range conversion still truncates.

Reproducer: `TestMistralStreamSaturatesFiniteUsageBeyondInt64` serves finite
`1e308` usage fields through the HTTP streaming boundary. Before the fix, input
became zero and output/total became MinInt64.

### D367

**Provider usage arithmetic saturates at the int64 boundary.**

Reference: pi v1.5.0 (`f9a26365`), `packages/ai/src/api/`:
`openai-completions.ts` `parseChunkUsage`, `anthropic-messages.ts` usage updates,
`mistral-conversations.ts` `consumeChatStream`, the Google adapters' usage
folding, `openai-responses-shared.ts` `processResponsesStream`,
`bedrock-converse-stream.ts` `handleMetadata`, `classifier-shared.ts`
`parseClassifierUsage`, and `openrouter-images.ts` `parseUsage` use JavaScript
Number arithmetic. Pier performs their token additions and multi-cache
subtraction in float space, then saturates results outside its int64 usage
representation instead of wrapping. Ordinary token counts are unchanged, and
larger in-range results follow JavaScript rounding, as in D313.

Reproducers: `TestParseChunkUsageDoesNotWrapHugeCacheOrTotalCounts` and
`ai/provider_usage_arithmetic_test.go` supply representable `6e18` counts whose
sums exceeded int64, and cache counts whose subtraction previously wrapped
into false positive input usage. The streaming tests cover initial/delta totals,
Mistral's missing-total fallback, Google thoughts, Responses cache subtraction,
and Bedrock's fallback total and repeated one-hour cache details.
`TestOpenRouterImagesUsageSaturatesHugeFiniteCounts` and
`TestClassifierUsageSaturatesHugeFiniteCounts` cover the non-streaming image and
classifier parsers.

### D368

**Goal token totals saturate rather than losing budget accounting.**

D223's native goal feature has no counterpart in pinned pi v1.5.0 (`f9a26365`).
Its token accounting follows the positive-usage accumulation of the originally
ported extension. The pinned `packages/coding-agent/src/core/usage-totals.ts`
uses JavaScript Number addition, which cannot wrap positive counts negative.
Pier's per-run goal total and live/replayed cumulative totals saturate at
MaxInt64 instead. Negative reports remain ignored. Ordinary counts, budget
limits, activation and entry/message shapes are unchanged. D253 retains the
native integer spelling on disk, including a saturated round's usage.

Reproducers: `TestGoalRoundUsageDoesNotOverflowBeforeBudgetCheck` restores a
valid token budget and supplies two positive `6e18` assistant records. The
previous wrapped round total was discarded as negative, bypassing the budget.
`TestGoalMachineDoesNotWrapCumulativeRoundUsage` and
`TestFoldGoalDoesNotWrapCumulativeRoundUsage` pin live/replayed round sums.
The controller regression also replays its emitted durable entries.

### D369

**Session usage totals saturate instead of wrapping or dropping model buckets.**

Reference: pi v1.5.0 (`f9a26365`),
`packages/coding-agent/src/core/usage-totals.ts` and `agent-session.ts`
`getSessionStats` add usage with JavaScript Numbers. Pier's shared usage fold
saturates each int64 field addition at its representation bounds. The final
four-field total uses Number arithmetic and saturates at int64 bounds too.
Session statistics and cached/reference cost breakdowns use that same fold.
The session-info and compaction-notice displays also sum through Number-space
helpers instead of wrapping the saturated columns. Ordinary counts, cost
arithmetic, bucket ordering/filtering and persistence layouts are unchanged.
Native integer spellings remain governed by D253.

Reproducers: `TestSessionStatsDoNotWrapPositiveUsage` covers huge cross-field
sums, repeated column additions and ordinary controls.
`TestUsageCostBreakdownDoesNotDropHugeTokenBuckets` verifies both paths retain
a positive-token model bucket even with zero cost, rather than filtering it out
after its total wraps negative. `TestSessionInfoSaturatesHugeTokenColumns` and
`TestCompactionCostNoticeSaturatesHugeUsage` cover the display sums.

### D370

**Cache-miss intermediates use Number arithmetic, with bounded public counters.**

Reference: pi v1.5.0 (`f9a26365`),
`packages/coding-agent/src/core/cache-stats.ts` `detectMiss`, `asPreviousRequest`
and `scan`. Private prompt totals, paid-token rates and sticky cache activity
use float64 Number arithmetic so an intermediate sum cannot wrap or lose a
miss before subtraction. Public int64 missed-token counts and their cumulative
totals saturate at representation bounds instead. Missed cost is computed from
the unbounded Number-space miss, not the saturated display count. The existing
candidate/full-scan paths, noise floor, price lookup and persistence are intact.

Reproducers in `coding/cache_usage_boundary_test.go` cover a wide prompt with
an in-range miss, a miss beyond int64, paid-rate overflow, sticky cache activity
and cumulative missed-token totals, through both cached and reference APIs:
`TestCacheMissDoesNotLoseHugePositivePromptUsage`,
`TestCacheMissKeepsPaidRateWhenTokenSumExceedsInt64`,
`TestCacheMissRemembersHugePositiveCacheActivity` and
`TestCacheWasteDoesNotWrapPositiveMissTotals`.
Ordinary cache-stat goldens remain unchanged.

### D371

**Failed manual imports clean up only their own incomplete copy.**

Reference: pi v1.5.0 (`f9a26365`),
`packages/coding-agent/src/core/agent-session-runtime.ts` `importFromJsonl`
delegates to Node `copyFileSync` with `COPYFILE_EXCL`. Pier's private-mode
copy uses Go file I/O (D289), which can fail after creating the destination.
On copy/close error it now attempts to remove only that newly created file,
retaining the original error and never removing a preexisting destination.
Successful copies whose later session validation fails remain unchanged.

Reproducer: `TestImportCopyFailureDoesNotLeaveAPartialSession` drives a
rejected directory read through `/import`, with fresh/existing-name controls.
Previously each failure left an empty numbered session file. This is a Go
error-path guarantee, not matching-input Node parity: local Node v26.10.0
accepts this directory input as an empty regular copy rather than throwing.
Go retains its existing import error instead of adopting that acceptance.

### D372

**Renaming the active session reuses its live manager and leaf chain.**

Reference: pi v1.5.0 (`f9a26365`),
`packages/coding-agent/src/modes/interactive/interactive-mode.ts` opens a
second `SessionManager` for the rename. Its synchronous `open` + `appendSessionInfo`
keeps that safe in the reference's single thread. Pier's session writes run on
the UI write queue, so opening a second manager while the active one still has
writes queued would read a stale leaf: the `session_info` entry can then become
the file's leaf while the newest message becomes a side branch. When the path
is the active session, the rename now appends through the active manager,
sharing its leaf chain and write queue. Other sessions keep the opened-manager
path. The visible entry shape and name sanitization are unchanged.

Reproducer: `TestRenamingCurrentSessionKeepsItsLatestLeaf` blocks the session
write queue, queues a message, renames, and reopens the file. Previously the
queued message was missing from the reopened branch and the active manager
still reported the old name. `TestRenamingAnotherSessionStillWrites` covers the
non-active path.

The selector's off-loop current/all-scope loaders capture their directory and
queue inputs on the owner, then drain accepted writes before reading disk.
Without this barrier, a rename's immediate refresh could outrun the queued
save, restore the old name in the list, and stay stale after the save finished.
`TestSessionSelectorRefreshWaitsForQueuedRename` drives the keyboard rename
with a controlled blocked writer and verifies the refreshed name. The UI never
waits on that barrier (upstream's rename/save finishes before its reload).

### D373

**Crash diagnostics cover worker goroutines and unnamed builds.**

Reference: pi v1.4.0 `packages/coding-agent/src/core/crash-log.ts` records an
uncaught exception's message, stack, session file and cwd. The reference runs
one thread, so every failure reaches its single handler. The port has an owner
loop plus off-loop queues, provider goroutines and timers, so three additions
keep a crash diagnosable:

- The owner loop records the recovered panic's stack; before this, `stack` was
  always null because only errors implementing `StackTrace()` supplied one.
- `offloop.Queue` reports a worker panic (with the panicking goroutine's stack)
  through a reporter before re-raising it. The panic stays fatal, as an
  uncaught exception is upstream; the report only fills the crash log, which
  otherwise never saw a worker's failure.
- A build without the version ldflags records the VCS revision Go embeds in its
  build info instead of the source default `0.0.0`, and `cmd.Execute` sets
  `GOTRACEBACK=all` so a fatal dump includes the other goroutines. Both change
  diagnostics only.

Tests: `TestAppRunConvertsAPanicIntoAnUncaughtCrash` (recorded stack),
`TestQueueReportsWorkerPanicBeforeDying` and
`TestQueueSurvivesAPanickingReporter` (worker report, fatal panic kept),
`TestCrashRecordIdentifiesTheBuild`.

### D374

**A git-describe build compares against its base tag, not as a semver prerelease.**

Reference: pi v1.4.0 `packages/coding-agent/src/utils/version-check.ts` compares
published package versions. Pier's justfile stamps `git describe --tags
--always --dirty`, so an ahead checkout is `v0.0.44-34-g<hash>`. Semver reads
that suffix as a prerelease of `0.0.44`, ranking the final `v0.0.44` release
newer: the update card appears and `pier update` would reinstall the older tag.
Pier recognizes the `<base>-<distance>-g<hash>[-dirty]` shape and compares the
candidate against `<base>` instead. At an exact tag, Git omits the distance
and emits `<tag>-dirty`; this local dirty marker is also removed before the
comparison, so local edits cannot make the base release look newer. Ordinary
semver, plain tags and the no-tag short-hash fallback keep their comparison.

Tests: `TestGitDescribeBuildIsNotOlderThanItsBaseTag`,
`TestDirtyTaggedBuildDoesNotOfferItsOwnRelease` (v1.5.0 `f9a26365`).

### D375

**A render walk retains its admitted child-line cache across reentrant invalidation.**

Reference: pi v1.5.0 (`f9a26365`) `packages/tui/src/tui.ts` renders child
components synchronously. Pier reuses unchanged children (D229) and defers
large Markdown (D204). When that bounded queue rejects a streaming thinking
render, its synchronous retry callback marks the chat dirty. This clears the
chat's cache while its Render walk is still running; a following unchanged
row then indexed the cleared cache and panicked. Container already snapshots
child identities against reentrant changes. It now also retains the admitted
cached-line slice for that frame, so invalidation affects the next cache
without invalidating reads already selected by the current walk. This adds
no cache, worker, copy or synchronization.

Reproducer: `TestStreamingMarkdownBackpressureKeepsCachedFollowingRows` uses
the real App queue with four occupied slots, streams thinking across 64KiB,
and renders a cached status row below it. Previously it panicked with
`index out of range [1] with length 0` at Container.Render. The previous
complete thinking frame and following row now remain visible under pressure.
`TestContainerKeepsCachedRowsWhenChildRequestsRetry` also pins the Container
contract directly with a rejecting Markdown backend and cached following row.

This is also the root cause of the crash record `index out of range [364] with
length 0` (2026-11-04, session
`--home-dat-repos-web--/2026-11-03T21-56-29-871Z_...jsonl`), which predated panic
stack capture and could not be matched at the time. Restoring the pre-fix read
(`c.cacheChildren[i]` instead of the frame's snapshot) reproduces that exact
message at that exact index, and
`TestContainerKeepsCachedRowsDeepInALongTranscript` keeps the 364-row shape as a
regression: a long transcript puts the cached following row at index 364, and a
rejected Markdown retry in the row above it must not lose the frame. The one-off
index is the row position, not a distinct defect.

### D376

**Config-command timeout also bounds inherited output-pipe waits.**

Reference: pi v1.5.0 (`f9a26365`)
`packages/coding-agent/src/core/resolve-config-value.ts` uses
execSync/spawnSync with a 10,000ms timeout. Go's CommandContext killed the
shell but Output kept waiting for EOF from a surviving child, so a timed-out
API-key command could block auth resolution indefinitely. Config commands now
set a minimal nonzero Cmd.WaitDelay only inside the cancellation hook. Wait
joins that hook before examining the delay. This closes inherited pipes after
the failed command is canceled, while normal commands still drain all output
without a time limit. The shell kill behavior and resolved key values stay
unchanged. The timeout policy stays 10s; tests shorten it to exercise a real
FIFO-held child without a slow ordinary test.

Same-input probe (`sleep 11; printf synthetic-value`, 10s timeout) took
11.006s before. Node execSync took 10.0104–10.0137s over three repeats; final
Go took 10.0086–10.0098s, returning failed resolution in every case. Normal
`printf synthetic-value` controls (50 samples per trial) remained correct:
Go baseline medians 1.70–1.92ms, final unloaded medians 1.60–2.18ms (overlapping
spawn-time noise), Node medians 2.96–3.85ms. No normal-output truncation or
extra deadline was introduced.

Reproducers: `TestConfigCommandTimeoutDoesNotWaitForDescendantPipeEOF`,
`TestConfiguredShellTimeoutDoesNotWaitForDescendantPipeEOF`. Both use a
controlled FIFO child and release/join it on failure; the original public-path
test was first observed failing at the real 10s deadline before shortening it.

### D377

**Pier's palette meets measured contrast floors.**

D154's hand-tuned hex values drifted below WCAG AA where text is drawn inside
the signal panels: `muted`/`toolOutput`/`toolDiffContext`/`mdQuote` reached
4.04–4.20:1 on the light fills and 3.53:1 on `userMessageBg` dark, `dim` sat at
1.99–2.98:1, and the pale light fills were only 1.15–1.20:1 from the page. The
result read as washed out in both appearances. `themes/{dark,light}.json` are
now adjusted along their existing hue/saturation, not replaced: normal text and
semantic colors clear 4.5:1 against the page and every panel fill, `dim`,
`mdLinkUrl`, `thinkingOff` and `thinkingMinimal` clear 3:1, and each fill is at
least 1.2:1 from the page so a panel edge stays visible. Light fills keep
mapping to distinct `rgbTo256` indices, pinned by
`TestPierThemeFilledBackgroundsDiffer`.

Upstream derives its palette from okhsl tokens and tests no contrast, so this is
a pier divergence, not parity. The gate is measured rather than stylistic:
`TestBuiltinThemesMeetContrastTargets` computes WCAG relative luminance for both
built-in documents; the regenerated interactive goldens carry the new values.
The amber identity (D219) and D154's structural choices are unchanged.

### D378

**`/tail` starts a new session from the live context.**

Reference: pi v1.5.0 (`f9a26365`). Upstream has no such command: `newSession`
takes only a `setup(sessionManager)` callback that extension code uses to seed
entries, and extension mechanics are out of scope here (D41). Pier adds
`/tail`, which writes a new session holding the same live context while the
compacted prefix stops traveling with the file. This is deliberate, so it does
not claim upstream behavior.

The copied set is exactly the window `ProjectedContextEstimate` reads: the
newest compaction, the branch entries its `firstKeptEntryId` keeps before it
(system messages are not replayed there, matching `walkCompactionWindow`), and
everything after it, including context edits. Entry IDs are preserved so the
compaction's `firstKeptEntryId` stays valid; parents are re-chained through
`setParentID` (and `setFirstKeptEntryID` when the compaction points outside the
window), because a loaded entry marshals its retained JSON line: setting the
struct field alone leaves the written parent pointing at an entry the window
drops, and the kept range stops being reachable from the leaf (M221).
Re-chaining re-roots the window, and a `model_change`/`thinking_level_change`
pair is prepended when the window no longer contains them, so a resume keeps the
current provider, model and effort. The new header records the source as
`parentSession`, and the same write path as a branch fork (`writeForkedSession`)
produces the file. A session without a compaction has no window to cut, so the
whole branch is copied and the context still matches. Sessions that do not
persist are refused with a status instead of silently starting empty.

The projection is the contract, not the byte count: the new session's messages
and token estimate are identical to the source's, verified by
`TestTailSessionKeepsTheLiveContext` (tool pair, context edit, kept system
message), `TestTailSessionKeepsTheLiveContextOnAReopenedSession` (loaded entries
carry their original lines, M221), and
`TestTailSessionWithoutCompactionCopiesTheBranch`. The command path is pinned by
`TestTailCommandStartsFromTheLiveContext` and
`TestTailCommandNeedsAPersistedSession`. Cold history remains readable only
through the source session; the tail session is a new starting point, not a
backup.

### D379

**Summary file lists are capped.**

Reference: pi v1.5.0 (`f9a26365`) `core/compaction/utils.ts`
(`formatFileOperations`) writes every tracked path into the summary, and
`core/compaction/compaction.ts` (`extractFileOperations`) seeds the next
compaction with the previous one's `details`, so the lists only grow. A real
session reached 2,688 paths: 123 KB of a 149 KB summary (83%, about 31K tokens)
on every request until the next compaction, including build-cache artifacts and
duplicate spellings of the same path.

Pier caps each list at `maxSummaryFileListEntries` (100) paths, keeps the most
recently touched ones, sorts them for stable output, and appends `+N more`
inside the block. `FileOperations.Order` records the last touch of every path so
the window outranks the inherited list; inherited entries keep the order of the
previous, already-sorted details. A caller that builds the exported
`FileOperations` sets without the mark helpers has no recency to rank, so the
remaining slots take the sorted paths and the omitted count stays exact. The cap
applies to the prose summary and to the persisted `details`, which is what the
next compaction inherits, so growth stops instead of being capped only in the
text. Lists at or under the cap are byte-identical to upstream, and the file-ops
parity fixture keeps exercising `ComputeFileLists`/`FormatFileOperations`
unchanged.

Measured on the real 2,688-path session: the inherited details alone were 1,386
read plus 1,397 modified paths (127,540 bytes); the cap renders 100 plus 100 with
the markers in 6,631 bytes, about 95% less per request until the next
compaction. Paths are still verbatim (no cleaning or existence check), so the
upstream label is preserved.

Tests: `TestSummaryFileListsCapToTheMostRecentPaths`,
`TestSummaryFileListsPreferTheCurrentWindow`,
`TestSummaryFileListsSmallInputMatchesUpstreamFormatting`,
`TestSummaryFileListsReportOmittedCounts`,
`TestSummaryFileListsAtTheCapMatchUpstream` (no marker at exactly the cap), and
`TestCompactSummaryCapsTheFileLists` (summary marker plus capped details).

### D380

**Skill frontmatter parses a YAML subset, including block scalars.**

Reference: pi v1.5.0 (`f9a26365`) `utils/frontmatter.ts` parses frontmatter with
the `yaml` package. Pier parses the subset the Agent Skills spec's frontmatter
uses: flat `key: value` pairs, quoted and plain scalars with the common boolean,
numeric and null values, and literal (`|`) and folded (`>`) block scalars with
either chomping indicator (`-`, `+`) and an indentation digit. Block scalars are
loaded from the same lines the reference passes to the parser, so the
frontmatter's closing delimiter drops its own final newline before chomping; that
one-line difference is what makes `|+` with a single trailing blank line render
as `"a\n"` in both. 145 generated cases (`coding/testdata/frontmatter-block`)
match the package for value text, string typing and body.

Remaining differences, both outside the Agent Skills spec:

- Nested mappings and sequences are not parsed; a nested key is skipped rather
  than stored as an object.
- Invalid YAML is reported differently: the `skills/invalid-yaml` fixture warns
  "description is required" where the reference prints the parser's "Flow
  sequence in block collection..." message. Values pier cannot parse never
  reach the prompt, so the warning is the actionable part.

A differential over whole skill trees confirms the subset holds where it
matters: block and plain descriptions, quoted values, `name: true` and `name: ~`
(which fall back to the directory name because the loader only accepts string
values), `disable-model-invocation` in both boolean and quoted form, a nested
mapping, a missing description and a numeric description. Both sides load the
same eight titles with the same descriptions and flags, and report the same two
`description is required` diagnostics (the missing and the numeric case), so the
typed values the parser distinguishes never change which skills reach the
prompt.

Every upstream skill fixture now matches, including `skills/multiline-description`
(the block-scalar case that motivated this row) and each fixture's diagnostic
wording.

Tests: `TestParseFrontmatterBlockScalars`,
`TestParseFrontmatterBlockScalarKeepsStringTypes`,
`TestLoadSkillWithBlockScalarDescription`, and
`TestParseFrontmatterBlockScalarsMatchUpstream`.

### D381

**CLI diagnostic color follows the pinned chalk's supports-color decision.**

Reference: pi v1.5.0 (`f9a26365`) prints parse and runtime diagnostics with
`chalk.red`/`chalk.yellow` (`cli/args.ts`, `main.ts`,
`core/agent-session-services.ts`), so terminal detection decides whether the
line carries ANSI codes. Pier colored them unconditionally: a redirected stderr,
a `TERM=dumb` shell and an unnamed CI all received `[31m`. `coding/clicolor.go`
ports the boolean answer of the bundled supports-color: stdout's terminal state,
`FORCE_COLOR` (empty, `true` and a numeric level force on; `0` and `false` force
off), `TERM=dumb`, the platform branch, the known CI providers, `TERM_PROGRAM`,
`TERM` patterns and `COLORTERM`. Detection reads stdout, not stderr, matching
chalk: a colored error still appears when only stderr is redirected.

The color close codes match chalk too: a foreground color ends with
`\x1b[39m` and intensity with `\x1b[22m`, not a full `\x1b[0m` reset, so a pty
run of `--mode nope`, `--thinking nope` or `auth check --bogus` is
byte-identical to the reference. The `auth` subcommands print their failures
through the same helpers, so their errors and the dim "Use ..." hint color with
the terminal instead of never coloring.

The pinned supports-color does not read `NO_COLOR`, so neither does the port: a
controlled pty run of the reference with `NO_COLOR=1` still colors, unlike
modern chalk.

Tests: `TestResolveCLIColorsMatchesTheReference` (every case observed on the
reference binary), `TestDiagnosticColorFollowsColorSupport` (redirected,
forced, forced off, dumb) and `TestFormatCLIDiagnosticDropsColorWhenDisabled`.

### D382

**A fresh session's agent differs in what it holds: tools, model placeholder and
compaction order.**

Reference: pi v1.5.0 (`f9a26365`) builds the coding agent with `tools: []`
(`core/sdk.ts`) and installs the loadout inside the per-prompt preparation
(`core/agent-session.ts` sets `agent.state.tools`), so an untouched session has
no messages: RPC `get_state` reports `messageCount: 0` and `get_messages`
returns `[]`. Pier passes the configured tools into the agent's initial state
(`coding/agent_session.go`), which makes `ai.CreateInitialSystemMessage` install
the leading `{role: "system", content: "", toolsAdded: [...]}` message at
construction. Until a turn runs, RPC therefore reports `messageCount: 1` and
`get_messages` returns that declaration (with the schemas of every enabled
tool), while the reference reports none.

The port keeps the early install because `GetActiveToolNames` (used by
`/tools` and by the session round-trip) must answer before any prompt, and
because non-prompt loops (goal reminders, injected bash messages, compaction
retries) rely on the agent already holding the executable set. Moving the
install into the prompt path would need the active names sourced from the tool
registry and was not worth the regression risk in the UI and loop paths. The
difference is limited to an untouched session: once a turn runs, both carry a
declaration message ahead of the prompt-section message, and the request bytes
converge. Verified against pi-written sessions: the reference's interactive
sessions persist the same shape (declaration entry first, then `model_change`
and `thinking_level_change`, then the section message carrying only the delta
of tools registered later), while a print-mode reference run folds everything
into one section message. Pier matches the interactive shape, so the extra entry
is the same one the reference writes whenever a session is created in the TUI.

The model placeholder is the second half of the difference: the reference's
session always carries a placeholder model (`id: "unknown"`,
`provider: "unknown"`) and `get_state` sends it; pier's `HasModel` treats that
placeholder as absent, so `get_state` omits the optional `model` field
(documented in [Stdio RPC](ARCHITECTURE.md#stdio-rpc)) and `compact` fails with
"No model selected" where the reference prepares the compaction first and
reports "Nothing to compact (session too small)" on an empty session. The
reference's own "No model selected" throw (`core/agent-session.ts`) needs
`session.model` to be `undefined`, which the placeholder makes unreachable.

Reproducer: `printf '%s\n' '{"type":"get_state","id":"1"}'
'{"type":"get_messages","id":"2"}' | pier --offline --mode rpc --no-mcp
--no-session` reports `messageCount: 1` and one system message, where the
reference reports `0` and `[]`; the same run with
`{"type":"compact","id":"3"}` (stdin kept open, since closing it cancels the
worker per D235) reports "No model selected", where the reference emits
`compaction_start`, `compaction_end` with "Compaction failed: Nothing to compact
(session too small)" and the same error on the response.

Test: `TestRPCFreshSessionHoldsTheToolDeclaration` pins the in-process shape
with the coding tools: `messageCount: 1`, no `model` field, the single empty
system declaration, and "No model selected" from `compact`.

### D383

**Print and JSON mode emit each streaming event with the state it had when it
was produced.**

Reference: pi v1.5.0 (`f9a26365`) serializes the same shared assistant partial
for `message_start` and every `message_update` (D285 describes the same live
helper). Because later deltas mutate that object before the bytes are written,
the reference's stream shows later state in earlier events: against a local
openai-completions endpoint whose usage arrives in the final chunk, its
`message_start` already carried the first text block and every `message_update`
carried the final usage (12 in / 4 out) while its text deltas were still
streaming. Pier snapshots the partial on the producing goroutine
(`AssistantMessageEventStream.Push`), so `message_start` carries an empty
assistant message and usage ramps per delta, reaching the same values at
`text_end`.

The frames, deltas, `message_end`, `turn_end`, `agent_end` and the persisted
assistant entry are identical; only the intermediate events differ. Mirroring
the reference would mean holding the response until its late fields are known,
which costs the streaming latency D285 exists to protect, so the snapshotted
stream stays. Compared on 2026-11-06 against the installed 1.5.0 over the same
local endpoint: request key order, streamed deltas, `message_end` usage/cost,
`stopReason`/`rawStopReason` and the session's assistant entry all match.

Tests: `TestAssistantStreamSnapshotDoesNotChangeLiveNext` pins the Push-time
snapshot (an empty start partial with zero usage while the live helper has
moved on), and `TestAssistantStreamForwardingPreservesProducerSnapshot` keeps
it across a forwarding stream.

### D384

**An MCP provider token that cannot be resolved is reported, not silently
omitted.**

Reference: pi v1.5.0 (`f9a26365`) wires `auth: { provider }` to
`modelRegistry.getApiKeyForProvider` (`extensions/mcp/index.ts`), which wraps
`runtime.getAuth(provider)` in a `try`/`catch` returning `undefined`
(`core/model-registry.ts`). A provider whose key cannot be resolved therefore
contributes no `Authorization` header, and the server's 401 is what the user
sees. Pier's `providerTokenForMCP` (`cmd/main.go`) returns the resolution error
instead, so the MCP request fails with the cause: `API key auth failed for
provider <id>: Failed to resolve API key for provider "<id>" from shell command:
...` or the missing environment variable.

The port keeps the error because a silent 401 sends the user looking at the MCP
server when the credential template was the problem; the failure is the same
either way (that server's tools are unavailable for the run). Unknown providers
and a missing runtime still return an empty token without an error, matching the
reference's `undefined`.

Test: `TestProviderTokenForMCP` covers four boundaries: resolvable key, unknown
provider, no runtime and unresolvable key. The reference side was read from the
pinned source; the equivalent template failure was not driven through its MCP
path.

### D385

**A goal's user follow-ups wait for its final answer.**

Reference: pinned pi v1.5.0 has no goal; pier's goal is the ported extension
described under D223. That extension yields to queued user input: its
`agent_before_settle` dispatch passes `HasPendingMessages` to the state machine,
which then starts no new round, so a follow-up is delivered between or after
goal rounds or starts its own run. A user who presses Alt+Enter during a goal
therefore mixes their message into the goal's stream before the goal has
finished.

Pier keeps the run's user follow-ups queued until the goal is done and the agent
has answered it, then delivers them as their own run:

- `agent.AgentOptions.DeferFollowUp` lets the caller hold individual queued
  follow-ups. The stop boundary and `Agent.Continue` drain the queue through
  `pendingMessageQueue.drainDeferring`, which skips held messages, keeps their
  order, and still returns the rest (goal rounds are custom messages, so they
  pass while user messages wait). `HasDeliverableMessages` keeps the post-run
  continuation from treating an all-held queue as work.
- `AgentSession` installs the predicate: a user message is held while the
  current run was entered with an active goal (`goalHeldThisRun`, latched by any
  agent event while the goal pursues, so a goal created mid-run counts) and the
  goal is still pursuing. Goal rounds continue as before (one run each under
  D223's delivery model).
- When the goal stops pursuing (complete, blocked, paused, cleared or capped),
  the session releases the held messages after that run's final answer:
  `releaseGoalHeldFollowUps` drains them and starts them as a new run through
  the session run pipeline, so the run emits `agent_settled` and keeps retry and
  compaction handling. On a host with a work slot it is queued there; without
  one it waits for the run in flight to settle before starting (starting from
  the agent-end callback would be rejected as already processing). Both paths
  check the disposed flag: a release queued during shutdown must not start a run
  on a closed session. `Dispose` sets the flag before its abort and cleanup
  steps, so a release the host runs while they are in progress is skipped too.
  The flag lives under the prompt-state lock that also installs a run, so a
  release racing `Dispose` either sees it or starts a run the abort reaches.
  An aborted run releases nothing; interactive's Esc
  restore owns that queue.
- The release keeps the queue's own granularity. `Agent.ReleaseFollowUpMessages`
  drains by mode and ignores `DeferFollowUp`, because the release runs only once
  the reason for holding is gone. In the default one-at-a-time mode the released
  run carries the first held follow-up, and that run's own stop boundary delivers
  the rest one turn at a time; `all` still delivers the whole queue in one turn.
  Before this, the release took every held message and one request answered all
  of them, so several Alt+Enter presses were answered together.
- The deferring drain clears the slots it empties, so a delivered message is not
  retained by the queue's backing array.

Tests: `TestDrainDeferringSkipsHeldMessages`, `TestDeferredFollowUpSurvivesTheRun`
and `TestDrainDeferringClearsConsumedSlots` (queue deferral, continuation, slot
clearing); `TestGoalHoldsFollowUpsUntilItsFinalAnswer` (two goal rounds complete
without the follow-up, a third run carries it only afterwards),
`TestGoalReleasedFollowUpsRunWithoutAHost` and
`TestGoalReleaseSkipsDisposedSession`,
`TestGoalReleaseDuringDisposeCleanupSkipsTheRun`,
`TestGoalReleaseRacingDisposeLeavesNoUnabortableRun`; `TestGoalReleasedFollowUpsAreDeliveredOneAtATime`
(three held follow-ups each get their own request, in queue order),
`TestQueuedFollowUpsAreDeliveredOneAtATimeWithoutAGoal` (the control, with a
handshake instead of sleeps) and `TestQueuedFollowUpsFollowTheAllMode` (the mode
still batches).

### D386

**HTML export substitutes template placeholders literally.**

Reference: pi v1.5.0 `packages/coding-agent/src/core/export-html/index.ts`
builds the document with `template.replace("{{HIGHLIGHT_JS}}", hljsJs)` and
friends. `String.replace` with a string pattern expands `$` replacements in the
*replacement* text, and the pinned bundles contain them: the highlight bundle's
CSS-selector character class includes `$&`, and `template.js` writes the footer
cost as `$${totalCost.toFixed(3)}`. The reference's own export therefore embeds
the literal text `{{HIGHLIGHT_JS}}` inside its highlighter (in place of `$&`) and
exports a cost line reading `${totalCost.toFixed(3)}`, which is a
`ReferenceError` for the undefined identifier `totalCost` when the document
renders.

Pier substitutes with `strings.Replace`, so the injected bytes are exactly the
bundled bytes: the `$&` selector survives, no placeholder text remains in the
document, and the literal `$${totalCost…}` is preserved for the footer renderer.
The exported HTML is still self-contained; it differs from the reference's
corrupted output only where the reference lost bytes.

Test: `TestExportSessionToHTML` asserts the document contains no
`{{HIGHLIGHT_JS}}` and keeps both `$&` and `$${totalCost.toFixed(3)}` byte for
byte.

### D387

**The trust store round-trips every value and sorts keys like JavaScript.**

Reference: pi v1.5.0 `core/trust-manager.ts` `writeTrustFile` copies entries
whose value is `true`, `false` or `null`, then serializes
`Object.keys(data).sort()` order. Pier dropped `null` members on every write and
sorted by Go byte comparison rather than UTF-16 code-unit order.

A `null` member is the "Trust parent folder" option's tombstone for the project
path (`setMany` clears the key when the decision is null). Both implementations
read `null` as no decision, so a dropped tombstone does not change which
decision wins; the defect is a persistence round-trip one. A store written by
pi (or by an earlier pier) loses those entries on the next unrelated decision,
and keys holding astral characters are re-serialized in a different order than
the reference, so `trust.json` bytes drift from the reference for the same
logical store. Astral ordering cannot change which entry resolves: rows that
differ only in path spelling separate at ASCII segments.

Pier keeps `null` members and sorts with `compareUTF16`.

Test: `TestProjectTrustStoreWriteMatchesReference` pins the exact bytes against
the reference's own output for the same store.

### D388

**Persisted JSON writers keep `JSON.parse` key order.**

Reference: pi v1.5.0 `settings-manager.ts` `persistScopedSettings`,
`extensions/mcp/config.ts` `editMcpServers` and `core/models-store.ts`
`FileModelsStore` all read the file, mutate the parsed object and
`JSON.stringify` it. Because the parsed value is an ordinary object, JavaScript's
own property enumeration applies: array-index keys (`"0"`, `"1"`, `"2"`,
`"10"`, but not `"01"`, `"-1"` or `"4294967295"`) come first in numeric order,
then the remaining keys keep insertion order.

Pier emitted pure file order for `settings.json`, `mcp.json` and
`models-store.json`, so a hand-written file with numeric keys was rewritten in a
different order than the reference produces for the same edit. The order is
stable and both sides preserve the other keys, so this is a byte-parity defect
rather than a data one; it also affected the read order pier reported for
`models-store.json` providers.

All three writers route their key list through the existing
`mcpJavaScriptObjectKeyOrder` helper.

Test: `TestPersistedJSONWritersUseJavaScriptKeyOrder` pins the exact bytes for
each file against the reference's own output (`"1"`, `"2"`, `"10"` before the
ordinary keys, `"01"`/`"4294967295"` staying in place) and failed on all three
writers before the fix.

### D389

**`CanonicalizePath` returns an absolute path like `realpathSync`.**

Reference: pi v1.5.0 `utils/paths.ts` `canonicalizePath` is `fs.realpathSync`,
which resolves a relative argument against `process.cwd()` and therefore always
returns an absolute path. Pier used `filepath.EvalSymlinks`, which resolves
symlinks but keeps a relative input relative, so the same directory produced two
comparison keys: `CanonicalizePath("sub")` was `sub` where the reference gives
`/work/sub`, and `PathsEqual` therefore treated one location as two.

The helper resolves through `filepath.Abs` before `EvalSymlinks`, so a
relative input yields the absolute physical path and a failed resolution still
falls back to the raw path as before. Every current caller passes an absolute or
already-resolved path (boot cwd, session cwd, a `ResolvePath` result), so this
was a latent contract mismatch rather than a reachable behavioral difference.

Test: `TestCanonicalizePathMatchesRealpathOnRelativeInput` pins the absolute
result for `""`, `"."`, `".."`, `"sub"`, `"./sub"` and `"sub/"`, checks that two
spellings of one directory compare equal, and keeps an already absolute path
unchanged as the control.

### D390

**`auth check` sees `models.json` providers, like the reference.**

Reference: pi v1.5.0 `cli/auth-check.ts` `createAuthCheckModelRuntime` calls
`ModelRuntime.create` with only `credentials`, `modelsStore` and
`allowModelNetwork`, so the runtime loads the default `models.json` and a
provider declared there is found by `checkProviderAuth`. Pier's auth-check
runtime passed `DisableModelsJSON: true` (added with the `pi auth` subcommands),
so `pi auth check --provider <models.json provider>` reported
`provider_not_found` with exit 1 while `pi auth print-api-key` for the same
provider resolved its literal, `$ENV` or `!command` key: the two subcommands
disagreed about which providers exist. `auth check --model <provider>/<id>` for
such a provider also failed with `invalid_state` and exit 2.

Pier builds the check runtime the way the reference does. The initial
catalog refresh stays off (`RefreshOnCreate: false`), matching
`refreshOnCreate: false`, so the check neither touches the network nor merges a
`models-store.json` overlay the reference would not have.

Test: `TestAuthCheckSeesModelsJSONProviders` covers a literal key, an `$ENV` key,
a provider without a key (`credentials_not_configured`, exit 1) and the
`--model` form; all four failed with `provider_not_found` before the fix. A
rebuild was also compared against the reference binary: eight `auth check` /
`print-api-key` cases now match on stdout and exit code.

### D391

**Spawned test children inherit the isolated agent directory.**

Not a reference divergence but a port-testing invariant recorded here because it
outlived a CI-shaped failure. `cmd`'s child-process tests spawn this binary as a
test helper to check exit codes, color decisions and headless startup. Three
sites built `command.Env` from scratch (a clean environment keeps the
developer's `CI`, `NO_COLOR` and `TERM` out of the color decision), which also
dropped `PI_CODING_AGENT_DIR`: the child then resolved the developer's real
`~/.pi/agent` instead of the temporary directory `isolatedAgentDir` sets.

The session-resolution case scans that directory. On the developer's host it
held 445 MB and 57 session files, so the child took ~15 s alone and was killed
at the 30 s `childProcessTimeout` under `go test -race ./...`, which reported
`exit = -1 (signal: killed)`. That violates the AGENTS.md rule that tests use
temporary agent state and never the real agent directory, and it made the gate
load-dependent.

`childAgentEnv` in `cmd/helpers_test.go` is the single environment builder for
those children: it passes `PI_CODING_AGENT_DIR` and `HOME` and appends the
case-specific entries. The session-resolution child dropped from ~15 s to 0.08
s.

Test: `TestChildProcessesUseTheIsolatedAgentDir` spawns the helper through that
builder and asserts the child's `GetAgentDir()` is the isolated directory; it
failed with `/home/dat/.pi/agent` at both sites before the change.

### D392

**The session directory honors the environment variable and the setting.**

Reference: pi v1.5.0 `main.ts` resolves the session directory as
`--session-dir` (`normalizePath`) ?? `$PI_CODING_AGENT_SESSION_DIR`
(`expandTildePath`) ?? `settingsManager.getSessionDir()`, and passes the result
to `createSessionManager`, so the resume picker, `--continue`, `--session-id`
creation and a fresh run all use it. Pier read only `--session-dir`: a run with
`PI_CODING_AGENT_SESSION_DIR` or the `sessionDir` setting configured wrote its
session into the default per-cwd agent directory instead, and `-c`/`-r` searched
there.

`resolveSessionDirectory` in `cmd/main.go` applies the reference's
precedence and expansion, and both the fresh and resume paths take the directory
from it. An unset configuration still yields the empty string, which keeps the
per-cwd default under the agent directory.

Tests: `TestResolveSessionDirectoryMatchesTheReferencePrecedence` (setting only,
environment over setting, flag over both, tilde expansion, unconfigured),
`TestResumeSessionIDUsesTheConfiguredSessionDir` and
`TestFreshSessionUsesTheConfiguredSessionDir`. The resume test failed against
the previous wiring, which created the session under
`~/.pi/agent/sessions/--<cwd>--`. A rebuilt binary was also compared with the
reference: a first run and `-c` land in the configured directory for both the
environment variable and the setting, and `-c` reports the same continued
session.

### D394

**An unset `defaultTools` layer keeps the inherited list.**

Reference: pi v1.5.0 `core/settings-manager.ts` `mergeDefaultTools` returns
`base` as its first check (`if (overrides === undefined) return base;`), so a
project settings file that does not mention `defaultTools` leaves the global
selection alone. Pier returned nil for that case while its merge caller assigned
the result only when non-nil, so a global `defaultTools` was dropped as soon as
the project layer set anything.

Reproduced through the settings files: global `"defaultTools": ["read","bash"]`
with a project `{"defaultTools": ["+grep"]}` gives the reference
`read, bash, grep` in the system prompt's tools section; pier exposed only the
built-in defaults, so the project's `+grep` added nothing. `mergeDefaultTools`
returns `base` before the modifier check, matching the reference.

Test: `TestMergeDefaultToolsKeepsTheBaseWhenTheOverrideIsUnset` covers base kept,
empty base kept as empty, both unset, modifier appends, plain replacement and a
nil base; the first case failed before the change. Verified after the fix with a
rebuilt binary: the same project now exposes `read, bash, grep` as the reference
does (pier additionally lists its own `set_goal`, D223).

### D395

**Session statistics tolerate an assistant message without a complete usage.**

Reference: pi v1.5.0 `core/agent-session.ts` `getSessionStats` calls
`addUsageToTotals(usageTotals, assistantMsg.usage)` for every assistant message,
and `core/usage-totals.ts` `addUsageToTotals` dereferences `usage.input`,
`usage.output`, `usage.cacheRead`, `usage.cacheWrite` and `usage.cost.total`
without a guard. A persisted assistant message whose usage is absent, `null` or
partially populated therefore makes the whole call throw: over RPC,
`get_session_stats` answers `success: false` with
`Cannot read properties of undefined (reading 'input')` (`null` gives the
`null` wording, and a usage without `cost` gives `(reading 'total')`).

Pier decodes usage into a value type during projection, so those three shapes
read as zeros and `get_session_stats` returns the counts, an empty token total
and the context usage. Every message either side writes carries a complete usage
record (verified on a persisted fake-provider session), so this only reaches the
boundary through a hand-edited or foreign session file. The port keeps the
tolerant read on purpose: a damaged transcript still reports how many messages it
holds, and an RPC surface that fails whole is a worse outcome than one reporting
zeros for the unreadable part.

Test: `TestSessionStatsToleratesAnIncompleteAssistantUsage` covers the absent,
`null` and partial-usage shapes (one assistant message, input tokens 0/0/5).

### D396

**MCP tool frames collapse arguments and preview results like the reference.**

Reference: pi v1.5.0 (f9a26365) `extensions/mcp/tools.ts`
`createMcpToolRenderers` renders a call through
`core/tools/render-utils.ts` `formatToolCallWithArgs` (collapsed: the
`server/tool` label plus `key=value` pairs cut to 100 characters; expanded: one
`key: value` line per argument) and a result through
`modes/interactive/components/visual-truncate.ts` `VisualLinePreview`
(`OUTPUT_PREVIEW_LINES = 5`, `keep: "start"`, `... (N more lines, ctrl+o to
expand)`, plus `Full output: <path>` while collapsed).

Pier's `agent.AgentTool` carries no renderers, so MCP calls fell back to
`ToolExecutionComponent.formatToolExecution`: the arguments as pretty-printed
JSON and every result line. A call with a handful of arguments spanned a dozen
rows, and a result with one long minified JSON line wrapped across the whole
screen. `coding/interactive/toolrenderer_mcp.go` ports both halves, and the
app's session adapter resolves them for an `mcp__` tool from its registered
`server/tool` label.

Collapsed arguments count UTF-16 code units through `ai.JSSlice`, and the key
order follows `Object.entries` (array-index names first, then file order), so
the header is byte-identical to the reference's for the same arguments.

Tests: `TestMcpToolCallFramesCollapseArguments` (collapsed pairs, the 100-unit
budget, `Object.entries` order against the reference's own output, and the
expanded indented form) and `TestMcpToolResultFramesLimitVisualLines` (5 wrapped
lines with the hint, one long line bounded, expanded shows everything, and the
saved full-output path) and `TestMcpToolFramesReachTheTranscript` (the app's
adapter resolves the renderers from the tool label).

### D397

**An expanded MCP frame keeps every row above the body in place.**

Reference: pi v1.5.0 (f9a26365) `extensions/mcp/tools.ts`
`createMcpToolRenderers` rebuilds the call header on every display update
through `core/tools/render-utils.ts` `formatToolCallWithArgs(label, args,
theme, context.expanded)`: collapsed, one line of `key=value` pairs; expanded,
the label plus one indented `key: value` line per argument.
`ToolExecutionComponent` wraps the header and the result in
`createResultRegion` (`modes/interactive/components/tool-execution.ts`), whose
handler toggles expansion on any left click in the region, without a position
check. Clicking the `... (N more lines, ctrl+o to expand)` hint therefore also
reflowed the header, moving the row under the pointer.

Pier keeps the header as one line in both states and previews the body at the
render width the preview receives, so expanding only appends rows below the
preview and collapsing restores the frame exactly.

This row also records the bug the first version of the port had. The collapsed
preview measured visual lines at a fixed 80 columns while the expanded body
wrapped at the real width, so on a wide terminal a row broken in the collapsed
view stayed whole in the expanded one, and the hint's count was wrong. Both
states now wrap once at the width `Render` receives, matching upstream's
`VisualLinePreview` and `Text` pair.

Collapsed arguments keep the reference's `Object.entries` order and 100-unit
budget, so a collapsed header is identical to the reference's.

Tests: `TestMcpCallHeaderStaysUnreflowedWhenExpanded`,
`TestMcpResultExpansionKeepsThePreviewInPlace`,
`TestMcpResultPreviewWrapsAtTheRenderWidth` (four widths, collapsed rows equal
expanded rows and never exceed the width),
`TestMcpResultPreviewHintCountsVisualLines`, and
`TestMcpToolCallFramesCollapseArguments` (the collapsed header still matches the
reference's output).


### D398

**A paragraph whose second character opens display math stays whole.**

Reference: pi v1.5.0 (f9a26365) `packages/tui/src/components/markdown.ts`
registers the `latexBlock` extension with the start hook
`/(?:^|\n) {0,3}(?:\$\$|\\\[)/`. marked 18.0.11 `Lexer.blockTokens` runs that
hook on `src.slice(1)` to clip the top-level paragraph before an extension
start, then joins the next paragraph back with `"\n"` when the extension
declines (`lastParagraphClipped`). Because the hook's `^` matches the start of
the sliced source, a paragraph whose second character (after up to three
spaces) begins `$$` or `\[` is clipped after its first character. Upstream
renders `a $$ b` as the lines `a`, a blank line and `$$ b`, and `x$$ y` as `x`
and `$$ y`.

Pier ports the line-start case. A `$$` or `\[` line ends a paragraph only when
the LaTeX tokenizer takes it, so `text\n$$ plain` stays one paragraph as it
does upstream. Pier does not split a paragraph at its second character, so the
sentence renders as written.

The same flag has a document-wide side effect that pier does not port either.
The hook finds any later opener, so `lastParagraphClipped` is set for every
paragraph before a display-math line. Two paragraphs that marked would emit
separately are then joined: `a\nb c\n-|-` is the paragraphs `a` and `b c\n-|-`,
but with `\n\n$$x$$` appended upstream lexes one paragraph. Porting it would
make every earlier paragraph depend on the rest of the source, which the
markdown component's windowed re-lex (`tui/markdown.go`) relies on not
happening.

Test: `TestMarkdownKeepsAParagraphWhoseSecondCharacterOpensMath`.

### D399

**Han, kana and Southeast Asian runs stay one word.**

Reference: pi v1.5.0 (f9a26365) `packages/tui/src/utils.ts` segments words
with `Intl.Segmenter`. That is ICU's word break iterator (Node 26, ICU 78.3,
Unicode 17), the UAX #29 rules as tailored by ICU's `word.txt`. Pier implements
those rules over a table that `scripts/gen_wordbreak.py` generates from the
Unicode 17 UCD. With Han, kana and Complex_Context scripts left out, its
segments and `isWordLike` match `Intl.Segmenter` on 55,000 random strings drawn
from every Word_Break class. Word navigation then matches at every grapheme
boundary.

ICU also hands runs of Han and kana, and of Thai, Lao, Khmer and Myanmar, to
dictionary break engines, which split them into words (`日本語` `の` `テキスト`).
Pier keeps the run that the rules' `$KanaKanji` chain, or the ALetter rules for
Complex_Context text, produce as one word. Matching the engines would mean
shipping ICU's dictionaries. Ctrl+arrow therefore skips a whole such run.

Test: `TestWordSegmentsKeepDictionaryRunsWhole`.

### D400

**A fractional read offset still reports an oversized first line.**

Reference: pi v1.5.0 (f9a26365) `packages/coding-agent/src/core/tools/read.ts`
sizes the oversized first line with `allLines[startLine]`. The schema accepts
any number, and a fractional `offset` such as `2.5` makes `startLine` fractional,
so the lookup is `undefined` and `Buffer.byteLength` throws a `TypeError` that
the model sees as the tool result. Pier sizes the line it actually sliced
(`slice` truncates the index) and returns the usual `sed -n` hint, which the
model can act on. Every other fractional-offset result matches upstream.

Test: `TestReadToolFractionalOffsetReportsOversizedLine`.

### D401

**Vertex Gemini 2.5 Flash-Lite uses the Flash-Lite thinking budgets.**

Reference: pi v1.5.0 (f9a26365) `packages/ai/src/api/google-vertex.ts`
`getGoogleBudget` has no `2.5-flash-lite` branch, so Flash-Lite matches the
`2.5-flash` table and minimal reasoning sends a 128-token budget, below the
512-token minimum Google documents for Flash-Lite. The same function in
`google-generative-ai.ts` has the Flash-Lite branch and sends 512. Pier uses
that one table for both APIs, so Vertex Flash-Lite also sends 512. The other
levels and models send the same budgets as upstream.

Test: `TestVertexFlashLiteMinimalBudgetUsesFlashLiteTable`.

### D402

**The working indicator shows elapsed time and tokens, and the transcript notes what a run used.**

Reference: pi v1.5.0 (f9a26365)
`packages/coding-agent/src/modes/interactive/components/status-indicator.ts`
shows only the working message, and an agent run ends without a transcript
line. Pier is a project choice here: the working period runs from the first
`agent_start` until `agent_settled`, so the agent runs a retry, a compaction
continuation or a queued follow-up starts inside it count as one period. An
active armed goal also keeps it open: an `agent_settled` whose goal is still
pursued (and not aborted) waits for the goal's next round, so a goal's rounds
are one period with one notice; if a user prompt arrives instead, the waiting
period closes at that settle before the prompt starts its own. The follow-ups
that goal held (D385) belong to the same period: the settle that releases them
reports the run still to come (`SessionEvent.FollowUpsPending`), so the
released run's time and tokens are added to the agent's until everything
finishes, with one notice at the end. Because that run opens with the
follow-up's own user message, the settle also marks the period as continuing,
and that one message starts no new period; an actual prompt still closes a
waiting period at its settle. The
working indicator appends the time since in whole seconds (`Working (1m 5s)`),
across every working indicator the period shows, and ticks once a second even
when a custom indicator hides the spinner frames. Once the provider reports usage it
adds the run's tokens in and out in the footer's token format
(`Working (1m 5s · ↑48k ↓3.2k)`): finished assistant messages plus the
streaming one's usage so far (never lower than that message already showed),
where in counts every prompt token processed
(input, cache reads and cache writes), so a multi-turn run counts its context
once per turn. At `agent_settled` the transcript adds a dim
`14:32 · 1m 23s · ↑48k ↓3.2k · CH89.4%` line (D416),
with tenths under a minute as the bash tool's `Took` label has. The line is display-only: it is not written to the session,
so a resumed session does not show it. A period belongs to the session it
timed, so replacing the session discards it in `teardownCurrentSession` instead
of carrying its elapsed time and tokens into the new session's first notice,
and drops the working indicator with it; upstream's own replacement paths begin
with `clearStatusIndicator` (`interactive-mode.ts` `handleResumeSession`,
`handleImportCommand`, `handleClearCommand`, `stop`).

Tests: `TestWorkingIndicatorShowsElapsedAndTranscriptNotesTheRun`,
`TestWorkingPeriodSpansGoalRounds`, `TestWorkingPeriodSpansTheReleasedFollowUpRun`,
`TestWorkingElapsedLabelFormats`, `TestLoaderElapsedLabelTicksEachSecond`,
`TestSessionSwitchDiscardsTheOldWorkingPeriod`;
`TestGoalSettleReportsThePendingFollowUpRun` covers the flag itself.

### D403

**A collapsed thinking block shows how long it has been thinking, then how long it thought.**

Reference: pi v1.5.0 (f9a26365)
`packages/coding-agent/src/modes/interactive/components/assistant-message.ts`
shows the hidden-thinking label alone in place of a collapsed thinking block.
Pier is a project choice here: each run of thinking blocks that streams into
the transcript is timed from when it first appears until a later block (text
or a tool call) appears or the message stops streaming. While it streams, the
collapsed label shows the elapsed whole seconds and ticks each second
(`Thinking... (12s)`); once it ends it reads `Thought for 12.3s`, or a custom
label followed by the time (`Pondering (12.3s)`). Expanded thinking is
unchanged. The timing is display-only and lives on the transcript component,
so a message loaded from the session, or a rebuilt transcript, shows the plain
label.

Tests: `TestCollapsedThinkingShowsHowLongItThought`,
`TestCollapsedThinkingTimesEachRun`,
`TestCollapsedThinkingTicksThroughTheRenderer`.

### D404

**RPC rejects a queue mode or thinking level it cannot honor.**

Reference: pi v1.5.0 (f9a26365) `modes/rpc/rpc-mode.ts` passes
`command.mode` straight to `setSteeringMode`/`setFollowUpMode` and
`command.level` to `session.setThinkingLevel` without validating either, so a
`"bogus"` mode or level is stored, persisted, and reported by `get_state`.
Pier fails those commands with `mode must be all or one-at-a-time` and
`level must be a valid thinking level`, leaving the current mode and level
unchanged, so the agent never adopts a value it cannot honor. The rest of the
command surface validates field types the same way.

Test: `TestRPCRejectsModesAndThinkingLevelsItCannotHonor`.

### D405

**The compaction spinner shows a ticking "Compacting" or "Auto-compacting"
label.**

Reference: pi v1.5.0 (f9a26365) `modes/interactive/components/status-indicator.ts`
`CompactionStatusIndicator` prints a sentence plus a cancel hint and no clock:
`Compacting context... (escape to cancel)` for a manual compaction,
`Auto-compacting... (escape to cancel)` at the threshold and
`Context overflow detected, Auto-compacting... (escape to cancel)` for overflow.

Pier prints `Compacting (12s)` for a manual run and `Auto-compacting (12s)` for a
threshold or overflow run: two short labels in place of the three sentences and
the hint, with the working indicator's elapsed-time suffix
(`tui.Loader.SetElapsed`, D402) refreshed on each whole second, so a long
compaction shows a moving clock instead of a static line. The reason still
reaches the session event and the RPC `isCompacting` state, and escape still
aborts a compaction because `handleCompactionStart` swaps `Editor.OnEscape`;
only the hint text and the overflow prefix are gone.

Test: `TestCompactionIndicatorShowsOneTickingLabel`.

### D406

**A live compaction notice reports how long the run took.**

Reference: pi v1.5.0 (f9a26365) `modes/interactive/interactive-mode.ts`
`addCompactionCostNotice` prints `${label}: ${formatTokens(tokens)} tokens
billed${cost}` and nothing about time.

Pier prefixes the elapsed time of a live compaction in the same form as the
"Worked for" line, e.g. `Compaction: 6m 18s · 2.0k tokens billed (~$0.03)`,
measured from the compaction start event to its result, so a multi-minute
auto-compaction reads as a run rather than an instant charge. A replayed notice
leaves the duration zero and keeps the reference text, because a session stores
the summary usage but not the run's wall time.

Tests: `TestTranscriptNotices`, `TestEventDispatcherCompaction`.

### D407

**A shell call without a timeout stops after 600 seconds.**

Reference: pi v1.5.0 (f9a26365) `packages/coding-agent/src/core/tools/bash.ts`
declares `timeout` as "Timeout in seconds (optional, no default timeout)" and
sets no timer when it is undefined, so a command that never exits (a pager, a
prompt waiting for input, a server or watcher run in the foreground) blocks the
turn until the user aborts it, and the abort ends the turn instead of returning
control to the agent.

Pier applies a 600-second timeout to a call that names none, through upstream's
own timeout path: the process tree is killed and the call fails with the output
so far, which the session stores in the call's tool result like any other, plus
`Command timed out after 600 seconds (the default timeout; pass a larger timeout
if the command needs longer)`. The schema's description says `(optional,
defaults to 600)` so the model knows to pass a longer timeout. An explicit
timeout replaces the default and keeps upstream's message. `!` commands run
through the user bash executor and are not affected.

The default is validated where upstream validates an explicit one: before the
shell is resolved, before the working directory is checked and before the
command is spawned, so a call whose timeout is rejected reports that error and
never runs the command. An earlier version checked the timeout after
`cmd.Start()`, which left every rejected call's shell spawned and unreaped.

Tests: `TestBashDefaultTimeoutEndsAHungCommand`,
`TestBuiltinToolSchemasMatchUpstream`, `TestBashRejectedTimeoutDoesNotSpawn`,
`TestBashTimeoutValidationPrecedesOtherInputChecks`.

### D408

**An inline image's declared canvas is bounded before a decoder allocates it.**

Reference: pi v1.5.0 (f9a26365) `packages/coding-agent/src/utils/image-resize-core.ts`
`resizeImageInProcess` decodes through Photon (`PhotonImage.new_from_byteslice`)
inside one `try`/`catch` that returns `null` on any throw, and
`utils/image-process.ts` `processImage` turns that `null` into
`[Image omitted: could not be resized below the inline image size limit.]`.
Photon runs in WASM, so a canvas larger than its linear memory traps as a JS
exception and takes the omission path.

Go's decoders allocate the whole canvas from the header before they read any
pixel data, and a failed allocation is a fatal runtime error that no `recover`
catches. A 74-byte PNG declaring a 30000x30000 canvas allocated 3.4 GiB before
reporting missing pixel data, and a larger declaration aborted the process
outright, so the same input that is an omission message upstream killed the
agent.

`decodeInlineImage` reads the header with `image.DecodeConfig` first and refuses
a declared canvas above 100 megapixels (which keeps every phone photo, medium
format capture and stitched panorama inside the bound while capping the
transient decode allocation), reporting the same omission message as a decode
that failed. `EncodeImageAsPNG` reaches the guard through the same decoder, so
the kitty transcode path is bounded too.

Tests: `TestProcessImageRejectsAnOversizeImageCanvas` (the allocation stays under
256 MiB for a 74-byte 30000x30000 PNG and the result is the omission message)
and `TestDecodeInlineImageKeepsNormalImages` (the control: 1x1 and 3000x100
still decode).

### D409

**Image base64 is decoded the way `Buffer.from(data, "base64")` does.**

Reference: pi v1.5.0 (f9a26365) `packages/coding-agent/src/utils/tool-result-images.ts`
`normalizeToolResultImages` and `core/agent-session.ts` `_normalizePromptImages`
both decode a block with `Buffer.from(block.data, "base64")` before calling
`processImage`. Node's decoder ignores characters outside the alphabet, accepts
either the standard or the URL-safe one (mixed freely), needs no padding, and
stops at the first `=`.

Pier used `base64.StdEncoding.DecodeString`, which rejects all of those. A block
whose payload was unpadded or URL-safe failed to decode, and both call sites then
keep the original block (a deliberate choice: a tool's output is never silently
deleted). The block therefore skipped the inline limits entirely: the same
3000x3000 PNG that is resized to 2.5 MB as padded standard base64 traveled at its
full 48 MB encoding when the tool emitted it unpadded or URL-safe, which is what
`base64 -w0`-style output and many encoders produce.

`DecodeImageBase64` ports Node's rules, and the tool-result normalizer, the
prompt-image normalizer, the kitty PNG transcoder and the MCP resource-blob path
all use it, so a payload is bounded identically whatever alphabet carried it.
The decoder returns one flag for "nothing usable", which keeps the existing
keep-the-original-block behavior for a payload that is really not base64; the MCP
path turns the same flag into its `invalid base64` note. Consolidating also
removed a second, hand-rolled copy of these rules in `coding/mcptools.go`.

Tests: `TestDecodeImageBase64MatchesNode` (padded, unpadded, URL-safe, mixed,
wrapped, padded-then-trailing-garbage, and the single-trailing-character drop,
each against bytes measured from Node) and
`TestToolResultImagesNormalizeUnpaddedAndURLSafeBase64` (all three alphabets
produce the same normalized image).

### D410

**The footer's git watcher polls the reftable table list too.**

Reference: pi v1.5.0 (f9a26365) `packages/coding-agent/src/core/footer-data-provider.ts`
watches the directory holding `HEAD` and, when the repository has a
`reftable` directory, also `.git/reftable` and `.git/reftable/tables.list`,
because "branch switches update files in the reftable directory instead of
HEAD". Pier replaces those fs events with a 500 ms poll (D75), and it polled
`HEAD` alone, so in a reftable repository it never saw a switch: the branch it
resolved at startup stayed on screen for the rest of the session. Such a
repository keeps `HEAD` at `ref: refs/heads/.invalid` permanently, so no
content comparison of `HEAD` can ever change. The poll now compares `HEAD` and
the table list together, and the table list is resolved once at setup, matching
upstream's one-time `existsSync` checks.

The `.invalid` sentinel itself is still resolved with `git symbolic-ref`, as
upstream resolves it. Upstream also watches the reftable directory, which the
port does not need: every reftable mutation rewrites `tables.list`.

Tests: `TestFooterDataProviderSeesBranchSwitchInReftableRepo` and the files
backend control `TestFooterDataProviderSeesBranchSwitchInFilesRepo`.

### D411

**Skill ignore files read bracket expressions with git's `wildmatch` grammar.**

Reference: pi v1.5.0 (f9a26365) `packages/coding-agent/src/core/skills.ts` hands
every `.gitignore`/`.ignore`/`.fdignore` line to the pinned `node_modules/ignore`,
whose `scanBracket` follows git's `wildmatch.c`. Pier translates each rule to an
RE2 pattern instead, and its bracket handling was a verbatim body copy: a
leading `^` became the literal member `^` (so `[^a]` matched `a` and `^` and
nothing else), `[:name:]` produced `[[:alpha:]` and the whole rule was dropped,
a leading `]` was read as the terminator, a reversed range (`[c-a]`) was dropped,
and an unterminated `[` matched literally instead of aborting the pattern.

`scanIgnoreBracket` now ports the member loop: `!`/`^` negate, a leading `]` is a
member, `\` escapes the next member, the twelve POSIX ASCII classes expand
(an unknown one aborts the pattern), an out-of-order range keeps its lower
bound, an unterminated expression aborts the pattern, and an escaped trailing
backslash aborts it. A bracket expression never matches `/`: a negated class
excludes it, and a positive one has `/` cut out of every interval that spans it,
since RE2 has no lookahead to subtract it the way node-ignore does.

Case-insensitivity is now the regexp `i` flag rather than folding the pattern
first. node-ignore compares raw characters when deciding whether a `-` opens a
range, so `[a-Z]` drops the reversed range and keeps `a`, while a folded
`[a-z]` matched every letter. `[[:upper:]]` and `[[:lower:]]` therefore expand
case-insensitively, which is what the pinned matcher's `i` flag makes them do.

Known remaining difference: node-ignore's `ignores()` also walks parent
directories, while the loader prunes a matched directory and never descends.
A single-segment comparison agrees; a rule whose effect depends on an
intermediate parent alone (`a/b` against the path `a/b/c`, where `a/b` is a
file-level pattern) can differ. No rule in the corpus depends on it.

Tests: `coding/skills_ignore_bracket_test.go`
(`TestSkillIgnoreBracketExpressionsMatchNodeIgnore`,
`TestSkillIgnoreUnterminatedBracketMatchesNothing`,
`TestSkillIgnoreMatchesCaseInsensitivelyWithoutFoldingThePattern`) and the
extended
`coding/testdata/skills-ignore/golden.json` cases `class-caret-negation`,
`class-posix-alpha`, `class-posix-digit`, `class-posix-upper`, `class-range`,
`class-escaped-close`, `class-unterminated` and `class-unknown-posix`, read by
`TestSkillsIgnoreDiscoveryMatchesUpstream`.

### D412

**An SGR parameter wider than an int is clamped, not wrapped.**

Reference: pi v1.5.0 (f9a26365) reads SGR parameters with JavaScript's
`Number.parseInt`: `packages/tui/src/utils.ts` `AnsiCodeTracker.process` and
`packages/coding-agent/src/core/export-html/ansi-to-html.ts` `applySgrCode`
(the latter as `parseInt(p, 10) || 0`). A field wider than 2^53 parses to a huge
Number that is truthy and matches no SGR branch, so the sequence leaves every
attribute untouched.

Go's `strconv.Atoi` fails on such a field and the port stored `0`, which is the
reset code: one malformed parameter in the middle of a styled line dropped every
attribute for the rest of it. `tui/tracker.go` `parseSGRNumber` accumulated into
an `int` instead, so `\x1b[18446744073709551616m` wrapped to exactly `0` and did
the same across a wrapped line. Both readers now clamp at `maxSGRParameter`
(2^20, past every code either parser reads), so a wide field falls through every
branch like the reference's huge Number.

`color256ToHex` is unchanged: the reference computes a gray ramp with plain
Number arithmetic, so an out-of-range 256-color index already produced the same
odd hex on both sides, and clamping it was a divergence, not a fix. A 256-color
index above `maxSGRParameter` therefore differs (both sides emit a nonsensical
color, in different nonsense).

Tests: `tui/tracker_overflow_test.go`
(`TestTrackerIgnoresOutOfRangeSGRParameter`,
`TestTrackerKeepsUnderlineAcrossOutOfRangeSGRParameter`),
`tui/tracker_sgrparameter_test.go` (`TestParseSGRNumberClampsWideFields`,
`TestParseSGRNumberNeverWrapsToReset`),
`coding/interactive/exporthtml_ansi_overflow_test.go`
(`TestAnsiToHTMLIgnoresOutOfRangeSGRParameter`,
`TestAnsiToHTMLKeepsStylesAcrossOutOfRangeParameter`) and
`coding/interactive/exporthtml_sgrparameter_test.go`
(`TestAnsiSGRParameterClampsWideFields`,
`TestAnsiSGRParameterNeverWrapsToReset`).

### D413

**Two reference grammars stop matching where the port keeps going.**

Reference: pi v1.5.0 (f9a26365) renders highlight.js 10.7.3. Two of its grammars
have rules that do not match the text they were meant to match, and a
bug-for-bug port would color less than the port does.

`languages/nginx.js` builds its third variable variant as
`begin: /[$@]/ + hljs.UNDERSCORE_IDENT_RE`. In JavaScript a RegExp plus a string
is string concatenation, so the reference compiles `/[$@]/[a-zA-Z_]\w*` — a
literal `/`, a `$` or `@`, another `/` — and matches no nginx variable at all:
`echo $HOME`, `set $x 1;` and `map $a $b { }` all render uncolored variables.
Pier writes the pattern the grammar meant (`[$@]` + `[a-zA-Z_]\w*`) and colors
them.

`languages/ruby.js` ends its variable rule with
`(?=[^@$?])(?![A-Za-z])(?![@$?'])`. The regex engine backtracks `(\w+)` to
satisfy those lookaheads, so the reference drops the last word character of a
variable that ends the input: `@a42` renders `@a4` plus a plain `2`, and
`x = @decorator42` renders `@decorator4` plus `2`. RE2 has no lookahead, so the
port checks the following character in a before-begin guard and declines the
match instead, leaving the text uncolored; a variable followed by anything else
matches in both. Reproducing the reference exactly would color a truncated name,
so the port keeps declining.

Neither difference is deliberate in the sense of a design choice the port made
first: both were found by a differential run of 4,584 documents over the 191
registered languages, where they are the only mismatches. They are recorded here
because the port keeps its own output.

The nginx variant colors a variable but scores nothing, because the reference's
broken pattern adds no relevance either: without that, `highlightAuto` picked
nginx for perl, bash and processing snippets where the reference picks those
languages. A second differential run of 444 documents through
`hljs.highlightAuto` matches the reference on every document once the variant's
relevance is pinned.

Tests: `internal/highlight/reference_grammar_quirks_test.go`
(`TestNginxVariablesAreHighlighted`, `TestRubyVariableAtEndOfInputIsNotPartial`)
and `internal/highlight/autodetect_nginx_score_test.go`
(`TestNginxVariableVariantScoresNothing`).

### D414

**Request-level `samplingParams` needs an explicit order channel to match the
reference's key order.**

Upstream keeps a request's `samplingParams` as a JavaScript object, so
`Object.assign` onto the request body and `JSON.stringify` emit its keys in
insertion order. A Go map has no order, so the port sorts the keys it was not
told the order of: `{top_k: 5, min_p: 0.1}` becomes `"min_p":0.1,"top_k":5`
where the reference sends `"top_k":5,"min_p":0.1`.

`StreamOptions.SamplingParamsOrder` is the channel. The builtin catalog uses it
for `Model.SamplingParams` (from the raw JSON keys), so model-level parameters
already match the reference byte for byte; a caller that builds a request-level
map from JSON should fill it the same way. Omitting it is supported and
deterministic, just not the reference's order.

Merging is unaffected: an extra that names an existing field keeps that field's
position and takes the extra's value, as JavaScript assignment does.

Test: `ai/sampling_order_channel_test.go`
(`TestRequestSamplingParamsOrderChannel`).

### D415

**A WebP chunk length is read unsigned, so a malformed RIFF chunk reports no
orientation instead of hanging.**

`utils/exif-orientation.ts` folds a chunk's four length bytes into a signed
int:

```ts
const chunkSize = bytes[o+4] | (bytes[o+5] << 8) | (bytes[o+6] << 16) | (bytes[o+7] << 24);
```

A length with the high bit set is therefore negative, and both uses misbehave:

- an `EXIF` chunk of `0x80000000` passes `dataStart + chunkSize > bytes.length`
  and `chunkSize >= 6`, so the size is ignored and the chunk's bytes are read as
  TIFF, reporting whatever orientation they encode;
- a non-`EXIF` chunk of `0xFFFFFFF8` computes `offset = dataStart + chunkSize +
  chunkSize % 2` back to its own start, so `while (offset + 8 <= bytes.length)`
  never advances. Measured on the pinned source: still looping after 3 seconds;
  the process needs a signal.

The port reads the length as unsigned and rejects a chunk that does not fit,
so a malformed WebP reports orientation 1 (the image is embedded unrotated) and
returns immediately. A truncated or hostile image cannot cost more than the
scan of its own bytes, which is what the rest of the sniffing code guarantees.

Test: `coding/exiforientation_chunk_sign_test.go`
(`TestExifWebPChunkSizeWithHighBitSet`), which also pins the two well-formed
controls.

### D416

**The worked-for notice names the time the agent finished.**

Reference: pi v1.5.0 (f9a26365) ends an agent run without a transcript line;
pier's `Worked for` notice is itself a project addition (D402).

The notice is read for when a run ended, so it leads with the local wall-clock
time the working period finished, in 24-hour hours and minutes, then the
duration, then the tokens, then the share of them the provider served from its
prompt cache: `14:32 · 1m 23s · ↑48k ↓3.2k · CH89.4%`. No word labels the first
two fields: a clock time and a duration are told apart by their shape, and the
notice only ever appears at the end of a run, so the time cannot be read as a
start. It reads the same clock as the period (`workingClock`), so the time is
the settle that closed it, and it stays display-only like the rest of the line.
The date is left out because the line never reaches the session: every copy was
written by the process showing it, whose day is the day of the notice.

The cache share is `cacheRead` over the counted input (D402's in, which already
includes cache reads and writes), to a tenth of a percent, written tight against
its label (`CH89.4%`) to keep the line narrow. A provider that
reports no cache activity has no share to show, so the field is omitted rather
than printed as 0%; the working indicator's own label is unchanged (D417 keeps
it narrow). The compaction notice keeps the duration-first form of D406.

Tests: `TestWorkedForNoticeNamesTheLocalFinishTime`,
`TestWorkedForNoticeLeadsWithFinishTimeAndCacheShare`,
`TestWorkingIndicatorShowsElapsedAndTranscriptNotesTheRun`.

### D417

**A working label too wide for the editor border ends in an ellipsis.**

Reference: pi v1.5.0 (f9a26365)
`packages/coding-agent/src/modes/interactive/components/status-indicator.ts`
`renderInBorder` renders the loader at the border's width and keeps its first
line, truncated with no ellipsis, so on a narrow screen the label stops at the
word break the loader wrapped at (`⠋ Working (1m 16s`) and reads as complete.

When the loader wraps, pier truncates the whole unwrapped line to the border
width with an ellipsis instead (`⠋ Working (1m 16s · ↑3.4M ↓…`), so the cut is
visible and the label keeps as much of the clock and tokens as fits. A label that
fits renders exactly as upstream's, and so does a border so narrow that
upstream's first line holds only the spinner (the editor goldens pin that
case).

Tests: `TestWorkingStatusInBorderElidesOnNarrowScreens`,
`TestCustomEditorAgainstUpstreamGolden`.

### D418

**Both goal tools stay exposed for the whole session.**

Reference: the goal extension's `DesiredToolNames` (D223, D234) exposes
`get_goal` only while a goal is pursued, and withdraws it when the goal
completes, pauses or is cleared.

A provider caches the request prefix, and the tool array plus the system
prompt's tools and rules sections sit at the front of it. Toggling `get_goal`
rewrote both, so every create, complete, pause and resume re-billed the whole
conversation as uncached input: in this repository's session log one `set_goal`
create was followed by a request of 53,685 uncached input tokens with
`cacheRead` 0. Both tools are now active from session start, and `get_goal`
answers `{"goal": null}` when there is none, so a transition only appends
messages.

Test: `TestGoalToolSetStaysConstantAcrossTransitions`,
`TestGoalControllerToolExposure`,
`TestDesiredToolNamesKeepsGetGoalAfterAGoalEnds`

### D419

**A compaction summarizes from the session's own last request.**

Reference: pi v1.5.0 (f9a26365) `core/compaction/compaction.ts`
(`generateSummaryWithUsage`, `serializeConversation`): a summary is a new
request whose system prompt is the summarization prompt and whose only user
message is the serialized conversation.

That request shares no prefix with anything the provider has processed, so its
whole input is a cold prefill of a transcript copy. This repository's own
session log records it: 20 compactions, every one with `cacheRead` 0, 4,828,865
cold input tokens and $0.78 of input, the largest a 290,519-token request for
$0.087.

`coding.requestPrefixCapture` records each request the agent sends, and
`Compact` replays it with the summary instruction appended, so the provider's
prefix cache covers the conversation and only the instruction is new. The replay
uses the conversation's `SessionID`, the captured thinking level (a template
that renders effort in the system prompt would otherwise re-prefill everything)
and the conversation's `CacheRetention`, resolved the way the turn's own request
resolved it (a `cache_control` marker is part of the cached prefix, so a replay
that asked for a different retention than the turn did would miss), instead of
the summarization path's usual `CacheRetentionNone`.

The replay is the captured request extended with the context that was appended
after it (the turn's own answer, a bash execution, a custom message). Replaying
the bare request would summarize an older context than the stock path does and
drop that content; extending it keeps the cached prefix untouched, so only the
appended tail is new input.

It steps aside, and the stock serialized prompt runs, when the capture is
missing, the current context no longer starts with the captured request (the
converter is re-run and every captured message must still be the same message at
its index, compared by content rather than by pointer, because `ConvertToLlm`
allocates a fresh message for every custom message, so a `!cmd`, a goal event or
a summary in the prefix is the same conversation at a new address; that check is
how a branch switch or a cleared session reads), the context now
ends in an unanswered tool call (which cannot be the last message of a request),
the model or provider changed (a wire format and its cache are per
model), the replay plus instruction would not fit the context window (overflow
recovery is where the window is the problem), or the answer cannot become a
checkpoint: a length stop, a tool call, an error. A captured request speaks only
for the branch that was current when it was sent, so the capture is released
when a compaction spends it, when the session is disposed, and when the session
replaces its context wholesale (`NavigateTree`, which also carries `/clear`):
replaying it after a branch switch would summarize a conversation the new branch
never had, and no conversion-level check can stand in for that, because the
capture holds the converted wire messages, not the live ones. A failed replay's usage still
joins the summary that replaces it. Because a replay describes everything above
the instruction, it produces one conversation checkpoint and no separate
split-turn section; a stock compaction keeps that structure (D264).

Tests: `TestCompactSummarizesFromTheCapturedPrefix`,
`TestSessionCompactionReplaysTheLastTurnPrefix`,
`TestCompactFallsBackWhenThePrefixModelDiffers`,
`TestCompactFallsBackWhenThePrefixSummaryDoesNotFit`,
`TestCompactSkipsThePrefixWhenTheWindowIsFull`,
`TestCompactPrefixInstructionFollowsTheConversation`,
`TestCompactReplaysTheCapturedCacheRetention`,
`TestSessionCompactionSkipsThePrefixAfterBranchNavigation`,
`TestSessionCompactionReplaysAppendedContext`,
`TestSessionCompactionReplaysAcrossTurns`,
`TestSessionCompactionReplaysPastACustomMessage`.

### D420

**A status call on a finished goal says so.**

Reference: the installed goal extension's `machine.ts` (the extension source,
D223). Its `set_goal` status path hands a `complete` change to the state
validator, and a goal that is already complete has no transition to complete,
so the model received `illegal transition complete -> complete`: the phase
graph's own wording, naming a state machine the model cannot see or act on. A
real session made exactly that call after finishing its goal.

The port answers instead: `Goal is already complete. Set an objective to start
a new goal.` The phase graph's error stays as it is for an impossible
transition the port never produces itself. The sibling refusals already name the
current state and the next step (a stale revision is answered with the current
id and revision), and this one now matches them. `blocked` on a finished goal
keeps its own guard, which reports the round count.

`blocked` on a finished goal is answered the same way. It used to hit the
round-count guard first, which told the model to keep working over a goal it
had already finished.

`/goal resume` on a finished goal is answered the same way; it used to reach
the validator and report `illegal transition complete -> active`.

Tests: `TestSetGoalCompleteOnAFinishedGoal`,
`TestSetGoalBlockedOnAFinishedGoal`, `TestGoalResumeOnAFinishedGoal`.

### D421

**A proxy event's content index is bounded.**

Reference: pi v1.5.0 (`f9a26365`) `packages/agent/src/proxy.ts`
(`processProxyEvent`) assigns `partial.content[proxyEvent.contentIndex]`. A
JavaScript array accepts an arbitrary index as a sparse entry, so an
out-of-range index costs one slot. Go's `grow()` in `agent/proxy.go` extends the
slice to `contentIndex`, materializing every interface slot below it.

The proxy server is untrusted input: it can be a remote endpoint, so a
compromised or buggy one must not size the agent's message from one line of
JSON. Measured on the current code: a single `text_start` with
`contentIndex: 50000000` allocated 4,934,256,400 bytes and grew the partial
message to 50,000,001 blocks, before the event type was validated.

`maxProxyContentBlocks` (4096) bounds it. A real answer holds a few dozen
blocks, so the cap leaves three orders of magnitude of room. An event past it is
rejected with the same shape as D252's negative-index rejection, which the
stream reports as an error instead of panicking.

Tests: `TestProxyRejectsAnOutOfRangeContentIndex`,
`TestProxyAcceptsARealisticBlockCount`.

### D422

**A custom message's label and renderer-failure text render as data.**

Reference: pi v1.5.0 (`f9a26365`)
`modes/interactive/components/custom-message.ts` (`rebuild`) and
`components/custom-entry.ts` interpolate the entry's own text into the frame
unfiltered:

```ts
const label = theme.fg("customMessageLabel", `\x1b[1m[${this.message.customType}]\x1b[22m`);
```

`customType` is persisted in the session and comes from an extension's
`sendMessage`; a value carrying an escape sequence ran a terminal control
function (an OSC 0 title set, a mode change) in the middle of a frame. The
custom entry's renderer-failure fallback interpolated both the `customType` and
the recovered panic text the same way.

The port already treats that content as data one line below (D342: model- and
extension-generated session text renders as data), so the label and the
fallback message now pass through `tui.SanitizeUntrustedText` too. What the
reference shows for ordinary labels is unchanged.

Tests: `TestTranscriptCustomMessageLabelIsSanitized`,
`TestCustomEntryFallbackSanitizesItsText`.

### D423

**The worked-for run totals saturate.**

D369 established that session usage display sums fold in Number space and
saturate at the int64 bounds, and 06be6972 applied that to the session-info panel
and the compaction notices. The worked-for notice (D402) reintroduced the wrap:
`workingTokensOf` added `Input + CacheRead + CacheWrite` in int64 and
`workingTokenCount.plus` added each column in int64, so a run over two messages
that each reported a large cache read wrapped negative and rendered
`↑-8446744073709551616`. A single message whose own prompt fields overflow
rendered `↑-2`.

`workingTokensOf` now folds the prompt total through
`ai.CalculateContextTokens` (the same Number-space helper the panel uses), and
`plus` adds through `saturatingTokenSum`, which clamps at the int64 bounds.
Ordinary counts are unchanged. The reference computes these with JavaScript
numbers, which saturate at 2^53 instead of wrapping, so no wrap is visible
there either.

Tests: `TestWorkingTokenCountsSaturateOnOverflow`,
`TestWorkingTokenCountsKeepOrdinaryValues`.

### D424

**`TruncateMiddle` resolves its slice indices like a JavaScript Buffer.**

Reference: pi v1.5.0 (`f9a26365`) `core/tools/truncate.ts` `truncateMiddle`.
It computes `headEnd = Math.floor(maxBytes / 2)` and
`tailStart = buf.length - (maxBytes - Math.floor(maxBytes / 2))`, then slices
the Buffer with both. `Buffer.prototype.subarray` resolves a negative index from
the end and clamps either sign to the buffer, so every `maxBytes` produces a
result, and a non-positive or tiny one returns a still-truncated document:
`truncateMiddle("abc", -1)` is `"ab…1 chars truncated…"`.

The port divided with Go's truncating integer division (`maxBytes / 2`, so -1
gave 0 rather than the reference's -1) and sliced the string with the raw
indices, which panicked with `slice bounds out of range` once `tailStart` fell
below zero or past the end. `floorDiv2`, `resolveSubarrayIndex` and the
`subarray(end, start)` empty-range rule now apply the reference's arithmetic.
`LimitMcpContent` only ever passes 20 KB, so this was latent rather than
reachable from the tool path, but a panicking helper reached from rendering is
not a contract worth keeping.

`truncateTail` had the same class of gap on the other side of the window: it
pre-sized its slice with `make([]string, 0, min(maxLines, totalLines))` (in both
`TruncateTail` and `TruncateTailLines`) and panicked with `makeslice: cap out of
range` for a negative `maxLines`, where the reference's loop condition simply
never holds. Its partial-line branch then reached
`truncateStringToBytesFromEnd` with a negative length, which sliced out of
range, where `Buffer.prototype.slice` clamps and returns "". A negative
`maxLines` or `maxBytes` now returns the reference's defined empty window.

Tests: the `middle/*`, `head/*negative*`, `tail/*negative*` and `line/*/-1`
cases in `coding/testdata/truncate`, regenerated from the pinned functions by
`coding/testdata/truncate/generate.mjs`. The negative cases run over non-empty
content only: the reference reads `lines[0]` before its loop, so an empty
content throws there.

### D425

**The session-listing string scan skips plain runs a word at a time.**

Reference: `packages/coding-agent/src/core/session-manager.ts` (pi v1.5.0,
`f9a26365`), `parseSessionEntryLine` over `JSON.parse`, and
`buildSessionInfosWithConcurrency` with `MAX_CONCURRENT_SESSION_INFO_LOADS = 10`.

The reference parses each line once, in V8's native JSON parser. The port's
`scanSessionLine` shells the line and hands the message body to
`scanListingMessage`, so every message body is scanned twice; on a 260 MB
session that is 466 MB of string scanning. `scanJSONStringStrict` did it one
byte at a time with a four-way `switch`, which was 54.6% of the CPU profile of
the listing path.

`scanJSONStringStrict` now skips runs of plain bytes eight at a time with
`scanJSONStringPlainRun` and only enters the byte-at-a-time `switch` at a quote,
a backslash or a control byte. `hasZeroByte` finds those with the usual
`(word-ones)&^word&0x80..` test; a control byte is detected by its high three
bits being clear. The accept set is unchanged: any byte >= 0x80 is still plain,
and invalid UTF-8 in a string is still left to the decoder, as
`scanPlainStringBytes` already did.

Measured on the real corpus under `~/.pi/agent/sessions` (60 files, 503 MB):

- one 260 MB session, single file: reference 939-998 ms, port 1143-1176 ms
  before, 942-963 ms after;
- whole corpus: reference 1952-1976 ms, port 2214 ms before, 1777-1785 ms
  after.

`BenchmarkSessionJSONStringScanStrict` reports 737 MB/s before and 2023 MB/s
after on the same input.

Tests: `TestSessionJSONStringScanMatchesStrictScan` pins the two scanners to the
same accept set and result, `TestSessionScanAcceptsOnlyValidJSON` and
`FuzzSessionScanAcceptsOnlyValidJSON` pin the shell against `encoding/json`, and
`TestScanSessionIndexEquivalence` pins the full-file scan.

### D426

**`JSSlice` walks only the prefix it needs for non-negative indices.**

Reference: `packages/ai/src/utils/text.ts` (pi v1.5.0, `f9a26365`), `ownedSlice`:
`` `${text.slice(start, end)} `.slice(0, -1) ``. V8's `String.prototype.slice`
returns a view, so a 500-character excerpt of a megabyte body costs the excerpt.

The port encoded the whole string to UTF-16 before clamping, so every call was
O(len(s)) in time and allocation. Measured `JSSlice(body, 0, 500)` against the
old implementation in one process: 64 KB 372 µs -> 6.0 µs, 1 MB 5.71 ms ->
10.0 µs, 8 MB 34.8 ms -> 5.5 µs; allocations 6 -> 5. The callers are error-body
truncation (500 characters from a provider body), radius body truncation,
mistral error text, `SanitizeID` (64) and pi-messages ids.

`jsSliceAbsolute` walks only up to `end` and appends UTF-16 units by index, so a
cut inside a surrogate pair still yields U+FFFD exactly as the full encode does.
A negative index keeps the full encode, because `slice` resolves it against the
string's UTF-16 length.

Tests: `TestJSSliceMatchesFullEncoding` compares the fast path against the full
encoding for every index pair over ASCII, astral, combining and invalid UTF-8
inputs; `TestJSSlicePrefixDoesNotScanTheWholeString` caps its allocations at the
full encoding's.

### D427

**A collapsed goal card bounds each line, not just the line count.**

The goal card keeps the objective unmangled: `goalPreview` returns the first
`goalPreviewLines` lines rather than flattening them into a summary. That cap
counts lines, so one long line was passed through whole and `tui.Text.Render`
then wrapped it to the transcript width, emitting one output line per width
band. Measured through the real renderer (`goalSetRenderers.RenderCall`, then
`Render(80)`): a 1 MB objective on a single line drew 12,502 lines in 82 ms, and
20 lines of 200 KB drew 12,503; 100,000 short lines correctly drew 8.

`set_goal`'s `objective` is free text with no length limit in the tool schema,
and the same objective is drawn from the call's arguments, from the durable goal
entry, and by `get_goal`'s result card. `goalPreview` now also clips each kept
line to `goalPreviewChars` on a rune boundary and counts each clipped line as
hidden, so the existing `... (N more lines, click or expand for full text)` hint
covers it and an expanded card still shows the whole objective.

The goal feature has no upstream counterpart (it is pier-only), so this is a
port-side fix with no reference to cite; the bound is 240 bytes, comfortably
wider than one screen line at any terminal width the UI supports.

D223 chose a line-count cap over a character cap so a long objective keeps its
line structure instead of a mid-word cut. That still holds for a multi-line
objective; what D427 adds is a per-line bound, because an unbroken 1 MB line
has no line structure to keep. `TestCollapsedGoalCardsCapByLines` keeps
covering the multi-line shape.

Tests: `TestGoalPreviewBoundsALongLine` (1 MB single line, 20x200 KB lines and
100,000 short lines each render at most 40 lines),
`TestGoalPreviewKeepsEverythingWhenExpanded` (expanded keeps every character,
collapsed hides some), and the updated
`TestCollapsedGoalCardClipsOneOverWideLine`.

### D428

**A chat notice is bounded; the goal state keeps the full text.**

`set_goal status=blocked` stores the model's `blocked_reason` trimmed and
otherwise unbounded (`internal/goal/machine.go`), and `pauseNotice` embeds it in
a `NotifyEffect` that reaches `RunWiring.ShowChatWarning`. That sanitized escape
sequences but appended every line to the transcript. Measured through the real
path, then `Render(80)`:

| blocked_reason | chat lines before | after |
|---|---|---|
| 100 KB on one line | 1,284 | 6 |
| 15,000 short lines | 15,001 | 6 |

`internal/goal.CollapsedNoticeText` applies the same bounds D427 set for a goal
card (five lines, 240 bytes each) and names the elided remainder in the existing
expand hint. Only the chat line is bounded: the goal state, the durable entry
and the `get_goal` card keep the whole reason, and the model-facing wrap-up
message carries it unchanged, so nothing that needs the full text loses it.

The goal feature has no upstream counterpart, so there is no reference to cite.

Tests: `TestChatNoticeIsBounded` (a 100 KB single line and a 15,000-line reason
each add at most eight chat lines) and `TestChatNoticeKeepsAShortMessage` (an
ordinary notice is unchanged).

### D429

**A JSON Schema `type` array keeps its members even when some are not strings.**

Reference: `packages/ai/src/api/constrained-sampling.ts` (pi v1.5.0,
`f9a26365`), line 40:
`typeof schema.type === "string" ? [schema.type] : Array.isArray(schema.type) ? schema.type : []`.
The membership tests compare each member against a type name, so a non-string
member can never match but does not remove its neighbours.

The port decoded the member into `[]string`, so one non-string element failed
the whole decode and `schemaTypes` returned nil. `isStructuredSchema` then
answered false for `{"type":["object",5]}` and the `anyOf` guard accepted a
union the reference rejects: `{"anyOf":[{"type":["object",5]}]}` built a strict
schema in the port and threw `object and array unions are unsupported` in the
reference.

`schemaTypes` now decodes the array into raw members, keeps the ones that are
JSON strings, and ignores the rest, which answers identically for every
membership test. The string form and a `type` that is neither a string nor an
array keep their previous answers. Two decode quirks are handled explicitly
because `encoding/json` decodes a JSON `null` into a Go string as `""` with no
error: a `type` of `null` previously produced the single type name `""`.

Tests: `TestSchemaTypesKeepsStringsFromAMixedArray` and
`TestMixedTypeArrayIsAStructuredUnion`, whose plain-object variant is the
control that both implementations reject.

### D430

**Only a JSON string becomes a text block for custom-message content.**

Reference: `packages/coding-agent/src/core/messages.ts` (pi v1.5.0,
`f9a26365`), line 163:
`typeof m.content === "string" ? [{type:"text", text: m.content}] : m.content`.
`typeof null === "object"`, so a null content passes through and adds nothing.

The port decoded the member into a Go string first, and `encoding/json` decodes
a JSON `null` into a string as `""` without an error, so a null content became
one empty text block: `ConvertToLlm` on `{"customType":"note","content":null}`
produced `[{"type":"text","text":""}]` where the reference produces no block.
That stray block reached every following request.

`customContentToBlocks` now requires the raw member to start with a quote before
treating it as string content, then falls through to the block path and finally
to the raw-text fallback. An empty string is still string content, as it is
upstream.

Tests: `TestCustomContentNullAddsNoBlock` (null, empty string, plain string and
block content) and `TestCustomMessageNullContentReachesNoBlock` (the converted
request carries no block).

### D431

**A null identity version reads as absent, not as an explicit empty version.**

Reference: `packages/ai/src/utils/identity.ts` (pi v1.5.0, `f9a26365`), line 63:
`identity?.version ?? PI_AI_VERSION`. `??` treats `null` like absent, so
`{"version":null}` renders the default version while `{"version":""}` renders an
empty one.

The port's decoder set `VersionSet` for any present `version` member, including
JSON null, so a stored null read as the explicit empty version and
`FormatUserAgent` emitted `pi/ (linux; ...)` with no version at all.
`isJSONNull` now keeps that member absent, and the explicit-empty case stays for
`""`, which is what `VersionSet` exists for.

A second null case in the same decoder is left alone deliberately:
`{"userAgent":{"mode":"custom","custom":null}}` reads as an empty custom string,
where the reference passes a null `custom` through. Neither value is a string a
header can carry, so there is no observable difference worth a special case.

Tests: `TestIdentityNullVersionUsesTheDefault` (null uses the default, `""`
stays explicit, a real version still wins).

### D432

**The unknown-action reply bounds the value it echoes.**

`set_goal` echoes a rejected action back to the model so it can retry with a
legal one. `goalUpdate` interpolated that value whole, so a 100,000-character
action produced a 100,057-character reply: the model's own malformed input
spent context to be read back verbatim.

The sibling command path already bounds the same echo: `command.go` prints
`Unknown subcommand "…"` through `TruncateObjective(trimmed, 20)`. `goalUpdate`
now uses the same bound, which also flattens whitespace and keeps the quoting
intact. A short typo is still shown as written.

The tool path bounds nothing extra: `coding/goalcontroller.go` rejects an
unknown status before dispatching, so this reply reaches the model only when
the machine is driven directly. It is fixed at the machine because that is
where the echo is written, and because the next caller should not have to
remember the rule.

The goal feature has no upstream counterpart, so there is no reference to cite.

Tests: `TestUnknownActionReplyIsBounded` and
`TestUnknownActionReplyKeepsAShortValue`.

### D433

**`FooterTruncate` returns a text that already fits.**

Reference: `packages/tui/src/utils.ts` (pi v1.5.0, `f9a26365`),
`truncateToWidth` lines 1192-1205, which `footer.ts` calls. When the ellipsis
does not fit (`ellipsisWidth >= maxWidth`) it first returns the text unchanged
if the text itself fits, and only then clips the ellipsis.

The port short-circuited every `maxWidth <= 3` to `"..."[:maxWidth]` before any
width check, so a fitting footer value became dots:

| text | width | reference | port before |
|---|---|---|---|
| `ab` | 2 | `ab` | `..` |
| `a` | 2 | `a` | `..` |
| `a` | 1 | `a` | `.` |
| `` | 2 | `` | `..` |
| `abc` | 3 | `abc` | `...` |

`FooterTruncate` renders the footer's cwd basename at 25 columns and the joined
status line at `min(80, width)`, so a narrow terminal reaches that branch. The
fix applies the fits-check first, in the reference's order; texts wider than the
budget still collapse to the ellipsis (`日本語` at 2 is `..` in both).

`tui.TruncateToWidth` already ports this branch correctly but emits ANSI resets,
which the footer's plain strings cannot carry, so the plain helper stays and
gains the missing check.

Tests: `TestFooterTruncateKeepsFittingText` (fitting and non-fitting cases at
widths 1-4, plus a CJK case).

### D434

**The loaded-resources listing sorts with root collation.**

Reference: `modes/interactive/interactive-mode.ts` (pi v1.5.0, `f9a26365`):
`formatScopeGroups` lines 1778-1787 sorts a group's paths, its package sources
and each package's paths with `a.localeCompare(b)`, and `formatCompactList`
lines 1888-1893 sorts its labels the same way.

The port used `sort.Slice`/`sort.Strings`, i.e. UTF-8 byte order, so five of six
corpora list in an order the reference never produces:

| input | reference | port before |
|---|---|---|
| `a/b.md`, `a/B.md`, `a/c.md` | `a/b.md`, `a/B.md`, `a/c.md` | `a/B.md`, `a/b.md`, `a/c.md` |
| `Zebra.md`, `apple.md`, `Banana.md`, `cherry.md` | `apple.md`, `Banana.md`, `cherry.md`, `Zebra.md` | `Banana.md`, `Zebra.md`, `apple.md`, `cherry.md` |
| `a/b/c.md`, `a/b-c.md`, `a/b_c.md`, `a/b.c.md`, `a/b1.md` | `a/b_c.md`, `a/b-c.md`, `a/b.c.md`, `a/b/c.md`, `a/b1.md` | `a/b-c.md`, `a/b.c.md`, `a/b/c.md`, `a/b1.md`, `a/b_c.md` |

D257 already settled this question for model and footer ordering by choosing
`golang.org/x/text/collate` root collation over byte order or a host locale;
this call site was missed. The same collator now sorts the group paths, the
package sources, the per-package paths and the collapsed label list. Root
collation reproduces `localeCompare` on all six corpora (0 diffs), including
case folding and `ä`. `sort:false` callers keep their discovery order.

Reachable from the expanded `/loaded` listing
(`coding/interactive/loadedresources.go:314` and `:337`).

Tests: `TestFormatScopeGroupsUsesLocaleOrdering`,
`TestFormatCompactListUsesLocaleOrdering`.

### D435

**The working indicator's cache share rounds halves like JS `toFixed`.**

`cacheShare` (`coding/interactive/statusindicator.go:165`) is pier's own display
(D416), so there is no upstream string to match. The question is internal
consistency: every other one-decimal or fixed display in this module formats
through `formatFixed` -> `ai.JSNumberToFixed`, which follows JS `toFixed`
(upstream `modes/interactive/components/footer.ts:27-29`, `:162`). `cacheShare`
used `strconv.FormatFloat(share, 'f', 1, 64)`, and Go rounds halfway cases to
even.

Over `cacheRead` 1..3 and `in` 1..2000, twelve values differed; nine were
halfway cases:

| in | cacheRead | port before | JS `toFixed(1)` |
|---|---|---|---|
| 16 | 1 | `CH6.2%` | `CH6.3%` |
| 32 | 2 | `CH6.2%` | `CH6.3%` |
| 80 | 1 | `CH1.2%` | `CH1.3%` |
| 400 | 1 | `CH0.2%` | `CH0.3%` |
| 1600 | 100 | `CH6.2%` | `CH6.3%` |

Reachable whenever `2000*cacheRead/in` is an odd integer. The other three
differences are the deliberate `> 100` clamp, which stays. The share now formats
through `ai.JSNumberToFixed`; the clamp and the empty cases (no cache activity,
no counted input) are unchanged.

Tests: `TestCacheShareRoundsHalvesLikeToFixed`.

### D436

**A rendered tool path becomes a clickable file link.**

Reference: `core/tools/render-utils.ts` (pi v1.5.0, `f9a26365`),
`renderToolPath` lines 170-180 returns
`linkPath(theme.fg("accent", shortenPath(value)), value, cwd)`, and `linkPath`
lines 15-19 returns the styled text when `getCapabilities().hyperlinks` is off
and otherwise wraps it in `pathToFileURL(resolvePath(rawPath, cwd)).href`.

The port implemented only the capability-off branch: `RenderToolPath` accepted
`cwd` and never used it, so output was byte-identical whether the terminal
reported hyperlinks or not, and a path in the transcript was never clickable
while markdown links beside it were.

The fix resolves the path against `cwd`, converts it with
`coding.PathToFileURL` (already present and byte-identical to Node's
`pathToFileURL` on seven cases including `#`, `?`, `%`, `+`, `[` and non-ASCII),
and wraps the styled text with `tui.Hyperlink`. With hyperlinks off the output
is unchanged, so terminals without support see the same bytes as before.

Reachable from the `read`, `write`, `edit` and `ls` renderers.

Tests: `TestRenderToolPathLinksWhenHyperlinksAreOn`.

### D437

**An image-only queued message still clears its dock-banner entry.**

Reference: `core/agent-session.ts` (pi v1.5.0, `f9a26365`) line 1141 guards
the dequeue removal with `if (messageText)`. A queued message with no text
(images only) records `""`: `contentText(content, "")` returns `""` for a block
list with no text block, and `_queueSteer` at line 2324 pushes that value. The
guard then skips removal, so the entry survives the `message_start` that
delivers it and `pendingMessageCount` (line 2499) never returns to zero: the
dock banner keeps showing a message already in the transcript.

pier drops the empty entry, so the banner clears. The removal is by text, as in
the reference (`indexOf` + `splice`), and an empty entry can only be the one
being delivered, since `Steer` records exactly one entry per queued message.

Tests: `TestQueueDisplayRemovalHandlesEmptyText`.

### D438

**A run start is exclusive, and idle transcript appends are ordered with it.**

Reference: `core/agent-session.ts` (pi v1.5.0, `f9a26365`): `prompt()` checks
`isStreaming` before awaiting `checkAuth` and `_startRun`, and
`recordBashResult` and `sendCustomMessage` check it before appending. On one
event loop the appends cannot interleave with each other, and a second
`_startRun` that slips past the check fails in `agent.prompt` but still runs its
`finally`, which settles the session while the first run continues.

In the port those callers overlap: a goal round or a bash completion runs off
the UI loop while a user prompt runs on it. The session's run start therefore
checks `runActive` in the critical section that sets it, and a start that finds
a run already active fails with the agent's "already processing" error, leaving
the winner's run flag, cancel and idle waiters alone. Clearing them would make
the session look idle between the winner's agent runs (its follow-ups and
retries, where the agent itself is not streaming). Bash results and idle custom
messages decide "append now or defer to the run" and append, to the agent
transcript and then the session, under that same lock, so a run cannot start in
between and two appends land in the same order in memory and on disk. Only the
entry's place is fixed under the lock: on a session with no write queue the
synchronous file write is held and made after the unlock, and any other
synchronous write makes the held ones first, so the file keeps that order
(A483). The run's
end delivers anything deferred after its own flush before clearing the flag.
A prompt flushes deferred messages only while idle, under the same lock. Tree
navigation claims the session under it too, so a run and a navigation never
overlap, and compaction and navigation reload the agent transcript from the
session in one step under it.

Tests: `TestAgentSessionConcurrentAPIKeepsTranscriptAndPersistenceInStep`
(the transcript and the session file diverged in about 8 runs in 20 before),
`TestAgentSessionConcurrentHeavyAPIKeepsTranscriptAndPersistenceInStep`
(compaction, navigation and tool changes mixed in) and
`TestALosingRunStartLeavesTheActiveRunsState`;
`TestIdleBashAppendWritesTheSessionFileOutsideTheRunLock` and
`TestHeldSessionWriteKeepsFileOrder` for the held write, with
`TestFlushWritesIncludesHeldSessionWrites`,
`TestHeldSessionWriteGoesAheadOfANewlyWiredQueue` and
`TestDeferredFlushPanicStillWritesAcceptedEntries` for its edges.

### D439

**Session and message JSON decode accepts what `JSON.parse` accepts: unpaired
surrogate escapes and duplicate member names.**

Reference: `core/session-manager.ts` and `core/agent-session.ts` (pi v1.5.0,
`f9a26365`) read session lines with `JSON.parse`, which accepts `"\ud83d"` (an
unpaired surrogate escape) and keeps the last value of a duplicate member name;
`JSON.stringify` writes the surrogate back as `\ud83d`. The port decodes session
lines and message content with `encoding/json/v2` for speed (707 ms to 276 ms on
a 55 MB session), and v2's default options reject both forms at the lexer, so
`UnmarshalFileEntry` dropped the entry and `Projection` dropped the message.

The port therefore calls `json/v2` with `jsontext.AllowInvalidUTF8` and
`jsontext.AllowDuplicateNames` on the session-entry decode and on the message
decode path. Both options only admit input that failed before: anything that
already decoded is unchanged, and a message's raw bytes stay byte-identical, so
re-persisting a session the reference wrote keeps its bytes. A Go string cannot
hold an unpaired surrogate, so the decoded text carries U+FFFD while the raw
JSON keeps the escape.

This mattered most for older files: opening a v1/v2-format session migrates and
rewrites it, so the dropped entries were deleted from the user's file rather
than only hidden.

A tool definition carries its own `UnmarshalJSON`, so the rule reaches
`Tool` and `ConstrainedSamplingConfig` too: one tool with a surrogate in its
description otherwise fails its whole system message, dropping the tool set from
the replay.

Test: `TestLoneSurrogateEntrySurvivesMigration` (the entry is gone from the
rewritten file without the options), `TestLoneSurrogateMessageReachesProjection`
(the projected message count is short without them),
`TestDuplicateMemberNameEntrySurvivesLoad`,
`TestSystemMessageKeepsToolWithLoneSurrogate` and
`TestToolConstrainedSamplingAcceptsLoneSurrogate`.

### D440

**`LoadPromptTemplates` takes the trust answer, so project prompts are off by
default for an embedder.**

Reference: pi v1.5.0 `f9a26365` splits this differently. Its
`core/resource-loader.ts` calls `loadPromptTemplates` with an explicit path list
and `includeDefaults: false`, and `core/package-manager.ts` builds that list
inside `if (projectTrusted)`, so the trust filter lives in the caller and the
loader itself never sees `<cwd>/.pi/prompts` when the project is untrusted.

The port has no package manager (out of scope, D41), so the loader resolves the
default directories itself. It therefore takes `ProjectTrusted` in
`LoadPromptTemplatesOptions` and gates only the project directory on it; the
user's prompts load either way, as upstream's do. The zero value is untrusted, so
an embedder that leaves the field alone gets no project commands until it says
otherwise. That is the deliberate divergence: upstream's loader would load them
when handed the path, and the port's cannot tell without being told.

Test: `TestProjectPromptsRequireTrust` (an untrusted project's command was
loaded before the gate, alongside the user's).

### D441

**`/tree` and `/export` do their work off the owner loop and hand the result
back, where the reference blocks the event loop.**

Reference: pi v1.5.0 `f9a26365` builds the tree inside the key handler
(`interactive-mode.ts showTreeSelector` → `buildSessionTree`) and serializes the
branch inside the export handler (`core/session-export.ts`), both synchronously
in front of the UI. Its parsed session entries are already JavaScript objects, so
the tree build only walks them; the port keeps the raw line bytes and has to
decode each entry's message, which costs the same on every open.

Measured on a 25,583-entry session (53 MB), opening `/tree` took 596 ms and
`/export out.jsonl` 887 ms, both on the owner loop, so the terminal painted
nothing for that long. Parity does not outrank responsiveness, so both now take
an `offloop.Queue` and a `PostToUI` callback: the work runs on the queue, the
resulting selector or the status message is delivered on the loop, and the loop
keeps painting. A nil queue or nil `PostToUI` keeps the synchronous path, which
is what tests and embedders use.

The same commit removes repeated decoding rather than moving it: the tree
decodes each entry's message and its content blocks once (596 ms → 274 ms) and
keeps tool-call arguments raw until a row is formatted (only the visible rows
decode them), and the export skips re-chaining an entry whose stored `parentId`
already equals the previous entry's id (887 ms → 482 ms, 1.84 M → 329 k
allocations). Those two are not divergences: output bytes and tree contents are
unchanged, and the golden fixtures still match.

Tests: `TestTreeSelectorOpenBuildsOffTheOwnerLoop` and
`TestExportCommandRunsOffTheOwnerLoop`; both packages' synchronous twins,
`TestTreeSelectorOpenWithoutAQueueStaysSynchronous` and
`TestExportCommandWithoutAQueueStaysSynchronous`, pin the fallback.
`BenchmarkTreeOpen` and `BenchmarkSessionExport` pin the cost.

### D442

**The session-file work of `/resume`, `/fork`, `/clone`, `/tail`, `/import` and
the picker's rename runs off the owner loop; the UI rebind stays on it. The
picker's delete stays inline.**

Reference: pi v1.5.0 `f9a26365` does this work inline in the handler
(`agent-session-runtime.ts switchSession`/`fork`/`importFromJsonl`,
`session-manager.ts` delete). Its file operations are async `fs` calls in an
event-loop runtime that yields between them, so the blocking part is small; in
the port the same operations are synchronous system calls on the goroutine that
paints.

Measured on a 25,583-entry session (53 MB), each on the owner loop: opening the
file for `/resume` 57 ms, writing the fork's new file for `/fork` and `/clone`
519 ms, copying an import 22 ms, and a rename of a session other than the active
one opens its file (57 ms). The first paint after each of those started late by
that much.

Each of those is now split: the file work runs on the session-file queue, and
the rebind (which owns attached UI state, the MCP manager and the transcript)
runs on the loop, delivered through `postToUI`.

The picker's delete was moved the same way and then put back, and its cause
turned out to be the subprocess itself: in a fullscreen session the confirmation
prompt cleared and nothing else happened, because the worker was parked in a
`trash` that never exited. D443 removes the subprocess, which both frees the
loop and makes the inline path a syscall, so the delete stays inline. The three processes share one
queue slot, so a switch, fork, import or tail started later supersedes an
earlier one instead of applying over the session the user chose; a rename keeps
its own slot, because cancelling it would leave the picker waiting for a report
that never comes. The queue and the post seam are wired only when the command
supplies an off-loop group: a test app or an embedder without a loop runs the
work inline, where it is also the only writer.

The synchronous entry points stay: `switchSession`, `importFromJSONL`,
`forkAtEntry`, `tailSession` and the picker's deleter are what the async paths
fall back to, so nothing depends on a loop that happens to be running.

Tests: `TestSwitchSessionOpensOffTheOwnerLoop`,
`TestForkSessionWritesOffTheOwnerLoop`, `TestImportCopiesOffTheOwnerLoop`,
`TestRenameOtherSessionOpensOffTheOwnerLoop` and
`TestPickerRenameWaitsForItsReport`; the delete stays covered by
`TestSessionSelectorDeleteFlow`.

### D443

**Deleting a session unlinks the file; the reference hands it to the `trash`
CLI first.**

Reference: pi v1.5.0 `f9a26365` `session-selector.ts` `deleteSessionFile` runs
`trash <path>` and only unlinks when that fails, so a mistaken delete can be
undone from the desktop trash.

A `trash` that does not exit blocks whoever waits for it. In the port that was
the owner loop: `/resume`'s Ctrl+D froze the terminal with no status and no way
out, and the off-loop version looked like a delete that did nothing, because the
worker stayed inside the subprocess and the result never came back. The reporter
hit both. `DeleteSessionFile` now calls `os.Remove` and reports `unlink`, so the
pick pause is a syscall. A missing file counts as deleted, so a repeated delete
is not an error.

The cost is the recoverability the reference buys: a deleted session is gone,
not in the trash. The picker still asks for confirmation, and the row names the
session it is about to delete.

Tests: `TestDeleteSessionFileDoesNotRunTheTrashCLI` (a `trash` stub on `PATH`
that records being run), `TestDeleteSessionFileMissingFileIsNotAnError` and
`TestDeleteSessionFileReportsAnUnlinkFailure`; the picker flow stays covered by
`TestSessionSelectorDeleteFlow`.

### D444

**The MCP manager a session replacement retires closes on the session queue; the
reference awaits its shutdown before the replacement starts.**

Reference: pi v1.5.0 `f9a26365` `extensions/mcp/index.ts` `reload()` awaits
`session_shutdown` before `session_start` reconnects, and the port's
`coding.ReloadMCPExchange` did the same: `manager.Close(ctx)` inline, then the
new manager.

The port's rebind runs on the owner loop, and closing one stdio server that
ignores stdin close and SIGTERM costs the transport's full sequence: 500 ms of
stdin-close grace, then SIGTERM, then 2000 ms before SIGKILL
(`mcp/transport_stdio.go`). Measured end to end on a `sh -c 'trap "" TERM; sleep
3600'` server: `Close` 2.502 s, `ReloadMCPExchange` 2.503 s, so every session
switch froze the UI for 2.5 s per such server. The user's own machine produced
exactly this class of hang once, with `trash` never exiting (D443).

Nothing waits on the answer. The replacement connects from the re-read config
independently, and `wireMcpManager` ignores callbacks from a manager that is no
longer attached, so a late close cannot disturb the new session. The cost is a
brief overlap in which both managers exist: a hung server is being killed while
its replacement starts, and a healthy one closes in microseconds. The close runs
inline only when there is no queue, which is a test app and also its only writer
(D442).

Tests: `TestMCPReplacementDoesNotWaitForTheClose`, which blocks in the transport's
`Close` and fails if the rebind waits for it.

### D445

**The session picker sanitizes a stored cwd and path; the reference renders them
verbatim.**

Reference: pi v1.5.0 `f9a26365` `modes/interactive/components/session-selector.ts:466`
puts `shortenHomePath(session.cwd)` into the row as-is, and the path toggle does
the same with the session path.

Both strings are untrusted in the port's terms: `SessionInfo.Cwd` is the session
header's `cwd` copied straight out of a JSONL file (`coding/session_manager.go`),
so a shared, imported or otherwise planted session file chooses the bytes shown
in every row of the all-projects scope. Escape sequences there reach the terminal
verbatim, which is a clipboard write away from `\x1b]52;c;…\x07` and a screen
clear away from `\x1b[2J`. The row already sanitized its name and message
columns, and D342/D422 make sanitizing untrusted terminal text the port's rule,
so the metadata columns now get the same treatment: escape sequences are dropped
and remaining control characters become spaces.

Tests: `TestSessionListSanitizesStoredCwd`.
