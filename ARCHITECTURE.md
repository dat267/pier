# Architecture and performance

The design reference for `pier`, a Go port of pi v1.5.0 (`f9a26365`): runtime
structure, performance evidence, the stdio RPC contract, and the lock inventory,
which is the source of truth for retained and retired UI locks. Deliberate
differences from pi are D-rows in [DECISIONS.md](DECISIONS.md), contributor
rules are in [AGENTS.md](AGENTS.md), and history is in `git log`.

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
  detailed in the D-rows ([DECISIONS.md](DECISIONS.md)) and lock inventory.

## Performance evidence

Measurements below are local probes, not guarantees. Methods and fixture sizes
are recorded with the corresponding changes and regression tests.

| Workload | Before | After | Change |
|---|---:|---:|---|
| Catalog decode | 69 ms / 16.2 MB | 17 ms / 2.45 MB | Decode each descriptor once |
| Branch preparation, 8,000 messages | 540 MB / 253 ms | 1.07 MB / 10 ms | Reverse once instead of repeated prepends |
| 8 MiB MCP SSE event | 8.6 GB / 2.49 s | 42 MB / 21 ms | Incremental line processing |
| HTML generation, 2 MiB prompt (allocated bytes) | 36.8 MB | 28.6 MB | Insert base64 session payload after fixed assets |
| Cold first interactive frame | 63.06 ms | 49.21 ms | Warm-filesystem tmux median |
| Resume 56 MB session | 174.74 ms | 147.98 ms | Warm-filesystem tmux median |
| Inline parse, 32 KB unmatched brackets | 748.7 ms | 0.57 ms | Reject impossible links before the tail scan |
| JavaScript highlight, 73.6 KB input | 735.8 ms | 104.6 ms | Guards reuse the document line-terminator class |

Startup timings used a warm filesystem with network and MCP disabled; they are
not additive guarantees. Catalog and replay optimizations retain allocation and
byte-parity regressions.

## Model catalogs

- Remote overlays merge complete chat, image and classifier catalogs. A
  nonempty personalized catalog replaces the whole list; builtins apply only
  when it is empty.
- `GetAllModels` returns every type and `GetModels` only chat models, so
  wrappers must not pre-filter their all-model getter or personalization input
  (`TestCatalogWrappersExposeCompleteModelLists`, online and offline).
- Each wrapper guards its immutable overlay slice snapshot/swap with a local
  mutex; base-provider getters and publication/personalization callbacks run
  outside it (`TestCatalogWrapperRefreshSynchronizesReads`).
- Refresh cleanup removes a provider's cancellation handle only while that
  refresh generation still owns it, so a retired completion cannot erase the
  handle its replacement needs to cancel current work on provider removal or
  replacement (`TestOldRefreshCompletionKeepsReplacementCancellation`, upstream
  v1.5.0 `f9a26365` models.ts controller-identity check).

## Session model

- `SessionManager.Projection` is the canonical branch-versioned view for model
  context. Do not rebuild it by hand or rescan entries per message.
- Session writes preserve JavaScript JSON bytes and key order through
  `ai.MarshalJSON`.
- Cached model/message reads preserve isolation and identity only at validated
  canonical boundaries (D271–D273 and the session/model-store tests).
- Starting a new session drops the manager's branch and projection caches, so
  they stop retaining discarded history. In-place forks also clear stale
  branch/projection snapshots and prune decoded-message memos to the retained
  entry IDs, keeping the identity of kept messages. Caller-held snapshots stay
  valid (`TestNewSessionReleasesPreviousPayload`,
  `TestCreateBranchedSessionReleasesDiscardedMessages`).
- Entry paths and their message memo are captured together under the manager
  lock. New and fork generations install a detached memo that keeps kept
  message identity; an old off-lock projection may finish with its own memo but
  cannot repopulate the new one
  (`TestConcurrentProjectionCannotRepopulateDiscardedSessionMemo` covers an
  in-flight decode and a later decode from the old path).

## Stdio RPC

