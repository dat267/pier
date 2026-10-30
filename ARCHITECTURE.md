# Architecture and performance

This is the design reference for `pier`, a Go port of pi v1.4.0
(`4a080252`). [AGENTS.md](AGENTS.md) contains contributor rules; use `git log`
for release and review history. The D-rows below record deliberate differences;
record deliberate differences; the lock inventory is the source of truth for
retained and retired UI locks.

## Runtime structure

Packages mirror upstream: `ai` owns provider/wire behavior, `agent` the agent
loop, `coding` sessions/tools/settings, `coding/interactive` the application,
`tui` terminal rendering, and `mcp` protocol support. `cmd` composes startup;
`main.go` only calls `cmd.Execute`.

The interactive application is single-writer. Its owner loop handles input,
session events, UI state and painting. Producers enqueue; blocking work runs on
owned workers or bounded queues. Workers publish results through guarded UI
posts and never mutate attached components. Callbacks are snapshotted under
locks and invoked after unlocking. This matches upstream's single-threaded
ownership while allowing Go I/O and expensive preparation off-loop (D143).

Key bounds and invariants:

- Lossless and partial session events use separate queues (256 and 8 entries);
  partials are latest-wins. Each pre-paint drain handles at most 64 events so
  input and cancellation continue to make progress (D196–D197).
- Optional work is admitted per domain; cancellation rejects late UI posts.
  Mandatory session/settings writes drain on shutdown. Optional work can be
  abandoned, but cancellation cannot interrupt every filesystem or OS call
  (D199–D201).
- Terminal output is serialized by an off-loop FIFO. Interactive paints pause
  under backpressure, but committed differential frames are not dropped. A
  single bounded shutdown grace may leave display bytes unwritten; saved
  sessions remain durable (D198, D200, D202).
- Large transcript, Markdown and tool-result preparation uses bounded,
  immutable snapshots where supported (D203–D209). Ownership and exceptions are
  detailed in the D-rows and lock inventory.

## Performance evidence

Measurements below are local probes, not guarantees. Methods and fixture sizes
are recorded with the corresponding changes and regression tests.

| Workload | Before | After | Change |
|---|---:|---:|---|
| Catalog decode | 69 ms / 16.2 MB | 17 ms / 2.45 MB | Decode each descriptor once |
| Branch preparation, 8,000 messages | 540 MB / 253 ms | 1.07 MB / 10 ms | Reverse once instead of repeated prepends |
| 8 MiB MCP SSE event | 8.6 GB / 2.49 s | 42 MB / 21 ms | Incremental line processing |
| Cold first interactive frame | 63.06 ms | 49.21 ms | Warm-filesystem tmux median |
| Resume 56 MB session | 174.74 ms | 147.98 ms | Warm-filesystem tmux median |

Startup timings used a warm filesystem with network and MCP disabled; they are
not additive guarantees. Catalog and replay optimizations retain allocation and
byte-parity regressions.

## Session model

`SessionManager.Projection` is the canonical branch-versioned view for model
context. Do not rebuild it by hand or rescan entries per message. Session writes
preserve JavaScript JSON bytes and key order through `ai.MarshalJSON`. Cached
model/message reads preserve isolation and identity only at validated canonical
boundaries; see D271–D273 and the session/model-store tests.

## Stdio RPC

`pier --mode rpc` runs the coding agent as a local subprocess controlled by
JSONL on stdin. Responses and session events share stdout; diagnostics use
stderr. The command and event shapes follow **pi v1.3.0** for the supported
subset below. This is not the framed-CBOR chord transport (removed from the
tree; D236), and it is not an MCP server.

RPC runs the enabled agent tools with the same permissions as the CLI. It is
not an authenticated network service. Do not expose it directly to untrusted
clients.

### Start and inspect

```bash
printf '%s\n' '{"id":"state","type":"get_state"}' \
  '{"id":"messages","type":"get_messages"}' |
  pier --offline --mode rpc --no-mcp --no-session
```

A host that submits prompts must **keep stdin open while work is running**.
Closing stdin shuts down the session and cancels active work. There is no
handshake or session-header record. Normal CLI model, credential, tool, trust,
and session-path flags apply. `--continue` and explicit `--session` file paths
can restore a session at startup. RPC never opens a trust dialog, a session
picker, or a cross-project fork confirmation. Use `--fork` or an explicit
session file path instead of a global session-id match that needs confirmation.
`--resume` and `@file` arguments are rejected. Attach images through commands.

### Supported commands

Every command accepts an optional string `id`. Responses repeat it when
present, including an empty string. Omitted ids remain omitted.

- `prompt`: `message` (string), optional `images`, optional `streamingBehavior`
  (`"steer"` or `"followUp"`).
- `steer`, `follow_up`: `message`, optional `images`. These queue input, even
  while idle; they do not start an idle agent. Skills and prompt templates
  expand before queueing.
- `abort`: cancels the current prompt, including auth preflight, and responds
  after the admitted prompt workers settle.
- `clear_queue`: returns `{ "steering": [...], "followUp": [...] }`.
- `get_state`: returns model/thinking selection, activity and queue modes,
  session identity, auto-compaction status, and message/queue counts. Optional
  model, session-file, and session-name fields are omitted when absent.
- `get_messages`: returns `{ "messages": [...] }`, with role-tagged messages.
- `get_available_models`: returns `{ "models": [...] }` from the authenticated
  availability snapshot.
- `set_model`: `provider`, `modelId`; returns the selected model.
- `cycle_model`: returns `{ "model": ..., "thinkingLevel": ..., "isScoped": ... }`
  or `null` when no cycle is available.
- `get_available_thinking_levels`: returns `{ "levels": [...] }`.
- `set_thinking_level`: `level`; the session clamps a valid level to the model's
  capabilities.
