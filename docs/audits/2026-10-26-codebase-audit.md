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

## CR-032, P3: idle goal turns started before tool exposure synchronized (fixed)

Authority: D223's active-goal tool policy, `internal/goal/tools.go`
DesiredToolNames and the existing pursued-goal prompt test. The installed
extension's `index.ts` apply/syncGoalTools source is identified by the CR-030
digest. Its nonblocking message delivery permits synchronization before provider
dispatch; Go's idle delivery can synchronously await an entire turn.

`/goal set` on an idle SDK session therefore dispatched its first model request
without `get_goal`, despite an active goal and prompt rules naming that tool.
`Apply` synchronized tools only after delivering the trigger-turn effect.

Repair: flush pending tool synchronization before a trigger-turn message starts
work. Remaining mutations retain the final synchronization. Durable entry order
and callback/lock ownership are unchanged.

```sh
go test -count=1 ./coding -run '^TestIdleGoalRoundReceivesActiveGoalTools$'
```

Observed red: idle-command first request lacked `get_goal`; a tool-created goal
followed by Prompt was a passing control. Both now expose it. The test inspects
actual transcript tool declarations at provider dispatch, not a private flag.

Verification: focused test and CR-031 scheduled-reservation controls pass;
internal/goal, coding and interactive tests pass; `just check`, `just cross`
(six targets), `just reference` pass. No local race run.

## CR-033, P2: concurrent goal tools corrupted durable replay ordering (fixed)

Authority: `internal/goal/state.go` ApplyChange/FoldGoal require ordered lifecycle
entries, D223's goal integration and D143's Go serialization responsibility.
The installed extension's synchronous dispatch/apply is identified by the
CR-031 machine digest; stock pi v1.4.0 has no goal tool.

Default parallel tool execution can call set_goal concurrently. The controller
locked Dispatch, unlocked, then persisted effects through Apply. Another
transition could therefore persist first, or interleave inside a clear/create
pair. Every call could succeed while the next restore discarded the accepted
goal as corrupt.

Repair: `dispatchLocked` commits append effects in the same critical section as
machine mutation. Its private session sink only writes records; interactive
disk I/O remains on the existing write queue. Remaining effects and tool
synchronization run after unlocking, preserving callback reentrancy. Start's
branch read and replay still share the state lock. Public Apply behavior is
unchanged. No new mutex or worker was introduced; lock ordering is recorded in
`docs/locks.md`.

```sh
go test -count=30 ./coding -run '^TestConcurrentGoalToolsPreserveReplayableTransitions$'
go test -count=20 ./coding -run '^TestGoalTransitionNotificationCanReadState$'
```

Observed red in all 20 exploratory repetitions: a real session's public tool,
called by 16 barrier-started workers, accepted writes but replay reported
`Goal state corrupt, ignoring: clear of unknown goal` and lost the goal.
Sequential calls were a passing control. Both now replay to the accepted goal;
30 focused repetitions pass, and a companion callback test reenters Snapshot
from the reload notice without deadlocking. No sleeps or private lock gates
control the concurrent calls.

Verification: all coding goal tests, internal/goal, coding and interactive tests,
`just check`, `just cross` (six targets), `just reference` pass. No local race run.

Completed subsequent pieces: `fdd90a9` (CR-030), `809e4c8` (CR-031),
`a5a2bd4` (CR-032).

## CR-034, P2: session disposal left compaction and branch summaries alive (fixed)

Authority: pi v1.4.0 `core/agent-session.ts`, dispose, aborts compaction and
branch summarization along with retry/bash/agent work. Pier's Dispose repeated
only retry/bash/agent cancellation, leaving the independent summary controllers
active. A summary could therefore continue and persist changes after session
replacement or teardown.

Repair: reuse the existing nonblocking `AbortAsync`, which includes the
summarization controllers and session run context, instead of duplicating an
incomplete subset. Listener/resource cleanup and nonblocking teardown remain.

```sh
go test -count=1 ./coding -run '^TestDisposeCancelsActive(Compaction|BranchSummary)$'
```

Observed red: disposal from compaction_start allowed manual and automatic
compaction to finish successfully; disposal at branch-summary provider dispatch
returned `Cancelled:false Aborted:false` with a persisted summary entry. All
now cancel and persist neither compaction nor branch-summary results. Existing
non-disposed summarization successes remain controls.

Verification: focused regressions; compaction, navigation and goal tests;
all coding and interactive tests; `just check`, `just cross` (six targets),
`just reference` pass. Cache-warmer teardown remains a separate inspection lead
at this checkpoint. No local race run.

## CR-035, P2: session disposal left paid cache warming scheduled (fixed)

Authority: pi v1.4.0 `core/agent-session.ts`, dispose, clears the warmer's
onWarmed callback and calls cancel. AbortAsync does not own this independent
background run. Pier did neither, so disposed/replaced sessions retained timers,
paid refreshes, usage writes and their old session callback.

Repair: a private warmer disposal operation clears its run/timer/context and
callback under the existing lock. Session Dispose calls it. Refresh workers
snapshot OnWarmed under that lock and invoke it after unlocking, so teardown
introduces no callback read/write race or callback self-join. An already-admitted
callback may finish, matching the port's snapshot-delivery ownership rule.

```sh
go test -count=20 ./coding -run '^TestDisposeStopsSessionCacheWarming$'
```

Observed red with testing/synctest: disposed warmer still reported scheduled,
made one refresh and one notification after the virtual clock advanced (want
zero), and retained its callback. A live-session control made one refresh and
one notification. All now match. The clock and stream are synthetic; no real
sleep, network call or credential is involved.

