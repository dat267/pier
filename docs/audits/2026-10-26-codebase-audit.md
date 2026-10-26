# Codebase audit, 2026-10-26

Continuation of `2026-10-25-codebase-audit.md`. Reference read: pi v1.4.0
(`4a080252f`), read-only. Baseline `11fa77a`, clean worktree and reference;
`GOPROXY=off GOSUMDB=off just check` and `just reference` pass. User budget:
until 17:00 workspace local time (+07). This remains a partial review, not a
certification of parity, security or race freedom. Inventory and inspection
notes are held outside the repository; this ledger records finding evidence.

## CR-021, P2: Mistral resumed already-ended content blocks (fixed)

Authority: `packages/ai/src/api/mistral-conversations.ts`, `consumeChatStream`,
clears `currentBlock` whenever tool arguments arrive, even for an existing call.
Pier inferred active state from the last persisted content block. If a tool call
started, text/thinking followed, and the call then continued, the content block
ended twice, or subsequent text extended it without a new start. Persisted
`middle` and `after` were merged instead of separate blocks.

Repair: `ai/mistral_conversations.go` tracks `contentOpen` in the existing stream
state. Finishing a block clears it; later text/thinking opens a new block.

Replay:

```sh
go test -count=1 ./ai -run '^TestMistralContinuedToolCallClosesTextBlock$'
```

Observed red: 11 events, no second `text_start`; expected and now observed:
12 events with two distinct text blocks. A source-executed draft corpus also
reproduced duplicate `thinking_end`; straight text, thinking-to-text, empty
content deltas and contiguous tool-call arguments were matching controls. The
corpus is not yet committed while independent findings in it remain unresolved.

Verification: focused test and all `TestMistral` tests pass; `go test -count=1
./ai`, `just check`, `just cross` (all six targets), `just reference` pass.
The CI race gate was not run locally.

## CR-022, P3: Mistral finalized parallel calls in Go map order (fixed)

Authority: the same reference's `toolBlocksByKey.values()` preserves first-seen
order. Pier ranged over a Go map when emitting `toolcall_end`, so parallel calls
ended in nondeterministic order. Finalization now walks persisted content in
first-seen order without introducing another ordering structure.

Replay:

```sh
go test -count=30 ./ai -run '^TestMistralToolCallsEndInFirstSeenOrder$'
```

Observed red order: `call8 call5 call0 call4 call3 call7 call2 call6 call1`;
expected: `call8 call3 call5 call0 call7 call2 call6 call1 call4`. The test uses
nonmonotonic provider indexes, so sorting provider indexes would not pass.
A three-call source-corpus probe also failed across 20 repetitions; contiguous
single-call streaming remains a control.

Verification: focused test passes 30 repetitions; all ai tests, `just check`,
`just cross` (all six targets), `just reference` pass.

## CR-023, P3: Mistral omitted the default object argument delta (fixed)

Authority: `consumeChatStream` in the same pinned reference uses strings
verbatim and `JSON.stringify(arguments || {})` for non-string arguments.
Absent and null arguments therefore emit `{}`. Pier instead emitted an empty
delta, so stream clients observed different tool arguments. The argument delta
now defaults to `{}` while explicitly present string arguments remain verbatim.

Replay:

```sh
go test -count=1 ./ai -run '^TestMistralDefaultsAbsentToolArgumentsToObjectDelta$'
```

Observed red: absent and null arguments each emitted `""`, want `"{}"`.
Empty-string and object argument controls passed before and after.

Verification: focused test, all ai tests, `just check`, `just cross` (six
targets), `just reference` pass. The draft 22-scenario source corpus now matches
in 20 repetitions and regenerates byte-identically. CR-021 and CR-022 corpus
reproductions also pass after their repairs. `TODO.md` has no confirmed
unresolved findings; this does not establish full codebase coverage.

## Mistral stream corpus

`ai/testdata/mistral-stream/generate.mjs` executes the pinned provider, replaying
the same SSE bodies as `TestMistralStreamEventsMatchUpstream`. The 22 scenarios
cover text/thinking switches, empty content, contiguous and parallel tool calls,
continued calls interrupting text/thinking, object/absent/null tool arguments,
usage-only chunks, cached-token bounds, `length`/`model_length`/`error`/unknown
finish reasons, missing finish reasons, empty streams, CRLF and an unterminated
final event. Four scenarios reproduced CR-021/CR-023 on the first run; parallel
call ordering reproduced CR-022 with repeated runs. The remaining scenarios
matched initially. All now match in 20 repetitions.

