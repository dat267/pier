# 2026-10-27 code review

## CR-056: P2 branch forks discarded bookmarks (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/session-manager.ts`, `createBranchedSession`.
The reference recreates resolved labels for retained targets after filtering
label entries out of the selected path. Pier filtered them out but never
recreated them, so RPC fork/clone lost every bookmark.

Public-manager reproduction printed `before label="bookmark"` and
`after label="" error=<nil>`, while the pinned TypeScript retained the label.
`TestBranchedSessionPreservesBookmarks` failed with `bookmark = "", want
"bookmark"` in both memory and file cases before the repair. It now verifies
recreated IDs, original timestamps, parent chaining and persistence on reopen.
The retained-line migration test now expects the recreated label as the pin
does; its exact retained-message byte assertion is unchanged.

The repair collects resolved labels in JavaScript Map insertion order and
recreates only labels whose targets survive the fork. A differential public
manager probe covered an off-path update, deleting and re-adding a label,
multiple retained targets and an excluded target; Node and Go output matched.

Verification: `go test -count=1 ./coding` passed, including branch, migration
and RPC callers.

## CR-057: P2 fork ancestry pointed at the fork itself (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/session-manager.ts`, `createBranchedSession`.
The reference captures `previousSessionFile` before changing the session path.
Pier instead stored `&m.sessionFile` in the new header, so replacing that field
made the header's `parentSession` point to its own file.

The public-manager reproduction reported `parent_matches_source=false
parent_matches_fork=true`; executing the pinned manager reported the inverse.
`TestBranchedSessionRecordsItsSourceFile` failed on the parent path before the
repair. A local copy of the source path now owns the pointer. The regression
checks the live header and persisted reopen, with the in-memory omission as a
matching control.

Verification: `go test -count=1 ./coding -run
'TestBranchedSession|TestCreateBranchedSession|TestFork|TestRPC.*(Fork|Clone)'`
passed; the original probe now reports `parent_matches_source=true
parent_matches_fork=false`.

## CR-058: P1 compacted forks lost retained messages (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/session-manager.ts`, `createBranchedSession`.
Compaction cuts back up over metadata, so `firstKeptEntryId` can name a label.
When filtering labels from a fork, the reference resolves each removed label
to the next retained entry. Pier stored only unresolved empty placeholders,
leaving a dangling target and discarding kept pre-compaction messages.

A public-manager probe went from two projected messages to one on fork; the
pinned Node control retained two. The regression
`TestBranchedSessionKeepsCompactionWindow` observed the same failure for memory
and file branches, while direct-message and self-target controls passed. The
repair resolves pending label IDs before rewriting each retained compaction.
The test checks both live projection and persisted reopen.

Verification: `go test -count=1 ./coding` passed; the original probe now retains
two messages with the remapped target, matching the pin.

## CR-059: P2 omitted responses made context usage falsely known (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/agent-session.ts`, `getContextUsage`.
After compaction, the reference establishes known context usage only from a
projected assistant after that compaction. Pier checked raw entries instead,
so omitting the only valid post-compaction response still made footer/stats
usage appear known. A public-session probe reported 2 tokens after omission;
executing the pinned method with its actual projection/estimation dependencies
reported null tokens and percent. Both controls reported 105 tokens first.

`TestContextUsageUnknownAfterOmittingPostCompactionResponse` failed on the
unknown-usage assertion before the repair. The branch-position check now uses
usage-bearing projected assistant IDs, as the reference does. The regression
also checks that another projected response restores known usage.

Verification: `go test -count=1 ./coding -run
'TestContextUsage|TestSession.*(Stats|Usage)|TestProjectedContext|TestContextEdit|Test.*Compaction'`
passed. The original public-session probe now reports null tokens/percent after
omission, matching the pinned method. The reference method was extracted
unchanged because directly importing its full class hits an unrelated cached
package's missing `setImageTranscoder` export; no reference files were edited.