Verification: focused test passes 20 repetitions; cache-warmer, SDK construction
and disposal tests pass; `just check`, `just cross` (six targets), `just reference`
pass. No local race run.

Completed subsequent pieces: `710aac0` (CR-033), `028989e` (CR-034).

## CR-036, P2: cache warming accepted a replaced message prefix (fixed)

Authority: pi v1.4.0 `core/sdk.ts`, cacheContextIsCurrent, checks the selected
model, prefix length and every captured message by object identity. Pier's SDK
path compared persisted context counts, and its direct-session helper compared
only live model/length. Both could keep warming an obsolete request after a
same-length replacement.

Repair: `Agent.MessagePrefixIsCurrent` captures the agent-owned read-only prefix
and compares message references under the existing agent mutex. SetMessages
replaces the backing slice, while appends preserve existing elements. The check
performs no session projection, entry-value copying or JSON decoding. SDK and
direct-session warming use this predicate; the count-only SDK helper is removed.
The no-projection allocation guard still runs against the actual currency path.

```sh
go test -count=1 ./coding -run '^TestSDKCacheWarmerRejectsReplacedMessagePrefix$|^TestCacheContextIsCurrentDoesNotProjectTheSession$'
go test -count=1 ./agent -run '^TestMessagePrefixCurrencyMatchesUpstream$'
```

Observed red through CreateAgentSession and public Agent.SetMessages: replacing
an earlier message in an owned slice left status scheduled, want
inactive/conversation context changed. Prefix-preserving append was a passing
control. The first harness draft mutated the live getter's backing slice; that
also mutates the reference's captured array and is not this scenario. It was
corrected to copy the slice, and the corrected test was observed red against
the unchanged implementation before reapplying the repair.

The reference predicate itself was extracted unchanged from the pinned SDK
module, type-erased with Node's stripTypeScriptTypes, and executed for append,
replacement, restoration, truncation and model change. Agent controls match all
five, plus replacement of an unwarmed tail. Restoration of original references
remains current, which an epoch-only approximation would incorrectly reject.

Verification: focused regressions and allocation guard; all agent, coding and
interactive tests; `just check`, `just cross` (six targets), `just reference`
pass. No local race run.

## CR-037, P2: cache warming omitted the late-dispatch deadline (fixed)

Authority: pi v1.4.0 `core/cache-warmer.ts`, schedule/refresh, sets
`refreshDeadlineAt = nextWarmAt + floor((ttlMs - delayMs) / 2)` and checks it
before policy evaluation and again before request dispatch. Timer/policy delays
past that margin risk a full-price cache write rather than the expected read.
Pier retained only the fixed one-hour/30-minute horizons and omitted this guard.

Repair: keep TTL/deadline on the existing run, compute the same deadline on each
schedule and check it at both dispatch points. The existing lock ensures an
expired old run cannot stop a replacement run through the new guard. No new
timer, worker or arbitrary threshold was added.

```sh
go test -count=20 ./coding -run '^TestCacheWarmerSkipsRefreshPastDispatchDeadline$'
```

Observed red with a virtual 11-second TTL: the planned refresh is at 1 second
and the dispatch deadline is at 6 seconds. Validation delayed to 7 seconds
still evaluated policy and sent a request; a decision delayed to 7 seconds also
sent a request. Both should send none. On-time and exactly-6-second controls
already passed. All now match. The pinned CacheWarmer was also executed with
fake timers/Date.now: late timer and decision stopped with
`cache refresh deadline missed`, zero requests, while the exact deadline allowed
one request.

Verification: focused test passes 20 repetitions; cache-warmer, SDK and disposal
tests pass; `just check`, `just cross` (six targets), `just reference` pass.
No real sleep/network or local race run.

## CR-038, P2: cache-warmer callback failures crashed the application (fixed)

Authority: pi v1.4.0 `core/cache-warmer.ts`, refresh, catches decision failures
and falls back to pi's own economics, then catches stream/persistence/notification
failures as best-effort and reschedules. The Go `Decide` field documents the same
fallback. Go callbacks signal failure with a panic, which escaped the timer
goroutine and terminated the whole process.

Repair: two narrow recover scopes mirror the reference catch regions. A failed
policy keeps the economic action; a failed refresh or notification reaches the
shared current-run reschedule. Currency checks, mode lookup and the existing
callback/lock ownership are untouched.

```sh
go test -count=10 ./coding -run '^TestCacheWarmerCallbackFailuresAreBestEffort$'
```

Observed red in isolated child processes: decision-warm, decision-stop, stream
and notification modes exited status 2 with their callback panic. The normal
control passed. All now preserve the process: the failed policy warms or stops
by economics, and refresh/notification failures leave the run scheduled. The
pinned warmer executed with fake timers produced the same four outcomes. Virtual
timers and synthetic streams mean no real sleep, provider request or credential.

Verification: focused test passes 10 repetitions; cache-warmer, SDK, disposal,
coding and agent tests pass; `just check`, `just cross` (six targets),
`just reference` pass. No local race run.

## CR-039, P2: a stale cache-warming run could cancel its replacement (fixed)

Authority: pi v1.4.0 `core/cache-warmer.ts`, validateRun/stop. The reference
checks `this.run !== run` and calls stop with no await between them, so a stale
run can never stop its replacement. In Go the currency and mode callbacks run
outside the warmer lock, so the check and the stop were separate steps.

