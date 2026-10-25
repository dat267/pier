# Codebase audit, 2026-10-25

## Baseline and scope

This is a **partial multi-round review**, not a completed codebase-wide audit.
Fifteen confirmed findings have been planned in `TODO.md`, repaired red-first,
and verified. Remaining files and unconfirmed leads below still need review.

- Starting commit: `80dcf0d09249222f98f39bccb2f93d278766009b`.
- Reference: pi v1.4.0 (`4a080252f`), verified by `just reference`;
  `git -C pi status --porcelain` empty before and after the pass.
- Existing untracked work: `.pi/skills/code-review/SKILL.md`, preserved.
- `GOPROXY=off GOSUMDB=off go vet ./...`: PASS.
- `GOPROXY=off GOSUMDB=off go test -short ./...`: PASS, all packages.
- Full race gate belongs to CI, not run locally. No race-freedom claim.
- No dependencies installed. The review initially had no commit authorization;
  the user subsequently requested a commit whenever each piece is done, so
  completed verified work is committed in logical pieces. No push is
  authorized. New probes only loaded local configs or used loopback HTTP;
  no new fixture executes a configured MCP server command.

Inventory: `git ls-files '*.go'` at the starting commit lists 1,258 Go files,
including tests: `agent` 28, `ai` 195, `cmd` 17, `coding` 541,
`internal` 269, `main.go` 1, `mcp` 59, `tui` 148. First-party scripts,
workflows and configuration were also inventoried. Generated catalogs,
dependency bodies, build output and reference sources are not first-party
coverage. Reference sources are read separately for supported parity checks.

## Coverage ledger

Each status applies **only to the named slice**. P = parity, L = logic,
C = concurrency, S = security, F = performance. Inspected means static source
review of that slice, not exhaustive testing or proof of safety. No measured
bottleneck is asserted without evidence. Unlisted files remain unreviewed.

### ai

- Credential subsection of `ai/authstore.go` (`beginWrite`, `Read`, `List`,
  `Modify`, `Delete`, `cloneMemoryCredential`): P/L/C/S/F inspected against
  `pi/packages/ai/src/auth/credential-store.ts`. Checked per-provider admission,
  canceled-tail draining, lock-free callback execution and owned snapshots.
  D272 is intentional, not a new finding.
- `ai/streamidle.go`: P/L/C/S/F inspected, with
  `pi/packages/coding-agent/src/core/http-dispatcher.ts` and D254/D255 as the
  timeout authority. Close/reset atomics and bounded request body lifetimes
  were traced. No new confirmed finding.
- `ai/sse_lines.go`: L/C/S/F inspected; P not yet inspected in this pass beyond
  existing baseline tests. The splitter is per-stream state, not a shared
  buffer. Provider-specific request/response code remains mostly unreviewed.
- `ai/jstext.go` number formatting (`JSNumberToString`, `JSNumberToFixed`):
  P compared in round 6 by a 188-value corpus recorded from Node, including
  subnormals, the 1e21/1e-6 notation switches, 2^53 boundaries and tie
  rounding. Matched on the first run; coverage only.
- `ai/overflow.go`: P/L/S inspected in round 6 against `utils/overflow.ts`; P
  compared by a 110-case corpus executing the pinned detector (provider texts,
  near-misses, Cerebras-only branch, silent and length-stop usage). Matched;
  coverage only.
- `ai/jstext.go` case mapping: `JSLower` was probed against Node's
  `toLowerCase` for 27 strings covering U+0130, final sigma, titlecase
  digraphs, `ẞ`, ligatures, fullwidth and astral letters. All matched; the
  probe is temporary and this row is its only record.

### agent

- `agent/proxy.go`: P/L/C/S/F inspected against
  `pi/packages/agent/src/proxy.ts` and the normalized `TranscriptContext` in
  `ai/messages.go` and `pi/packages/ai/src/types.ts`. Checked terminal/EOF
  handling, request projection, content-index guards and builder accumulation.
  D252 already covers negative indexes. No new confirmed finding.
- `agent/agent.go` `Subscribe`, `State`, setters, queue/abort/idle methods,
  `Continue`, `runPromptMessages`, `runWithLifecycle`, `handleRunFailure`,
  `processEvents`: L/C/S/F inspected; P not yet fully compared for these
  lifecycle methods. Callbacks are delivered outside retained locks; pending
  tool map snapshots are copied. `agent/loop.go` and the rest of the package
  remain unreviewed in this pass.

### coding core, tools and persistence

- `coding/trustmanager.go`, resolver portion of `coding/projecttrust.go`,
  and loader portion of `coding/mcpconfig.go` (through `readMcpConfigFile`):
  P/L/C/S/F inspected against `core/trust-manager.ts`, `core/project-trust.ts`,
  and `extensions/mcp/config.ts`. **CR-004** confirmed and repaired.