```sh
go test -count=20 ./ai -run '^TestMistralStreamEventsMatchUpstream$'
node --experimental-strip-types ai/testdata/mistral-stream/generate.mjs ./pi > /tmp/mistral-stream.json
cmp ai/testdata/mistral-stream/golden.json /tmp/mistral-stream.json
```

Regeneration is byte-identical. The projection excludes live `partial`, clocks
and `argumentsError` diagnostics, not a claim about their parity. Request
conversion is covered separately by `testdata/mistral-wire`. Stream cancellation,
network failures and observer errors remain bounded by their existing Go tests,
not source-executed differential cases in this corpus.

Completed pieces: `97dc354` (CR-021), `42db086` (CR-022), `087fdd7` (CR-023),
`5dc51eb` (Mistral stream corpus).

## CR-024, P2: Mistral simple streams ignored reasoning maps and off (fixed)

Authority: `packages/ai/src/api/mistral-conversations.ts`, `streamSimple`, uses
`model.reasoning ? model.thinkingLevelMap : undefined` to choose the effort
path. Disabled/unspecified reasoning uses `effortMap.off`; enabled reasoning
uses the clamped level's mapped value (fallback `high`). Only reasoning models
without maps use `prompt_mode`.

Pier instead used a hardcoded model-id list and omitted off mapping. Built-in
`mistral-large-4` high and `zai-glm-5-3` low sent `prompt_mode: reasoning` instead
of `reasoning_effort: high/low`. `mistral-small-2603` with off/unspecified omitted
`reasoning_effort: none`, so turning reasoning off was not honored in the
request. Custom maps failed by the same root cause.

Repair: select mode by map presence and honor the off mapping. Removed the
obsolete private model-id predicates and their incorrect assertions; public
source-executed cases cover those decisions instead. The mapped-effort/fallback
helper assertions remain.

```sh
go test -count=1 ./ai -run '^TestMistralSimpleReasoningMatchesUpstream$'
node --experimental-strip-types ai/testdata/mistral-simple/generate.mjs ./pi > /tmp/mistral-simple.json
cmp ai/testdata/mistral-simple/golden.json /tmp/mistral-simple.json
```

Six of nine cases failed red: small off/unspecified, large-4 high, GLM-5.3 low,
custom mapped model, and a small-named custom model without a map. Small high
with a map, custom reasoning without a map and nonreasoning were controls.
All now pass. The fixture compares only reasoning mode fields, not other base
options; max-token defaults/context clamping remain a separate inspection lead.

Verification: all `TestMistral` tests and all ai tests pass; regeneration is
byte-identical; `just check`, `just cross` (six targets), `just reference` pass.

## CR-025, P2: simple provider streams bypassed context output ceilings (fixed)

Authority: pi v1.4.0 `api/simple-options.ts`, `buildBaseOptions`, applies
`clampMaxTokensToContext(options.maxTokens ?? model.maxTokens)`. Anthropic,
Bedrock, Google/Vertex, Mistral, Completions, Responses and Azure simple wrappers
spread those base options before reasoning mapping. Raw stream calls do not.
Codex serializes no output cap; pi-messages intentionally bypasses the helper.

Pier's simple wrappers copied request options unchanged. For a 5000-token
context with one estimated input token and model cap 8192, the reference reserves
4096 safety tokens and sends 903; Pier sent 8192/10000 or omitted the cap. This
can produce provider context-limit errors as conversations fill. Bedrock's
budget-thinking path also ignored explicit maxTokens 2000, sending 8192 instead
of the reference's budget-adjusted 4048.

Repair: `withSimpleMaxTokens` returns an options copy with the model-defaulted,
context-clamped ceiling. Only the eight affected simple wrappers use it. The
Anthropic/Bedrock budget paths consume that base ceiling; raw-stream defaults
and caller option objects remain unchanged. No sampling changes were made.

```sh
go test -count=1 ./ai -run '^TestSimpleStreamsClampMaxTokensLikeUpstream$'
node --experimental-strip-types ai/testdata/simple-max-tokens/generate.mjs ./pi > /tmp/simple-max-tokens.json
cmp ai/testdata/simple-max-tokens/golden.json /tmp/simple-max-tokens.json
```