`validateRun` locked, saw itself current, then called `run.isCurrent()` outside
the lock. A session request could start a replacement run in that window; the
stale callback's false result then called `stop`, which cleared whatever run was
current and reported "conversation context changed". Warming stopped for the new
request, the deadlock-free equivalent of losing a race the reference cannot lose.
The overdue-age stop and timer installation had the same shape.

Repair: `stopRun` clears the current run only while the caller's run is still
current; `validateRun`, the policy stop and the age limit use it. A timer
installed after replacement is stopped instead of attached to the stale run.
Cancellation, mode changes and disposal still clear the current run as before.

```sh
go test -count=20 ./coding -run '^TestCacheWarmerStaleRunCannotStopReplacement$'
```

Reproduced with a synctest bubble: the stale run's currency callback blocks
inside the refresh, a public `Start` installs the replacement, then the stale
callback returns false. Before the fix the status became
`inactive/conversation context changed` with the replacement's timer discarded;
after it the replacement stays `scheduled`. No sleeps beyond virtual time, and
no real provider or network traffic.

Verification: 20 repetitions of the focused test; all `TestCacheWarmer*`, the
cache-warmer/SDK/disposal set, and the `coding`, `agent` and `coding/interactive`
packages pass; `just check`, `just cross` (six targets), `just reference` pass.
No local race run. Sequence note: this finding's reproduction and repair were
done before its tracker entry was written; the evidence above is the durable
record, and `TODO.md` stays at "no confirmed unresolved findings".

## CR-040, P3: a malformed MCP HTTP URL panicked on first send (fixed)

Authority: pi v1.4.0 `packages/mcp/src/transports/streamable-http.ts` parses the
URL in its constructor (`this.url = new URL(options.url)`), so a malformed value
is a caller-visible error before any request. The Go constructor cannot return
an error, and its own comment already assigned validation to `Start`.

Pier discarded the parse error, kept a nil `parsedURL`, accepted `Start`, and
then dereferenced the nil URL in `Send`. CLI config validation hid this, but the
exported transport is public API: one malformed string crashed the process
instead of returning an error.

Repair: retain the constructor's parse failure and return it from `Start`, ahead
of the started/closed checks. `Send` already requires a started transport, so a
rejected start now yields the closed-transport error rather than a panic. No URL
rewriting or new validation rules were added.

```sh
go test -count=1 ./mcp -run '^TestStreamableHTTPRejectsMalformedURLAtStart$'
```

Observed red: `Start` returned nil for `http://[::1`, and a probe then logged
`send panic = runtime error: invalid memory address or nil pointer dereference`.
The regression now sees a Start error and no panic. Loopback/HTTP fixtures only.

Verification: `go test -count=1 ./mcp/...`, the coding MCP tests, `just check`,
`just cross` (six targets), `just reference` pass. No local race run.

## CR-041, P2: pi-messages callback failures terminated the process (fixed)

Authority: pi v1.4.0 `packages/ai/src/api/pi-messages.ts` awaits onPayload,
onResponse and onProviderStreamEvent inside the stream's try/catch, so a host
callback failure fails that stream only. Every other Go adapter already recovers
at this goroutine boundary (CR-026 added the Bedrock one).

Repair: the pi-messages provider goroutine recovers error and non-error panic
values through its existing `fail` / `createPiMessagesErrorEvent` path, the same
shape as the adjacent adapters. Wire parsing, cancellation and callback
contracts are untouched.

```sh
go test -count=5 ./ai -run '^TestPiMessagesCallbackFailuresStayInStream$'
```

Observed red in isolated child processes: payload, response and event modes
exited status 2 with `panic: hook failed`. The first fixture produced an invalid
pi-messages body, so its control also failed; it was corrected to a
`text_delta` plus `done` SSE stream, and the control then passed while the three
callback modes still crashed. All now return a stream error whose message is the
callback's, and the control completes normally. Loopback HTTP and synthetic
credentials only.

Verification: focused test passes 5 repetitions; all ai tests, `just check`,
`just cross` (six targets), `just reference` pass. No local race run.

## CLI argument corpus

`coding/testdata/cli-args/generate.mjs` executes the pinned `parseArgs` for 93
argv vectors and records a projection of the parsed `Args`. The projection is
the parsed contract: values, booleans, list contents, unknown extension flags,
messages/file arguments, and diagnostics in order. `TestParseArgsMatchesUpstream`
compares it with `coding.ParseArgs`.

```sh
node --experimental-strip-types coding/testdata/cli-args/generate.mjs ./pi > coding/testdata/cli-args/golden.json
go test -count=1 ./coding -run '^TestParseArgsMatchesUpstream$'
```

All vectors matched on the first run, so this records coverage rather than a
finding. The first 68 covered the flag families; 25 more add `--list-models`
(with and without a provider), empty list values, `=`-joined value flags,
missing values, repeated flags, ordering between `--no-approve`/`--approve` and
`--print`/`--`, repeated `--tools`, and `--export`/`--use-theme`/`--fork`
without values. Harness normalizations were needed, none of them product
defects: Go nil slices become the reference's empty arrays, an unset mode
becomes null rather than the empty string, and the port's split
`ListModelsAll`/`ListModels` fields project back to the reference's
`string | true`. Regeneration is byte-identical; `go test -count=1 ./coding`,
`just check`, `just cross` (six targets) and `just reference` pass.

## CR-042, P3: find prechecked the search path before resolving fd (fixed)