- `coding/session_persistence.go` and `coding/session_manager.go`
  `rewriteFile`/`appendLine`/`persistEntry`: L/C/S/F inspected, P not yet fully
  compared in this pass. Snapshot closures connect to **CR-003**.
- `coding/updatecommand.go` and release-resolution portion of
  `coding/versioncheck.go` (`getLatestPortReleaseFrom` onward): L/C/S/F
  inspected. P not applicable to the port-specific binary updater under
  D174/D175. Checked checksum validation before replacement, temp placement,
  fallback restore and redirect handling. No new confirmed finding; this is
  not a signature-verification or upstream trust-policy claim.
- `coding/paths.go`, `coding/mutationqueue.go`, `coding/tools_edit.go` and
  `coding/tools_fs.go`: P/L/C/S/F inspected in round 2 against
  `utils/paths.ts`, `core/tools/{path-utils,file-mutation-queue,write,ls}.ts`,
  the read execution path in `read.ts`, and edit preparation/execution in
  `edit.ts`. Checked original-content edits, BOM/line-ending restoration,
  mutation lifetime across cancellation, directory/byte limits and path
  normalization. Local file tools intentionally accept absolute paths and
  follow symlinks; they are not a filesystem sandbox. No new reproduced
  finding in this slice. Platform-specific file URL handling and queue FIFO
  admission still need independent probes (see leads).
- `coding/bashexecutor.go` `ExecuteBashWithOperations`: L/C/S/F inspected in
  round 5 against `core/bash-executor.ts`; P compared for the rolling display
  buffer, sanitization order and full-output file. **CR-015** repaired.
- `coding/truncate.go`: P/L/C/S/F inspected in round 5 against
  `core/tools/truncate.ts`; P compared by a generated 128-case corpus that
  matched on its first run (head, tail, line, middle, size). Recorded as
  coverage. One deliberate representation difference: Go options use zero for
  "unset" while the reference distinguishes an explicit `0` via `??`; no
  upstream caller passes zero, so the corpus omits that unreachable input.
- `coding/tools_bash.go` `OutputAccumulator` and `bashUpdateThrottle`: P/L/C/S/F
  inspected in round 5 against `core/tools/output-accumulator.ts` and the
  throttle in `renderers/bash.ts`. A generated 12-case corpus (limits, partial
  lines, multibyte, CRLF, BOM, split chunks) matches on the first run, so it is
  recorded as coverage, not a finding. `readFullOutput` (structured content for
  codemode callers) is out of scope with codemode.
- `coding/tools_bash.go` `CreateShellTool` and `coding/tools_powershell.go`:
  L/C/S/F inspected in round 5 against `createShellToolDefinition`,
  `createLocalShellOperations` and `resolveSpawnContext`. Checked abort/timeout
  kill paths, pipe-drain ordering, streamed-update throttling, truncation
  notices and exit-status mapping. One prompt-guideline divergence is reachable
  only from a Go host's own wiring (see leads), not from the CLI.
- `coding/agent_session_control.go` `GetLastAssistantText`: P/L/C/S/F
  inspected in round 4 against the pinned `agent-session.ts` method and
  generated session/RPC cases. **CR-014** repaired; message selection and
  non-text/abort controls match. Initial find/search and bash-executor code
  was also read, but those files are not yet fully covered.
- `coding/settingsmanager.go`: the declarations, timeout parser and storage
  implementations (`FileSettingsStorage`, `InMemorySettingsStorage`, lock
  admission/refresh/release and callback/write ordering) were inspected for
  L/C/S/F. P is not yet fully compared for storage in this pass; no new
  reproduction there. Search/bash tools, the remainder of settings management,
  session projection/compaction and SDK lifecycle remain unreviewed.
- `coding/rpcmode.go`: L/C/S/F inspected in round 3 across parsing/validation,
  byte pumps, prompt admission, blocking workers, replacement and command
  projections. P compared for replacement/fork ordering against
  `core/agent-session-runtime.ts`; the rest of the protocol has not been
  independently probed in this pass. **CR-012/CR-013** repaired. No network-auth claim is
  made for this intentionally local, trusted subprocess protocol.

### coding/interactive

- `coding/interactive/interactivemode_eventqueue.go`: L/C/S/F inspected;
  P inspected at the documented D143/D196/D197 ownership and coalescing
  boundary, not as byte-identical event scheduling. Checked bounded channels,
  generation stamping, lossless cancellation/close release and partial drops.