- `cycle_thinking_level`: returns `{ "level": ... }` or `null`.
- `set_steering_mode`, `set_follow_up_mode`: `mode` (`"all"` or
  `"one-at-a-time"`).
- `get_last_assistant_text`: returns `{ "text": "..." }`, or an empty object
  when there is no assistant text.
- `get_session_stats`: returns the camelCase session counters, token totals,
  cost, and context usage when a model window is known.
- `export_html`: optional `outputPath`; renders the current session branch with
  the same exporter as `/export` and returns `{ "path": ... }`. An in-memory
  session fails with `Cannot export in-memory session to HTML`; a persisted
  session with no file yet fails with `Nothing to export yet - start a
  conversation first`.
- `get_fork_messages`: returns `{ "messages": [{ "entryId": ..., "text": ... }] }`
  for the user messages that can seed a fork.
- `get_entries`: returns `{ "entries": [...], "leafId": ... }`. An optional
  `since` entry id returns the entries after it; an unknown id fails with
  `Entry not found: <id>`.
- `get_tree`: returns `{ "tree": [...], "leafId": ... }` with resolved labels.
- `set_session_name`: `name` (trimmed); an empty name fails with
  `Session name cannot be empty`.
- `get_commands`: returns `{ "commands": [...] }` for prompt templates and
  skills, each with `name`, optional `description`, `source`, and `sourceInfo`.
- `compact`: optional `customInstructions`; returns the compaction result
  (`summary`, `firstKeptEntryId`, `tokensBefore`, optional `estimatedTokensAfter`,
  `usage`, `details`).
- `set_auto_compaction`, `set_auto_retry`: `enabled` (boolean).
- `abort_retry`: cancels an in-flight auto-retry.
- `bash`: `command`, optional `excludeFromContext`; runs it in the session cwd
  and returns the bash result (`output`, optional `exitCode`, `cancelled`,
  `truncated`, optional `fullOutputPath`), streaming `bash_execution_update`
  events.
- `abort_bash`: cancels a running `bash` command.
- `new_session`: optional `parentSession`; starts a fresh session in the
  current session directory and returns `{ "cancelled": false }`.
- `switch_session`: `sessionPath`; opens that session file and returns
  `{ "cancelled": false }`.
- `fork`: `entryId` and optional `position` (`"before"` default, or `"at"`).
  `before` requires a user message, branches at its parent and returns
  `{ "text": ..., "cancelled": false }`; `at` keeps the entry and omits `text`.
- `clone`: forks at the current leaf and returns `{ "cancelled": false }`. An
  empty session fails with `Cannot clone session: no current entry selected`.

The replacement commands run through the host's session factory. `new_session`,
`switch_session`, `fork` and `clone` fail with `Session replacement is not
available in this build` when no factory is supplied (a Go caller of
`coding.RunRPCMode` must set `RPCModeOptions.ReplaceSession`). A replacement
aborts in-flight work and disposes the old session first, then subscribes the
new one, so its events and state queries use the replacement.

Model, thinking, queue-mode, compaction and retry toggles are session-local;
`set_auto_retry` writes the same persisted retry setting the run loop reads.
Images use the upstream shape:

```json
{"type":"image","data":"<base64>","mimeType":"image/png"}
```

### Acceptance and events

```json
{"id":"turn-1","type":"prompt","message":"Explain this repository"}
{"id":"turn-1","type":"response","command":"prompt","success":true,"data":{"disposition":"started"}}
```

`prompt` responds once after successful preflight, not after the model finishes.
Its disposition is `started` or `queued`. Extensions are absent, so `handled`
is never produced. Sending another prompt during a run without an explicit
`streamingBehavior` fails. `steer` and `follow_up` return `queued`.

`bash` and `compact` run off the read loop and respond when they finish. The
loop keeps reading while they run, so `abort` and `abort_bash` can cancel them.

Session events stream independently of responses using the same event shaper
as JSON print mode. Wait for `agent_settled` to observe final completion;
`agent_end` may be followed by a retry or another queued turn. Once a prompt
has been accepted, later model failures appear in events, not as a second
response for the same command.

```json
{"id":"bad","type":"response","command":"set_model","success":false,"error":"Model not found: missing/model"}
```

Unknown commands return a correlated `Unknown command: <type>` failure.
Malformed JSON returns a failure with `command: "parse"`; the connection stays
open. Invalid fields retain a valid command type and id. Records are delimited
only by LF, with CRLF and a final unterminated record accepted. Payload strings
may contain U+2028/U+2029; do not split records on those characters. Records
larger than 64 KiB are supported.

### Bounds and shutdown

D235 admits at most four pending prompt preflights in addition to an accepted
turn. Additional requests receive a visible failure and may be retried.
Preflight admission stays in input order while abort and state queries remain
readable. Output uses one unbuffered byte handoff: a slow consumer backpressures
producers rather than accumulating an unbounded output queue.

EOF, cancellation, and stdout failures stop work and dispose the session.
Accepted session writes drain without a deadline. SIGTERM/SIGHUP return
143/129 after host cleanup; ordinary EOF returns 0 unless I/O or persistence
failed. Pending display output may be discarded during shutdown, never accepted
session saves.

An OS read or write that cannot be interrupted may outlive owner teardown in a
byte-only pump until the OS releases it or the process exits. The pumps never
mutate session state. A Go caller of `coding.RunRPCMode` must reserve its streams
for that run and must not reuse a canceled output while an in-flight write may
still be blocked.

### Go client

`coding/rpcclient` ports the reference `RpcClient`: it spawns a peer with
`--mode rpc` (or `Options.PeerArgs` for a custom peer), correlates responses by
id with a 30s default request timeout, forwards non-response records to
`OnEvent`, and exposes one typed method per command (`Prompt`, `State`,
`SetModel`, `Compact`, `ExportHTML`, `Entries`, `Commands`, and so on).
`WaitForIdle`, `CollectEvents` and `PromptAndWait` resolve on `agent_settled`.
`Stop` sends SIGTERM and escalates to a kill after one second. An in-flight
request is rejected when the child exits, with the child's stderr in the
error.