Authority: pi v1.4.0 `core/tools/find.ts` calls `ensureTool("fd")` before
spawning and never stats the search path in the default branch; only its custom
operations branch checks existence. Pier added a Go-only `os.Stat` first, so a
missing path reported `Path not found: ...` even when fd was unavailable, and
masked fd's own diagnostics when it was installed.

Repair: remove the precheck. The existing unavailable-tool error and fd's
trimmed stderr now carry the failure, as in the reference. The `fdfind`
fallback, abort checks and exit-code handling are unchanged.

```sh
go test -count=1 ./coding -run '^TestFindToolDefersMissingPathToFD$'
```

Executing the pinned find definition with PI_OFFLINE=1 and a disposable
HOME/cwd printed `fd is not available and could not be downloaded` for a missing
path with no fd on PATH, and fd's `[fd error]: Search path ... is not a
directory.` with fd installed. The Go regression observed `Path not found: ...`
in both cases before the fix and now matches. POSIX-only test; the installed
case skips when fd is absent, and no network is used.

Verification: find/grep tests, all coding tests, `just check`, `just cross`
(six targets), `just reference` pass. The fixture regeneration corpus is
unaffected. No local race run.

## Other coverage this round

- `coding/updatecommand.go` and release resolution: L/C/S reread against the
  documented D174/D175 port-specific updater. Checksum-before-replace, temp file
  placement next to the target, the Windows quarantine fallback, redirect
  handling and the missing-asset/404 paths were traced again; no reproducible
  defect and no signature-verification claim.
- `coding/session_projection.go`, `coding/session_messagecache.go` and the
  path/window/settings helpers in `coding/session_store.go`: compared with the
  pinned `buildSessionPath`/`buildContextEntries`/`buildSessionProjection`. The
  compaction window, the older-compaction suppression at index > 0, context-edit
  application, and forward vs reverse settings precedence agree. A cache-reset
  poisoning hypothesis was not reachable through supported APIs (file open builds
  a new manager; branched sessions keep copied values), so it stays a lead, not a
  finding.
- `ContextSignature` may over- or under-count checkpoint system messages and
  context edits; it no longer drives cache warming after CR-036 and no supported
  consumer failure was reproduced, so it is recorded as a lead only.
- `coding/tools_fs.go` write/ls and `coding/cliargs.go` were reread. `ls` order
  was compared against the pinned `ls.ts` sort for 16 adversarial names
  (punctuation, digits, case, `ä`, spaces): root collation matched JS
  `localeCompare` exactly, including `10` before `9` and `a` before `ä`.
- `ai.JSLower` was compared with JS `toLowerCase` for Turkish dotted I, capital
  sharp S, final sigma in context, titlecase digraphs, long s, Kelvin sign and
  Roman numerals: identical.
- Local race runs (informational, CI remains the gate): `go test -race` passes for
  `./ai`, `./agent`, `./cmd`, `./coding`, `./coding/interactive`, `./tui`,
  `./mcp/...` and `./internal/...`.
- Fixture regeneration stays byte-identical for google-stream, mistral-stream,
  mistral-simple, simple-max-tokens, cli-args, edit-diff-patch and edit-apply.

Coverage remains partial: interactive wiring and renderers, the full settings
manager, compaction cut selection (D264), MCP OAuth recovery and platform terminal
I/O were not exhaustively re-probed this round. Passing tests and static review
cannot certify the absence of defects.

## Corpus inventory

Source-executed corpora added during this session, all regenerated byte-identically
at the closing checkpoint:

| Corpus | Cases | Module executed from the pin |
|---|---|---|
| `ai/testdata/google-stream` | 12 | `api/google-generative-ai.ts` |
| `ai/testdata/mistral-stream` | 22 | `api/mistral-conversations.ts` |
| `ai/testdata/mistral-simple` | 9 | `api/mistral-conversations.ts` streamSimple |
| `ai/testdata/simple-max-tokens` | 44 | `api/simple-options.ts` + eight providers |
| `coding/testdata/cli-args` | 93 | `cli/args.ts` |
| `coding/testdata/semver` | 30 + 16 | the checkout's `semver` dependency |
| `coding/testdata/model-resolve` | 26 | `core/model-resolver.ts` |
| `coding/testdata/serialize-conversation` | 16 | `core/compaction/utils.ts` |
| `coding/testdata/file-ops` | 11 | `core/compaction/utils.ts` |
| `coding/testdata/estimate` | 14 | `core/compaction/compaction.ts` |
| `coding/testdata/cut-point` | 19 | `core/compaction/compaction.ts` |
| `coding/testdata/convert-to-llm` | 11 | `core/messages.ts` (D248 excluded) |
| `ai/testdata/system-prompt` | 19 | `ai/src/utils/{text,transcript}.ts` |
| `ai/testdata/transcript-tools` | 30 | `ai/src/utils/transcript.ts` |
| `ai/testdata/collapse-system` | 15 | `ai/src/utils/transcript.ts` |
| `coding/testdata/read-tool` | 16 | `core/tools/read.ts` (D279 excluded) |
| `coding/testdata/edit-tool` | 11 | `core/tools/edit.ts` |
| `coding/testdata/ls-tool` | 8 | `core/tools/ls.ts` |
| `coding/testdata/write-tool` | 7 | `core/tools/write.ts` (D279 excluded) |
| `coding/testdata/grep-tool` | 12 | `core/tools/grep.ts` |
| `coding/testdata/find-tool` | 7 | `core/tools/find.ts` |
| `coding/testdata/image-mime` | 26 | `utils/mime.ts` |
| `ai/testdata/short-hash` | 18 | `ai/src/utils/hash.ts` |
| `ai/testdata/mistral-wire` | 11 | `ai/src/api/mistral-conversations.ts` |
| `tui/testdata/wrap` | 204 | `tui/src/utils.ts` |
| `tui/testdata/truncate` | 206 | `tui/src/utils.ts` |

Earlier corpora (`edit-diff-patch`, `edit-apply`, `truncate`, `js-numbers`,
`overflow`, `parse-key`, Anthropic/OpenAI/Responses stream corpora) remain green.

## Leads, not findings

These were inspected or hypothesised this round and deliberately left out of
`TODO.md` because no supported reproduction exists yet:

- `SessionManager.ContextSignature` can over-count checkpoint system messages or
  under-count messages removed by context edits. After CR-036 no cache-warming
  path consumes it; remaining users (`cacheContextIsCurrent` was removed) are the
  status/estimation surfaces, and no user-visible failure was reproduced.
- The projection cache is keyed by leaf id plus `len(byID)`. A reopened manager
  could reuse an entry id at the same count, but every supported open path builds
  a new manager and `buildIndex` resets the id map, so the scenario needs an
  API that does not exist. Recorded, not planned.
- `find`/`grep` kill the direct child on abort; a grandchild shell can survive,
  matching the reference's single-child kill. No divergence is claimed.
- Compaction cut selection, the interactive renderer/layout and the full settings
  manager were not re-probed against the pin this round; D264's deliberate
  splitting/retry changes were not revisited.

## Model-resolution corpus

`coding/testdata/model-resolve/generate.mjs` executes
`resolveModelReference` from the pinned `core/model-resolver.ts` over a synthetic
ten-model pool and 26 requests: bare and `provider/id` references, case-insensitive
provider prefixes, gateway ids containing `/` and `:`, `:high`/`:off`/invalid
thinking suffixes in `lenient`, `strict` and `none` modes, a provider constraint,
fuzzy substrings and typos, empty/whitespace text, and an unusable-provider
tie-break. The projection covers the resolved model, thinking level, warning, and
the not-found custom-target shape (provider, provider models in order, model text
and id).

```sh
node --experimental-strip-types coding/testdata/model-resolve/generate.mjs ./pi > coding/testdata/model-resolve/golden.json
go test -count=1 ./coding -run '^TestResolveModelReferenceMatchesUpstream$'
```

All 26 cases matched on the first run, so this records coverage rather than a
finding; the documented locale-collation and minimatch deviations (D257/D262)
apply to scoped-model patterns and ordering, not this reference resolver.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Package-version corpus

`coding/testdata/semver/generate.mjs` loads the pinned checkout's `semver`
dependency, the module `utils/version-check.ts` calls, and records `valid` for
30 inputs plus `compare` for 16 pairs. `TestPackageVersionComparisonMatchesSemver`
compares them with `ComparePackageVersions`.

```sh
node coding/testdata/semver/generate.mjs ./pi > coding/testdata/semver/golden.json
go test -count=1 ./coding -run '^TestPackageVersionComparisonMatchesSemver$'
```

Cases cover `v`/`=` prefixes, leading zeros, missing components, build metadata,
prerelease leading zeros, empty identifiers, `_` in identifiers, numeric versus
alphanumeric prerelease ordering, numeric identifiers compared by value
(`2 < 10`) rather than length, prerelease-versus-release ordering, build metadata
ignored in precedence, and 20-digit numeric identifiers that must not lose
precision. All matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Summary-serialization corpus

`coding/testdata/serialize-conversation/generate.mjs` executes the pinned
`core/compaction/utils.ts` `serializeConversation` for 16 LLM-message sets;
`TestSerializeConversationMatchesUpstream` decodes the same JSON with
`ai.UnmarshalMessage` and compares the serialized text byte for byte.

Cases: user strings, user blocks mixing text and an image, empty user blocks,
CRLF text, text-only and thinking-plus-text assistants, tool calls with nested
arguments in insertion order and `<`/`&` characters, call-only and multi-call
assistants, empty assistant content, short/empty/image-only tool results, a
2500-character ASCII result and 1500 astral emoji (the 2000-UTF-16-unit
truncation boundary and its counted suffix), and a mixed conversation.

All 16 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## File-operation corpus

`coding/testdata/file-ops/generate.mjs` executes the pinned
`extractFileOpsFromMessage`/`computeFileLists`/`formatFileOperations` for 11
message sets; `TestFileOperationsMatchUpstream` decodes the same JSON and
compares both lists and the rendered XML.

Cases: read-only files, written and edited files, a read path later edited (it
moves to the modified list), duplicates, calls with a missing or non-string
`path`, an unrelated tool name, codemode `nestedCalls` recorded on a tool result,
a mixed conversation, and `😀.ts`/`ｚ.ts`/`Z.ts` together, which pins
`Array.prototype.sort`'s UTF-16 order rather than rune order.

All 11 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Context-estimate corpus

`coding/testdata/estimate/generate.mjs` executes the pinned
`estimateContextTokens` for 14 message sets; `TestEstimateContextTokensMatchesUpstream`
compares tokens, usage tokens, trailing tokens and the last-usage index.

Cases: user strings and astral text (UTF-16 length), user text plus an image, a
system message with sections and `toolsAdded`, assistant text/thinking/tool-call
blocks, a tool result with text and an image, a custom message, usage taken from
`totalTokens`, usage summed from components, usage ignored for aborted and error
responses, zero usage, and trailing messages after the last valid usage.

All 14 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Cut-point corpus

`coding/testdata/cut-point/generate.mjs` executes the pinned `findCutPoint` for
12 synthetic entry lists and 19 windows/budgets; `TestFindCutPointMatchesUpstream`
compares the first kept index, split-turn start and split-turn flag.

Scenarios: a single short message, user/assistant pairs at two budgets, three
pairs across four budgets, a tool call with an oversized result, adjacent label
and thinking-level entries that must be absorbed into the kept range, a system
message between messages, an existing compaction boundary, branch-summary and
custom-message entries, a context edit, windows starting inside the list, an
empty window, and a mid-turn cut.

All 19 matched on the first run. This covers cut selection only; D264's
deliberate summarization splitting and retry behavior is unchanged and not
re-probed here. Regeneration is byte-identical; `go test -count=1 ./coding`,
`just check`, `just cross` (six targets) and `just reference` pass.

## Convert-to-LLM corpus

`coding/testdata/convert-to-llm/generate.mjs` executes the pinned
`core/messages.ts` `convertToLlm`; `TestConvertToLlmMatchesUpstream` decodes the
same JSON and compares canonical message arrays (each message marshalled with its
`role` member).

Cases: system/user/assistant/tool-result pass-through, `bashExecution` with and
without output, the `!!` context exclusion, a cancelled and a truncated command,
custom notes as a string and as text-plus-image blocks, branch and compaction
summary wrappers, a mixed sequence and an empty input.

D248 deliberately reorders a custom message that lands between an unanswered tool
call and its result, so those inputs are excluded; the corpus pins the shared
behavior around that one documented divergence. All 11 matched once the harness
was corrected to include the `role` member (Go's message marshal adds it per
message rather than in the array encoding). Regeneration is byte-identical;
`go test -count=1 ./coding`, `just check`, `just cross` (six targets) and
`just reference` pass.

## System-prompt corpus

`ai/testdata/system-prompt/generate.mjs` executes the pinned `utils/text.ts` and
`utils/transcript.ts` helpers; `TestSystemPromptHelpersMatchUpstream` compares
rendered text, update framing, the replayed system message (marshalled with its
`role`), the current prompt and the resolved tools.

Cases: content strings and text/image blocks, sections rendered, a null section
omitted from the prompt but framed as removed in an update, empty content and
empty sections, appended system messages, section patches and removals across
messages, tools added then replaced/removed, tools-only and no-system inputs, and
a non-leading system message.

All 19 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./ai`, `just check`,
`just cross` (six targets) and `just reference` pass.