- `coding/interactive/interactivemode_run.go` `drainReadyEvents`,
  `drainReadyRawInput`, `renderUI`, worker dispatch adapters and the main select
  portion of `runLoop`: L/C/S/F inspected, P not yet independently compared.
  Checked bounded drain loops and owner-only mutation, not every callback's
  transitive work.
- `coding/interactive/interactivemode_schedule.go`: L/C/S/F inspected in
  round 2; P inspected at the input-timeout boundary against
  `pi/packages/tui/src/stdin-buffer.ts` `process`/`flush`, with the scheduling
  topology intentionally Go-only under D143/D145/D198. **CR-008** repaired in
  round 2, and **CR-010** (completed work capture retention) in round 3.
  Wiring lifecycle, component workers, clipboard, transcript preparation and
  remaining UI files still need review.

### coding/rpcclient

- `coding/rpcclient/client.go`: P/L/C/S/F inspected against the start/stop,
  listener and collection/send sections of
  `pi/packages/coding-agent/src/modes/rpc/rpc-client.ts`.
  Existing subprocess and listener-failure tests were read. Listener snapshots
  and request correlation were traced. In round 2 both process-lifecycle leads
  were reproduced and repaired as **CR-005/CR-006**; exit code 0 is **CR-007**.
  Round 3 reproduced overlapping startup admission 5/5 times; **CR-009**
  reserves process ownership before spawning and releases failed setup.
- `coding/rpcclient/commands.go`: P/L/C/S/F inspected in round 2 against the
  pinned public methods. Optional arguments and typed decoders were traced;
  no new supported-path command-shape finding was reproduced. This is not
  proof of all concurrent startup/stop or late-delivery behavior.

### tui

- `tui/keys.go` `ParseKey`: P/L/S inspected in round 6 against
  `packages/tui/src/keys.ts`; P compared by a 234-case corpus executing the
  pinned parser in legacy and Kitty modes (control characters, symbols, CSI/SS3
  arrows and function keys, modifiers, mouse reports, CSI-u variants, bracketed
  paste, astral and combining runes, malformed prefixes). Matched; coverage only.
  `matchesKey`, the kitty negotiation globals and keybinding resolution are not
  yet compared in this pass.
- `tui/stdinbuffer.go` sequence parsing, paste, timeout and clear paths:
  L/C/S/F inspected; P not yet independently compared in this pass.
  Consumer ownership is explicit; paste terminator search advances over the
  untested suffix. D49/D169 are intentional. No new confirmed finding.
- `tui/terminalqueries.go` `BeginTerminalColors`, query waiting,
  `consumeTerminalColorResponse`, completion helpers and notify lifecycle:
  L/C/S/F inspected; P not yet independently compared. Delivery is snapshotted
  under `colorMu` and invoked after unlocking. Full terminal I/O, platform
  files, renderer/layout, image decoding and remaining input paths need review.

### mcp

- `mcp/oauth/callback.go`: P/L/C/S/F inspected against
  `pi/packages/mcp/src/oauth/callback.ts`. **CR-001/CR-002** repaired; state,
  expected redirect path, error and code-delivery paths were also traced.
- `mcp/transport_stdio.go`: P/L/C/S/F inspected against
  `pi/packages/mcp/src/transports/stdio.ts`. Checked owned output pipes,
  process-exit/read-drain ordering, listener reentrant close, message/stderr
  bounds and process-group cleanup. D182 remains intentional.
- `mcp/transport_http.go`: P/L/C/S/F inspected in round 3 against
  `pi/packages/mcp/src/transports/streamable-http.ts`, including SSE parsing,
  auth retries, headers, POST/GET lifetimes, response resumption and backoff.
  **CR-011** repaired. D183's Go network-error retry classification is
  intentional. Malformed URL construction and token-wait cancellation remain
  unconfirmed leads rather than fixes.
- `mcp/oauth/discovery.go` default fetch, challenge parsing and protected
  resource metadata fetch: L/C/S/F inspected; P not independently compared in
  this pass. Remaining discovery, OAuth flow/provider, in-memory transport,
  client request handling and protocol decoders need review.

### cmd and root

- `main.go`, `cmd/trust.go`, startup trust/MCP integration sites in
  `cmd/main.go`: P/L/C/S/F inspected against the resolver references above
  within this slice. The root only calls `cmd.Execute`; MCP receives the trust
  result. Existing `cmd/trust_test.go` and project-scope MCP tests are controls.
  **CR-004** affects this call chain. The rest of CLI startup, signal teardown,
  mode selection and RPC replacement remain unreviewed.

### internal

- `internal/offloop/offloop.go`, `internal/offloop/group.go`: L/C/S/F inspected,
  including admission, replacement/coalescing, mandatory drain and optional
  cancellation. P not applicable to Go-only scheduling under D143/D199, with
  pi `core/session-manager.ts` as the persistence context. **CR-003** repaired.