Observed red: 23 of 44 cases differed, including null/8192/10000 instead of 903
and Bedrock 8192 instead of 4048. Small explicit caps, all raw-stream controls,
and existing budget-context clamping were matching controls. All cases now
match. The public OnPayload boundary captures the cap before cancellation, so
no provider HTTP is needed; the source generator throws after the same capture.
The harness's initial panic-based stop exposed a separate Bedrock callback
recovery lead, not part of this repair. A one-token estimate mismatch in the
first draft was a harness input difference (`hello` versus `hi`), corrected
before recording the finding; no estimator change was needed.

Verification: focused corpus and all ai tests pass; fixture regeneration is
byte-identical; `just check`, `just cross` (all six targets), `just reference`
pass. The CI race gate remains unrun locally.

## CR-026, P2: Bedrock callback failures crashed the application (fixed)

Authority: `packages/ai/src/api/bedrock-converse-stream.ts`, `stream`, awaits
OnPayload, OnResponse and OnProviderStreamEvent inside its try/catch. A throwing
payload hook was executed against the pinned source and returned stopReason
`error`, errorMessage `payload hook failed`, without terminating Node.

Pier's Bedrock goroutine lacked the defer/recover used by adjacent adapters.
A panic from any of the three host callbacks therefore terminated the entire
process. Added the equivalent recovery boundary, passing failures through the
existing finalization, cancellation normalization and diagnostic path. Response
body cleanup remains deferred.

```sh
go test -count=5 ./ai -run '^TestBedrockCallbackFailuresStayInStream$'
```

Observed red: isolated child processes exited status 2 with `panic: hook failed`
for payload, response and provider-event callbacks. The local-stream control
exited successfully. After the fix all cases return the expected stream result
and child processes exit successfully. Each child has a test timeout and a
parent cancellation deadline; all credentials are synthetic and HTTP is loopback.

Verification: focused test passes five repetitions; all ai tests, `just check`,
`just cross` (all six targets), `just reference` pass. No local race run.

Completed subsequent pieces: `a3f104a` (CR-024), `60a036b` (CR-025),
`859a82e` (CR-026).

## CR-027, P2: MCP dispatched canceled requests after token acquisition (fixed)

Authority: pi v1.4.0 `packages/mcp/src/transports/streamable-http.ts`,
`authorizedFetch`, checks the sender's signal after shared token acquisition
and before invoking fetch. Shared auth must not be canceled when one sender
abandons a request, but that request must not reach the server afterward.

Pier checked cancellation only after fetch returned. A canceled request could
therefore invoke an injected fetch which ignored its canceled context, even for
side-effecting `tools/call`. Added `ctx.Err()` admission immediately after
headers/token resolution; shared token/recovery ownership is unchanged.

```sh
go test -count=30 ./mcp -run '^TestStreamableHTTPCanceledTokenWaitDoesNotDispatch$'
```

Observed red: canceled case invoked fetch once, want zero. A channel gate holds
the token operation while the caller cancels, then releases it. Noncanceled
control invokes fetch once and succeeds. The pinned-source probe reports
`AbortError` and zero fetch calls when canceled, versus success and one call for
the control. The regression uses an injected fetch only, with no external HTTP.

Verification: focused test passes 30 repetitions; `go test -count=1 ./mcp/...`,
`go test -count=1 ./coding -run 'MCP|Mcp'`, `just check`, `just cross` (six
targets), `just reference` pass. No local race run.

## CR-028, P2: successful MCP auth recovery leaked the rejected body (fixed)

Authority: pi v1.4.0 `packages/mcp/src/transports/streamable-http.ts`,
`authorizedFetch`, brackets OnUnauthorized in try/finally and always awaits
`discard(response)`. Pier discarded only on cancellation or callback error;
when recovery succeeded it retried with the rejected response body still open.
Repeated recoveries could retain HTTP resources and prevent connection reuse.

Repair: scope the callback in a closure with deferred response discard. The
retry cannot dispatch before cleanup, and error/panic unwinding also cleans up.
Removed the redundant error/cancellation-path discards at that call site.

```sh
go test -count=20 ./mcp -run '^TestStreamableHTTPClosesUnauthorizedBodyBeforeRetry$'
```

Observed red: success retried with the body open, and close count was zero
instead of one. Failed-recovery control already closed once. The pinned-source
probe canceled its rejected response body before retry on success and on failure.
All regression cases now close exactly once, with success cleanup before retry.
Only an injected fetch is used; no external HTTP or real tokens.