`pier --mode rpc` runs the coding agent as a local subprocess controlled by
JSONL on stdin. Responses and session events share stdout; diagnostics use
stderr. The command and event shapes were verified against **pi v1.3.0** for
the supported subset below; upstream `modes/rpc` is unchanged through the
v1.5.0 pin. This is not the framed-CBOR chord transport (removed from the
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
  model, session-file, and session-name fields are omitted when absent. A fresh
  session reports one message (D382).
- `get_messages`: returns `{ "messages": [...] }`, with role-tagged messages.
- `get_available_models`: returns `{ "models": [...] }` from the authenticated
  availability snapshot.
- `set_model`: `provider`, `modelId`; returns the selected model.
- `cycle_model`: returns `{ "model": ..., "thinkingLevel": ..., "isScoped": ... }`
  or `null` when no cycle is available.
- `get_available_thinking_levels`: returns `{ "levels": [...] }`.
- `set_thinking_level`: `level`; the session clamps a valid level to the model's
  capabilities. An unknown level fails with `level must be a valid thinking
  level`, where the reference accepts and stores it (D404).
- `cycle_thinking_level`: returns `{ "level": ... }` or `null`.
- `set_steering_mode`, `set_follow_up_mode`: `mode` (`"all"` or
  `"one-at-a-time"`). Any other value fails with `mode must be all or
  one-at-a-time`, so the agent never adopts a mode it cannot honor; the
  reference stores and persists any string (`"bogus"` then appears as
  `followUpMode` in `get_state`; D404).
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

Field types are validated; the reference only casts the parsed JSON to
`RpcCommand`. `bash` without a string `command`, a non-boolean `enabled`,
`set_session_name` without a name, `switch_session` without a path, `fork`
without an entry id, `prompt` without a string message, and malformed `images`
all fail with a pier message. The reference instead coerces (`enabled: "yes"`
enables compaction), throws a `TypeError` (`prompt` without `message`), or
interpolates `undefined` (`set_model` without fields).

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

### Not implemented

- Extensions, extension UI requests/responses, and extension commands.

The underlying CLI and session APIs may support operations that are not yet
exposed through RPC. Unsupported commands fail explicitly; pier does not claim
complete upstream RPC compatibility.

## Lock inventory

Retained and retired locks, and the invariant that governs retirement.

The `tui/` and `coding/interactive/` refactor retired every UI-state mutex.
This is the authoritative inventory for that ownership boundary: retained UI
handoff, terminal I/O and logging locks, plus selected cross-goroutine `coding/`
collaborators. It is not an inventory of every core, provider or storage lock.
File paths intentionally omit line numbers so ordinary edits do not stale them.
None of the mechanisms below adds a mutex beyond the ones it names, or a
UI-state worker.

Three gates keep the inventory honest. `internal/lockcall` fails the tests when
code calls a func value (field, parameter, variable, map entry) with a sync
mutex held, outside a short allowlist of locks that exist to cover their call.
`internal/leakcheck`, run by every package's `TestMain`, fails a package whose
tests leave goroutines running. `just test-stress` (also in CI) race-tests the
most concurrent packages shuffled on one CPU and on four, beside lifecycle
stress tests that overlap prompt, abort, Dispose, Close and Stop.

`ai.EventStream.mu` protects its FIFO, waiters and terminal state. Completion
closes a shared notification channel so every result waiter observes the same
immutable result. Caller-supplied completion predicates and result extractors
run outside the mutex and may reenter `Push`/`End`; terminal admission stops
new pushes before extraction, while EOF waits for publication of the terminal
event unless `End` explicitly closes delivery. A terminal claim is made under
that mutex after predicate evaluation, so concurrently evaluated pushes cannot
both publish terminal events.

`agent.pendingMessageQueue.mu` protects the steering and follow-up queues. Its
caller-supplied defer predicate (`AgentOptions.DeferFollowUp`) runs outside the
mutex on a snapshot, and the drain re-checks a change counter before applying.
An enqueue, another drain, a clear or a mode switch meanwhile makes it decide
again, so a predicate that reenters the queue, and the UI thread's clear or
restore racing a drain, can neither deadlock nor double deliver (A425).

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

`coding.CacheWarmer.mu` also guards callback detachment at session disposal and
synchronizes the mutable run phase read by status, policy evaluation, mode
changes and scheduling. Disposal clears the current run, stops its timer,
cancels its request context and releases `OnWarmed`. Refresh workers snapshot
an admitted callback under that mutex and invoke it outside the lock; an
already-admitted callback may finish without making disposal join itself. Host
currency and mode callbacks run outside that mutex, so their stop is
run-scoped: stale refreshes and lifecycle notifications stop only the run they
observed, matching the reference's single-threaded check-and-stop.

`coding.GoalController.mu` serializes machine transitions with their durable
append effects. A clear/create pair cannot interleave with another tool's
transition records, even when tools execute in parallel. The private session
sink acquires `SessionManager.mu` for record append; interactive file I/O stays
on the manager's existing write queue, while standalone callers retain their
synchronous storage path. No user callbacks run in this locked phase. Tool
exposure synchronization, notices and model work are applied after unlocking;
notification callbacks may reenter `Snapshot`. Durable entry events share one
publisher admitted under this mutex, with listener calls outside it. Reentrant
transitions enqueue behind the current batch, so every subscriber sees
clear/create entries in log order (`TestGoalEntryEventsKeepAppendOrderOnReentry`).
Deferred idle round workers also check their goal id, revision and pending turn
before starting, so a stopped or superseded reservation cannot launch later.

`coding.McpServerConnection.mu` protects attachment, shared-open admission and
lifecycle. Each attempt publishes its own client/error before closing its done
channel; caller cancellation only stops waiting, while `Close` cancels the open.
Tool-change handlers run discovery off the transport reader, and the live client
owns request timeout/close cancellation. Stale results are rejected under the
connection mutex. Manager error queries snapshot state and diagnostic together;
callbacks remain outside locks.

MCP listener registries retain stable entry pointers, so slice growth and
snapshot compaction cannot invalidate unsubscribe handles. Unsubscribe unlinks
the entry and clears the backing-array slot under that same mutex, releasing
captured state while preserving already-admitted immutable callback snapshots.
Empty notification-method registries are removed. Callbacks remain outside
their existing registry mutexes. `mcp.Client.mu` admits incoming request
cancellation before its handler starts on a worker; the stdio reader remains
free to dispatch cancellation notifications. Unknown-method error replies also
write off-reader, so output backpressure cannot stall unrelated responses. Close
snapshots and clears incoming cancellation handles under the mutex, then invokes
them outside it. User handlers run outside locks and their panics are converted
to JSON-RPC error responses. In-memory transports use
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
the interactive owner loop.

The transcript-preparation rows keep their state owner-only and add no queue:

- D205: tool result generations, displayed lines and shell elapsed state stay
  on the owner. Workers mutate only private component snapshots and return
  owner-applied completions through D204's bounded optional queue and UI-post
  gate.
- D206 extends those snapshots through Box padding/background preparation. Only
  the owner adopts the completed cache, rebinding cache and mouse metadata to
  its original children; elapsed clocks and arbitrary callbacks never run on the
  worker.
- D207: completed-frame retention, live clock-row refresh and displayed-width
  mouse dispatch are owner-only. Prepared and displayed metadata stay separate,
  and parent render-version skipping cannot suppress pending admission.
- D208 uses owner revisions for accepted pending clock updates, mutating only
  the owner's retained frame rows. Unadmitted work stays unversioned and copies
  rows when needed; detached worker snapshots are immutable after handoff. The
  tool frame lifecycle lives in `coding/interactive/toolframes.go`; callers
  supply captured input and delegate render/hit-test metadata without reading
  its admission or publication state.
- D209 extends owner-only in-place Container flattening to first-child changes.
  Revision/change-offset metadata guards parent caches, and removed string slots
  are cleared on shrink.
- D231 (syntax highlighting) adds no off-loop path:
  `coding/interactive/codehighlight.go` is pure and calls the `highlight`
  package, whose compiled-language cache has that package's own mutex.

| Lock | Where | Protects | Retirement |
|---|---|---|---|
| `Renderer.postMu` | `tui/render.go` | the posted-callback queue (`Post` is called from off-loop goroutines: loaders, watchers) | **retained**: serializes the handoff only; the owner drains it in the render pass and never runs a callback under it |
| `ProcessTerminal.stdinReaderMu` | `tui/terminal.go` | stdin-reader admission and exit across terminal restarts | **retained**: I/O lifetime handoff only; reads, buffer parsing and callbacks run outside it. Restart reuses an in-flight reader or starts another after the old reader relinquishes ownership; the legacy buffer is atomically published |
| `ProcessTerminal.writeMu` | `tui/terminal.go` | terminal writes, raw-mode transitions, Kitty negotiation bookkeeping shared with the reader | **retained**: not UI state |
| `ProcessTerminal.writesMu` | `tui/terminal.go` | the console-write FIFO, byte backlog counters, open-frame buffer, deferred metadata hints and cancellation-aware optional-admission waiters (`Write` appends essential output in submission order; a dedicated goroutine drains it) | **retained**: off-loop writer; the UI loop never writes to the console itself |
| `resizeWatcherMu` | `tui/terminal_windows.go` | the Windows console-resize poller's stop/done channels | **retained**: the poller runs off the loop; `Stop` signals it without joining, so an admitted callback finishes instead of deadlocking (CR-063) |
| `prerenderState.mu` | `coding/interactive/transcript.go` | the transcript warm-ahead handoff (ready chunk, width, generation) | **retained**: loop ↔ warm-worker handoff, not UI state |
| `systemThemeState.mu` | `coding/interactive/theme.go` | the terminal-report colors the system theme is generated from (written by the terminal query reply path, read at theme load) | **retained**: a pointer swap of two values, never UI state |
| `themePublishMu` | `coding/interactive/theme.go` | the order of writes to the current theme and the registered-theme map (readers load the atomics without it) | **retained**: a leaf around atomic stores; the custom-theme watcher re-checks its selection and publishes its reload under it, so a theme switch made during the file read stands (`TestThemeSwitchDuringAWatcherReloadStands`) |
| `terminalQueries.colorMu` | `tui/terminalqueries.go` | pending combined/background/scheme query queues and settlement shared by blocking waits and owner-loop reply dispatch; listener registries remain owner-only and delivery runs after unlocking | **retained**: query handoff bookkeeping, not UI state |
| `FooterDataProvider.mu` | `coding/footerdata.go` | cwd/git/status + listener registry, shared with its 500 ms git-HEAD watcher | **retained (D149)**: closing it needs the poll result posted to the loop and the listener fan-out delivered outside the lock; a `coding/` change outside this refactor |
| `AgentSession.promptOptionsMu` | `coding/agent_session.go` | `SystemPromptOptions`, re-read by the prompt/tool loadout between turns | **retained**: the work goroutine re-applies the loadout while the UI loop rebuilds the options on `/reload` and tool changes; only a struct copy is taken under it |
| `cellDimensionsState.mu` | `tui/terminalimage.go` | the global terminal cell pixel size, written by the size-report reply and read during image layout | **retained**: package-global value shared with the terminal query path, not UI state |
| `kittyRegistry.mu` | `tui/terminalimage.go` | the package-global kitty placement registry (order, id index, transmission generation) | **retained**: image-protocol bookkeeping, not UI state |
| `imageTranscodeState.mu` | `tui/imagecomponent.go` | the package-global image transcode cache, injected transcoder and registration generation | **retained**: shared decode cache, not UI state; converter callbacks run unlocked and superseded conversions cannot refill the replacement cache |
| `modelCompletionCache.mu` | `coding/interactive/interactivemode_helpers.go` | the autocomplete model-item cache keyed by the session scope revision | **retained**: caches immutable item slices across completion queries, not UI state |
| catalog wrapper local `modelsMu` | `coding/remotecatalog.go` | immutable remote/personalized overlay slice snapshots and swaps | **retained**: shared between refresh workers and catalog readers; callbacks and base-provider getters run after unlocking |
| `SettingsManager.mu` | `coding/settingsmanager.go` | the global/project layers, modified-field sets, recorded errors and project binding; setters merge, persist and publish under it | **retained** for setters only: every merge builds a fresh deep copy published to `SettingsManager.merged` (an `atomic.Pointer`), so getters that read only the merged view take no lock and never wait for a persist in progress (`TestSettingsGettersDoNotWaitForAPersistInProgress`) |
| `oauthAdapter.mu` | `mcp/mcp_federation.go` | shared OAuth refresh admission (`inFlight`) | **retained**: provider token callbacks run unlocked and admission is rechecked afterward, so custom token stores can re-enter (`TestOAuthAdapterTokenCallbackCanReenter`) |

`StreamableHttpTransport.Send` links its caller context to the transport stop
context. Synchronous responses release that link on return; an SSE response
worker owns it through stream consumption. Closing the transport therefore
cancels both POSTs and GETs, without canceling a live SSE body at the initial
Send return or changing shared auth recovery ownership.

The typed `coding/rpcclient.RpcClient` snapshots request/listener state under
`mu` and serializes stdin writes under `writeMu`. Event callbacks run outside
both. Output readers own their pipe handles, so `Cmd.Wait` cannot close unread
final events. Natural process exit still rejects pending requests immediately;
`Stop` waits for that process-exit publication, not reader completion, since a
reader callback may itself call `Stop`. Startup reserves admission under `mu`
before spawning outside it, and releases that reservation on setup failure.
No callback queue is added.

D235's stdio RPC runtime adds no UI lock. The existing `AgentSession.mu` also
guards steering/follow-up display queues across command admission and agent
events; snapshots are taken under it and listeners are called outside it.
`rpcRuntime.outputMu` guards only the first output error. The byte writer and
all callbacks run outside it. Prompt admission and cancellation handles belong
to the command owner; four pending preflights and an unbuffered byte handoff
bound concurrent work without unbounded queues. Prompt workers have separate
settlement accounting, so `abort` joins canceled preflight cleanup without
waiting for independently running user bash. The owner admits and joins that
group (`TestRPCAbortWaitsForCanceledPreflightToSettle`).

Retired locks, all removed from the code. "Retired in" names what replaced
each: the owner loop, or a step of the ownership refactor (the
`refactor(stage N)` commits; `git log --grep='(stage '`). Searching non-test Go
files in `tui/` and `coding/interactive/` for mutex declarations finds only the
retained locks above (UI-state handoff plus the package-global caches):

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

The PTY watchdog flows used to consume this list: while UI mutexes existed, a
flow drove the real binary through an interaction that could park the loop
behind one and checked the `SIGQUIT` dump. With every UI-state lock retired,
those deadlock paths are gone. Core locks and blocking work can still stall the
loop, so `internal/uiblock` checks the owner-loop invariant statically, and the
flows' functional coverage moved to `coding/interactive/ptyflow_test.go`, which
drives the app in process. Other real-binary PTY tests and the deliberately
deadlocking parser helper remain.