```go
client := rpcclient.New(rpcclient.Options{CLIPath: "pier", Args: []string{"--offline"}})
if err := client.Start(ctx); err != nil {
	return err
}
defer client.Stop()
state, err := client.State()
```

### Not implemented in this milestone

- Extensions, extension UI requests/responses, and extension commands.

The underlying CLI and session APIs may support operations that are not yet
exposed through RPC. Unsupported commands fail explicitly; this milestone does
not claim complete upstream RPC compatibility.

## Deliberate divergences from pi (D1–D303)

Representative rationales for deliberate differences from pi: Go runtime
constraints, corrected upstream defects, and project choices. D IDs also appear
in code comments; not every ID has a full entry here, since closed, retired,
withdrawn, historical and removed-code rows are pruned. The range is
**D1–D303**. Entries are ordered numerically; overlapping rows refer to the
owning explanation. Port and review history are available with `git log --oneline`.

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

Go uses function fields instead of overridable methods; listeners are compared
by code pointer.

### D50

**Go stdin reads cannot be paused like Node's stdin stream.**

`ProcessTerminal.Stop` gates input delivery without joining a blocking console
read. If that read completes while still stopped, it discards its bytes and
relinquishes reader ownership. A later `Start` reopens input with a fresh buffer
and handler, reusing an in-flight reader rather than competing for the same fd.
`stdinReaderMu` serializes admission and exit only; reads, parsing and callbacks
run outside it. The raw buffer stays on the UI owner, while library-mode buffers
are atomically published to the one reader. Restarting inside an input callback
is supported. A blocked read may remain until input/EOF arrives; this is not
Node's synchronous `process.stdin.pause()` behavior.

Reference: pi v1.4.0 (`4a080252`), `packages/tui/src/terminal.ts` `start`/`stop`.
Scenario: a renderer-mode switch or suspend/resume stops and starts the same
terminal; new input must work, with no second concurrent stdin reader. CR-053
repairs the old permanent closed flag and restart reader admission.

### D51

`StdinBuffer` uses callbacks instead of `EventEmitter`.

### D90

Narrow runtime interfaces provide test seams across the wirings.

### D105/D106

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

**Narrowed: edit preview matches the pin; extension renderer definitions stay out.**

Tool renderers still resolve to built-ins, not extension definitions (D41). The
synchronous-preview gap is closed: `computeEditsPreview` runs on a worker and
applies through the owner post, matching the asynchronous preview in v1.2.0
`core/tools/renderers/edit.ts`. `TestEditPreviewAppliesThroughOwnerPost` covers
that handoff.

### D140

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
apply on that loop (stages 3–4). Upstream awaits prompts on the JS event loop.
D144/D145 define input/render ownership; D196/D197 define queue ordering and
fairness. [architecture.md](ARCHITECTURE.md) holds the full design.

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

### D149

**The footer provider retains its cross-goroutine lock.**

`FooterDataProvider.mu` (`coding/footerdata.go`) protects cwd, git paths/branch,
statuses, and listeners shared by footer rendering and its 500 ms git-HEAD
watcher. Retirement would require posting poll results to the UI loop and
delivering listeners outside the lock, a `coding/` change outside the UI
refactor. The watcher reads git state and calls `Refresh`. See
[ARCHITECTURE.md](ARCHITECTURE.md) for the authoritative inventory.

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
  User panels are warm (`#393630` dark, `#f4eee1` light), rather than the
  original upstream snapshot's `#343541` blue-gray or `#e8e8e8` gray.

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
are created synchronously and browser exec stays on the flow goroutine. Nil
`post` applies inline for tests. This was the fifth deadlock, found by source
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

Upstream `getFileSuggestions` expands `~` before splitting directory/filter;
`path.join`/`filepath.Join` clean `~/.` to home, then split home into its parent
and name. Tab consequently listed `/home` and offered `~/dat/`. Pier splits
first: trailing `.` filters dotfiles inside home, while `..` clears the filter
and remains in the suggestion. Upstream has the same bug.
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

Upstream leaves truncated output files to the OS. On this host, one 174 MB
command persisted for days; after three days, 230 files occupied 442 MB of
scratch RAM on tmpfs. Pier sweeps oldest-first when creating a log, down to
256 MB, covering `pi-bash-<id>.log`, `pi-output`, and `pi-powershell` prefixes.
The file being created and other output files still open by this process,
unrelated tool logs, and matching directory names are never touched. Closed
files remain eligible for the budget sweep. Files open by another pier process
are not visible to this process's active-file registry and remain subject to the
oldest-first sweep. The model still receives
"[Output truncated. Full output: <path>]". `TestSweepTempOutputFiles` checks
exclusions and oldest-first order.

The sweep stops at the tool-output prefixes on purpose. MCP output files
(`pi-mcp-<hex>.txt` and binary resource dumps, written by `SaveMcpToTempFile`
through the same `utils/output-files.ts` convention) are left to the OS exactly
as upstream leaves them: they are not swept, are not part of the budget, and a
sweep triggered by another tool skips them. Extending the sweep to them would
delete files the model was told it could read back, a further departure this
port does not need, so a growing MCP scratch directory on tmpfs is accepted and
matches the reference (CR-072).

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
replays the toggle.

`HandleTerminalInput` delivers OSC 11 to both the combined query and the
listener. A background-only late reply can therefore update an auto theme
without the combined query's final DA1 sentinel. No reply leaves the
environment/system fallback. D218 covers off-loop combined-query waits.