## CR-043, P3: in-place tool support returned null instead of an empty list (fixed)

Authority: pi v1.4.0 `ai/src/utils/transcript.ts` `resolveTranscriptTools`
returns `{ requestTools: [], foldedSystemMessages: 0 }` for `"changes"`. The
other branches already produce an empty array, which the port reproduced.

Pier returned the zero value for that branch, so the exported `RequestTools`
slice encoded as `null` rather than `[]`. Request builders that test length were
unaffected, which is why no user-visible failure showed up, but a caller that
persists, compares or forwards the value sees different bytes.

Repair: the branch returns an empty non-nil slice. The 30-case corpus
(`ai/testdata/transcript-tools`) executes the pinned resolver over nine system
message sequences in all three support modes and is now green; the `none` and
`additions` cases were already matching.

```sh
go test -count=1 ./ai -run '^TestResolveTranscriptToolsMatchesUpstream$'
```

Verification: focused corpus, `ai`/`agent`/`coding`/`coding/interactive` suites,
`just check`, `just cross` (six targets), `just reference`; regeneration is
byte-identical. No local race run.

## System-collapse corpus

`ai/testdata/collapse-system/generate.mjs` executes the pinned
`collapseSystemMessages` and `resolveTranscript` for five message sequences in
collapse and both resolve modes (15 cases). Covered: an empty sequence, no
system message, a leading system message carrying tools, a merged
mid-conversation system message, and systems-only section patches.