Verification: focused test passes 20 repetitions; all mcp tests, coding MCP
tests, `just check`, `just cross` (six targets), `just reference` pass.

## CR-029, P2: find ignored an installed fdfind executable (fixed)

Authority: pi v1.4.0 `core/tools/find.ts` uses `ensureTool("fd")`, whose
`utils/tools-manager.ts` lookup tries system names `fd`, then `fdfind`.
Debian/Ubuntu commonly install the latter name alone. Pier looked up only fd,
so find failed despite a supported installed binary.

Repair: fall back to `exec.LookPath("fdfind")` when fd lookup fails. Existing
fd preference and missing-tool diagnostics stay unchanged; no download or
installation behavior was introduced.

```sh
go test -count=1 ./coding -run '^TestFindToolUsesFdfindWhenFDUnavailable$'
```

Observed red: fdfind-only PATH returned `fd is not available and could not be
downloaded`; a both-name control already preferred fd. Both now pass. A source
probe executed actual find/discovery code with a temporary fdfind-only PATH and
PI_OFFLINE=1; it returned the stub's fixture path. Only unused renderer/wrapper
modules were replaced to avoid an uncached Markdown package, not find or tool
lookup. Probe HOME/agent directory were disposable; no dependency was installed.

Verification: find/grep tests, all coding tests, `just check`, `just cross`
(all six targets), `just reference` pass. POSIX test stubs skip on Windows;
production lookup is platform-neutral. No local race run.

## CR-030, P3: goal tool accepted an invisible BOM-only objective (fixed)

Authority: D223's installed goal extension `index.ts`, set_goal.execute, trims
objectives with JavaScript String.trim. Source read SHA-256:
`5b0d421772a0b3c37f6486092acf9301aeaf7bc2aa288574dff47ba1ce3d662e`.
Stock pi v1.4.0 has no goal tool; this is not a claim about that checkout's
source. D234 changes the action interface, not objective normalization. The
existing empty-objective regression establishes the same local invariant.

Pier used Go TrimSpace at the tool boundary. It accepted and armed a goal whose
objective was only U+FEFF, preserved BOM boundaries, and removed U+0085 that JS
preserves. An invisible objective could enter the automatic round loop. Changed
only objective trimming to `ai.JSTrim`; status handling is unchanged.

```sh
go test -count=1 ./coding -run '^TestSessionGoalObjectiveUsesJavaScriptTrim$'
```

Observed red: BOM-only returned success and a persisted active goal, BOM-wrapped
text retained BOM, U+0085-wrapped text lost U+0085. ASCII-space control already
passed. All now pass through a real session's registered tool. The installed
extension's actual callback was separately executed with the same four inputs
and matched the expected outcomes; only unused rendering exports were stubbed.

Verification: all coding goal tests, `go test -count=1 ./internal/goal ./coding`,
`just check`, `just cross` (six targets), `just reference` pass. No local race run.

Completed subsequent pieces: `920f058` (CR-027), `65a50ca` (CR-028),
`2f44ef4` (CR-029).

## CR-031, P2: deferred goal rounds ran after their reservation was stopped (fixed)

Authority: D223's stop/replace semantics and D143's asynchronous ownership. The
installed extension `machine.ts` read has SHA-256
`afcc74bdd5f979d8584cdce12b44e450f683d720b3c3b81a88dc0f0872debba0`.
This is a Go scheduling bug, not a stock-pi parity claim: the extension does not
have the Go host's work queue.

A public host scheduler can defer an idle goal round while the user pauses,
clears, replaces or pauses/resumes the goal. The old closure unconditionally
started its captured prompt later, causing a model turn after stop or an extra
turn for a superseded objective. Interactive wiring can queue this behind other
work while still handling slash commands.

Repair: capture reservation state and recheck goal identity, revision, active
armed phase and pending turn immediately before starting deferred work. Callbacks
remain outside the machine lock; no new queue or cancellation policy was added.

```sh
go test -count=20 ./coding -run '^TestGoalScheduledRoundSkipsInvalidatedReservation$'
```

Observed red through a real session and its public scheduler hook: clear/pause
started one model run (want zero), replacement and pause/resume started two
(want one). An unchanged reservation was a passing control. All now match.
The revision check matters for pause/resume of the same goal and turn number.

Verification: focused test passes 20 repetitions; internal/goal, coding and
coding/interactive tests pass; `just check`, `just cross` (six targets),
`just reference` pass. No local race run.