## CR-060: P2 branch-summary cancellation missed auth resolution (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/agent-session.ts`, `navigateTree` and
`_getSummarizationRequestAuth`. The reference passes the branch-summary signal
through auth resolution. Pier passed nil overrides instead, so cancellation
and disposal left credential commands or OAuth refresh work alive until their
own timeouts, even though summarization itself was cancelled.

`TestBranchSummaryAbortCancelsAuthResolution` calls the public abort boundary
from a provider auth callback and observes its request context. Before the
repair it failed `AbortBranchSummary did not cancel auth resolution context`;
the normal-summary control passed. Passing the already-owned `summaryCtx` to
`GetAuthForModel` fixes the lost propagation without a new worker or controller.

Verification: `go test -count=1 ./coding -run
'TestBranchSummary|Test.*NavigateTree|TestDisposeCancels|TestCompactionAuth'`
passed. The regression checks cancellation error propagation and that cancelled
auth cannot persist a branch summary.

## CR-061: P2 result-heavy tool chains defeated compaction fallback (fixed)

Authority: D264's existing escalation contract. Upstream pi v1.4.0
(`4a080252`), `core/compaction/compaction.ts`, supplies the preserved prompts
and result rules but intentionally has no escalation. When every boundary
separates a tool call from its result, D264 allows a last-resort weight split.
For a small call and a large final result, the midpoint was the input's end;
returning it yielded no smaller chunks and rejected a recoverable compaction.

`TestCompactionSplitsResultHeavyToolPairAtLastResort` drives public `Compact`
with a synthetic provider that can summarize either message but not both. It
failed with the token-cap error after one request before the repair. The
fallback now clamps to an interior boundary; it keeps the documented policy,
including preferring a boundary outside a tool chain whenever one exists.
The regression verifies whole-input failure followed by two successful chunks,
previous-checkpoint merging and usage accounting for all three requests.

Verification: `go test -count=1 ./coding -run
'TestCompactionSplitsResultHeavy|TestSummarization|TestTurnPrefix|Test.*Compaction'`
passed, including the existing paired/parallel-tool and bounded-retry controls.

## CR-062: P2 branch preparation copied growing slices quadratically (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`core/compaction/branch-summarization.ts`, `prepareBranchEntries`. Its contract
is newest-first admission and chronological output. Pier implemented each
prepend by allocating a new Go slice and copying every previously admitted
message, so 8,000 small messages within the ordinary token budget allocated
540,135,296 bytes and took 253.48 ms locally. At 4,000 messages it allocated
138,825,384 bytes and took 66.21 ms.

`TestBranchPreparationAllocationsStayLinear` uses allocation counters on 512
messages, not timing or benchmark loops. Before the repair it allocated
2,308,352 bytes against a generous 1,048,576-byte ceiling. The repair appends
during the backwards walk and reverses once; budget/important-summary branches
are unchanged. The test also verifies every output message's chronological
position.

Verification: branch/preparation/generation/navigation tests passed with
`-count=30`. The original 8,000-message probe retained the same count, allocated
1,073,128 bytes and took 10.09 ms; 4,000 messages allocated 528,920 bytes and
took 4.96 ms. These are local preparation measurements, not end-to-end summary
latency. Probe source and before/after outputs are in `/tmp/pier-5am-audit/`.

## CR-064: P2 long MCP SSE lines caused quadratic allocation churn (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/mcp/src/transports/streamable-http.ts`, `consumeSseStream`.
One valid SSE `data:` line can approach the ordinary 16 MiB cap. Pier copied
the entire pending line at every decoded read and rescanned its old prefix,
so an 8 MiB payload dispatched one event only after 8,615,139,480 bytes of
allocation traffic and 2.49 seconds locally. These are cumulative allocations,
not retained heap. The 1 MiB and 4 MiB controls used 137,408,568 and
2,160,101,720 allocated bytes respectively.