Tests: `TestBackgroundListenerSeesLateReplyDuringColorQuery`,
`TestSystemColorQueryDoesNotHideStartupBackgroundReply`, and
`TestBackgroundProbeRoundTripThroughRenderer`.

### D166

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

`AbortAsync` cancels retry, bash, and agent runs and returns immediately. Queue
restoration already precedes cancellation. `Abort` retains its wait where
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
or pi.dev's changelog. No module-proxy publication is required. Unorderable
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
inherited output pipes open beyond the killed tree. Pier closes output readers
on kill and bounds draining with `waitForPipeDrain`, the existing grace helper
based on upstream `waitForChildProcess`.

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
- OAuth uses a per-process credential store, not `auth.json`; `/mcp` sign-in
  persistence is unported. `read_mcp_resource` is absent, so resource links do
  not advertise it.
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
pending-frame clocks. References: `packages/coding-agent/src/modes/interactive/components/tool-execution.ts`
and `packages/coding-agent/src/core/tools/renderers/bash.ts`.

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
- `WaitForDirectTools` polls every 20ms instead of awaiting ready promises.

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
strand a pipe read or input-copy goroutine after timeout. The descendants
are not killed merely for retaining clipboard ownership. This original policy did not cover process-wide copy queues/detached
work; D200 extends ownership and bounds permanent terminal exit. Session/provider
serialization is unchanged; accepted saves still have no timeout.

`TestGroupShutdownCancelsOptionalWorkButDrainsSaves` uses `testing/synctest` to
hold a save and cancellation-ignoring worker: cancellation precedes save release,
saves remain ordered, shutdown need not release the optional worker, and
queued/late tasks never run. App tests cover StopMode/Close, late reads/watchers,
posted switch/preview callbacks, paste, and canceled transcript publication.

### D200

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
uncommitted hint each until recovery to 128 KiB. Essential progress-clear
invalidates delayed active hints. Permanent shutdown discards only uncommitted
metadata, never committed frames.

Interactive OSC 52 fallback uses the stable FIFO through a cancellation-aware
off-loop API, not raw stdout. Whole packets wait for 256 KiB capacity and a closed
renderer frame; oversized packets reject. Accepted packets/frames stay FIFO.
Essential protocol/lifecycle output, SDK writes, and standalone APIs retain their
admission, so this is not a global cap; one oversized title can overshoot.
The inventory below is authoritative.
Tests cover blocked backlog, cancellation, atomic recovery, stale progress,
oversized-title progress, and app clipboard routing.


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

A title remains one atomic existing value: an oversized title on an idle
terminal is allowed to overshoot instead of truncating or deadlocking. Deferred
metadata memory is therefore bounded by one title value plus one fixed progress
sequence, not by an absolute title-byte limit. Generic `Write`, standalone
clipboard APIs and standalone fullscreen selection defaults retain compatibility;
this is not a global hard byte cap or an admission rule for arbitrary SDK writes.
No saved session/settings JSON or provider payload changes.

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

### D205

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

The UI selects on wakeups and reports chat errors. Teardown drains saves then
routes unresolved diagnostics through D200's output grace, never direct stdout.
Print mode flushes on ordinary and signal returns; persistence failures report
on stderr and cause exit 1 on ordinary returns, while signal returns retain
129/143. This preserves JSON stdout. Formats remain unchanged. Tests cover sync/queued
initial and append failures, prefix/truncation recovery, per-task duplicate
prevention, coalesced wakeups, idle reporting, teardown, and print exit codes.

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
Tests: `TestStaleSystemColorSnapshotDoesNotClobberLateBackground`.

The retained blocking OSC 11 background and color-scheme helpers also use the
existing query handoff mutex for pending requests and timeout/reply settlement.
Scheme waiters are separate from owner-only listener registries; off-loop cleanup
never mutates attached callbacks. Listeners run after unlocking and before the
scheme waiters receive their results. No UI-state lock is added.
Tests: `TestBackgroundQueryTimeoutReplyRace`,
`TestColorSchemeQueryTimeoutReplyRace`, `TestColorSchemeQueryWaitsForPriorListeners`.

Tests: `TestProbeSystemColorsDoesNotBlockTheCaller`
(`coding/interactive/interactive_test.go`) and
`TestRendererBeginTerminalColorsWaitsOffLoop` (`tui/terminalcolors_test.go`).
Reference: `packages/coding-agent/src/modes/interactive/theme/theme-controller.ts`.

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
Idle round delivery uses the host work scheduler (D143). A queued closure
checks that its goal id, revision and pending turn still identify an armed,
active reservation before starting a run; pausing, clearing, replacing or
pausing/resuming while it waits invalidates it. Already-started work retains
its existing cancellation policy. Tool exposure is synchronized before a
trigger-turn message can start an idle model request inline, so the first round
has the active goal's tools rather than waiting until that run returns.
Machine transitions and their durable append effects share the controller lock,
so concurrent goal tools cannot persist clear/create entries out of order. The
private session adapter appends records under that lock (interactive disk I/O
still uses the existing write queue); tool changes, notices and model work run
after unlocking.

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

### D224

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

`ConvertToLlm` now holds converted custom messages while an assistant message's
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
`highlight` package already returns the same scope tree, so `renderHighlightedNodes`
walks the tree directly and skips the HTML round-trip (equivalent, since the
HTML walk was exactly escape-then-unescape). On the 10.9 KB
`internal/highlight/testdata/sample_big.go`, the wired path measures 1.86 ms (5.86 MB/s)
against the reference's JS wired path at 3.20 ms (3.42 MB/s) in the same
session, so that stall does not return, and the port has no third-party
dependency:
`github.com/alecthomas/chroma/v3` and its `dlclark/regexp2` indirect dependency
stay removed, keeping the binary and startup gains that removal produced.

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