The corpus compares structure rather than raw bytes for pass-through members:
those keep the harness's input key order, while message byte order is pinned by
the session round-trip tests. All 15 matched. A remaining representation note,
not a finding: `CollapseSystemMessages` returns a nil message slice for an empty
input where the reference returns `[]`; no supported consumer observes it, and it
is recorded as a lead only.

Regeneration is byte-identical; `go test -count=1 ./ai`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Complete fix list for this session

Every fix below was reproduced through a supported boundary first, then verified
with its focused regression, `just check`, `just cross` (six targets),
`just reference`, and byte-identical corpus regeneration where a corpus applies.

| Finding | Commit | Scope |
|---|---|---|
| CR-021 | `97dc354` | Mistral: close content blocks once on continued tool calls |
| CR-022 | `42db086` | Mistral: finalize parallel tool calls in first-seen order |
| CR-023 | `087fdd7` | Mistral: default absent tool arguments to `{}` |
| CR-024 | `a3f104a` | Mistral simple: honour reasoning maps including `off` |
| CR-025 | `60a036b` | Simple streams: context-aware output ceilings (8 providers) |
| CR-026 | `859a82e` | Bedrock: contain host callback failures |
| CR-027 | `920f058` | MCP HTTP: reject canceled requests after token acquisition |
| CR-028 | `65a50ca` | MCP HTTP: discard unauthorized responses before retry |
| CR-029 | `2f44ef4` | find: use `fdfind` when `fd` is unavailable |
| CR-030 | `fdd90a9` | Goal tool: JavaScript trim for objectives |
| CR-031 | `809e4c8` | Goal: invalidate stopped or superseded queued rounds |
| CR-032 | `a5a2bd4` | Goal: expose active tools before an idle round |
| CR-033 | `710aac0` | Goal: persist transitions in machine order |
| CR-034 | `028989e` | Session dispose: cancel compaction and branch summary |
| CR-035 | `eff197f` | Session dispose: stop cache warming |
| CR-036 | `bb142fb` | Cache warming: message-identity prefix check |
| CR-037 | `45834ef` | Cache warming: late-dispatch deadline |
| CR-038 | `6a8d7f0` | Cache warming: contain callback failures |
| CR-039 | `904e1a9` | Cache warming: run-scoped stops |
| CR-040 | `a1a9550` | MCP HTTP: reject a malformed URL at Start |
| CR-041 | `5a4576d` | Pi-messages: contain callback failures |
| CR-042 | `f5ea097` | find: let fd report a missing search path |
| CR-043 | `dccf7f8` | Tool resolution: empty list, not null, in `changes` mode |
| CR-044 | `890d715` | Cache warming: unrecognized stored mode falls back to streaming |
| CR-045 | `b86c541` | Settings: empty steering/follow-up modes resolve to the default |
| CR-046 | `2197292` | OpenAI-compatible usage always carries a reasoning count |
| CR-047 | `5fad435` | Responses usage always carries a reasoning count |
| CR-048 | `53600ee` | HTML export ships the pinned Marked and CJK assets |