`TestConsumeSseLongLineAllocationsStayLinear` uses a 256 KiB valid event and
asserts its exact payload plus a generous 3,145,728-byte allocation ceiling.
Before the repair it allocated 9,218,064 bytes. The pending suffix now uses a
builder and scans only newly appended bytes, resetting only after consuming
complete lines. Parsing, metadata, UTF-8, EOF and event limits are unchanged.

Verification: all `TestConsumeSse*` controls passed with `-count=30`, and
`go test -count=2 ./mcp` passed. The original 8 MiB probe dispatched the same
single event without error using 41,716,456 allocated bytes and 21.07 ms;
1 MiB used 5,253,864 bytes/2.89 ms and 4 MiB used 21,121,784 bytes/11.22 ms.
These are local parser measurements, not end-to-end transport latency.

## CR-065: P2 progress-enabled requests swallowed serialization errors (fixed)

Reference: read-only pi v1.4.0 (`4a080252`), `packages/mcp/src/client.ts`
`requestInternal` and HTTP/stdio transport `JSON.stringify(message)`.
Cyclic Go SDK arguments failed serialization without `OnProgress`, but adding
that callback caused the helper to ignore the marshal error and send a request
without params. A server could even return success for that malformed request.

`TestClientRejectsCyclicArgumentsWithProgress` failed only the progress case,
which returned nil error. An offline Node control executed the pinned client
and HTTP transport with an injected fetch; both modes rejected the cycle.
The helper now returns serialization errors and the caller uses its existing
pending-request cleanup path, before sending anything. The regression checks
both modes and verifies that no malformed tool request reaches the server.

Verification: `go test -count=2 ./mcp` passed, including client progress-token
replacement, cancellation, timeouts, and HTTP/stdio controls.

## CR-066: P2 negative zero incorrectly aborted OpenAI-compatible streams (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/ai/src/api/openai-completions.ts` and its pinned OpenAI SDK.
The SDK's error-field check uses JavaScript truthiness. A valid SSE event with
`{"error":-0}` is falsy and does not interrupt a normal response. Pier's
`jsJSONTruthy` parsed positive zero but treated all minus-prefixed numbers as
truthy, ending that stream with error `0` instead of a normal completion.

The source-generated stream corpus grew from 17 to 20 cases: negative zero,
positive-zero control and negative-nonzero control. Before the repair only the
negative-zero case failed, with four events rather than five and a terminal
error instead of done. The existing numeric check now also handles a leading
minus, preserving negative nonzero truthiness.

Verification: `go test -count=1 ./ai` passed; regenerating the 20-case corpus
through the pinned source and SDK produced byte-identical fixture output.

## CR-067: P2 RPC client request timeout did not bound blocked stdin writes (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/modes/rpc/rpc-client.ts`, `send` arms the response
timeout before Node's non-blocking stdin write. In Go the write is a blocking
syscall, and Pier performed it before starting the timeout select, so a child
that stops reading its stdin left the caller blocked past `RequestTimeout`
indefinitely. `DefaultRequestTimeout` documents that it bounds one command
round trip.

`TestClientRequestTimeoutIncludesBlockedStdinWrite` uses a disposable peer that
signals readiness and never reads, with a 256 KiB prompt and a 100 ms timeout.
Before the repair it failed the 2 s hang guard. The same probe hung for 30 s
(exit 124) against the pre-fix build, while post-fix it reports
`blocked_write_timeout=true`. Writes now run on one admitted writer whose slot
wait and response wait both observe the request timer, and a writer that
finishes after a restart is generation-checked so it cannot set `exitErr` or
resolve a pending request belonging to the replacement process. No unbounded
writer queue was added: at most one write is admitted at a time, in FIFO
request order.

Verification: the probe also covers a second request queued behind the blocked
writer (`queued_write_timeout=true`) and a restart whose state call succeeds
(`restart_error=<nil> session="restarted"`). Focused RPC tests passed with
`-count=20`; `go test -count=2 ./coding/rpcclient`, `just check`, `just cross`
(six targets) and `just reference` passed.

## CR-063: P2 stopping the terminal from its resize callback deadlocked (fixed)