`get_goal` is the only other tool. `DesiredToolNames` keeps `set_goal` always
active and `get_goal` only while a goal is pursued.

All three `set_goal` properties explicitly accept null, while remaining optional.
Reproducer: a caller exposes every property as required without strict-schema
conversion, forcing a non-null `status` alongside a new `objective`; execution
rejects both and retries cannot express the intended call. Nullable properties
let that caller send `{"objective":"fix it","status":null,"blocked_reason":null}`.
The reverse ends a goal with a null objective. Exactly one nonempty objective or
status is still required; two active values and two null values remain errors.
The regression test exercises both strict and non-strict Responses declarations
with all properties required, normal agent validation, and execution. The
upstream reference is the extension's separate create/update tools above, not a
pi built-in tool.

### D235

**Stdio RPC has a bounded extension-free first milestone.**

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
reproduces and guards the shell cancellation fix. See the Stdio RPC section
above for usage and the complete supported subset.

### D236

**The unreachable reference packages are removed from the tree.**

Reference: pi v1.4.0 `packages/{chord,durable,telemetry}`; the `client`,
`protocol` and `server` packages were already replaced by the typed host before
v1.3.0. The Go port had a counterpart for these and for the already-removed
`client`/`protocol`/`server` packages, but none is reachable from the built CLI:
`go list -deps github.com/dat267/pier` never pulls in `chord`,
`chord/{delta,services,facets}`, `client`, `protocol`, `server`,
`server/testing`, `telemetry` or `durable`. The CLI's only consumers are the
interactive, print and RPC modes over `ai`/`agent`/`coding`/`tui`/`mcp`; the
chord transport belongs to the durable remote-execution host, which the CLI does
not run, and upstream's typed-host rewrite already replaced those packages
(see Git history). `telemetry` is ported but imported by nothing:
upstream `ai` only carries an optional `telemetryContext`, which the port does
not set.

The packages and their tests are removed from `main` and preserved on the
`unwired-packages` branch, which points at the pre-removal commit. `go mod
tidy` drops the dependencies they alone used (`github.com/sgtdi/fswatcher`,
`modernc.org/sqlite`). Later D-rows that describe
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
(`cmd.sessionPickerOutput`). `restoreStdout` has no
counterpart.

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

This applies to transient HTTP/2 stream resets as well: D171's classifier now
recognizes Go's `stream error: stream ID ...; PROTOCOL_ERROR; received from
peer` and HTTP/2 GOAWAY wording, which upstream's fetch/websocket error list does
not contain.

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

Tests: `TestFindToolPreservesWhitespaceInFileNames`.

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
frame joins the transcript as soon as the command is triggered. The session
entry is unchanged: a `bashExecution` custom message recorded by
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
same state. The port now expires a state only when the map still points to that
timer's entry.

Test: `TestStaleCallbackTimeoutDoesNotDeleteReplacementWaiter`.

### D248

**A `!` frame replayed from the session keeps the position the live transcript
gave it, not its entry order.**

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

### D252

**Proxy content events reject negative array indices.**

Reference: `packages/agent/src/proxy.ts` assigns `partial.content[contentIndex]`.
A proxy sending `{"type":"text_start","contentIndex":-1}` creates a JavaScript
array property that is absent from JSON output. The same assignment panicked
in the port's streaming goroutine and could terminate the process. Indexed
proxy events now return a protocol error for negative indices; the stream
reports its ordinary error event instead of attempting a Go slice access.

Test: `TestProxyRejectsNegativeContentIndex` covers every indexed event type.

### D253

**Integer JSON tokens retain the port's native integer precision.**

Reference: `core/session-manager.ts` and AI providers serialize JavaScript
numbers with `JSON.stringify`. Go's typed timestamp and token-count fields use
`int64`; rewriting their integer tokens through a double can move a valid Go
value outside its type's range. For example, `9223372036854775807` rounds to
`9223372036854776000`, which cannot be read back into `int64`. The shared JSON
encoder retains integer token precision while normalizing floating-point
spellings and negative zero. Tool arguments still use JavaScript double
semantics through their ordered value decoder.

Test: `TestMarshalJSONPreservesTypedIntegerRange` observes the round-trip
failure when the encoder normalizes all integer tokens through a double.

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

Tests: `TestTranscriptRestoresBashCommandHistory`.

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

### D254

**The settings-backed HTTP idle timeout bounds provider requests.**

Reference: `core/http-dispatcher.ts` sets undici's `headersTimeout` and
`bodyTimeout` to the configured idle timeout, and the provider APIs pass
`timeoutMs` into their requests (`api/anthropic-messages.ts`,
`api/openai-completions.ts`, `api/openai-responses.ts`,
`api/google-generative-ai.ts`, `api/google-vertex.ts`,
`api/openai-codex-responses.ts`, `api/openrouter-images.ts`; Bedrock's API takes
no `timeoutMs` and relies on the dispatcher alone). Go's `http.Transport` has no
per-request header timeout, and writing `ResponseHeaderTimeout` on the process-wide
`http.DefaultTransport` races with in-flight requests (D40). The port arms one
timer per provider request (`ai/streamidle.go`), including the single-request
OpenRouter image generation. Dispatcher-only requests use the
raw settings delay for both header and body idle phases, matching Undici's
FastTimer. OpenAI, Anthropic, and OpenRouter image SDK requests also have a Node
`setTimeout` deadline around `fetch` (`pi/node_modules/openai/src/client.ts`,
`pi/node_modules/@anthropic-ai/sdk/src/client.ts`): the port uses that timer
semantics while waiting for headers, then switches to the raw Undici body idle
delay after headers arrive. In either case every body read re-arms the timer, so
only silence fails the stream and a stream that keeps producing data outlives
the timeout. The timer counts time blocked in a body read, not wall-clock
silence: a callback that fires while the consumer is processing re-arms instead
of cancelling, and the deadline runs from the last delivered bytes, so a loaded
machine, a GC pause or a slow renderer cannot abort a connection whose bytes
already arrived. A nil or non-positive timeout sends the request unbounded.

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
agent proxy stream (`agent/proxy.go`) are guarded the same way; both carry
`TimeoutMs` through their options.
The port's other idle-timeout wording ("Provider response headers idle",
"Provider stream idle") is its own: upstream surfaces undici's
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
`TestCodexRejectsNegativeTimeoutBeforeFetch` and
`TestGuardIdleBodyPreservesFastTimerDelayAndSaturatesDuration`.

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

**Model ordering uses locale-neutral collation.**

Reference: pi v1.4.0 `core/model-resolver.ts`, `bestFuzzyMatch`, and
`cli/list-models.ts` sort providers and IDs by Node's default `localeCompare`.
Pier uses `golang.org/x/text/collate` root
collation, consistent with its file listing, rather than byte order or a
host-selected locale. This restores the reported punctuation and case ordering
(`model-2` over `model_1`, `model-Z` over `model-z`) while keeping model selection
deterministic. On a Swedish Node host, `model-ä` ranks after `model-z`; Pier
selects `model-z` regardless of the host locale. Locale and Unicode collation
version differences are not an assertion of byte-identical ICU behavior.

Tests: `TestFuzzyModelAliasLocaleOrdering`, `TestListModelsTextUsesLocaleOrdering`.

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
names `/login <provider>`, which exists. The OAuth form now points at the
`oauth` block in `mcp.json` instead of at a command that does not exist.

Test: `TestMcpConnectionAsksOAuthServersToSignIn`.

### D260 (resolved)

The non-RGI ZWJ width divergence is gone: the port carries the generated Unicode
17.0 `RGI_Emoji_ZWJ_Sequence` table (`tui/emojizwjdata.go`, matching the
reference's ICU 78 data), so a chain Unicode never joined keeps its part-by-part
width and a real sequence is one two-cell emoji.

### D261

**An invalid `file://` URL is returned unchanged instead of raising.**