Coverage commits: `5dc51eb`, `22a6532`, `df8e227`, `f720e5b`, `edba452`,
`6494612`, `0605146`, `3c71c43`, `e204928`, `6cc1920`, `3652608`, `eb681b5`,
plus the inventory docs `f8c4df4`, `1a2da12`, `0b7ba10`.

## Read-tool corpus and D279

`coding/testdata/read-tool/generate.mjs` creates a fixture directory, runs the
pinned `read.ts` definition over 16 inputs and records each result's text and
details. `TestReadToolMatchesUpstreamTextPath` recreates the same files and
compares the Go tool's output after canonicalizing details keys.

Cases: an empty file, three lines, a file without a trailing newline, blank
lines, CRLF, a BOM, invalid UTF-8, offsets 2/0/9 (the last out of range), limits
2/1.5, offset plus limit, a 60,000-character line (the first-line-exceeds-limit
branch), 2,500 lines (the measured truncation notice), and a nested path.

All 16 matched on the first run. The only difference found is the wording of a
missing-file error: Node reports libuv's
`ENOENT: no such file or directory, access '<path>'`, the port reports Go's os
error. That is recorded as D279 (runtime-diagnostic wording is not emulated,
consistent with D269/D270) with `TestReadToolMissingFileError` pinning the
port's message; the case is excluded from the parity corpus. Regeneration is
byte-identical; `go test -count=1 ./coding`, `just check`, `just cross` (six
targets) and `just reference` pass.

## Edit-tool corpus

`coding/testdata/edit-tool/generate.mjs` runs the pinned `edit.ts` definition
over 11 cases, resetting the seed files before each one, and records the result
text, details and the file bytes after the edit. `TestEditToolMatchesUpstream`
recreates the seed files and compares all three.

Cases: a simple replacement, CRLF restoration, BOM preservation, two disjoint
edits, a fuzzy-indent rejection, adding a final newline, and the duplicate,
not-found, no-change, empty-`oldText` and missing-file errors. Error wording
matches as well, including `Could not edit file: nope.txt. Error code: ENOENT.`.

All 11 matched. The first harness draft shared one directory across cases, so an
earlier successful edit changed the file a later error case expected to read;
resetting the seed per case fixed the fixtures, not the product. Regeneration is
byte-identical; `go test -count=1 ./coding`, `just check`, `just cross` (six
targets) and `just reference` pass.

## ls-tool corpus

`coding/testdata/ls-tool/generate.mjs` runs the pinned `ls.ts` definition over 8
cases and records result text and details. `TestLsToolMatchesUpstream` recreates
the directory and compares both.

The fixture directory mixes punctuation, digits, case and `ä` (`_x`, `-y`,
`.hidden`, `10`, `9`, `a`, `ä`, `a b`, `A1`, `B`, `Z`, …) so the corpus pins the
collator-based sort end to end, not just the sort helper. Cases also cover an
empty directory, a nested path, integer and fractional limits, the default cwd,
and the missing-path and not-a-directory errors with their full text.

All 8 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Write-tool corpus

`coding/testdata/write-tool/generate.mjs` runs the pinned `write.ts` definition
over 7 cases, resetting the fixture files per case, and records the reply text
and the written bytes; `TestWriteToolMatchesUpstream` recreates the fixture and
compares both.

Cases: creating a file, overwriting one, creating nested directories, empty
content, CRLF content, content without a trailing newline, and non-ASCII text.
All matched.

A directory target is the D279 case: Node reports
`EISDIR: illegal operation on a directory, open '<path>'` while the port keeps
Go's os error, so it is pinned separately by `TestWriteToolDirectoryError`
rather than in the parity corpus. Regeneration is byte-identical;
`go test -count=1 ./coding`, `just check`, `just cross` (six targets) and
`just reference` pass.

## Grep-tool corpus

`coding/testdata/grep-tool/generate.mjs` runs the pinned `grep.ts` definition
with real ripgrep over one fixture tree; `TestGrepToolMatchesUpstream` recreates
the tree and compares result text and details.