Reference: read-only pi v1.4.0 (`4a080252`), `packages/tui/src/terminal.ts`.
`stop()` removes its resize listener and never waits for it, so a host may stop
the terminal from inside its own resize callback. Pier's watcher joined the
goroutine currently executing that callback, so such a host hung. The normal
interactive path only enqueues from the callback and never hit it.

Reproduced with a disposable PTY running a tiny program that stops the terminal
from its `onResize` callback: `outside exit=0`, `callback HANG`, and a SIGQUIT
dump showing `ProcessTerminal.Stop -> stopResizeWatcher` waiting on the active
watcher goroutine. `TestStopInsideResizeCallbackDoesNotDeadlock` covers it in
process and initially failed the same way.

The fix signals the watcher and returns instead of joining it, matching the
reference's listener removal. A resize generation, bumped on every start and
stop, stops a superseded or stopped watcher from delivering another callback,
and the watcher exits at its next select. `TestResizeWatcherRestartDeliversOnce`
replaces a watcher whose callback is still blocked and asserts the replacement
delivers while the superseded one never does again. The Windows poller uses the
same signal-and-generation rule; it is cross-built and vetted but not run here.

Verification: the PTY probe now reports `callback exit=0 output='resize-entered
stopped'`; the two regressions pass with `-count=5`; `go test -count=1 ./tui`
passed; `CGO_ENABLED=0 GOOS=windows go vet ./tui/` passed.

## CR-068: P3 StripJSONComments kept a trailing comma before non-ASCII whitespace (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/utils/json.ts`, `stripJsonComments`. JavaScript's
`\s` includes the Unicode space separators and U+FEFF; RE2's is
`[\t\n\f\r ]`. A trailing comma followed by one of them was stripped by the
pin and kept by Pier.

This is a pure parity defect: a byte-level differential over seven inputs
showed three differing cases, but no user-visible failure was reproduced.
ECMA-404 JSON accepts only ASCII whitespace, so both `JSON.parse` and Go's
decoder reject the leftover character, and `LoadModelConfig` reports a parse
error either way. The finding is recorded at P3 and the repair is limited to
reusing the existing `jsWhitespaceClass` constant, so the exported and
golden-pinned function now matches the reference exactly.

`TestStripJSONCommentsHandlesJavaScriptWhitespace` covers the whole JavaScript
class (ASCII space, tab, vertical tab, form feed, U+00A0, U+1680, U+2000,
U+200A, U+2028, U+2029, U+202F, U+205F, U+3000, U+FEFF) plus a non-whitespace
control. `TestLoadModelConfigRejectsUnicodeWhitespaceAfterTrailingComma` pins
that the loader still rejects such a file, so the change cannot silently widen
what is accepted.

Verification: all seven differential inputs are now byte-identical to the pinned
function; `go test -count=1 ./coding -run 'TestStripJSONComments|TestLoadModelConfig|TestModelConfig'`
passed; `just check`, `just cross` and `just reference` passed.

## CR-069: P3 ported patterns used RE2's \s where the reference uses JavaScript's (fixed)

Reference: read-only pi v1.4.0 (`4a080252`): `cli/auth-command.ts`
`/^Bearer\s+(.+)$/iu`, `api/bedrock-converse-stream.ts`
`id.replace(/[\s_.:]+/g, "-")`, and `mcp/src/transports/streamable-http.ts`
`needsAuthorization`'s `/(?:^|[\s,])error="?insufficient_scope"?/i`. JavaScript's
`\s` includes the Unicode space separators and U+FEFF; RE2's is
`[\t\n\f\r ]`. A credential header, a Bedrock model identifier or a bearer
challenge separated by one of those characters parsed differently here.

Each is a parity defect with no reproduced user-visible failure: real headers,
model identifiers and challenges separate with ASCII whitespace, and the
repository's catalogs contain none of these characters. They are recorded at P3
and fixed by reusing one exported class, `ai.JSWhitespaceClass`, which
`coding/prompttemplates.go` now also builds its prompt-command pattern from, so
there is one definition instead of two.