Reference: `utils/paths.ts`, `normalizePath`, calls Node's `fileURLToPath`
unguarded, so a non-local host (`file://host/x`), an encoded slash
(`file:///tmp/a%2Fb`), malformed percent encoding or invalid encoded UTF-8 throws
and the caller reports
a URL error. `NormalizePath` returns a string and has no error result, so it
returns the input unchanged when `filePathFromURL` rejects the URL; the caller
then fails on a path ("file not found: file://host/x") instead of on the URL.
The follow-up URL-parser audit matched 1,174 generated inputs against Node:
dot segments resolve before encoded-slash validation, invalid encoded UTF-8 is
rejected, raw tabs/newlines and trailing C0 whitespace follow WHATWG rules,
escaped localhost is recognized, and drive-letter roots survive parent segments
(including the legacy `C|` spelling).

Tests: `TestNormalizePathKeepsAnInvalidFileURL`,
`TestNormalizePathFileURLRemovesEncodedSlashBeforeValidation`,
`TestNormalizePathFileURLRejectsInvalidUTF8`,
`TestNormalizePathFileURLIgnoresParserWhitespace`,
`TestNormalizePathFileURLAcceptsEscapedLocalhost`,
`TestNormalizePathFileURLRecognizesDriveRoots`.

### D262

**An escaped suffix after a wildcard is matched literally, without minimatch's
fast-path shortcut.**

Reference: `core/model-resolver.ts` matches scoped-model patterns with
`minimatch(value, pattern, { nocase: true })`. A follow-up comparison of 61,412
generated pattern/value pairs left 87 mismatches, all in the escaped-suffix
shortcut below, for brace expansion (comma groups,
`{1..3}`-style sequences, literal `{a}`, inner-first nesting, escapes), path
canonicalization (`..` removes the preceding segment; `.` stays literal), the
`.`/`..` and dotfile segment rules, empty paths, absolute paths, repeated and
trailing separators, and globstars.

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

### D267 (partially resolved)

**Request params keep JavaScript member order; map-backed options need order sidecars.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/api/openai-completions.ts`
`buildParams`, `packages/ai/src/api/openai-responses.ts` `buildParams`, and
`packages/ai/src/api/bedrock-converse-stream.ts` `requestMetadata`.

The Go port now retains sampling-parameter key order when decoding model JSON,
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
upstream byte-for-byte. Same class as D221.

Tests: `TestProviderPayloadReplacementsMatchUpstream/google-generative-ai-*`
(`ai/provider_payload_replacements_test.go`) and
`TestGoogleAPIErrorMessage` (`ai/google_generative_ai_test.go`). The remaining
V8 parse-diagnostic wording difference is an accepted runtime-specific
divergence, not a pending fix: the port does not emulate JavaScript engine error
messages.

### D270

**A malformed Bedrock HTTP error body uses Go's status/body text, not the SDK's JSON parse error.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/api/bedrock-converse-stream.ts`
`formatBedrockError`, plus the pinned `@aws-sdk/core` REST-JSON `handleError` and
`@aws-sdk/client-bedrock-runtime` ConverseStream deserializer. The SDK parses
HTTP error bodies as JSON regardless of content type. For an invalid body such
as `proxy exploded`, V8's `JSON.parse` error and the SDK's deserialization
suffix become the assistant error. Pier currently reports `502 Bad Gateway:
proxy exploded`. This is user-visible text only; modeled errors, valid unmodeled
JSON errors, empty bodies, and modeled stream exceptions match the source
fixture.