Cases: matches across directories in ripgrep's traversal order, a file path, a
subdirectory path, literal (metacharacter) search, case-insensitive search, a
glob filter that matches nothing, context lines and fractional context, integer
and fractional limits with their notices, a 600-character line (truncation
notice), many matches, and the missing-path error.

All 12 matched on the first run. The corpus uses the installed `rg`; the suite
skips when ripgrep is unavailable. Regeneration is byte-identical;
`go test -count=1 ./coding`, `just check`, `just cross` (six targets) and
`just reference` pass.

## Find-tool corpus

`coding/testdata/find-tool/generate.mjs` runs the pinned `find.ts` definition
with real fd over a fixture tree (a git repository with `.gitignore`, hidden
files and nested directories); `TestFindToolMatchesUpstream` recreates the tree
and compares result text and details.

Cases: a basename glob, a path-containing glob, hidden files, no match, a result
limit with its continuation notice, a file path, and a missing path. The last two
pin CR-042 end to end: the caller sees fd's own diagnostic rather than a Go
precheck. All 7 matched; the suite skips when fd/fdfind is unavailable.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Corpus stabilisation

The first find/grep tool corpora compared raw output order. `fd` and `ripgrep`
do not promise a cross-file traversal order, and `fd`'s limited traversal
returns an unspecified subset, so `just check` caught the grep fractional-limit
case returning a different file order and the find limit case selecting different
files. The corpora now compare sorted match lines with trailing notices kept in
place; the grep limit case is scoped to one file, and the find limit selection is
asserted only by its notice and count in `TestFindToolResultLimitNotice`. Both
pass 20 repetitions and the whole coding suite twice afterwards. Shared
tool-corpus helpers moved to `coding/tool_parity_helpers_test.go`.

## Closing state

- Findings fixed this session: 23 (CR-021 through CR-043), each reproduced at a
  supported boundary first and committed with its regression.
- Coverage: 22 corpora added or extended; every generated fixture in the
  repository (54 `generate.mjs` directories) reproduces byte-identically from the
  pinned checkout at close.
- Gates: whole suite passes with `-count=2`; `just check`, `just cross` (six
  targets) and `just reference` pass; informational local `-race` runs pass for
  `ai`, `agent`, `cmd`, `coding`, `coding/interactive`, `tui`, `mcp/...`,
  `internal/...`.
- Divergences recorded this session: D279 (filesystem error wording keeps Go's os
  errors; Node's libuv text is not emulated).
- `TODO.md` holds no confirmed unresolved findings.
- Still not exhaustively probed: interactive wiring and renderers, the full
  settings manager surface, D264's summarization splitting/retry, MCP OAuth
  recovery, and platform terminal I/O. Passing tests and static review cannot
  certify the absence of defects.
- Nothing was pushed, released, or installed.

## Image-MIME corpus

`coding/testdata/image-mime/generate.mjs` executes the pinned
`detectSupportedImageMimeType` over 26 byte buffers and records each result;
`TestDetectSupportedImageMimeTypeMatchesUpstream` decodes the same hex and
compares.

Cases: JPEG and JPEG 2000 codestreams, a valid PNG, an animated (acTL) PNG, a
truncated signature-only PNG, a wrong IHDR length and a non-IHDR chunk type,
GIF87a/GIF89a versus an unknown version, WebP versus other RIFF payloads, BMP
with 12-byte and 40-byte DIB headers, wrong plane count and bit depth, an
oversized DIB header, a pixel offset before the header or at the declared size,
declared sizes that are zero, too small or larger than the buffer, a short
buffer, empty input and plain text.

All 26 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./coding`, `just check`,
`just cross` (six targets) and `just reference` pass.

## shortHash corpus

`ai/testdata/short-hash/generate.mjs` executes the pinned `utils/hash.ts`
`shortHash` for 18 inputs and `TestShortHashMatchesUpstream` compares the exact
output. Inputs cover empty, ASCII, astral pairs, mixed BMP/astral text, U+0130,
NUL, whitespace, a 1000-character string and the `toolcall:<n>` seeds Mistral
derives its tool-call ids from, so the persisted ids are pinned against the
reference in addition to being stable in the port.

All 18 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./ai`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Wrap corpus

`tui/testdata/wrap/generate.mjs` executes the pinned `tui/src/utils.ts`
`wrapTextWithAnsi` over 204 cases; `TestWrapTextWithAnsiMatchesUpstream`
compares the line arrays. Cases cover empty and whitespace-only input, long
words, CR/LF/CRLF breaks, SGR styles carried across wrapped lines and literal
newlines, underline/bold line-end resets, 256-color and truecolor sequences,
CJK, emoji with modifiers, combining marks, tabs, table rows and 160 seeded
mixed sequences built from words, spaces, newlines, wide characters and codes.

All 204 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./tui`, `just check`,
`just cross` (six targets) and `just reference` pass.

## Truncate corpus

`tui/testdata/truncate/generate.mjs` executes the pinned
`tui/src/utils.ts` `truncateToWidth` over 206 cases (curated plus 160 seeded
mixtures); `TestTruncateToWidthMatchesUpstream` compares the strings. Inputs
cover zero and negative widths, empty text with and without padding, ASCII fast
paths, ellipses that are empty, wide, exactly the width or longer, CJK, emoji
with modifiers, ZWJ sequences, combining marks, tabs, newlines, SGR styles
(after content and bare), OSC-8 hyperlinks with and without padding, and random
mixtures of the above.

All 206 matched on the first run, so this records coverage, not a finding.
Regeneration is byte-identical; `go test -count=1 ./tui`, `just check`,
`just cross` (six targets) and `just reference` pass.