Red-first evidence: `TestGetAuthCredentialAcceptsJavaScriptWhitespace` failed
for U+00A0, U+3000 and U+FEFF; `TestBedrockCandidateSeparatorsUseJavaScriptWhitespace`
failed for U+00A0 and U+3000; `TestNeedsAuthorizationAcceptsJavaScriptWhitespace`
failed for U+00A0 and U+3000 and passed after the change, and reverting only the
pattern reproduced both failures. ASCII space, tab, comma and non-separator
controls pass in both states.

Verification: `go test -count=1 ./ai ./coding ./mcp` passed; `just check`,
`just cross` (six targets) and `just reference` passed.

Remaining, unverified: `tui/latex.go`, `coding/changelog.go`,
`coding/interactive/exporthtml.go` and `internal/highlight` also use `\s` in
patterns ported from JavaScript, but those sites are display-only, so a
Unicode-space input changes glyphs or link text rather than behavior. They are
left unchanged rather than patched without a reproduced case.

## CR-070: P2 a malformed user theme crashed the session (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`modes/interactive/theme/theme.ts`. `loadThemeFromPath` and the `Theme`
constructor throw on an invalid color or an unknown/circular variable
reference, and every caller wraps them: custom-theme discovery, `setTheme` and
the theme file watcher all catch and skip the theme. Pier's `ResolveThemeColors`
and `NewTheme` panic instead, and the two error-returning entry points
propagated that panic.

Impact: a user theme with a typo (`"accent": "nope"`, `#12345`, `rgb(999,0,0)`,
`"vars": {"a": "b", "b": "a"}`) crashed the process. Opening `/theme` reached it
through custom-theme discovery on the UI loop, and saving such a file reached it
through the watcher goroutine, where an escaping panic always ends the process.

A probe over six malformed documents panicked in all six.
`TestLoadThemeFromPathReportsBrokenThemes` reproduced it and now asserts a
reported message for each, and `TestCustomThemeInfosSkipsBrokenThemes` checks
discovery keeps the good theme and reports the broken one, including loading it
by declared name.

The repair adds an internal `createThemeChecked` that recovers and returns an
error; `LoadThemeFromPath` and `loadTheme` both use it, so the watcher's existing
`err != nil` guard and discovery's warning path now work. `CreateTheme` keeps its
exported signature and documented panic, so no public contract changed.

Verification: `go test -count=1 ./coding/interactive` passed (14.2 s);
`just check`, `just cross` (six targets) and `just reference` passed.

## Coverage and limits

Baseline: clean `main` at `d1aaf68` (v0.0.38), read-only reference `./pi` at
pi v1.4.0 (`4a080252`), confirmed with `just reference`. Gates run before any
change and again at the end: `just check`, `just cross` (six targets),
`just reference`, `go test -count=2 ./...` with a freshly built
`PIER_TEST_BIN`, plus the affected packages at `-count=5`/`-count=20`/
`-count=30` where a finding touched them.

Read in full or in the cited ranges this session: `coding` session manager,
store, projection, message cache, persistence, reload, retry, compaction,
branch summary, trust, trust store, settings storage/manager entry points,
auth storage, `resolveconfigvalue`, `updatecommand`, tools search, bash
executor and process helpers, `mcptools`, `mcpruntime` tool dispatch,
`cmd/main.go` boot half, `cmd/trust.go`, `cmd/rpcmode.go`; `agent/loop.go` and
`agent/agent.go` run/failure paths; `ai/retry.go`, `ai/streamidle.go`,
`ai/sse_lines.go`, `ai/openai_completions_stream.go`, `ai/json.go`,
`ai/google_generative_ai.go` request half, `ai/jstext.go`; `mcp/client.go`,
`mcp/transport_stdio.go`, `mcp/transport_http.go`; `coding/rpcclient`;
`internal/goal/machine.go`, `internal/offloop`; `tui/terminal*.go`,
`tui/stdinbuffer.go`, `tui/text.go`, `tui/altscreen.go` head,
`tui/colors.go`/`oklab.go` via differential; `coding/interactive` theme,
sessionswitch, transcript chat-assembly, tool-execution fallbacks,
event queue and assistant message.