The exact V8 wording is intentionally not emulated, consistent with D269; this
runtime-specific message difference is accepted rather than tracked as a pending
parity fix. `ai/testdata/bedrock-errors/golden.json` is generated by executing pi
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
starts with `[]`. Pier previously cleared that body and persisted `"models":null`.
Both tools share `~/.pi/agent/models-store.json`, so pi's restoration through
`remoteModels(...).filter(...)` could throw on the null array.
`ModelsStoreEntry.MarshalJSON` emits `[]` for a nil catalog, and catalog refreshes
now retain the body like upstream.

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

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/extensions/mcp/
tools.ts` `blockToContent`. Both members below are malformed: the MCP schema
types `size` as a number and `blob` as a string.

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

Pier models the documents with structs. `ParseClientInformation` now copies
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

Test: `mcp/oauth/testdata/parse/golden.json` pins the whitespace cases and the
`selectResource` decisions; the default-port and host-case helper outputs are
excluded there for this reason.

### D277

**A mistyped settings member falls back to its typed default.**

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/
settings-manager.ts`. The reference parses `settings.json` into a raw object and
reads each member with a `??` default, so a wrong-typed value survives the load
and its getter can return it as-is: with `"compaction": {"enabled": "yes"}`,
`getCompactionEnabled()` returns the string `"yes"`, and a mistyped
`thinkingBudgets.low` is echoed by `getThinkingBudgets()`. A mistyped project
value also shadows the global one before the getter defaults.

Pier decodes into typed structs. `MigrateSettingsRaw` now decodes member by
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

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/
session-manager.ts`. `loadEntriesFromFile` requires the first parsed entry to be
a `session` header whose `id` is a string; an empty string counts, while a
missing, `null` or non-string id discards the file. `buildSessionInfo` (the
resume listing) only checks `type === "session"`, so the reference *lists* such
a file with the invalid id (`undefined`, `null` or a number) even though it
cannot open it — the picker offers a session that fails to resume.

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
Go's os error. Every other part of the read tool's output is
token-for-token: a generated corpus compares it over 16 cases (empty files,
offsets and limits including fractional and out-of-range values, blank lines,
CRLF, a BOM, invalid UTF-8, a >50 KB line, >2000 lines with the truncation
notice, and nested paths).

Tests: `TestReadToolMatchesUpstreamTextPath`, `TestReadToolMissingFileError`
(`coding/read_tool_parity_test.go`).

### D280

**An older successful queued settings write does not clear newer dirty values.**

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/
settings-manager.ts` clears the whole modified-field set after each successful
queued task. If another setter runs while that task is pending, its fields are
already in the shared set; an earlier success can clear them before a later
write fails. Subsequent saves then omit those failed changes.

Pier's queued worker clears only the fields in the completed task's snapshot,
and only if the current value still equals the persisted snapshot. Later fields
and newer values remain dirty for retry. This intentional reliability fix is a
narrow divergence from upstream; synchronous writes still clear under the
manager lock.

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

**AI requests retain Go client hooks and the ten-redirect limit.**

Reference: pi v1.4.0 (`4a080252`), `packages/ai/src/api/openai-responses.ts`
uses the SDK's fetch transport. Pier removes authorization, cookies and proxy
credentials after an origin change and keeps them removed on subsequent hops,
but retains the configured `http.DefaultClient` transport, timeout and redirect
hook, and its existing default ten-hop limit rather than fetch's twenty.
The global client and original request headers are not mutated.
Tests: `TestResponsesRedirectCredentials`, `TestProviderRequestRedirectPolicy`.

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

Reference: pi v1.4.0 (`4a080252`), `packages/coding-agent/src/core/session-manager.ts`
and `packages/coding-agent/src/core/session-export.ts` use Node's default
0666/0777 creation modes, filtered by umask; `packages/coding-agent/src/core/agent-session-runtime.ts`
preserves imported source modes with `copyFileSync`. Pier creates and rewrites session JSONL files with mode 0600,
uses mode 0700 for session directories, hardens existing session files and known
session directories when opened, and does not copy source permissions to imported
sessions. Newly created export parent directories use 0700; explicitly selected
existing export directories retain their permissions. On Windows these POSIX
mode bits do not replace inherited ACLs; this hardening addresses the reproduced
permissive-umask exposure on POSIX.

Tests: `TestSessionManagerPersistsFilesWithPrivateMode`,
`TestOpeningExistingSessionMakesFilePrivate`, `TestForkedSessionUsesPrivateModes`,
`TestSessionManagerUsesPrivateDirectoryMode`, `TestSessionDirectoryUsesPrivateMode`,
`TestSessionExportUsesPrivateDirectoryMode`, `TestSessionExportUsesPrivateFileMode`,
and `TestImportedSessionUsesPrivateMode`.

### D290

**Rendered OSC 0/1/2/52 controls and terminal-title control characters are suppressed.**

Reference: pi v1.4.0 `packages/tui/src/tui.ts` `applyLineResets` and
`packages/tui/src/utils.ts` `normalizeTerminalOutput` pass OSC 52 through;
`packages/tui/src/terminal.ts` `setTitle` wraps the provided title without
removing control characters. Pier removes OSC
0/1/2 title changes and OSC 52 clipboard operations at the final rendered-line
boundary while preserving OSC 8 hyperlinks and supported image sequences; it
also strips Unicode control characters from OSC 0 title text. This is a
deliberate terminal-output security divergence.

Tests: `TestApplyLineResetsStripsUnsafeOSC` covers BEL, ESC-ST, C1 ST,
unterminated controls, image lines and OSC 8;
`TestProcessTerminalSetTitleStripsControlCharacters` verifies the emitted OSC 0
sequence cannot be terminated by title content.

### D291

**Custom export colors are validated before HTML interpolation.**