- `internal/goal/machine.go`: L/S/F inspected in round 3 for mutation/CAS,
  budgets, round reservation, pause/reload/abort and effect construction. C not
  applicable within the pure owner-driven machine; its controller handoff is
  not yet inspected. P not independently compared with the separate user
  extension source in this pass; D223/D232/D239/D242/D243 already describe
  deliberate behavior. State declarations were read, but full replay
  validators and command/render/tools implementations still need review.
- Highlight/markdown/jsdiff implementations and `uiblock` analyzer internals
  are not manually reviewed in this pass; their tests ran.

### Scripts, workflows and configuration

- `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `justfile`:
  L/S/F inspected for gate/release/checksum/platform wiring; C inspected for
  the single-runner build structure, not remote concurrent release behavior;
  P not applicable to port-specific build/release automation.
- `scripts/gen_catalog.py`, `scripts/gen_image_catalog.py`,
  `scripts/gen_bedrock_wire.mjs`: L/S/F inspected. C not applicable to these
  sequential generator bodies. P not yet independently compared with all
  upstream generator transforms in this pass. No generator fetch was run and
  generated catalogs were not modified. Remaining configuration and fixture
  generators still need review.

## Confirmed findings and resolutions

### CR-004, P1: MCP-only project bypasses trust (fixed)

A repository author can supply only `.pi/mcp.json` with a stdio command. The
resource list omitted `mcp.json`, so `ResolveProjectTrusted` took its no-resource
fast path before a stored denial, `defaultProjectTrust: "never"`, or a prompt.
A config-only differential probe showed pi `hasResources=true, trusted=false`
versus Pier `hasResources=false, trusted=true, servers=1`. No command was run.
This crosses the existing project-resource trust boundary, not a request for
new sandboxing or a deliberate departure from pi.

Planned in `TODO.md` before editing production code. The generated ten-case
`coding/testdata/project-trust/` corpus calls pinned resource detection,
trust resolution and MCP config loading. `TestMCPOnlyProjectRequiresTrust`
failed on the missing resource flag, skipped prompts and admitted servers.
Adding `mcp.json` to the existing list makes every case pass. Empty/settings
projects, headless ask, stored decisions, explicit overrides and interactive
accept/refuse are controls. Existing CLI trust/MCP tests also pass.

Replay:

```sh
node coding/testdata/project-trust/generate.mjs ./pi
go test -count=1 ./coding -run '^TestMCPOnlyProjectRequiresTrust$'
go test -count=1 ./cmd -run 'Trust|MCP'
```

### CR-001, P2: MCP OAuth IPv6 callback cannot bind (fixed)

`Host: "::1"` produced `listen tcp: address ::1:0: too many colons in address`;
the same pinned listener bound and served the request. IPv4 matched already.
The red `TestCallbackIPv6MatchesUpstream/ipv6` observed that exact failure;
`net.JoinHostPort` now formats the listen address. The test skips only on a
host without independent IPv6 loopback support. IPv6 was available and the test
ran here. Reference: `packages/mcp/src/oauth/callback.ts`, `listen`.

### CR-002, P2: MCP OAuth routing decodes URI paths (fixed)

Configured `/callback%2Fcustom` returned 404 instead of reaching the waiter;
`/call%62ack` incorrectly aliased `/callback`. The reference compares escaped
JS `URL.pathname`, whereas Go used decoded `URL.Path`. After planning in
`TODO.md`, `TestCallbackPathsMatchUpstream` failed on four escaped/alias cases.
Both allowed-path and expected-waiter-path checks now use `EscapedPath()`.
Ordinary/extra paths and wrong pending redirect paths remain controls; real
pending escaped callbacks now deliver the code successfully. No auth bypass
claim is made from the old path alias, since state validation still applied.

CR-001/CR-002 share the nine-case source-generated callback corpus. Replay:

```sh
node mcp/oauth/testdata/callback/generate.mjs ./pi
go test -count=1 ./mcp/oauth -run '^TestCallback(IPv6|Paths)Match'
```

### CR-003, P2: completed worker tasks retain captured payloads (fixed)

`Queue.loop` advanced its task slice without zeroing the consumed slot. Its
live backing array retained completed closures, including session snapshot
captures in `SessionManager.rewriteFile`/`appendLine`, during later work or idle
periods. A public `Queue` regression fixes the schedule with channels, captures
a non-tiny 1,024-byte object, lets its task finish, parks the next job, and runs
GC. The weak target remained live before the repair, without wall-clock or
heap-noise thresholds. This is a Go-only retention bug, not an upstream parity
claim or a measured end-to-end latency claim.

After recording the plan in `TODO.md`, the consumed slot is cleared under the
existing mutex before slicing. The same regression passes 20 repeated runs;
ordering/coalescing/drain controls and coding/UI tests pass. No cache,
threshold, worker or scheduling change was added.

```sh
go test -count=20 ./internal/offloop -run '^TestQueueReleasesCompletedTaskCaptures$'
go test -count=1 ./internal/offloop ./coding ./coding/interactive
```

## Round 2 findings and resolutions

The reference and dirty-tree baseline were rechecked at round start. Earlier
repairs and the untracked skill were preserved. No commit was made.

### CR-005, P2: event listener Stop joins itself (fixed)

The supported `OnEvent` callback called `Stop` after the peer emitted its
request event. The process exited, but `Stop` waited on the reader wait group,
including the goroutine currently executing that callback. The public
`TestClientEventListenerCanStopClient` failed at its 3-second deadlock guard
with `event listener Stop waits for its own stdout reader`. The equivalent
pinned `RpcClient` listener-initiated stop settled in a Node source probe.

After recording the finding in `TODO.md`, removed the reader self-join and its
now-unneeded wait group. `Stop` still waits for process-exit publication and
pending rejection. This preserves callback execution outside mutexes without
adding another worker or an unbounded callback queue. External stop,
idempotence, restart and process-exit controls passed ten repeated runs.
Reference: `packages/coding-agent/src/modes/rpc/rpc-client.ts` `stop`.

### CR-006, P2: natural RPC process exit discards unread final events (fixed)

`TestClientNaturalExitDrainsFinalEvent` blocks the first event listener, then
sends the second request. The peer only then writes its final event and exits
43. Both requests observe process exit before the first listener is released,
so the final event cannot have been buffered in that listener's original read.
The old client fails with `natural process exit discarded the unread final
event`. A pinned Node source probe receives `first,final` on natural exit.

The planned repair owns stdout/stderr `os.Pipe` handles instead of letting
`Cmd.Wait` close `StdoutPipe`/`StderrPipe`. Parent writer handles close after
spawn; readers close on EOF; failed setup closes owned handles. Process exit
still rejects requests immediately, and `Stop` does not join reader callbacks.
The new scheduled regression and CR-005/restart/exit controls pass ten repeated
runs. Reference: `modes/rpc/rpc-client.ts` and `modes/rpc/jsonl.ts`; this fixes
the Go process/read lifetime race, not a new queueing or timeout policy.

### CR-007, P3: successful RPC peer exit reports a null code (fixed)

The peer waits for a request and calls `os.Exit(0)`. The old
`TestClientReportsZeroExitCode` failed on
`Agent process exited (code=null signal=null)`; the pinned source probe reports
`code=0 signal=null` (and reports 43 for the nonzero control). Go `Cmd.Wait`
returns nil for success, so the client now explicitly assigns 0 for that case.
Signal/non-exit wait errors retain the prior handling. This is an actual exit
status value, not V8 JSON diagnostic emulation. The finding was recorded in
`TODO.md` before production edits. Reference: `rpc-client.ts`
`createProcessExitError` and child exit event handling.

Replay for CR-005/CR-006/CR-007:

```sh
go test -count=1 ./coding/rpcclient -run '^TestClient(EventListenerCanStopClient|NaturalExitDrainsFinalEvent|ReportsZeroExitCode)$'
go test -count=10 ./coding/rpcclient -run '^(TestClientNaturalExitDrainsFinalEvent|TestClientEventListenerCanStopClient|TestClientCanRestartAfterStop|TestClientRejectsInFlightOnExit)$'
```

### CR-008, P2: an idle beat cancels the pending Escape deadline (fixed)

A lone Escape in the real `tui.StdinBuffer` arms a 10 ms timeout. A second UI
scheduling decision before expiry falls through the equal-deadline branch and,
when nothing animates, stops that same timer. The loop then has no input wake;
Escape can remain pending until another event arrives. The real-input/host-seam
regression `TestSchedulePreservesPendingEscapeAcrossIdleBeats` fails at
`idle beat discarded a still-pending Escape deadline`, entirely within a
`testing/synctest` clock. A pinned `StdinBuffer` source probe still dispatches
Escape after an unrelated immediate callback.

After recording the repair plan, `arm` now returns the retained earliest timer
channel whenever a future input deadline remains, even if it was already armed.
No new timer or worker is added. The regression verifies actual Escape delivery;
existing schedule/input/backpressure tests and UI/TUI tests pass. Reference:
`packages/tui/src/stdin-buffer.ts` `process`/`flush`, with D143/D145's owner-loop
adaptation. This is a Go scheduling defect, not an upstream timer defect.

```sh
go test -count=1 ./coding/interactive -run '^TestSchedulePreservesPendingEscapeAcrossIdleBeats$'
go test -count=1 ./coding/interactive -run '^Test(LoopSchedule|DeferredPaintRetry)'
```

## Round 3 findings and resolutions

The starting commit/reference stayed unchanged, and all earlier work was
preserved. This round expanded RPC server, HTTP transport, settings storage and
goal-machine inspection. Five new confirmed findings were planned before
production edits and repaired. No commits or dependency installations occurred.

### CR-009, P2: overlapping RPC starts spawn multiple peers (fixed)

`Start` checked `started` under its mutex but only set it after spawning. Eight
callers behind one gate therefore admitted 8, 2, 4, 3 and 4 peers in five
pre-fix runs, instead of exactly one. Only the last stored process was reachable
through the client's ordinary request/stop ownership. The fixture's shared
context canceled all peers, so the red runs left no deliberately detached child.
Reference: `modes/rpc/rpc-client.ts` assigns `this.process` before its first
await and rejects overlapping starts.

After the plan was recorded in `TODO.md`, startup reserves `started` under the
existing mutex before spawning outside it; failed setup releases the reservation.
The admission regression verifies a usable winning peer and passes 20 repeats.
A separate local-only public API probe verified missing-cwd startup failure,
creation of that directory, then successful retry and `State` round-trip.
Existing stop/restart/exit controls pass. No callback runs under the new claim.

```sh
go test -count=20 ./coding/rpcclient -run '^TestClientConcurrentStartAdmitsOnePeer$'
go test -count=1 ./coding/rpcclient
```

### CR-010, P2: completed UI work retains its captures (fixed)

The scheduler's pending-function slice advanced without clearing the consumed
slot, retaining a completed prompt closure through the backing array while
later work ran. `TestLoopWorkReleasesCompletedCaptures` proves a non-tiny
1,024-byte weak target survives GC before the repair. Channels fix work order;
`testing/synctest.Wait` lets the previous per-unit workers exit, so a live old
worker cannot be mistaken for queue retention. The refined test was also
rerun red with the slot-clear repair removed, then green after restoring it.

After planning, `finishWork` clears the consumed function on the owner loop.
The regression passes 20 repeats, with work-order/input/schedule controls green.
This is a Go-only D143 lifetime bug, like CR-003 in a separate queue, not a
claimed end-to-end speedup. No queue, worker or admission policy was added.
Reference context: awaited work in `interactive-mode.ts`.

```sh
go test -count=20 ./coding/interactive -run '^TestLoopWorkReleasesCompletedCaptures$'
```

### CR-011, P2: closing MCP HTTP leaves in-flight POST alive (fixed)

A loopback handler announces POST entry and waits for its context to cancel.
The old `Close` returned without canceling `Send` or the network request;
`TestStreamableHTTPCloseCancelsInFlightPost` failed at its bounded guard with
`Close did not cancel the in-flight POST`. The pinned transport's injected-fetch
probe observes both its dispatch signal aborted and Send rejected. Reference:
`packages/mcp/src/transports/streamable-http.ts` uses its lifetime controller
for all dispatch, not just GET.

After the plan, each Send context is linked to transport stop. Synchronous
responses release the link on return; SSE response consumption owns it after
headers, so returning from Send cannot abort its live body. Caller cancellation
and shared auth recovery ownership remain intact. Existing HTTP response-stream,
GET, replay, auth and canceled-request controls all pass. No new mutex or helper
queue was introduced.

```sh
go test -count=1 ./mcp -run '^TestStreamableHTTP'
```

### CR-012, P2: in-memory RPC fork receives outgoing aborted tail (fixed)

With a provider turn held, fork before the original first message should select
an empty replacement. The old JSONL implementation returned an aborted assistant
in that replacement instead. The stream-entry channel establishes that the turn
is active before the command. `forkManager` reused and cleared the manager
before `installSession` settled workers, so abort persistence hit the new branch.

The pinned `core/agent-session-runtime.ts` `fork` orders in-memory work as
`settle,branch`. Its direct import could not load an uncached generated
`compat-schema.js` artifact; the source probe instead executes the exact method
and its local text helper with Node's built-in type stripping and a host seam.
It does not substitute an installed bundle or manually copy the implementation.
The durable Go regression exercises the real JSONL/provider cancellation path.

After planning, in-memory branching settles workers before mutating the shared
manager. The original regression and existing memory/persisted fork, clone,
replacement and failure tests pass; the session projection is not hand-built.

```sh
go test -count=1 ./coding -run '^TestRPCInMemoryForkSettlesBeforeChangingBranch$'
```

### CR-013, P2: unavailable fork/clone mutate the session anyway (fixed)

A Go RPC host without `ReplaceSession` correctly returned
`Session replacement is not available in this build`, but it first changed the
in-memory session ID, and fork before the first message cleared its entries.
The JSONL test observed both command failures and changed subsequent state/entry
queries. This is a Go-only optional-host-seam error under D235, not an upstream
feature gap: upstream RPC always has a runtime factory.

After recording the plan, `forkManager` rejects the absent factory before branch
side effects. Rejected fork/clone now preserve IDs and entries, while configured
factory, validation, clone/fork and failed-replacement controls remain green.

```sh
go test -count=1 ./coding -run '^TestRPCUnavailableForkPreservesInMemorySession$'
go test -count=1 ./coding -run '^TestRPC'
```

## Round 4 finding and commit policy

### CR-014, P3: last-assistant text uses Go's whitespace set (fixed)

A reply surrounded by U+FEFF stayed surrounded in Pier rather than trimming to
`answer`. Conversely U+0085 boundaries were stripped even though JS retains
them, and a U+FEFF-only reply incorrectly appeared as nonempty RPC text.
The nine-case source-generated corpus executes the exact pinned
`core/agent-session.ts` `getLastAssistantText` method after Node's built-in type
stripping, not a copied implementation or installed bundle. The regression
failed on all three cases through both the public session method and JSONL RPC
responses. Normal trim, text-block joining, aborted empty/nonempty messages,
missing assistants and non-text tails are controls.

After planning in `TODO.md`, the extraction boundary now uses existing
`ai.JSTrim`. Selection, concatenation and RPC's absent-field behavior are
unchanged. Focused tests and the repository gates pass, and the corpus
regenerates byte-identically. This also fixes the text passed to `/copy`.

```sh
node coding/testdata/last-assistant-text/generate.mjs ./pi
go test -count=1 ./coding -run '^TestLastAssistantTextMatchesUpstream$'
```

During this round the user explicitly requested a commit whenever a piece is
done. Earlier no-commit instructions are superseded for completed, verified
work. Logical commits now separate the skill, trust fix, OAuth callbacks,
worker/scheduler fixes, RPC lifecycles/session integrity, MCP POST cancellation,
assistant-text parity, and audit documentation. No push is authorized.

## Completed-piece commits

After explicit user authorization:

- `5573e40`: CR-004, require trust for MCP-only projects.
- `d23e5df`: CR-001/CR-002, callback IPv6 and escaped-path parity.
- `270ba29`: CR-003, release off-loop task captures.
- `dc3f50e`: CR-008/CR-010, preserve input wakes and release UI work captures.
- `d236c3c`: CR-005/CR-006/CR-007/CR-009, RPC subprocess lifecycle/ownership.
- `c678533`: CR-012/CR-013, RPC fork ordering and unavailable-host integrity.
- `c1ec637`: CR-011, cancel MCP HTTP POSTs on transport close.
- `c67ac9e`: CR-014, JS-trim parity for last-assistant text.
- `56b5ca4`: manual-only workspace code-review skill.
- `b47e0d8`: review skill commits each finished piece.
- `6fedf16`: CR-015, bash display output sized in UTF-16 units.
- `2e02b2b`: output-accumulator snapshot corpus (coverage, no divergence).
- `5af0539`: truncation corpus (coverage, no divergence).
- `9320c6a`: JavaScript number-formatting corpus (coverage).
- `f3c2399`: context-overflow corpus (coverage).
- `8080a42`: key-parsing corpus (coverage).

All changes were verified before committing. Audit status and lock ownership
are recorded with this ledger in a separate documentation commit. No push.

## Round 5 finding

### CR-015, P2: bash display output collapses for multibyte text (fixed)

A command emitting 40,000 `漢` characters plus `tail` showed the model only
`tail` (4 bytes, `truncated: false`), while the pinned function kept a 51,199
byte tail flagged truncated; 30,000 `😀` reproduced it (4 bytes versus
51,200). ASCII output of comparable size, small output, and a many-small-chunk
run matched, so the boundary is character width, not the budget itself. The
persisted full-output file hash matched in every case, so file recovery was
unaffected and only the tool result was misleading.

The corpus (`coding/testdata/bash-executor/`) executes the pinned
`core/bash-executor.ts` function with its real helpers and records output and
file SHA-256 values; it was added before any production edit. After the plan
was recorded in `TODO.md`, the rolling buffer gained a separate
JavaScript-length counter (`JSLength`) while the temp-file threshold kept its
byte counter, matching upstream's distinct `totalBytes` and `text.length`.
Truncation limits, sanitization, callbacks and file handling are unchanged.

Replay:

```sh
node coding/testdata/bash-executor/generate.mjs ./pi
go test -count=1 ./coding -run '^TestBashExecutorRollingOutputMatchesUpstream$'
go test -count=1 ./coding -run Bash
```

## Unconfirmed leads, not TODOs

- Previously listed RPC lifecycle leads are CR-005/CR-006, and overlapping
  startup is CR-009; all are repaired. Explicit-stop/new-start late-event
  boundaries and Stop during unfinished spawning still need bounded probes.
  No claim of lifecycle completeness is made from the new regressions.
- Platform file URLs: common `filePathFromURL` code and Windows Node
  `fileURLToPath` drive/UNC rules merit a target-runtime comparison. This pass
  ran on Linux and did not execute the Go implementation on Windows. No
  Windows behavior change is justified from cross-builds alone.
- Bash prompt guidelines are not gated on `exposeSessionEnvironment`
  (`core/tools/bash.ts` sets `promptGuidelines` only when it is true). Pier's
  contributions are static per-tool maps, so a Go host that builds the shell
  tool with `ExposeSessionEnvironment: false` still gets the "You can inspect
  PI_* environment variables" bullet while the environment is stripped. No
  shipped CLI or SDK path sets it false, so this has no user-facing
  reproduction yet; a fix would have to move the bullet onto the tool
  definition. Recorded, not planned.
- `find` checks the search path before resolving `fd`, while `find.ts` resolves
  the tool first. The difference only shows when both the path is missing and
  `fd` is unavailable, and only changes which message wins. Unverified.
- Mutation admission still uses a Go mutex rather than upstream's promise
  FIFO; a supported-boundary ordering reproduction remains needed. The prior
  UI pending-slice retention lead is now CR-010 and repaired.
- HTTP transport malformed URL construction and cancellation while awaiting
  a token merit separate probes. No change to those paths is justified solely
  from inspection. The prior `GetLastAssistantText` whitespace lead is now
  CR-014 and repaired using source-generated differential fixtures.

## Verification

All completed with `GOPROXY=off GOSUMDB=off` on Go commands/recipes:

- Focused red/green tests above: expected red assertions observed before each
  production change; all green after the repairs.
- `go test -count=1 ./mcp/...`: PASS.
- `go test -count=1 ./internal/offloop ./coding ./coding/interactive`: PASS.
- `go test -count=1 ./cmd -run 'Trust|MCP'`: PASS.
- Round 2 focused RPC lifecycle and real-input scheduler tests: expected red
  failures observed, then PASS after minimal fixes. `go test -count=1
  ./coding/interactive ./tui ./coding/rpcclient`: PASS. The four-process-lifecycle
  control set passed ten repetitions.
- Round 3 RPC startup and scheduler retention regressions each passed 20
  repeats after their observed reds. Real HTTP POST-close and JSONL fork
  integrity/unsupported-host tests also failed red, then passed. `go test
  -count=1 ./coding -run '^TestRPC'` and `go test -count=1 ./mcp/...
  ./coding/rpcclient ./coding/interactive`: PASS.
- Round 6: the number-formatting, overflow-detection and key-parsing corpora
  each matched on their first run; `go test -count=1 ./ai ./tui` passes.
- Round 5: bash-executor corpus failed on two multibyte cases before CR-015,
  then passed; the output-accumulator and truncate corpora matched on their
  first run.
  `go test -count=1 ./coding -run 'Bash'` and the full `coding` package pass.
- Round 4 last-assistant-text session/RPC corpus: observed red assertions,
  then PASS with `ai.JSTrim`. All three new corpora regenerate byte-identically.
- `just check`: PASS in all four rounds (formatting, vet, all package tests,
  including non-short).
- `just cross`: PASS in all four rounds for windows/amd64, windows/arm64, darwin/arm64,
  darwin/amd64, linux/arm64 and android/arm64.
- `just reference`: PASS, v1.4.0 (`4a080252f`).
- The three new fixture corpora regenerated into temporary files and compared
  with `cmp`: byte-identical. Only loopback HTTP used by callback tests.
- `git diff --check`: PASS. Reference checkout unchanged.
- Race gate not run locally. CI race verification is still required.

`TODO.md` returns to `No confirmed unresolved findings.` after these verified
repairs. That does not mean all first-party code has been audited. The user's
later instruction authorizes committing completed pieces; it does not
request a push.

## Remaining work and resume point

Continue with the remaining concurrency/platform probes, then the unreviewed
areas identified above, especially search/bash tools, provider request/wire
parsers, session/RPC cancellation, interactive wiring and workers, platform
terminal I/O, and internal parser/rendering libraries.
Mark coverage only for files/functions actually read. Retain this ledger and
add subsequent commands/results; do not turn inspection leads into TODOs
without reproducing them. A complete codebase-wide audit remains unfinished.