Differential evidence produced (no finding): OKHSL/OKLCH parse and conversion
match the pinned `tui/src/colors.ts` over 10 inputs including out-of-range hue,
missing `%` and padded whitespace; `ai.JSWhitespaceClass` matches JavaScript's
`\s` across every Unicode code point; the 65 generator-backed corpora that can
run offline regenerate byte-identically (64 in the sweep plus `angle-html`
re-run separately); all 15 interactive golden families regenerate
byte-identically; the HTML export escapes adversarial message text and keeps
session data in a base64 `application/json` block, with a scheme allow-list for
links and images; documented RPC lifecycle behaviour (`SIGTERM` → 143,
`SIGHUP` → 129, stdin close → 0, empty-string ids echoed, unknown command
error shape) matches `docs/rpc.md`.

Not verified, and therefore not cleared:

- `internal/markdown/testdata/angle-html` needs `marked` 18.0.11 and
  `marked-cjk-friendly` 0.1.2 installed in a temporary `node_modules`; the
  second package is not in the pinned checkout and installing it is out of
  scope, so that fixture was checked against `marked` alone only by the
  interactive theme/export goldens.
- `internal/highlight` (32k lines of generated language definitions) was not
  reviewed beyond its API surface and the existing golden tests.
- Deep renderer paths in `tui/editor.go`, `tui/latex.go`, `tui/keys.go`,
  `coding/interactive/treeselector.go` and `coding/interactive/settingsselector.go`
  were skimmed, not traced.
- `ai/bedrock_converse.go` and `ai/mistral_conversations.go` were covered by
  their corpora and the whitespace finding only, not by a full re-read.
- `cmd/main.go`'s print-mode half and the model-catalog scripts were not
  re-read this session.
- No race-detector run was possible locally (`CGO_ENABLED=0`); CI remains the
  race gate. This session's new worker code (the RPC writer slot and the
  resize generation) is therefore covered by hand-traced ownership plus
  repeated runs, not by `-race`.
- Untrusted-input fuzzing: the seven pre-existing targets were run for 120 s
  and five more for 300 s (all passed); JSON repair, message codec, session
  entries, MCP messages, stdin, keys and incremental Markdown are covered,
  glob/grapheme/model-config/MCP-config/settings were added. Provider wire
  parsers, the theme loader and the HTML template are not fuzzed.

## Later probes without a finding

- **Session files truncated mid-line.** A session cut inside its last record, or
  followed by a NUL line, loads the intact prefix and skips the partial tail
  (`get_messages` returned the user message only, `success: true`), matching the
  pin's `parseSessionEntryLine`. No crash, no whole-file rejection.
- **`Text.Render` with a negative width** panics in `strings.Repeat`. The pin's
  `" ".repeat(width)` throws `RangeError` for the same input, so this is shared
  behaviour, and every in-tree call site clamps first (`Box` uses
  `max(1, width-paddingX*2)`, `toolframes.go` uses `max(1, …)`). Left unchanged:
  a Go-side clamp would accept input the reference rejects, and no reachable
  call passes a negative width.
- **Image-only user messages render nothing.** `AddMessageToChat` skips a user
  message whose joined text is empty, exactly as `interactive-mode.ts`
  `getUserMessageText` plus its `if (textContent)` guard does. Shared with the
  reference, so not a parity finding.
- **RPC fork reads the session file from disk.** `forkManager` opens the current
  file without first draining an optional `SessionManager` write queue. Not
  reproduced: the interactive app and the CLI RPC host both create managers with
  synchronous writes (`WriteQueue` nil) unless a library caller supplies one, and
  no such caller exists in the tree. Recorded as a lead rather than patched.