Reference: pi v1.4.0 `packages/coding-agent/src/modes/interactive/theme/theme-schema.ts`
accepts string color values; `packages/coding-agent/src/modes/interactive/theme/theme.ts`
`getThemeExportColors` returns custom CSS strings, and
`packages/coding-agent/src/core/export-html/index.ts` inserts them into the
`<style>` element. A color such as
`#123456</style><script>…` closes the style element and executes when the export
is opened. Pier now validates export values with its color parser and
omits invalid values. This is an intentional export-security divergence.

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

## Lock inventory

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
`Agent.MessagePrefixIsCurrent` retains an immutable message-reference prefix and
checks it under the same agent mutex. Transcript appends preserve the prefix;
SetMessages replaces its backing slice. Checks decode no JSON, copy no session
entries, and invoke no user callback while locked.

`coding.CacheWarmer.mu` also guards callback detachment at session disposal.
Disposal clears the current run, stops its timer, cancels its request context
and releases `OnWarmed`. Refresh workers snapshot an admitted callback under
that mutex and invoke it outside the lock; an already-admitted callback may
finish without making disposal join itself. Host currency and mode callbacks run
outside that mutex, so their stop is run-scoped: a stale refresh stops only while
it is still the current run, matching the reference's single-threaded
check-and-stop. No new worker or mutex is added.

`coding.GoalController.mu` serializes machine transitions with their durable
append effects. A clear/create pair cannot interleave with another tool's
transition records, even when tools execute in parallel. The private session
sink acquires `SessionManager.mu` for record append; interactive file I/O stays
on the manager's existing write queue, while standalone callers retain their
synchronous storage path. No user callbacks run in this locked phase. Tool
exposure synchronization, notices and model work are applied after unlocking;
notification callbacks may reenter `Snapshot`. Deferred idle round workers also
check their goal id, revision and pending turn before starting, so a stopped or
superseded reservation cannot launch later.

`coding.McpServerConnection.mu` protects attachment, shared-open admission and
lifecycle. Each attempt publishes its own client/error before closing its done
channel; caller cancellation only stops waiting, while `Close` cancels the open.
Tool-change handlers run discovery off the transport reader, and the live client
owns request timeout/close cancellation. Stale results are rejected under the
connection mutex. Manager error queries snapshot state and diagnostic together;
callbacks remain outside locks. No new mutex or UI-state worker is introduced.

MCP listener registries retain stable entry pointers, so slice growth and
snapshot compaction cannot invalidate unsubscribe handles. Unsubscribe unlinks
the entry and clears the backing-array slot under that same mutex, releasing
captured state while preserving already-admitted immutable callback snapshots.
Empty notification-method registries are removed. Callbacks remain outside
their existing registry mutexes. `mcp.Client.mu` admits incoming request
cancellation before its handler starts on a worker; the stdio reader remains
free to dispatch cancellation notifications. Unknown-method error replies also
write off-reader, so output backpressure cannot stall unrelated responses. Close snapshots and clears incoming
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

Auth storage's `authFileLock` owns a directory identity, not merely a pathname.
Auth transactions check that identity before and after their callback/write;
a lost lease fails instead of committing. Its refresher only touches its own
directory, and release joins the refresher before removing that directory,
never a successor's lock after stale recovery. The trust store shares the
identity-safe release primitive; its trust decision policy is unchanged.
No credential callback runs under the lease's bookkeeping mutex.
Test: `TestAuthStorageLostLeaseDoesNotCommitOrReleaseSuccessor`.

Auth mutations also retain the parsed raw document: modifying/deleting one
provider re-encodes only that provider, leaving unsupported credentials and
unknown members of other credentials intact, like upstream's object spread.
The typed read view skips unreadable entries without discarding readable
siblings. The source-generated `auth-preservation` corpus checks persisted
bytes through the public storage API.

Settings storage uses a `settings.json.lock` directory, matching proper-lockfile.
It reclaims artifacts older than 10 seconds (including legacy regular files),
refreshes live leases every 5 seconds, and joins its refresher before release.
Release checks file identity before removing the lock; a write callback also
checks identity before commit (D288). Storage callbacks and file I/O remain off
the interactive owner loop; no new UI-state lock is added.

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
| `ProcessTerminal.stdinReaderMu` | `tui/terminal.go` | stdin-reader admission and exit across terminal restarts | **retained**: I/O lifetime handoff only; reads, buffer parsing and callbacks run outside it. Restart reuses an in-flight reader or starts another after the old reader relinquishes ownership; the legacy buffer is atomically published |
| `ProcessTerminal.writeMu` | `tui/terminal.go` | terminal writes, raw-mode transitions, Kitty negotiation bookkeeping shared with the reader | **retained**: not UI state |
| `ProcessTerminal.writesMu` | `tui/terminal.go` | the console-write FIFO, byte backlog counters, open-frame buffer, deferred metadata hints and cancellation-aware optional-admission waiters (`Write` appends essential output in submission order; a dedicated goroutine drains it) | **retained**: off-loop writer; the UI loop never writes to the console itself |
| `resizeWatcherMu` | `tui/terminal_windows.go` | the Windows console-resize poller's stop/done channels | **retained**: the poller runs off the loop; `Stop` signals it without joining, so an admitted callback finishes instead of deadlocking (CR-063) |
| `prerenderState.mu` | `coding/interactive/transcript.go` | the transcript warm-ahead handoff (ready chunk, width, generation) | **retained**: loop ↔ warm-worker handoff, not UI state |
| `systemThemeState.mu` | `coding/interactive/theme.go` | the terminal-report colors the system theme is generated from (written by the terminal query reply path, read at theme load) | **retained**: a pointer swap of two values, never UI state |
| `terminalQueries.colorMu` | `tui/terminalqueries.go` | pending combined/background/scheme query queues and settlement shared by blocking waits and owner-loop reply dispatch; listener registries remain owner-only and delivery runs after unlocking | **retained**: query handoff bookkeeping, not UI state |
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
