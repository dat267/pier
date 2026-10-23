# Port status

How much of upstream pi this port covers, package by package, plus the upstream
pin's change log. The user-facing summary lives in `README.md`; this file is the
authoritative per-area status.

Reference pin: `4a080252` (Release v1.4.0), read from the `./pi` checkout.
`just reference` checks that the checkout really is at that tag, and
`just reference-fetch` refreshes its tags without moving the checkout.
`ai/models_catalog.json` is regenerated from the pin's own generator
(`packages/ai/scripts/generate-models.ts --strict --data-only`, which fetches
models.dev) through `scripts/gen_catalog.py`.

Session aborts now retain cancellation across retry startup, backoff and
continuations, and `agent_settled.aborted` reports the run's abort flag rather
than the final assistant's stop reason (`core/agent-session.ts`, `abort`,
`_handlePostAgentRun`, `_emitAgentSettled`). Regression tests cover aborting from
the retry-start listener, cancelling backoff and starting a later fresh prompt.
Compaction cleanup also releases idle waiters on success and failure, general
abort cancels manual compaction and branch summarization, and automatic
compaction installs cancellation before publishing its start event (the same
reference's `_clearManualCompactionState`, `_runAutoCompaction` and `abort`).
Further v1.3.0 audit fixes: tree navigation releases idle waiters and forwards
the session retry policy to branch summarization (`core/agent-session.ts`,
`navigateTree`). The shared retry helper accepts partially supplied callbacks
and propagates callback errors (`ai/src/utils/retry.ts`). File-mutation queues
release completed paths, use absolute missing-path keys and propagate realpath
errors other than missing paths (`core/tools/file-mutation-queue.ts`).
Transcript transformation now applies JavaScript signature truthiness: an empty
signature does not preserve blank thinking, and cross-model tool calls retain
explicitly empty thought signatures (`ai/src/api/transform-messages.ts`). A
randomized comparison of 8,000 transcripts against the pin matched after these
fixes; 27,000 head/tail/middle truncation cases also matched.

A follow-up v1.3.0 correctness audit fixed custom-message delivery: triggering
messages during a run enter the steer/follow-up queue, context-only messages
flush after tool results at `turn_end`, and appended custom messages publish
lifecycle events after persistence (`core/agent-session.ts`). The fork selector
now extracts text from block-shaped user content, including normal submitted
prompts. `utils/paths.ts` parity fixes make relative path resolution absolute
and exclude file-URL query/fragment metadata while retaining escaped `?`/`#`.
Each fix has a regression test observed failing before the change.

The next v1.3.0 audit corrected JSON line-separator escaping, floating-point
spellings (with native integer precision retained under D253), JavaScript numeric
property enumeration and overflowing tool-argument numbers (`ai/src/utils/`
`json-parse.ts`, `validation.ts`). Literal enum/const validation now compares
decoded values, `oneOf` requires exactly one match, and object validation
enforces required keys, forbidden extras and boolean property schemas.
Strict schemas retain nested transformations and wrap optional properties in
`anyOf` objects (`ai/src/api/constrained-sampling.ts`).
Session forks write one newline per record and report serialization failures;
JSONL export preserves an existing destination when serialization fails
(`core/session-manager.ts`, `core/session-export.ts`). Session projection now
normalizes missing content, excludes retained older compaction summaries and
limits context edits to the upstream roles; custom edits preserve field order.
Tree children sort by millisecond timestamps rather than timestamp spelling
(`core/session-manager.ts`). Timeout settings and read-range labels share
JavaScript number parsing (`core/http-dispatcher.ts`, `core/tools/render-utils.ts`).
Event-stream completion broadcasts its result to all callers, and callbacks
can reenter `end` without holding a mutex (`ai/src/utils/event-stream.ts`).
Prompt argument slices clamp huge lengths instead of overflowing
(`core/prompt-templates.ts`), and version comparison follows npm semver's
numeric bounds and prerelease comparisons (`utils/version-check.ts`). Proxy
streams match HTTP error status handling and optional-field assignments
(`agent/src/proxy.ts`); negative content indices return protocol errors (D252).
Generated comparisons matched 10,000 argument objects, 4,000 session
projections, 32,000 argument validations, 5,000 strict schemas, 1,500
output-accumulator cases, 19,989 finite numeric encodings and 10,000 version
pairs against the pin after the fixes; concise regression tests cover each
changed behavior. Eight-minute fuzz runs of session entries, JSON repair,
message codecs, terminal input, incremental Markdown and MCP messages passed.

The 13 reproduced findings recorded in `TODO.md` were resolved against the same
v1.3.0 pin, with red-first regression tests. Event streams atomically claim a
terminal push; agent snapshots own their pending-call map, completed runs release
parent cancellation registrations, and failure-event listener errors reach the
caller (`agent/src/agent.ts`, `ai/src/utils/event-stream.ts`). Overflow patterns
now reproduce JavaScript whitespace, line terminators and ASCII case folding.
Raw JSON values canonicalize escaped strings, duplicate fields and numeric keys
while retaining ordinary insertion order and D253 integer precision. Validation
diagnostics walk the failing properties and array items instead of blaming a
valid root container (`ai/src/utils/{overflow,validation}.ts`). Fuzzy model IDs
use root collation (D257), quoted frontmatter scalars decode YAML escapes, empty
files read as empty text, read-tool options distinguish omitted resizing from
explicit false, and whitespace-only edit targets use JavaScript split counts
(`core/model-resolver.ts`, `utils/{frontmatter,mime}.ts`, `core/tools/`
`{read,edit-diff}.ts`). Settings saves recover stale lock directories and legacy
files, maintaining live leases with the proper-lockfile default timing
(`core/settings-manager.ts`). Completed findings are removed from `TODO.md`.

A further v1.3.0 audit bounds glob expansion and matcher allocations, preserves
literal suffixes after expansion limits, and corrects brace/class parsing and
numeric sequences (`core/model-resolver.ts`'s minimatch dependency; D262).
File-URL parsing now validates the normalized pathname, rejects invalid encoded
UTF-8, handles WHATWG whitespace and escaped localhost, and retains drive roots
through parent segments (`utils/paths.ts`; D261). Each change has a regression
test observed failing first; differential checks cover 61,412 glob pairs and
1,174 file URLs. Glob differences in that pool are confined to D262.
Byte-only tail truncation no longer reserves an unlimited line-budget slice,
large size formatting avoids integer multiplication overflow, and the shared
`toFixed` helper switches to exponent notation at the JavaScript 1e21 boundary
(`core/tools/truncate.ts`, `utils/image-resize.ts`). Eight-minute fuzz runs of
JSON repair and incremental Markdown passed after this audit.
The model-list renderer also preserves the caller's catalog order and sorts its
own copy using locale-neutral collation (`cli/list-models.ts`; D257).
HTML entity lookahead counts UTF-16 units rather than UTF-8 bytes
(`utils/html.ts`), retaining byte offsets for Go callers. Ten-minute fuzz runs
of message codecs (2,525,130 executions) and key parsing (3,710,540 executions)
also passed.
Twenty-minute fuzz runs of session entries (4,360,518 executions) and settings
codecs (4,757,807 executions) passed as well.

The broader lifecycle audit corrected MCP unsubscribe handles invalidated by
listener-slice growth (`mcp/src/{client,transports/transport}.ts`). Incoming MCP
request handlers now run off the stdio read pump after cancellation admission,
so `notifications/cancelled` can reach a waiting handler; handler panics become
JSON-RPC errors as upstream's try/catch requires (`mcp/src/client.ts`). Completed
HTTP SSE readers close their Go response bodies (`transports/streamable-http.ts`),
and provider-body reads cannot resurrect idle timers after close (D254).
The typed RPC client can restart after `Stop`, matching `modes/rpc/rpc-client.ts`.
Each finding has a regression test observed failing before its fix.

The eight reproduced compaction findings from the audits of `48081fe` and
`5918595` were resolved the same way, with red-first regression tests in
`coding/compaction_findings_test.go`. Recovery omissions refresh finalized
context immediately and resolve source entries through persisted and positional
provenance, so a resumed session can recover overflow and a failed summary
cannot replay an omitted response (`core/agent-session.ts`
`_omitRecoveryAttempt`, `_findPersistedMessageEntryId`). The next-turn
threshold check estimates the complete projection including new tool results
(`_compactBeforeNextAssistantResponse`), and usage is trusted only when its
source entry follows the latest context edit or compaction in branch order
(`core/compaction/compaction.ts` `estimateProjectedContextTokens`). Summary
requests forward provider environment and the selected reasoning level, and
both compaction paths pass the request signal into auth resolution
(`createSummarizationOptions`, `_getSummarizationRequestAuth`). File lists sort
by UTF-16 code units like JavaScript (`core/compaction/utils.ts`
`computeFileLists`). The promoted probes left `coding/testdata/compaction-audit/`
with their findings; the directory is gone and the tests live in the normal
suite.

**Upstream status.** v1.4.0 (`4a080252`) is the latest released tag after
refreshing upstream tags and is the port's reference pin. It is 81 commits past
v1.3.0. The pin identifies the sources to review, not a claim that every change
is ported. The v1.4.0 catalog, transcript tool-change modes, Markdown work and the
completed delta audit are described below. The audit classifies all 81 commits;
the remaining in-scope gaps are client identity in pi-ai, telemetry and the
durable v1.4 harness, plus the documented out-of-scope areas.
The v1.3.0 delta from v1.2.0 is ported (see the historical v1.3.0 audit below);
the v1.2.0 delta from v1.1.0 was audited. Confirmed gaps, porting progress and
out-of-scope areas are recorded below. Earlier release notes retain upstream
versions and commit IDs as historical context.

### v1.4.0 delta audit

The v1.4.0 delta is 81 commits and 343 files. Every commit is classified
below; "ported" groups were implemented before this audit, "fixed" was
corrected here, "gap" is an in-scope behavior still missing, and "out"/"n/a"
are documented out-of-scope areas or non-runtime changes. This audit found and
fixed four parity regressions:

- **PI user agent on pi-messages**: the request sent Go's default
  `Go-http-client/1.1`, because only the other providers set it. It now uses
  `GetPiUserAgent` before caller headers, matching `getIdentityHeaders` in
  upstream `api/pi-messages.ts` (`adc5fa9f8`).
- **Cloudflare attribution**: the pre-v1.4 coding-agent override
  `User-Agent: pi-coding-agent` was still sent. Upstream moved attribution to
  pi-ai and no longer maps Cloudflare, so the `pi` agent is used
  (`adc5fa9f8`, `04a93bad8`).
- **OpenCode client attribution**: `x-opencode-client: pi` was sent even with
  install telemetry off. It is now telemetry-gated attribution, while
  `x-opencode-session` stays unconditional, as `getOpenCodeSessionHeaders` and
  `getClientIdentity` specify (`04a93bad8`).
- **Rex truecolor**: `TERM_PROGRAM=rex` now reports truecolor (and still
  reports kitty images and OSC 8 links) without COLORTERM, matching
  `detectCapabilitiesFromEnvironment` (`0ed1b0431`, `396ecd7b8`, #11131).

Remaining in-scope v1.4.0 gaps:

- **Client identity in pi-ai**: there is no per-request `identity` option, so
  the detailed `pi/<version> (<platform>; <runtime>; <arch>)` user agent,
  custom/short modes, the Bedrock SDK user-agent pairs, and the
  `telemetry`/identity split are absent. The port keeps its older
  `pi (<platform> <release>; <arch>)` form and applies attribution in `coding`.
- **Telemetry**: the chord/pi-ai/durable telemetry stack (spans, metrics,
  SQLite store, trace renderer) is not ported, so entries marked "out" below
  stay out of scope.
- **Durable**: the v1.4 harness features (sleeping tasks and host timers,
  crash-loop guard, `ReportInfo`, `checkpointWhen`, per-model compaction in the
  harness, function-valued model/settings options, IndexedDB storage) and the
  remaining conformance cases are unported, as is the reverted commit-log
  replication. `ToolControl.terminate` typing is TypeScript-only.
- **Chord**: the SQLite driver move, exclusive/replicated operations, WAL
  reader and telemetry store belong to the unported typed-host rewrite; the
  replication commits were reverted upstream.
- **Extensions, codemode, classifier, package manager, HTML export**: the
  extension-dialog stacking, extension tool activation, codemode worker queue,
  System One classifier image input and managed-install marker changes stay
  out of scope (D41, D140); the HTML export renderer is injected at the RPC
  boundary, so the vendored Marked CJK bundle does not apply.

| Commit | Change | Status |
|---|---|---|
| `ae7b16d43` | Add [Unreleased] section for next cycle | n/a: changelog metadata |
| `4954d7605` | feat: check the model catalog against the latest release before publishing (#11007) | out: release tooling |
| `6085986e4` | fix(durable): type ToolControl.terminate as boolean | gap: durable harness |
| `c4cf7957b` | feat(durable): per-model compaction settings via compaction.modelOverrides | gap: durable harness |
| `790271cd0` | fix(durable): restore ToolControl.terminate changes dropped by c4cf7957b | gap: durable harness |
| `0fbdbd3f8` | fix(ai): retry node-fetch 'request to ... failed' errors | ported: retry classification |
| `99fe14897` | fix(ai): use mimo-v2.6 models in Xiaomi tests | ported: catalog tests |
| `90d863f7c` | fix(coding-agent): use mimo-v2.6-pro as Xiaomi default model | ported: catalog default |
| `6fcb48670` | fix(coding-agent): remove fixed output limits from summary requests (#11036) | ported: summary output limit |
| `4dd2b149d` | fix(ai): treat whitespace-only tool output as empty | ported: empty tool output |
| `698a2d23b` | fix(ai): retry more gateway upstream failure errors | ported: retry classification |
| `7ec1f648e` | feat(durable): accept functions for HarnessOptions.models and settings | gap: durable harness |
| `2331785c3` | fix(durable): run the Codex prompt-cache e2e test on gpt-6.1-sol | gap: durable tests |
| `14ff455a0` | fix(env): make the read/bash differential test deterministic and meaningful | out: env package |
| `95364a54e` | feat(coding-agent): default NVIDIA to Nemotron 3.5 Super VL (#10910) | ported: catalog default |
| `8efd314fa` | docs(ai,coding-agent): generate models.json reference from schemas (#11038) | n/a: generated docs |
| `c268154ca` | feat(chord): move Durable's SQLite drivers into chord/sqlite | out: chord |
| `f75b3240a` | feat(durable): durable sleeping state replaces runtime.sleep | gap: durable timers |
| `ea7c64843` | feat(durable): workerd tests for Durable Object hosting | out: workerd runtime |
| `de47e488f` | docs(durable): changelog for sleeping tasks, timers, and wakeups | n/a: docs |
| `26b5535a5` | docs(durable): clarify the SQLite schema migration in the changelog | n/a: docs |
| `ec1be23eb` | fix(durable): review fixes for sleeping tasks and host timers | gap: durable harness |
| `00bf7193d` | fix(durable): arm before resume in the Durable Object example; timer wording | out: durable example |
| `0b102787c` | docs(coding-agent): restore extension guide details (#11039) | out: extension docs |
| `e17e2e105` | fix(durable): release the second slow call when the first finished, not after 200 polls | gap: durable timers |
| `a30c2a81d` | feat(durable): crash-loop guard, report sources, inbox after failed runs, blocked tasks in the task graph | gap: durable harness |
| `440a5fcdb` | fix(durable): review fixes for the crash-loop guard, reports, and blocked tasks | gap: durable harness |
| `5ba1cbaf3` | fix(durable): close keeps admitted aborts, reports name the extension that ran, review nits | gap: durable harness |
| `91b33cab3` | feat(durable): ReportInfo says what happened, with typed reasons | gap: durable reports |
| `482c1381a` | fix(ai): update GLM-4.6V Coding Plan cache read price in test | ported: catalog test |
| `48909d987` | feat: telemetry propagation, span links and metrics; telemetry member in chord/context | out: telemetry |
| `1402973a9` | fix: chord boundary test allows pi-telemetry, the dependency of Context.telemetry | out: chord |
| `2ec55d808` | feat: Durable and pi-ai telemetry; Context.telemetry is always a span | out: telemetry |
| `e28e70804` | ignore .pi/settings.json | n/a: repo hygiene |
| `d2e7d8b7c` | feat(ai): add MiniMax-M3.1-Flash and hide only old MiniMax models | ported: catalog |
| `d9025a5f5` | feat: SQLite telemetry store in Chord, shared TelemetryStore and trace renderer | out: telemetry |
| `e86b05475` | fix(ai): OpenRouter reasoning test uses deepseek-r1-0528, deepseek-r1 left the catalog | ported: catalog test |
| `5c1736f82` | feat: checkpointWhen is required on Durable document definitions | gap: durable documents |
| `28bea006b` | fix(coding-agent): accept Windows Store shell aliases | ported: Windows shells |
| `61060ddf8` | fix(coding-agent): resolve Windows shells from installation directories | ported: Windows shells |
| `0ed1b0431` | feat(tui): detect Rex terminal for OSC 8 hyperlinks | fixed: Rex truecolor; images already ported |
| `73f8de80b` | fix(ai): recover from Copilot request-size overflow | ported: overflow detection |
| `caa6967b6` | fix(ai): keep the prompt cache across tool changes on Responses (#11142) | ported: tool-change cache |
| `530015d93` | fix(ai): only list announced removals as still unavailable | ported: tool-change cache |
| `ea25697e9` | fix(coding-agent): accept managed install markers with a UTF-8 BOM | out: package manager |
| `1d01b3b1f` | Break single Mermaid diagram into two | n/a: docs |
| `b2d0da6fe` | fix(tui): render CJK emphasis next to punctuation (#10730) | ported: CJK emphasis |
| `a731ba8b8` | docs(tui): add changelog entry for CJK emphasis fix | n/a: changelog |
| `fbc456d82` | fix(ai): expect xhigh thinking for Grok 4.3 and Grok 4.5 | ported: catalog test |
| `26293e74f` | fix(coding-agent): render CJK emphasis in HTML export (#11168) | out: HTML export |
| `48c8f3228` | feat(chord): exclusive SQLite operations and a replicated connection wrapper | out: chord |
| `fdce48eb4` | fix(ai): drop Codex models rejected for ChatGPT accounts (#11173) | ported: catalog |
| `396ecd7b8` | feat(tui): enable Kitty images for Rex | ported: Rex images |
| `2ed3118b0` | fix(tui): stop unclosed display math from swallowing Markdown tables | ported: math/table lexing |
| `9df819de3` | fix(tui): restore terminal modes when the process exits without stop() | ported: exit restore |
| `38847563f` | fix(coding-agent): stack concurrent extension dialogs instead of replacing them | out: extension dialogs |
| `f8ab362cc` | fix(coding-agent): keep clipboard helpers out of pi's process group so Ctrl+Z does not stop them | ported: clipboard detach |
| `885a564b3` | fix(coding-agent): queue codemode scripts at the worker limit | out: codemode |
| `22dc6c26f` | fix(coding-agent): expose @earendil-works/pi-ai/models in the extension loader | out: extension loader |
| `bdd9f0399` | fix(coding-agent): accept mcp.json files with a UTF-8 BOM | ported: MCP BOM |
| `16f5f0af4` | feat(durable): IndexedDbStorage, a browser storage backed by IndexedDB (@earendil-works/pi-durable/storage/indexeddb) | out: browser storage |
| `e5fe9777b` | fix(ai,coding-agent): drop models retired upstream | ported: catalog |
| `cedefc532` | fix(ai): handle ChatGPT Business workspace usage limit errors | ported: usage-limit error |
| `82432219a` | docs: note generated model data is gitignored in AGENTS.md | n/a: docs |
| `29057ece3` | fix(ai): retry Codex CLI stream disconnect errors relayed by proxies | ported: retry classification |
| `6e491cfa9` | fix(ai): match retryable status codes only as standalone numbers | ported: retry classification |
| `612a172e6` | fix(tui): escape regex metacharacters in single-segment fd queries (#11212) | ported: fd query escaping |
| `adfc14461` | fix(coding-agent): apply /compact instructions to split-turn prefix summary | ported: split-turn instructions |
| `adc5fa9f8` | feat(ai,agent,coding-agent): move client identity and provider attribution into pi-ai | fixed: attribution/UA; identity option remaining |
| `04a93bad8` | fix(ai,agent,coding-agent,durable): client identity follow-ups | fixed: attribution/UA; identity option remaining |
| `4759f09fb` | feat(chord): WAL reader, page checksums and segment container for SQLite replication | out: chord |
| `89e309581` | fix(chord): remove WAL page replication | out: chord |
| `4c25119f5` | feat(durable): replicate storages as a commit log | out: durable replication (reverted) |
| `cae9fd28a` | Revert "feat(durable): replicate storages as a commit log" | out: durable replication (reverted) |
| `bd3c9b823` | fix(coding-agent): activate extension tools registered after the branch recorded its loadout | out: extension tools |
| `8c4e87389` | fix(coding-agent): grep tool ignores ripgrep config via --no-config | ported: grep --no-config |
| `02726086d` | fix(coding-agent): recovery omits tool results with the truncated tool call | ported: recovery tool results |
| `7f225f25d` | feat(ai,coding-agent): image input for System One classifiers | out: classifier images |
| `f76b248ca` | docs(ai,tui,coding-agent): audit changelogs since v1.3.0 | n/a: this audit |
| `18e56a069` | fix(ai): expect compat on OpenRouter decision models in catalog test | out: classifier test |
| `4a080252f` | Release v1.4.0 | n/a: release commit |

**v1.2.0 parity fixes so far.** The built-in catalog is regenerated from the
pinned v1.2.0 generator and current upstream catalog APIs. OpenRouter model
refresh now filters by the credential's `GET /models/user` list, applies its
limits/prices and retains the personalized cache offline (`#10672`, `ee409a85f`);
negative router placeholder prices normalize to zero. The runtime also refreshes
persisted pi.dev catalogs for built-in providers, except Radius. Mistral's
request timeout now applies only while
waiting for response headers, not during an active response stream (#10609,
`6b07b4e57`). Retry classification includes the gateway error wording
"The upstream provider returned an error" (#10842, `7bd01fb92`). Compaction
summaries omit assistant thinking so replayed reasoning does not inflate the
prompt (`15e7bdc14`). Shifted-symbol keybindings match the produced character
under Kitty and modifyOtherKeys (`7f9e1198f`), and `showHardwareCursor` no
longer draws a second software cursor (`1cedd3272`). `PI_CLIPBOARD_OSC52=1` also
sends an OSC 52 copy after a successful local clipboard write (`3e477eab5`).
Unhandled mouse-wheel input scrolls the transcript behind a focused overlay
(`#10769`, `11f991145`). Recognized delayed terminal color, program-status,
version, and Kitty graphics replies remain buffered until their terminator
(`edb2eb966`). MCP tool `structuredContent` accepts every JSON value, including
arrays, scalars, and `null` (`67996ab75`), and canceled HTTP tool calls are not
retried after authorization recovery (`da0eadf04`). MCP startup now records
resource capability without listing every resource or template (`#10526`,
`f4cca2dc2`). Durable display-only writes append immediately even while busy,
except when a queued reset must precede them; input submissions can also be
placed as application entries (`#10407`, `e6b9a6af5`), and the host-facing
`InputSubmissionDraft` carries the `InputEntry` as well. A durable
`pi.tool-result` for an executed call carries its `durationMs` (a recovery
rerun measures only itself; interrupted and aborted calls have none). Text print mode warns on
a truncated final response and exits with status 1 (`#10799`,
`441f5767c`). Durable rejects waits that close a live wait-for cycle and permits a
non-cyclic wait on an owner (`#10411`, `eb2fce43d`). Anthropic retries
preferred strict tools once without strict constraints for documented schema-size/tool-limit
rejections (`#10409`, `d4d53612b`). A failed blocking threshold compaction now ends its
waiting generation with `model_error` instead of sending an oversized request
(`89e5c2c07`). The newest compaction failure stays in `pi.live.compactionFailure`
until a summary succeeds, and failed `compaction_end` events carry the error
(`eba849739`). Bash and PowerShell nonzero exits now return error tool results
with an `exit_code` diagnostic rather than throwing (`eba849739`). Durable
Storage failures, including reads, now fail and close the Session; later calls
return the first failure as `SessionFailed`, watches end with
`session_failed` and that cause, and `Closed`/`End` settle with `closed` or
`failed` (carrying the cause) once Storage has closed (`eba849739`). The
Session's document cache is bounded by use: an unobserved loaded document
leaves memory once it was not read or written for `documentRetentionMs`
(default ten minutes; `0` drops it right after use) and loads from Storage
again, while a watched or state-observed document stays (`eba849739`). A
throw in the scheduler's own commit, such as a failed fault write, ends the
invocation and fails the Session, so the task is not reserved and run again in
the same process (`eba849739`). The compaction status indicator embedded in a
custom editor now uses the editor border color (or the thinking-level color),
like the working indicator, instead of the default muted colors (`#10809`,
`#10810`). MCP OAuth sign-in now rejects an authorization response's `iss` when
no authorization server metadata was discovered, where it previously skipped
the check and dereferenced the missing metadata. Overlay compositing keeps the
zero-width cursor marker at the boundary when an overlay ends directly left of
the cursor, instead of dropping the hardware cursor position. A worktree
context file that symlinks to its main repo's file loads again: the shadow
comparison uses the directory's real path, not the resolved file path
(`#10681`). Theme `$schema` references use the published
`https://pi.dev/schemas/theme.schema.json`, and a keybindings `$schema` is not
read as a binding. The shell's byte decoder drops only a byte-order mark at the
very start of the stream, keeping a later U+FEFF as text, and flushes an
incomplete trailing character as the replacement character at the end (the
v1.0.4 `StreamDecoder` behavior). `OpenBinaryReader` takes the v1.0.3
`noFollow` option, refusing a symbolic link as the final path component while
still resolving earlier components. A read whose caller already cancelled
fails only that call, not the Session, and `SnapshotAsOf` errors reach the same
failure guard as other reads. A throwing host commit/close
listener is reported through `onReport` instead of failing the operation that
ran it, the Session's own listeners fail the Session, a throwing watch listener
is reported as well as ending the watch with `listener_error`, and a throwing
clock is reported once and falls back to the wall clock (`eba849739`). The
Durable `read` tool returns a supported image within the model's limits as one
image block instead of an `unsupported_image` error, with the header-only
dimension check and the model's `inputLimits.images.resize` overrides
(`eba849739`). `auth.json` lock
acquisition retries transient Windows `EPERM` within a bounded count while
non-Windows failures remain immediate (`#10801`, `e6fb2581d`). Focused regression
tests cover these fixes.

**Fixes folded into the pin.** A resumed default-tools session restores the
tool loadout its transcript records, with modifier-only `--tools` entries
applied (`6f2f164ab`, `core/agent-session.ts`). The invalid `\u` escape repair
(`4b994d5e5`) was already the port's behavior. The error-tool-result image fix
landed as `34a8bb38b` (a text note) and was replaced within the release by
`5ab7fe74c`: the images now follow the tool results of the turn.

**v1.3.0 delta audit (previous pin).** The v1.3.0 release (`71023aec`) is
63 commits past v1.2.0 (207 files) and was the port's previous pin. This mapping
records the completed in-scope v1.3.0 work.
`packages/tui/src`, `packages/agent/src` and `packages/mcp/src` are unchanged
in the delta, so only `ai`, `coding-agent` and `durable` carry work. Out of
scope (documented, not ported): chord's typed-host rewrite and protocol
versions, the `chord/output` relocation, codemode worker admission and
cancellation, the Nix packaging changes, and the extension `ctx.ui.custom`
overlay fix.

Ported so far:

- Retry wording: "The service is temporarily unavailable" and "Unable to
  forward upstream stream" (`ai/utils/retry.ts`, #10965/#10920).
- Gemini 4+ models use the discrete thinking level (`ai/api/google-shared.ts`,
  #10890).
- Reasoning deltas are materialized before the text block when a completions
  chunk carries both (`ai/api/openai-completions.ts`, #10926).
- Prompt templates keep empty quoted arguments
  (`coding-agent/core/prompt-templates.ts`, #10867).
- `read` line-range labels accept numeric strings and omit malformed ranges, in
  the TUI renderer, the session tree and the HTML export
  (`coding-agent/core/tools/render-utils.ts`, #10938); the port mirrors the
  upstream `read-line-range-display.test.ts` cases.
- Error tool-result images for Anthropic and Claude on Bedrock follow all tool
  results of the turn instead of being dropped (`ai/api/transform-messages.ts`,
  `anthropic-messages.ts`, `bedrock-converse-stream.ts`, #10794).
- Thinking from earlier assistant messages produced by a different model is
  dropped instead of being sent as text (`ai/api/transform-messages.ts`,
  #11000).
- A finished tool call whose complete arguments text is not a JSON object keeps
  no parsed prefix, is reported with the raw text, and fails validation
  (`ai/utils/json-parse.ts`, `ai/utils/validation.ts`, the wire APIs, #10944;
  D221 for the message wording).
- `durable` context derivation leads with the baseline system message even past
  kept assistant messages, so a post-compaction run still has its initial prompt
  and tool set (`durable/src/harness/context.ts`, #10904).
- Kimi Code moved to OpenAI Chat Completions at
  `https://api.kimi.com/coding/v1` (`ai/providers/kimi-coding.ts`, #10720), and
  OpenCode Go gained Google Generative AI (`ai/providers/opencode-go.ts`).
  These came with the catalog regeneration below.
- `off` thinking level handling for Claude Haiku 5.5 and Opus 5 (#10753), the
  Xiaomi MiMo thinking on/off map (#10818) and the Codex GPT-6.1 Sol 500K
  context window (#10840) come from the regenerated catalog; the Anthropic
  `thinkingDisabled`/managed-effort restructure that reads a nil `off` mapping
  is ported (`ai/api/anthropic-messages.ts`).
- Cache-preserving thinking-level changes on OpenAI GPT-6: the thinking level
  is sent as `configuration_update` items at a fixed `reasoning.effort` of
  `medium`, and the reply records it for replay
  (`ai/api/openai-responses*.ts`, `providers/compat-schema.ts`,
  #10930/#10853). Sign in with ChatGPT keeps the request-level effort. The
  Anthropic side (`supportsMidConvoEffort` on `anthropic-messages`) already
  existed.
- `--model` and scope resolution collect candidates before choosing a provider,
  report ambiguity across providers instead of using catalog order, prefer an
  authenticated provider only to break a tie, and warn when a scope pattern
  matches several providers (`coding-agent/core/model-resolver.ts`,
  #10160/#10913). `--api-key` keeps the provider the key is for. `pi auth`
  credential printing resolves `--model` through the same resolver, so an
  uncatalogued id on another configured provider is not a match
  (`cli/credential-print.ts`).
- `durable` SQLite storage namespaces its tables and indexes with an optional
  `Prefix` (`SqliteStorageOptions` / `OpenSqliteStorageWithOptions`) and
  validates it, for a database shared with host tables
  (`durable/src/storage/sqlite/schema.ts`, #10937). The port rewrites the base
  names at the database boundary instead of rendering them into every query;
  the conformance suite additionally runs over a `pi_` prefix. The
  Cloudflare Durable Object adapter (`storage/sqlite/cloudflare.ts`, its
  `SerialQueue` and shared per-storage queue) is Workers-runtime specific and
  out of scope; the port serializes through `database/sql` with a single
  connection.
- The unused assistant-message frame API (`AssistantMessageFrameEncoder`,
  `ReduceAssistantMessageFrames`) was removed to match the pin; the port had no
  runtime user (`ai/src/utils/assistant-message-frame.ts`, `b39979799`).
- Invalid `\u` escapes in tool-call arguments stay literal text instead of
  failing the parse and dropping the field (`ai/utils/json-parse.ts`, #10868);
  the port already behaved this way and now mirrors the upstream
  `json-parse.test.ts` cases.

All in-scope v1.3.0 items are ported.

The `durable` base64 `btoa` fix (#10923) is a V8 concatenation-tree memory fix;
the port already encodes with Go's `encoding/base64`, so it does not apply.

**Catalog regenerated for v1.4.0.** `ai/models_catalog.json` is rebuilt from the
v1.4.0 generator (`packages/ai/scripts/generate-models.ts --strict --data-only`,
schema 6, manifest
`ac52f772486d829cd2ed5ae7bd07f67712ee7ad440bd58143b44a10d06070f0f`, generated
2026-10-23) through `scripts/gen_catalog.py`: 41 providers, 1590 chat models
(1614 before; 52 added, 76 removed, 167 entries changed).

Almost all of that churn is live provider data moving in the 8 days since the
previous snapshot, not the tag: running the previous pin's own generator today
produces the same 1590 ids. The generator's own deltas are two models. It adds
`MiniMax-M3.1-Flash` (upstream `d2e7d8b7c`) and drops `openai-codex`'s `gpt-5.5`
and `gpt-5.3-codex-spark` (upstream `e5fe9777b`); it also carries Grok's `xhigh`
thinking level (`fbc456d82`) and the corrected GLM prices (`482c1381a`). The
data files are generator output, not committed sources, so a refresh has to
fetch models.dev; the tag only pins the generator.

`DefaultModelPerProvider` follows the tag: `github-copilot` moves to
`gpt-6.1-sol` (upstream `e5fe9777b`), the four Xiaomi providers to
`mimo-v2.6-pro` (`90d863f7c`) and `nvidia` to its Nemotron 3.5 Super VL entry
(`95364a54e`). The refresh retired the old Copilot and Xiaomi ids, so those
defaults would otherwise resolve to the provider's first model; a test now
asserts every default exists in the shipped catalog.

Emphasis follows Marked 18.0.11 with the `marked-cjk-friendly` 0.1.2 extension
upstream adds (`b2d0da6fe`, #10154). Failed delimiter runs emit one marker at a
time, preserving Marked's text predecessor across underscores and clearing it
after inline tokens. Mixed-marker spans use the right-delimiter regex's initial
skip alternative, and nested underscore openers contribute to the delimiter
balance. Opening guards distinguish punctuation from word content before the
CJK overrides, including JavaScript whitespace and UTF-16 predecessor semantics.
This resolves the pathological runs previously recorded in `TODO.md`, including
`：__1_` and `**__(___*`, without changing intraword underscore behavior. Red-first
regression tests and the existing 95-case token corpus pass. Two deterministic
50,002-case comparisons against the pinned Marked/extension versions matched:
one emphasis-only pool and one expanded with supplementary CJK, variation
selectors and JavaScript-specific whitespace.

Tool changes after the first request now say how a transport can express them
(`TranscriptToolSupport`): `"none"` keeps the current tools at the top,
`"additions"` adds a tool at a later system message, and `"changes"` declares
every tool in place. The Responses transports take the mode from the model's
compat flags, and `additional_tools` items start at the head of the input like
Codex, so a later item can replace an earlier in-place definition but never a
top-level tool. Removals are announced as text (no wire item can remove a
declared tool) and every later item is preceded by the incremental tools notice,
restating the tools that are still unavailable (upstream `caa6967b6`, #10940).
The resolver reports how many leading system messages the top-level field covers,
so a removal invalidates the cache once instead of on every later load, and the
notice restates only removals announced by earlier messages rather than the one
it precedes (upstream `530015d93`).

`ai/image_catalog.json` is refreshed too. The `generate-image-models` script the
older pin used no longer exists; the same `--data-only` run writes the image
entries into the provider data files, and `scripts/gen_image_catalog.py` now
flattens the `type: "image"` ones the way upstream's
`flattenImageModelCatalog` does. 63 OpenRouter image models (55 before), adding
`flux-3-image`, `gemini-nano-banana-2.1`, `qwen-image-2.1-*`, `seedream-5-0-flash`
and others, dropping `inclusionai/ming-image-0.1-design`, and carrying the
`inputLimits` and `type` keys verbatim like the chat catalog.

Out of scope in the v1.3.0 delta (in addition to the chord, codemode, Nix and
extension items above): `sessionShareDestination` and the `/share` destination,
because the port already excludes `/share` and its gist/Radius upload transport
(see the interactive row below); the unknown settings key is ignored, so a
settings file carrying it still parses.

**v1.2.0 audit still open.** The largest unported change is chord's v1.2.0
typed-host runtime, which replaced the facet/service runtime the port mirrors
and removed the `client`, `protocol` and `server` packages the port keeps; that
rewrite is not attempted here. The new published configuration schemas
received a source-level review: the theme schema URL and the keybindings
`$schema` skip are fixed, but the four generated schema artifacts and their
typebox validation are not ported (the port keeps its hand-written theme
validator and has no settings/models/keybindings schema validation); they are
configuration-editor support, not a runtime request path. The remaining Durable v1.2.0 items are the unported
optional features: `listFiles` belongs to the `packages/env` daemon (already
classified as a separately scoped remote-execution feature above), and
structured tool outputs and nested/deferred tool execution belong to the nested
call host whose triggers (codemode and extension tools) are out of scope.
Codemode implementation remains out of scope. Document retention is
eviction-on-commit/line in the port (upstream also evicts on a timer), so an
idle Session holds a loaded document past its retention until the next commit;
the timer is not ported because the port's Session is synchronous. Do not treat
the fix list above as a complete v1.2.0 port.

Ported from it so far: the built-in catalog, regenerated from the tag's own generator
(`claude-haiku-5-5`, the prompt-length pricing tiers for 154 models, and the model churn
that comes with live models.dev data: 89 added, 58 removed). The regeneration also carries
the Azure provider rename, `azure-openai-responses` to `azure` (upstream `9714`), which is
breaking for `auth.json`, `settings.json` and `models.json` entries, and Azure's second
API: Foundry Chat Completions deployments now stream through the endpoint and deployment
resolution the provider shares with the Responses API (`ai/providers/azure.go`).
`ai/image_catalog.json` was not regenerated for v1.2.0: its upstream input
(`image-models.generated.ts`) is produced by a script that was not present at that tag, so a
re-run needed that tool first. The v1.4.0 generator carries image entries in the same data
files as the chat catalog, so that refresh covers both (below). The catalog's `radius` snapshot also supplies the Radius
provider's default-gateway cold-start baseline, which `43d376399` keeps as the fallback
under a fetched or cached catalogue rather than merging into it (`ai/radiusconfig.go`).

The agent loop follows the `packages/agent` contract introduced in v1.1.0 and unchanged in v1.2.0: `PrepareRequest`
runs before every provider request, `FinishTurn` runs before `turn_end` and can end or explicitly
continue the run, and loop-level callback errors enter the run-failure lifecycle. `RunToolCall`
exposes the single-tool execution path with the same validation and tool hooks, including returned
`isError` and execution duration; tool-hook errors remain tool results. Tool updates reach the sink
through one ordered consumer per call, so `OnUpdate` is never re-entered and events keep the tool's
emission order; update listener errors also fail the tool call, matching upstream's awaited update
events. A panic from a tool's
`prepareArguments` shim, `beforeToolCall`/`afterToolCall` hook, or `execute` is the same throw
upstream catches, so it becomes an error tool result and the batch continues; parallel-worker
panics are re-raised on the loop goroutine so the run-failure contract still applies. A panic that
escapes the interactive owner loop also follows upstream's `uncaughtException` contract: the app's
entry point converts it to the handled crash, which restores the terminal, records the crash and
exits 1, instead of dying with the terminal left in raw mode. `ShouldStopAfterTurn` remains as
a deprecated compatibility callback; new code should use `FinishTurn`.

The response lifecycle follows the v1.1.0 `agent-loop.ts` contract, unchanged in v1.2.0: cancellation reaches
provider work through its request context, while the loop keeps consuming events
and awaits the provider's terminal result. Abandoning those waits on cancellation
left an assistant component open and synthesized a second aborted message, producing
two visible `Operation aborted` notices for one committed abort. A cancellation
also never reaches the model as Go's context text: tool results, lazy-stream
setup failures, provider error normalization and run failures all report the
canonical abort (D256). Start/update events now carry shallow message-field
snapshots, matching upstream's object spreads, so later provider cleanup cannot
change their queued stop reason. Regression tests
cover delayed abort cleanup, preserved partial content, and one cancellation notice
when the UI drains its queue after cleanup. The final message also records the
requested Pi thinking level, matching upstream's `Object.assign(result, { thinkingLevel:
config.reasoning ?? "off" })` (`agent-loop.ts:409`), so a port-produced assistant
message carries the same `thinkingLevel` key as upstream.

Compaction keeps upstream's request shape and failure message. Summary requests
carry no output limit (upstream v1.4.0 `6fcb48670`), so the provider sizes the
answer from the request, and a summarization that still stops at the cap
escalates instead of failing: the same input with reasoning off, then the input
split in halves merged through the update prompt, never between a tool call and
its result, and bounded per compaction (D264).

Tool output was aligned with the pin's `read.ts`/`edit-diff.ts` line handling:
the `read` tool splits on `\n` the way `read.ts:136` does, keeping the empty
element after a trailing newline, so the final newline and the reported total
line count match; `GenerateDiffString` sizes its line-number column from the
same raw split; and the `edit` prepareArguments shim accepts an empty `oldText`
in the stringified single-edit form, like `isSingleEditInput`.

Tool-result `details` also match upstream: `TruncationResult` serializes camelCase
with `truncatedBy: null` when no limit fired, and the tools upstream leaves
`details` undefined for (`read` when not truncated, `write`, `ls`/`find`/`grep`
without a notice, `bash` without truncation) omit it instead of writing `{}`.
That also stops a non-truncated `read` from duplicating the file text in
`details.truncation.content`.

Custom-message persistence stores the entry's `content` and `details` as their
actual JSON values, like upstream `appendCustomMessageEntry`; the port marshalled
raw JSON text as a string, and the projection (`CreateCustomMessage`) added
another escaping layer, so content compounded quotes on every save/load.
`CreateCustomMessage` now carries `details` too. The system-message token
estimate sums the content chars, each section, and `toolsAdded` like upstream
`estimateTokens`, instead of the rendered prompt text.

The v1.1.0 coding-agent `--tools` modifier lists (`+name`/`-name`) are ported:
they modify the configured default selection, survive settings reloads, and reject
mixed allowlist entries and patterns (`ddaa0a034`, `cli/args.ts`, `core/sdk.ts`).
Explicit allowlists and exclusions also apply to asynchronously registered MCP tools,
including wildcard matches, and `--no-tools` keeps those tools inactive. Direct-tool
startup readiness waits for registry publication. MCP reloads replace changed same-name
tool definitions and remove withdrawn tools from the session registry; interactive
manager callbacks post those mutations to the owner loop and discard stale-manager updates
(`04b97ef00`, `core/agent-session.ts`, and `2db5e359b`). Codemode/deferred MCP execution
and the `/mcp` manager UI remain out of scope (D185).

Also ported: the retryable `server_busy`, `servers are currently busy`
and `pending stream has been canceled` provider patterns (#10543, #10379), the
Mistral `finish_reason: "error"` retry (#10487), model and caller headers
overriding the Codex `originator` and `User-Agent` (#10429), the 3.5
characters-per-token request estimate (#10497), the Anthropic OAuth fallback to a
free loopback port (#10571), and the environment detection of OSC 8 hyperlink
support, Herdr included (#10573). The last one is `coding.HyperlinksSupported`,
which is what first installs the capabilities the tui package reads (D69), so
`terminal.hyperlinks` and the whole OSC 8 branch now work.

Ported as well: the recorded response and tool-execution times of #10549. `ai`'s
`AssistantMessageEventStream` stamps `durationMs` on the final message of a response it saw
start (a forwarded or deferred message stays untimed), the agent measures `Execute` with a
monotonic clock and rounds to the nearest millisecond, carrying duration on the tool result message,
the `tool_execution_end` event and `RunToolCall` outcome. Both message shapes carry the field with
its wire name, and the shell `Took` label renders the
recorded value: `ToolRenderContext.DurationMs` reaches it from the live event and from the
session replay, so a replayed result shows the same `Took` it did live instead of measuring
nothing, and a live one no longer counts the steps between the events. A lazy setup failure
stamps its message with the request start time rather than the failure time
(`ai/models_runtime.go`). The print/JSON event serializer also carries
`durationMs` on `tool_execution_end`, including a measured zero; it omits the
field only when the tool never ran.

Ported as well: OSC 7501 program status (`tui/programstatus.go` for the encoding, the
query and the reply match, the terminal's support detection with `PI_PROGRAM_STATUS=1|0`
and the clear/restore around `Stop`, and `coding/interactive`'s reporter for runs,
compaction and the settle outcome). `SetBlocked` also marks a login blocked while it waits
for the user, and a session switch resets the reporter so the previous session's status does
not linger.

Also ported from the delta: the MCP session `DELETE` reuses the token of the last request
instead of asking the auth provider, whose `Token()` call can refresh over the network before
a close that is happening anyway (`mcp/transport_http.go`, upstream #10565). A canceled OAuth
token refresh now returns cancellation rather than falling through to a new authorization redirect
(`f10993bc7`); OAuth UI cancellation and its login timeout remain outside D185.

Ported as well: rotated OAuth tokens persist when a refresh is cancelled (`bde882c74`).
`refreshStoredOAuthCredential` (`ai/authresolve.go`) lets the caller's cancellation stop only
the wait for the credential lock; once a refresh starts it completes under the 15 s timeout and
is persisted, and the request-auth and model-list-refresh paths share it. The caller still
observes the abort, matching upstream's `raceWithAbortSignal`.

Also ported from v1.1.0: bash output keeps incomplete trailing CSI/OSC sequences across
chunks (bounded to 256 bytes) before ANSI stripping (`27c7b6ff4`), and the interactive
lifecycle treats `ENOTTY` as a dead-terminal error for revoked ttys (`4c6b724e`). MCP OAuth
dynamic registration derives `application_type` from redirect URIs, preserving an explicit value
(`147b50281`, MCP SEP-837). Bedrock Converse maps OpenAI GPT reasoning to nested
`reasoning.effort` and gpt-oss to clamped `reasoning_effort` (`2989eb581`). OpenAI OAuth
login accepts `AuthInteraction.AgentName` for ChatGPT's `agent_name_hint` and Codex's
`originator`, defaulting to the existing names (`9ad083102`). MCP connection shutdown cancels
and awaits an in-flight connect (`8c911797c`). The faux provider's prompt-cache estimate
compares equal messages whole and characters only from the first differing message, with
unchanged usage numbers (`fe11328b0`). The extension manager's background UI actions and
in-place enable/disable attempt fencing from `2db5e359b` remain out of scope with `/mcp` UI (D185).

The Home/End mapping matches the v1.1.0 reference (`#10314`, `6100fe5a8`):
Home/End move the editor cursor, while Ctrl+Home/Ctrl+End move the fullscreen
transcript. The component and fullscreen-renderer goldens record that mapping.
The v1.2.0 keybinding table retains this mapping.

Ported the Termux clipboard read (upstream `592fb57b7`, #10391): Termux
reports `GOOS` "android", not "linux", so the paste command selection checks
`TERMUX_VERSION` before the platform gate and the failure hint names the
Termux:API package. The `durable` delta is also audited
against v1.1.0 (`abe508e1`); `origin/main` at `f1b2e77` has no later durable source
changes. Ported the initial system-message ordering fix (upstream #10542,
`92216ada1`) and order-aware table scans/cursors (upstream #10546,
`4dd2af42c`). Context ranges
incrementally extend scanned and derived views within invocations and persist
per conversation across tasks with configurable idle retention (`68ccef176`,
`da866ada1`). Conversation and TaskRuntime context accept an `at` entry cutoff
(upstream #10512, `76f6c06da`, `636703a0a`). Task records stamp wall-clock
`startedAt` at first run and `endedAt` at terminal transition, with injectable
clock defaulting to current time (upstream #10549, `36a686ee8`); durable tool and
hook APIs expose `models` (upstream #10395, `b0114ef5f`). Progress commits now
use configurable partial and output intervals (upstream #10357, `674d64f09`).
Bounded directory readers, direct argv execution and stream-tagged output are
ported (upstream `4748c627a`), as are output-window metadata and skipped
output accounting (`cdf79797b`). `OSShell` emits every chunk, matching upstream
`env/node.ts`; optimized environments may omit text and report skip counts. The
PowerShell tool is ported (`68c22123b`). `FileSystem.watch` uses native events
to trigger snapshot diffs where supported, with polling fallback; Windows and
Android poll (upstream `a84510819`). The read tool uses a positional
binary reader and fixed-size line scans, so it no longer materializes entire
files before truncating output. Go cross-stream multiplex
ordering is preserved. The 21-commit protocol/client/server range was audited: only changelogs
and package manifests changed (including a dev-only `shx` downgrade); no source
or tests changed, so no behavioral gap was found. Audited AI commits `ce8972a0e`
and `6b5854454`: they add the OpenAI Decisions classifier, classifier image
inputs, and a TypeScript-only empty-catalog type fix. Pier has no classifier
execution API and intentionally drops classifier-role catalog entries, so this
is an unported optional classifier feature, not a chat API behavior gap. The
17-commit v1.1.0 `packages/env` delta adds a separate optional `@earendil-works/pi-env`
package:
a Rust daemon and SSH deployment/client for
`RemoteExecutionEnv` on Durable. No v1.1.0 coding-agent runtime consumes it.
Pier's `durable.ExecutionEnv` and injectable `coding.BashOperations` are seams,
not SSH support. Classify remote execution as an unported optional feature, not a
behavior mismatch in existing local execution; port it only as a separately
scoped feature. Shared durable changes such as `cd60a5b99` are covered by the
durable audit above. The remaining package deltas (chord, client, protocol,
server, telemetry, evals) changed only changelogs, package manifests and the
evals install script.

The v0.99.1→v1.0.0 delta (62 commits) is ported: it released the experimental
`durable` packages and landed MCP and codemode work (codemode stays out of
scope; MCP and `durable` are ported). The v1.0.1 (`a7229ddc`) → v1.0.2
(`cd32f772`) delta's applicable runtime and catalog changes are ported:

- `ai` (catalog): regenerated from the v1.0.2 generator, which adds the
  models.dev pricing tiers for Amazon Bedrock models (`4665fafb4`, the
  long-context tier above 272k input tokens), the Cloudflare Clef classifiers
  (classifier-role entries, which the chat catalog drops) and the dashed
  Cloudflare AI Gateway Claude IDs (`c10bfb0d7`). `scripts/gen_catalog.py`
  normalizes the gateway IDs so a regen from older data cannot reintroduce the
  dots; a test pins them.
- `ai`/`coding`: per-thinking-level sampling parameters (`76dfb88f6`, #9776).
  `ai.Model.SamplingParamsByThinkingLevel` and `ai.ResolveSamplingParams`
  (model defaults, then the clamped thinking level, then the request keys)
  feed the OpenAI-compatible adapters (`BuildOpenAICompletionsParams`,
  `BuildOpenAIResponsesParams`), and a summary-only Responses/Azure request
  resolves the medium level. `models.json` gains the field with the
  seven-level schema (`off`..`max`), the per-level override merge, and the
  custom-model pass-through.
- `durable`: provider session identities (`70eceaade`). `durable/harness_provider.go`
  ports `harness/provider.ts`: the `pi.provider` document (kind `pi.provider`,
  latest history, initial fork) initializes to a UUIDv7, the built-in
  conversation-creation hook writes it (`harness.go`), the view mounts it, and
  `EnsureProviderSessionID` returns the persisted id or lazily backfills a
  legacy conversation in one commit. Generation and compaction requests forward
  it as the model request's session id for prompt-cache affinity.
- `coding`/`ai`: small fixes. `--models` drops empty entries (`9b3c19da5`), so
  a trailing comma no longer adds a substring-match-everything pattern; the
  NVIDIA default is `nvidia/nemotron-3-ultra-550b-a55b` (`49b9df489`); and the
  retryable-provider-error pattern accepts `model is at capacity` (`3874b3e98`).
- `ai` (Bedrock): stale-thinking-block replay (`69f0be6f0`).
  `SupportsThinkingBlockBinding` covers Opus 4.7/4.8/5, Sonnet 5 and Fable 5;
  `BuildBedrockAdditionalModelRequestFields` adds
  `thinking.block_binding.prefix_mismatch_behavior = drop_block` and the
  `thinking-binding-controls-2026-08-01` beta for those adaptive models (Opus
  4.6 and Sonnet 4.6 reject it; GovCloud is skipped).
- `ai` (OpenAI Codex OAuth): the held-callback-port handling stays the paste
  fallback of `openai-codex.ts` (port 1455 is shared with the Codex CLI). The
  `eeac84ca9` "fail on port in use" change applies to the separate
  `openai-chatgpt.ts` ChatGPT-subscription flow, which this port does not
  implement, so it is not carried over to the Codex provider.
- `ai` (OpenAI ChatGPT subscription OAuth): the `openai-chatgpt` flow
  (`auth/oauth/openai-chatgpt.ts`): dynamic per-login client registration
  (`client_id=dynamic_agent_client` + `ext_agent_host_id` from the
  installation UUID), the callback's issued client id used for the token
  exchange and refresh, the direct-token scope check, and the credential's
  `clientId`/`scopes` keys. `ai.OpenAIChatGPTOAuth` is attached to the `openai`
  provider next to its api-key auth; `coding` gains
  `SettingsManager.GetOrCreateDeviceID` (global `deviceId`) and the login wiring
  forwards it through `AuthInteraction.GetDeviceID`. This closes the port's
  missing OpenAI ChatGPT flow. The built-in `OpenAIProvider` uses the
  Responses adapter, matching `providers/openai.ts`; both `Stream` and
  `StreamSimple` post to `/v1/responses`. The former Chat Completions adapter
  sent valid ChatGPT OAuth tokens to `/v1/chat/completions`, which OpenAI
  rejected with `rejected_by_access_enforcement` / `no_matching_rule` (401).
  `TestOpenAIProviderUsesResponses` pins both public streaming paths.
  `BuildOpenAIResponsesParams` also follows `isChatGPTSignIn` in
  `api/openai-responses.ts`: direct ChatGPT OAuth requests omit
  `prompt_cache_options`, `prompt_cache_retention`, `max_output_tokens`, and
  `temperature`. This includes compaction's `cacheRetention=none` request;
  sending explicit cache options there caused a 400 `invalid_parameter`.
  API keys and proxy endpoints retain their controls, and ordinary cached
  requests retain their session prompt-cache key. The credential-specific
  regression tests live in `ai/openai_responses_oauth_test.go`.
  The `error` stream event follows the openai SDK's flattening
  (`lib/responses/ResponseStream.ts`): first-party OpenAI nests the payload as
  `{"error":{...}}`, so the nested message becomes the displayed error instead
  of an empty `Error Code :`; flat events fall back to their own message. A
  subscription-limit message also gains the ChatGPT usage URL, matching
  `api/openai-responses.ts`.
- `ai` (GitHub Copilot OAuth): `GitHubCopilotOAuth()` existed but was never
  attached, so the provider offered only api-key auth. `GitHubCopilotProvider`
  now wires it, and a new `TestBuiltinProviderOAuthSurface` pins every
  provider's OAuth presence and flow name so a lost or added flow fails.
- `ai` (Anthropic mid-conversation tools): inline tool definitions
  (`b271b0a52`). Native tool changes now use the `inline-tools-2026-09-15` beta
  (`InlineToolsBeta`): the request-level list stays fixed at the initial tools
  plus the deferred placeholder, and later tools are defined by value in
  `tool_addition` blocks (`tool_definition`), so a same-name redefinition no
  longer falls back to resending the full tool list (the matching
  `tool_removal` is skipped). `HasToolRedefinitions` is deprecated.
- `coding` (MCP config): project `mcp.json` overrides (`1387af7b4`). A project
  entry without `command`, `url` or `type` merges with the global server of the
  same name and may set only `enabled`, `exposure` and `toolExposure`; the
  entry keeps its global scope and records the overriding project path
  (`McpServerEntry.Override`), and `LoadedMcpConfig.ProjectConfig` carries the
  trusted project's `mcp.json` path. A base-less or extra-key override is a
  load error.
- `coding/interactive`: copy the OAuth sign-in URL (`ced72c2f0`). The new
  `AuthUrlComponent` replaces the login dialog's raw OSC 8 lines: it renders
  the URL plus a click hint and an `app.message.copy` hint, and `Copy` puts the
  URL on the clipboard (off-loop, reporting the outcome in the hint). The
  dialog clears it on a device-code or details step. (The MCP-extension sign-in
  UI that also uses it stays out of scope with the extension system.)
- `coding/interactive`: the `/changelog` command (upstream
  `handleChangelogCommand`), which was missing: it renders every changelog
  entry (newest first, links normalized) between dynamic borders, and
  `changelog` is in `BuiltinSlashCommands` for autocomplete. (`/share` and
  `/bug` remain out of scope with the upload transport, so the
  `maybeSuggestBugReport` hint stays out of scope too; its
  `maybeShowInstallChangeWarning` companion is inert for a compiled binary that
  embeds its code, as upstream documents for the Bun binary.)
- `coding`: the `terminal.trueColor`/`terminal.hyperlinks` capability overrides
  were ignored. `SettingsManager.GetTerminalCapabilityOverrides` (upstream
  `getTerminalCapabilityOverrides`) resolves them, `cmd` forces truecolor before
  the theme boot's capability stage and passes hyperlinks to the app, and
  `ThemeBoot.SetTrueColorOverride` applies it. Hyperlinks also default to the ported
  environment detection (`coding.HyperlinksSupported`, upstream
  `detectCapabilitiesFromEnvironment`), which `cmd` installs into the renderer
  through `tui.SetTerminalCapabilities`; before that, nothing wrote the capability
  state, so the OSC 8 branch was unreachable and the answer was always the legacy
  `text (url)` form. The image override is inert like every other image path
  (D26/D140).
- `mcp` (OAuth client ID metadata documents, `1499466d8`): the generic
  `mcp/oauth` layer — `OAuthClientMetadataDocument`, the provider's
  `ClientMetadataDocument` chooser, the document's redirect URI threaded through
  the authorization and token requests, and the callback server's
  `ExtraPaths`/per-waiter path matching, with a stale-timeout identity guard
  for Go's concurrent timer callbacks (D247).

Not ported from the v1.0.1/v1.0.2 delta, with the reason:

- `ai` catalog docs (`221cbcb02`) and the Together model-ID set change
  (`28eaccb8e`, whose new ID the regenerated catalog already carries).
- `coding`/MCP sign-in (`1499466d8`): the `clientRegistration: "cimd"` config
  field and the `/mcp` sign-in UI are not ported; the port connects MCP servers
  with stored tokens and has no sign-in UI (the extension boundary), so they
  have no reachable effect. The generic `mcp/oauth` layer above is ported.
- `coding`/extensions (`11449730c` tool-renderer resolvers): the port has no
  extension system, and its MCP tools carry no renderers, so the resolver has
  nothing to resolve.
- `tui`/images (`672000c80` WezTerm scroll, `a276dabe5` non-PNG Kitty): the
  kitty/iterm image transport internals are documented out of scope.
- `coding`/easter eggs (`7fbbd5f4a`, `1c1e9c0ef`, `c450f2c0f`): the 3D Armin/pi
  logo animation is cosmetic, and the port keeps the existing half-block
  easter eggs rather than the lazy 3D renderer.
- npm and Nix build tooling (`581e7ba78`, `0495646a8`, `9fba660cf`, `c71a496ff`,
  `83692682f`), codemode output capping (`319fecb89`) and the changelog/docs
  commits.

Ported from the v0.99.1→v1.0.0 delta so far (the rest is still pending):

- `tui`/`coding/interactive`: configurable fullscreen wheel scrolling with auto
  acceleration (`f1927c2d5`). `WheelScrollLines` (`"auto"` or a fixed 1..100)
  and `WheelScrollAccelerator` live in `tui/wheelscroll.go`; `AltScreen` uses
  the accelerator per wheel event with the five-times Alt multiplier and gains
  `SetWheelScrollLines`; the `fullscreenWheelScrollLines` setting is global-only
  (clamped 1..100) and its row sits above Theme in `/settings`. Ported tests:
  the upstream `wheel-scroll.test.ts` cases and the runtime-update case from
  `tui-alt-screen.test.ts`.
- `tui`/`coding/interactive`: the system theme (`bf8e4b953`). `tui/oklab.go`
  ports Oklab/OKHSL/OKLCH; `coding/interactive/systemtheme.go` generates the
  theme's colors from the terminal's reported foreground, background and
  16-color palette; `tui.QueryTerminalColors` performs the OSC 10/11/4 burst
  ended by a DA1 request; the theme registry lists `system` first, the selector
  puts it above Automatic, and the controller probes the terminal once and
  regenerates the active system theme. `tui.ParseColor`/`OklchToRgb` let theme
  JSON use `oklch()` and `okhsl()` values, and `ThemedText` (plus the startup
  header's `ExpandableText`) rebuilds its text on invalidation so baked-in
  colors recolor. The generator is verified byte for byte against the upstream
  TypeScript over the `system-theme.test.ts` terminals. The no-color tier's
  faint (`dim`) tokens render with SGR 2 (`Theme.dimTokens`, closed by
  `\x1b[22m`), matching the upstream `renders faint tokens with SGR 2` case.
  Departure (**D193**): when the terminal reports nothing, the port fills the
  `filledBackgroundColors` panels from `pierSignalPanelHex` so tool calls, user
  messages and custom-message frames keep their signals; the generator and the
  golden stay upstream-parity.
- `tui`/`coding/interactive`: color values and theme styling (`567469096`).
  `packages/tui/src/colors.ts` becomes `tui/colors.go`: the `Color` value
  (palette index, sRGB, OKLCH) with `ParseColor`, `ColorToRgb`/`ColorToHex`/
  `ColorToOklch`/`ColorToOkhsl`, `MixColors`, and `ForegroundAnsi`/
  `BackgroundAnsi`/`StyleText`. Theme JSON accepts `#rgb`, `oklch()` and
  `okhsl()` values and an optional `appearance` (detected from the theme's own
  colors; a palette-only theme follows the terminal background), `theme.style`
  combines tokens or concrete colors with attributes, and `theme.colors`
  exposes concrete colors, filling `""` tokens from the terminal's reported
  defaults (OSC 10/11). `tui.ParseColor` accepts numeric palette indexes as well
  as color strings, while theme JSON retains the tagged `ColorValue` form. The
  boot uses upstream's truecolor environment detection (`COLORTERM`, `-direct`,
  and recognized terminal families), then `PI_TRUE_COLOR`, then the configured
  `terminal.trueColor` override. The injectable capability setter (D69) remains
  available for callers and tests. The combined OSC 10/11/4 query applies the
  background to automatic theme selection, while the input router also delivers
  OSC 11 to background listeners during the query, so delayed background-only
  replies still update the auto theme. The default `system` theme's
  accent family uses Pier amber while retaining terminal-derived lightness,
  contrast and appearance (D219).
- `coding`: `defaultTools` accepts `+name`/`-name` entries (`30a1d1849`). A
  modifier-only list layers on the inherited selection (`mergeDefaultTools`), so
  a project `["-codemode","+tool_search"]` applies to the global
  `["read","bash","+codemode"]`, and `["+codemode"]` alone applies to the
  built-in defaults; a list with any plain name replaces them, and an explicitly
  empty list selects nothing (`resolveDefaultTools`). The built-in list is the
  exported `DefaultToolNames`. Tests: the `settings-manager.test.ts` modifier and
  layer cases plus the `default-tools-setting.test.ts` modifier case (the port's
  `grep` stands in for the extension tool, which the port does not have).
- `coding`: a new session file is created at the first user or assistant message
  (`ff72faba2`), so a first turn that never finishes still keeps the prompt on
  disk and the session in `/resume`; setup entries alone (model, thinking level)
  stay in memory, so starting and quitting without chatting leaves no file. The
  branch/fork path uses the same `hasConversation` rule, so the two cannot drift
  and write the header twice. Tests: the `file-operations.test.ts` "session file
  creation" cases.
- `coding/interactive`: the vendored HTML-export templates are re-synced to the
  pinned v0.99.1 (`b2bd111f2`; the port had the 0.86.1 files). Custom messages
  with `display:false` export hidden by default behind an `H` toggle and an
  `aria-pressed` header button, and navigating to such an entry reveals them.
  The template also carries the codemode `nestedCalls` renderer, inert here
  (no codemode). Tests: `TestExportHiddenMessageToggle` (upstream adds none).
- `ai`: the builtin model catalog is regenerated from the pinned v0.99.1.
  `gen_catalog.py` now flattens the schema-6 role-prefixed provider data,
  keeping only the chat catalog and dropping chat-less providers (the classifier
  provider `typesafe` stays out). It adds `gpt-6.1-sol` and the newer
  pricing/context metadata. `DefaultModelPerProvider` follows the pin:
  `openai-codex` → `gpt-6.1-sol`, `fireworks` →
  `accounts/fireworks/models/kimi-k3`, `together` → `moonshotai/Kimi-K3`,
  `opencode-go` → `kimi-k3`. Tests:
  `TestDefaultModelPerProviderTracksCurrentModels`.
- `coding/interactive`: the system theme keeps pastel palettes pastel
  (`409e808f5`): a palette color carries its OKLCH chroma along with its OKHSL
  channels, and `anchored` caps chroma at the source's times the same falloff,
  so moving a color to another lightness never gains chroma. Lightness and hue
  are unchanged. The system-theme golden is regenerated from the pinned
  generator (the driver now lives beside it,
  `coding/interactive/testdata/gen_systemtheme_golden.mjs`). Tests: upstream's
  "keeps pastel palette colors pastel at other lightnesses" case
  (`TestSystemThemePastelPalettesStayPastel`).
- `ai`: an unparseable `Retry-After` falls back to the exponential backoff
  (`2bbfcca43`; Go's `ParseFloat` accepts `NaN`/`Infinity` without error), and
  the Z.AI CN endpoint's `Prompt exceeds max length` overflow message is
  detected, while the first pattern regains the upstream
  `prompt (?:is )?too long` shape the port had collapsed (`3dd803d7e`).
- `cmd`: `--provider` without `--model` fails with an error instead of silently
  running another provider's default, and the `--provider` help line drops the
  outdated `default: google` (`0c453048b`).
- `coding/interactive`: the user message's Markdown pads and colors its own
  background, dropping the Box that kept a second full-width copy of every
  rendered line with identical output (`e792ba131`).
- `tui`: the Markdown's parsed token list is held weakly, so a transcript
  message's tokens are collected between renders (`54c19a252`). The
  `flattenLines` half of that commit is a V8 string-representation concern with
  no Go equivalent (D180).
- `mcp`: the protocol layer, transports (in-memory, stdio, streamable HTTP),
  the client, and the oauth module are ported (`mcp/`, `mcp/protocol/`,
  `mcp/oauth/`), with Go fixture ports for the stdio tests and the full
  oauth.test.ts port. The exit hook watches terminating signals and
  re-raises (D182); the resource-contents arm is discriminated by key
  presence (D181).
- `coding` (MCP integration): the config half of `core/mcp-servers.ts` and
  `extensions/mcp/config.ts` (`coding/mcpservers.go`, `coding/mcpconfig.go`:
  mcpServers shape, validation (including WHATWG-compatible HTTP URL normalization, D245), exposure patterns, agent-dir plus trusted
  project `mcp.json`), `extensions/mcp/tools.ts` (`coding/mcptools.go`:
  `mcp__server__tool` names, CallToolResult conversion, 20 KB middle
  truncation, `truncateMiddle` in `coding/truncate.go`) and
  `extensions/mcp/runtime.ts` (`coding/mcpruntime.go`, `coding/mcpmanager.go`:
  connections with lazy reconnect, HTTP connect retries, session-expiry retry,
  the 401 -> needs-auth path, tool/resource list refresh, default
  stdio/streamable-HTTP transports). The CLI starts the connections in the
  background at boot (`NewMcpManagerAsync`), waits once at the first turn for
  the direct-tool servers (bounded by `McpStartupWaitMs`, upstream
  `DEFAULT_STARTUP_WAIT_MS`), attaches the late tools to the live session
  (`AgentSession.AttachExtraTools`, the once-per-session `BeforeFirstTurn`
  hook at `runAgentPrompt`), and `/reload` exchanges the manager for one
  built from the re-read config (`ReloadMCPExchange` + `AttachSession`;
  D195). D185 covers what is not ported: the
  extension registry, codemode and `tool_search` exposure, `/mcp` sign-in and
  status UI, resource reads, and a durable OAuth store.
- `durable`: the port's Storage surface (conversation/entry/task/submission)
  uses upstream's submission record shape (`SubmissionRecord` with its own
  `type`, `SubmissionCreate`/`SubmissionSettlement`, `submission`/
  `submissionByRequest` and the `submission` write kind; the earlier port
  called these records "inputs" and omitted the type) and
  now has a SQLite backend on modernc.org/sqlite (user-approved dependency;
  the upstream sqlite migrations run verbatim and the Storage suite runs
  against memory, SQLite and JSONL). Conversation scans take the upstream
  `ConversationQuery` owner filter. The transaction kernel
  (`session/transaction.ts`) is ported over the synchronous storage:
  conversation create/fork (with the fork document copies), entry append with
  task attribution, task create/set with owner validation and terminal task
  document retirement, submission create/place/settle, document acquire/
  create/retire with the tracker, the base-vs-delta plan with the checkpoint
  predicate, storage-admission assembly and adoption publications. The
  session kernel (`session/session.ts`) is ported as a mutex-serialized mutation
  line: commit callbacks with the transaction host (loaded tracker cache,
  cold-loading and migration on version change), read-on-line derivations,
  snapshots and as-of snapshots, document states and exact-frame watches
  attached to committed incarnations with version-aware operations, commit and
  close listeners, post-admission poisoning, and `unloadDocuments`. Still
  unported: the harness/tools and the remaining upstream conformance cases.
  `ScanSubmissions` (`SubmissionQuery` by conversation and status) is ported;
  the storage conformance suite from `testing/storage-conformance.ts` is
  replayed in Go for the cases the port needs (entry ordering and cursors,
  conversation owner filters and paging, complete task replacement and status
  scans including waiting/completing, submission request indexing and passive
  writes, exact-scope document address scans with prototype-like keys, the
  global ID namespace and exhausted minting at Number.MAX_SAFE_INTEGER).
  The typed helpers are ported (`entries.ts` entry kinds and guards,
  `tasks.ts` DefineTask, the harness `ToolDiagnostic` and `CompactionReason`
  types) and the local execution environment's filesystem half (`env/index.ts`
  + `env/node.ts`: portable `FileSystem`, path resolution, error codes and
  tracked temp cleanup). The shell half is ported too (`OSShell`: shell
  resolution with the Windows Git Bash candidates and `sh -c` fallback, timeout
  validation, direct argv execution, stream-tagged `onOutput`, output spill,
  process-tree kill on timeout or cancellation, and callback-error handling).
  The `tools/` helpers that depend only on the
  environment are ported (`path-utils.ts`: Unicode-space and `@` normalization
  with the meridiem/NFD/apostrophe read-path variants; `file-mutation-queue.ts`:
  per-filesystem, per-canonical-path mutation serialization, with a
  not-yet-created file keyed through its canonical parent). `truncate.ts`
  (bounded head truncation with the two independent limits) and
  `tools/image.ts` (the PNG/JPEG/GIF/WEBP/BMP signature sniffer) are ported.
  `tools/edit-diff.ts` is the same shared engine as the coding-agent's
  `core/tools/edit-diff.ts` (the two upstream files differ only in the
  coding-agent's extra preview helper), so it is already ported in
  `coding/editdiff.go` and the durable tool definitions will reuse it. The
  harness tool API and the tool definitions are unported. The harness
  foundation is started: `json.ts` (leaf-by-leaf assignment), `util.ts`
  (keyed waiters, scan-all, closed error) and `usage.ts` (the `pi.usage`
  document and its typed ledger: counter addition with the optional
  cacheWrite1h/reasoning fields, bucket merging and the commit-time
  `recordUsage`), plus the bounded output layer (`output.ts`: control-character
  sanitization, the exact head/tail byte-or-line slice, the chunked
  `OutputBuffer` with head/tail retention and exact dropped totals, and the
  adaptive `Progress` committer). The task graph is ported too
  (`task-graph.ts`: the session-line mount built by its first observer from
  live task records and owned conversations, advanced from commit publications
  with per-node and conversation-ownership operations, and the disposable state
  and exact-frame watch surfaces). The committed transcript context is ported
  (`context.ts`: bounds captured on the session line with two O(1) reads, the
  active-entry selection around the newest head marker, edits resolved
  newest-first, excluded assistant stop reasons, and tool results ordered after
  their assistant in call order with a synthesized result for a missing one).
  The inbox is ported (`inbox.ts`: the `pi.inbox` queue document, the boundary
  read, the boundary placement of writes/steers/follow-ups with the reset
  promotion and stale-write settlement, item removal, and the queued-input
  withdrawal that keeps writes). Entry records and drafts gained their
  persisted codecs: the message list is encoded per message so concrete roles
  round-trip (`entryRecordJSON`/`entryDraftJSON`), and the draft's `head` is
  the upstream `id | "self"` value.
- `coding`: the remote catalog merge is linear (`c34f2d6ad`); the virtual-model
  selection helpers (`core/virtual-models.ts`) are ported (`coding/virtualmodels.go`:
  `IsVirtualModel`, `FindLatestResponse`, `GetBranchSelection`,
  `GetVirtualModelState`, `CreateVirtualModel`, the route/state types and the
  `pi-virtual`/`pi.virtual-model-state` constants) and the provider wrapper
  (`WithVirtualModels`, through `ai.WrapProviderWithVirtualModels`, so a
  virtual model hides a same-id physical one, filtering keeps virtuals and an
  unrouted stream fails); the branch model resolution still walks its branch
  per submission (`a0660b174`). Virtual-model **routing** itself is not ported:
  upstream resolves the route in `_installAgentRequestProjection`
  (`agent-session.ts:788`, a per-request `prepareRequest` hook that also
  re-projects the transcript, re-reads the model and thinking level, and
  records router state), and the port has no per-request seam. It refreshes at
  turn boundaries instead, which matches for physical models because the loop
  calls `PrepareNextTurn` before every request but the first. Consequences: a
  virtual model streams as unrouted, `GetBranchSelection` has no production
  caller, and the threshold-compaction skip upstream applies to virtual
  selections (`_compactBeforeNextAssistantResponse`) has no counterpart. No
  builtin catalog entry is virtual, so this is dormant until routing lands.
  Dynamic
  provider registration does not exist in the port, so `registerProvider`'s
  provisional configured entry (`fddc968b9`) has no surface. Anthropic
  workload identity federation (`a9424cd43`) is ported at the protocol level
  (`ai/federation.go`): the five `ANTHROPIC_FEDERATION_*` env names,
  `ResolveFederationConfig`, the secure-endpoint and redaction rules, the
  RFC 7523 jwt-bearer exchange against `{baseURL}/v1/oauth/token` with the
  federation beta header, and the per-config token cache (the port has no
  Anthropic SDK, so the exchange the SDK performs is implemented). The
  exchange is wired into the Anthropic request auth: when no key or auth
  header is resolved, the federation env resolves a cached token and the
  request carries `Authorization: Bearer` (the SDK's client-level federation
  config).
- `coding`: `/reload` enables tools newly added to the `defaultTools` setting
  (`db6cc71dc`): the session records whether its initial selection came from
  the setting (`SessionConfig.UsesDefaultTools`, set when neither `--tools` nor
  `--no-tools` was given) and its CLI exclusions, and Reload activates the
  names the re-read selection adds. Removed names stay active, tools disabled
  during the session stay off unless newly added, and explicit
  `--tools`/`--no-tools` sessions never take the path. Tests:
  `default-tools-setting.test.ts` "reload" (the extension tool stands in as the
  built-in `grep`).
- `ai`: an OpenAI Responses stream that completes with a tool call whose
  `output_item.done` never arrived now fails the stream instead of running it
  (`1b2aa0ca0`). Cut-off or mixed-up arguments (for example from a server that
  omits `output_index`, like llama.cpp) no longer reach the tool loop. Tests:
  the `openai-responses-terminal-event.test.ts` unfinished and
  no-`output_index` cases.

v0.87.1 refresh (originally the pinned tag `f07218c4d`, now superseded by the
v0.99.1 reference above, read from the
`./pi` checkout in the workspace — see `AGENTS.md`; the previous pin
`16787ad5b` is the v0.87.0 release):

- `ai`: the stealth Claude Code version is `2.1.280` (was `2.1.251`), and
  `openai-completions` drops empty text parts from a user message that also
  carries an image (#9797). Both ported with
  `TestConvertOpenAICompletionsMessagesOmitsEmptyTextParts`.
- `coding`: `--mode` validates `text`/`json`/`rpc` and reports a missing or
  invalid value instead of silently ignoring it (#9045); the split-turn
  summarization prompt uses Markdown `# Conversation` / `# Instructions`
  boundaries so a reasoning model does not continue the prefix (#9652). Ported
  with `TestParseArgsModeValidation` and
  `TestTurnPrefixPromptUsesMarkdownBoundaries`.
- The model catalog is regenerated from the tag's committed
  `packages/ai/src/providers/data/` (`schemaVersion` 3, `generatedAt`
  2026-09-26) with `scripts/gen_catalog.py`, and the image catalog from
  `image-models.generated.ts` with `scripts/gen_image_catalog.py`. The refresh
  adds the generator's new models (Claude Opus 5.5, GPT-6 Sol/Luna, the Copilot
  entries), the `inclusionai/ming-image-0.1-design` image entry, and the
  per-model `inputLimits` the newer generator emits; `DefaultModelPerProvider`
  moves xai to `grok-4.7`.

v0.87.0 refresh (the previous pin; `36b60d2e8` sat on a divergent upstream line
with no common ancestor):

- Tool renderer parity re-probed against the 0.87 sources (a probe script
  driving the upstream renderer): the edit block is byte-identical to the Go
  golden.
- TUI delta: one renderer fix. `compositeScrollToEndIndicator` now truncates
  the label to the clip width, centers it in the clip, then clamps it to the
  scrollbar column (pre-0.87 centered in the space left of the scrollbar,
  shifting it one cell for an even remainder). Ported with
  `TestScrollToEndIndicator*`.
- The follow-ups this refresh recorded have landed: the coding-agent delta
  (`agent-session.ts`, `compaction.ts`, `session-manager.ts`,
  `prompt-templates.ts`, the new `crash-log.ts`) and the `read` tool's
  model-driven image resize (`resizeInlineImage` in `coding/fileprocessor.go`).
  Extension mechanics and image transports stay out of scope (D41/D140).

Update the pin whenever upstream source is re-read for a port.

## Per-area status (mirrors upstream `packages/`)

| Go package | Upstream | Status |
|---|---|---|
| `ai` | `packages/ai` core (`types.ts`, `utils/event-stream.ts`, `utils/text.ts`, `utils/transcript.ts`, `utils/diagnostics.ts`) | ported |
| `ai`, `agent` (provider stream observer) | `StreamOptions.onProviderStreamEvent` and `AgentOptions.onProviderStreamEvent` (`types.ts`, `agent.ts`, upstream #9901): parsed raw provider events are delivered with their model before normalization across Anthropic, Bedrock, Google/Vertex, Mistral, OpenAI Completions/Responses/Azure/Codex, and pi-messages | ported (extension event dispatch stays out of scope, D41) |
| `ai` (assistant message frames) | `utils/assistant-message-frame.ts` — the compact replayable stream codec: `AssistantMessageFrame` (the `content` start/end union), `AssistantMessageFrameEncoder` (per-block UTF-16 offsets, the tool-call checkpoint/catch-up and JSON-prefix logic), and `ReduceAssistantMessageFrames` | ported |
| `ai` (tool-call ID sanitization) | provider conversions in `api/{google-shared,anthropic-messages,bedrock-converse-stream,openai-completions,openai-responses-shared}.ts` — non-Unicode regex replacement operates on UTF-16 code units, so astral characters produce two replacement characters | ported |
| `ai` | `packages/ai` models/compat (`models.ts` surface, compat interfaces) | ported (types) |
| `ai/providers` (faux) | `packages/ai/src/providers/faux.ts` | ported |
| `ai` (auth) | `packages/ai/src/auth/*` (types, resolve, credential-store, helpers) | ported |
| `ai` (models runtime) | `packages/ai/src/models.ts`, `models-store.ts`, `api/lazy.ts` (lazyStream) | ported |
| `ai` (model types) | the public half of `utils/model-operations.ts` (`getModelType`/`isModelType`, re-exported from `models.ts`) and the `Model.type` field, which the catalog carries and the port previously dropped | ported (the internal `assert*Model` guards are inert in a chat-only catalog) |
| `coding` (nested tool calls) | `core/nested-tool-calls.ts` — `NestedCallLimits`, `NestedCallRecorder` (start/finish/duration, the argument-size and call-count limits, usage summing, snapshot) and the `nestedCalls` field on `ai.ToolResultMessage` | ported (the execution host `NestedToolCallRunner` and the session `ctx.executeTool()` wiring are out of scope: the port has no tool-execution API and their only triggers, codemode and extension tools, are out of scope) |
| `coding` (context edits) | the `context_edit` session entry (`ContextEditEntry`) — `SessionManager.AppendContextEdit` with upstream's validation and assistant/tool-result string normalization, the `replacement` field preserved on load and rewrite, and the projection applying it (a null replacement omits the target, a value replaces its content) | ported |
| `ai` (catalog) | generated model data via upstream's own generator (`scripts/gen_catalog.py`), `env-api-keys.ts`, `utils/provider-env.ts` | ported |
| `ai` (anthropic wire) | `api/anthropic-messages.ts` request construction (`buildParams`), `transform-messages.ts`, `simple-options.ts`, `constrained-sampling.ts`, `utils/{json-parse,estimate,sanitize-unicode}.ts` | ported |
| `ai` (google-vertex) | `api/google-vertex.ts` — Vertex AI dialect over the shared Google streaming loop: `{location}`-templated and custom base URLs with API-version detection, project/location resolution with upstream's error messages, Vertex API keys (including the ADC marker and `<placeholder>` fallback), Application Default Credentials (authorized-user refresh-token and GCE metadata flows with token caching; service-account keys reported per D27), bearer + `x-goog-user-project` headers, and the Vertex thinking budget/level mapping | ported |
| `ai` (azure-openai-responses) | `api/azure-openai-responses.ts` — Azure dialect over the shared Responses loop: base-URL resolution (options → env → resource name → model base URL) with Azure-host normalization to `/openai/v1`, query stripping only on Azure hosts, `v1` default API version, the `api-version` query parameter, `api-key` header auth, deployment-name resolution (explicit → `AZURE_OPENAI_DEPLOYMENT_NAME_MAP` → model id) sent as the body model, and the Azure tool-call provider set | ported |
| `ai` (mistral) | `api/mistral-conversations.ts` — the native Mistral chat completions endpoint: SDK-to-wire key remapping (maxTokens/toolChoice/promptMode/... and per-message/per-chunk keys), message conversion (string user content, image data URLs with omission notes, assistant thinking parts and tool calls, tool-result text forms), function tools with strict sampling, deterministic 9-character tool-call id normalization, prompt caching via `prompt_cache_key` plus the `x-affinity` header, the CRLF-tolerant SSE event reader with the `[DONE]` sentinel, streaming assembly of text/thinking/tool-call blocks with incremental argument parsing, cached-token usage accounting, reasoning effort vs prompt mode per model, and upstream's error formatting with body truncation | ported |
| `ai/providers` (factories) | `providers/{all,thin factories,opencode-headers,github-copilot,google-vertex,azure,mistral,meta}.ts` — the built-in provider list in upstream order with per-provider base URLs, names, and env API-key auth (29 thin specs plus the anthropic/google/vertex/openai/azure/mistral/copilot/meta factories), the OpenCode session-header wrapper, the GitHub Copilot OAuth available-model filter, a `builtinProviders`/`builtinModels` aggregate, and spec/catalog api consistency | ported (all built-in provider factories, including Meta's Muse OAuth) |
| `ai` (cloudflare) | `providers/{cloudflare-auth,cloudflare-stream}.ts` + the endpoint constants in `api/cloudflare.ts` — per-field credential/env merging for the API key plus account/gateway ids (a key-only credential still picks those up from the environment), the Workers AI key auth vs. the AI Gateway `cf-aig-authorization` form with the SDK placeholder headers suppressed, the login prompts, `{CLOUDFLARE_ACCOUNT_ID}`/`{CLOUDFLARE_GATEWAY_ID}` base-URL materialization before dispatch (cloning only when the URL changes), and both providers registered with the gateway's three-API map pinned | ported (the Workers AI binding fetch is Workers-runtime specific and out of scope) |
| `ai` (oauth) | `auth/oauth/{pkce,device-code,oauth-page,anthropic}.ts` — PKCE (S256) generation, the RFC 8628 device-code polling loop with `slow_down` interval handling and the upstream cancel/timeout/clock-drift messages, the OAuth callback pages (logo, escaping, success/error), the loopback callback server (fixed port, state validation, success/error pages, cancel), manual-paste parsing (URL, `code#state`, query string, bare code), the Anthropic Claude Pro/Max flow (authorize URL, code exchange, refresh with the five-minute expiry margin, upstream error messages, api-key derivation) wired into the Anthropic provider's OAuth slot | ported |
| `ai` (oauth, device code) | `auth/oauth/{xai,kimi-coding,meta}.ts` — RFC 8628 device-authorization flows over the shared poller: the xAI flow (form POST, `referrer=pi`, https-only verification URIs, interval 0 fallback, non-rotated refresh tokens, upstream failure messages per error code), the Kimi Code flow (host override envs, trusted http(s) verification URIs, 30s request timeouts, raw failure text in messages, refresh with exponential backoff, terminal 401/403/invalid_grant handling), and the Meta Muse flow (device authorization against auth.meta.com, the identity token exchanged through the Muse Code key-mint endpoint with the identity token stored as the refresh token and the minted key as the access token, so a 401/403 from mint reports the session dead), all wired into their providers' OAuth slots; non-streaming auth requests carry a default 60s bound when the caller has none (D255) | ported |
| `ai` (oauth, openrouter) | `auth/oauth/openrouter.ts` — the OpenRouter PKCE flow: `callback_url` authorize URL, a one-shot loopback server on an ephemeral port with a per-login callback path, 404/409/denied/no-code responses, the JSON key exchange (`code_verifier`, `code_challenge_method`) with a 30s timeout and body-derived error details, the permanent-key credential (no refresh, max expiry), manual-paste handover racing the callback, and upstream's progress/auth-URL notifications | ported |
| `ai` (oauth, github copilot) | `auth/oauth/github-copilot.ts` — the Copilot flow: enterprise-domain prompt/normalization, the GitHub device flow (client id, `read:user`, https-only verification URIs), the Copilot token exchange with the `proxy-ep` -> `api.` base-URL derivation and five-minute expiry margin, the account model catalogue (picker flags, policy states, tool-call support, the Individual-account policy fallback limited to real catalog ids), best-effort policy enabling with a 429-aware retry budget, available-model merging into the credential, and `toAuth` returning the credential-specific proxy base URL | ported |
| `ai` (retry) | `utils/retry.ts` + `utils/provider-retry.ts` — the agent retry policy (`baseDelayMs`/`maxAgentDelayMs` with the 60s default, `retryDelayMs` with its jitter), the `RetryCallbacks` surface and `retryAssistantCall` (attempt budget, abort normalisation to an aborted `AssistantMessage`, exhaustion), the retryable and non-retryable error classifiers, and the provider-request retry (`retryProviderRequest`, including network errors that correspond to SDK `APIConnectionError`, JavaScript `parseFloat` numeric-prefix handling for server retry headers, server-delay validation, the max-delay override) | ported (D171: the retryable-error list also matches OS socket aborts and Go HTTP/2 peer resets, which upstream's list omits) |
| `ai` (proxy env) | `utils/node-http-proxy.ts` (`getProxyForUrl`/`resolveHttpProxyUrlForTarget`, the `no_proxy` parser, the per-provider `env` lookup) and its Bedrock/Codex callers | not ported (D240: Go's `http.ProxyFromEnvironment` resolves the process proxy environment; a provider-scoped proxy override and upstream's `no_proxy` parser are replaced by Go's `httpproxy` semantics. The Codex caller is the out-of-scope WebSocket transport, D28; the Bedrock caller only selects HTTP/1 plus proxy agents, and the port is already HTTP/1.1) |
| `ai` (oauth, openai codex) | `auth/oauth/openai-codex.ts` — the ChatGPT OAuth flow: the authorize URL (extra `id_token_add_organizations`/`codex_cli_simplified_flow`/`originator` params), PKCE + random hex state, the fixed-port loopback server whose listen failure hands over to manual paste, the WHATWG-tolerant JWT decoding used to require a `chatgpt_account_id` claim into the credential, token exchange/refresh with the upstream operation-labelled messages, and the device-code path (string-or-number interval, 403/404 and `deviceauth_authorization_pending`/`slow_down` polling, nested `error.code`) with the device redirect URI | ported |
| `ai` (radius) | `providers/{radius-config,radius}.ts` + `auth/oauth/radius.ts` — gateway URL normalization, the strict catalogue validator (model shape filtering, thinking-level maps), the `/v1/config` loader with body truncation, the dynamic provider (default-gateway baseline, per-provider catalogue cache, stored-entry restore, legacy credential import, network refresh with persisted check timestamps), and the OAuth flow (discovery endpoint, browser sign-in with PKCE/state/`handoff=url` on the fixed loopback port, device-code sign-in with the coded error mapping, refresh, api-key derivation) | ported with the pi-messages API adapter wired |
| `ai` (pi-messages) | `api/pi-messages.ts` — pi's own message protocol: the `{model, context, options}` POST to `<baseUrl>/messages` (context rendered through the canonical message marshaller), optional `debug=1`, bearer + SSE + identity `User-Agent` headers, header overrides, the SSE reader (CRLF folding, `data:` lines, `[DONE]`), the event converter assembling text/thinking/tool-call blocks with streaming argument parsing and terminal done/error payloads (usage, response id, provider thinking level), rewrite-impact diagnostics, the coded response error with structured diagnostic details (provider, model, url, status, error object or truncated body), the legacy `PI_CACHE_RETENTION` mapping, and the missing-terminal-event error | ported |
| `ai` (images) | `image-models.ts`, `api/openrouter-images.ts`, `providers/openrouter-images.ts` + the generated image catalogue — the image-generation counterpart of the model runtime (provider auth/model listing/refresh with shared in-flight fetches, generation that resolves auth and merges explicit options per field, error results instead of rejections), the OpenRouter images API (chat completions with `modalities`, data-URL extraction with remote URLs skipped, cache-write/read usage accounting and per-million cost), and `builtinImagesProviders`/`builtinImagesModels` | ported (the catalogue comes from upstream's generated data evaluated with Node's type stripping) |
| `ai` (bedrock plumbing) | AWS SigV4 request signing (canonical request, derived signing key, session tokens, verified against the AWS documentation vector), credential/region/endpoint resolution (explicit env credentials, `~/.aws/credentials`/`config` profile parsing, bearer tokens with `AWS_BEDROCK_SKIP_AUTH`, ARN-embedded regions, standard-endpoint detection and the explicit-endpoint rule, GovCloud detection), Bedrock error formatting with the stable exception prefixes and data-retention hint, failure diagnostics, reserved-header rules, stop-reason mapping, and the AWS event-stream binary decoder (prelude/message CRC validation, typed headers, varints) | ported with the Converse adapter wired |
| `ai/providers` (extension providers) | the user's pi provider extensions ported as providers — Charm Hyper (`hyper`: https://hyper.charm.land/v1, `HYPER_API_KEY`, openai-completions, the 11-model catalog with the deepseek thinking format) and Command Code (`commandcode`: https://api.commandcode.ai/provider/v1, `COMMAND_CODE_API_KEY`, Claude models over anthropic-messages from the base URL minus `/v1`, the seed catalog minus the extension's unavailable/dominated ids) | ported (`ExtensionProviders()` registered alongside the built-ins, D140) |
| `ai` (bedrock-converse) | `api/bedrock-converse-stream.ts` — the Converse adapter: message conversion (text/image/tool-call/thinking blocks with signature replay, redacted reasoning as opaque bytes, the placeholder rules for empty content, consecutive tool results collapsed into one user message, cache points on the system prompt and last user message), tool configuration with strict sampling and forced choices, thinking request fields (adaptive vs budget with the GovCloud display omission, custom budgets, interleaved-thinking beta), the request payload with model/inference/system/tool/metadata fields, the SigV4-or-bearer request, the event-stream fold (messageStart validation, text/thinking/tool-call block assembly with streaming arguments, stop reasons, usage with the one-hour cache split and cost), model predicates (Claude detection, adaptive thinking, native xhigh, prompt caching incl. the force-cache override), and the Bedrock auth (bearer token, AWS profile, credential chain) | ported |
| `ai` (openai-codex) | `api/openai-codex-responses.ts` (SSE transport) — the Codex Responses dialect: ChatGPT account-id extraction from the OAuth token, the request body (instructions instead of a system message, verbosity, encrypted-reasoning include, tool choice/parallel calls, reasoning effort/summary with the model's off mapping, prompt cache key), the Codex headers, the usage-limit friendly message and retry classification with `retry-after`/`retry-after-ms` bounds, the codex event mapping (error/failed events surfaced, terminal statuses normalized to the Codex vocabulary, `end_turn` preserved) feeding the shared Responses processor, the flex/priority service-tier precedence and pricing, and the provider factory. **All upstream providers are now ported** (D28: the WebSocket transport is not; the default `auto` uses SSE, and an explicit `websocket` request is rejected) | ported |
| `agent` | `packages/agent/src/{agent-loop.ts, types.ts, stream-fn.ts}` — the loop, tool execution (sequential/parallel), steering/follow-up queues, terminate rules, validation (D6 subset) | ported |
| `agent` (Agent) | `agent.ts` — stateful wrapper: state, subscribe, steer/follow-up queues (one-at-a-time default), continue semantics, run-failure lifecycle, reset baseline | ported |
| `agent` (proxy) | `proxy.ts` — `StreamProxy`/`ProxyStreamOptions`: a stream function that POSTs the model, normalized transcript and serializable options to `{proxyUrl}/api/stream` with a bearer token, reads the `data:` SSE events (partial stripped) and rebuilds the assistant message (text/thinking/toolcall start/delta/end, done/error), failing a clean EOF without a terminal event | ported |
| `agent` | the loop and its harness: agent state/queues (steering + follow-up with modes), tool execution (sequential and concurrent modes with per-call lifecycle events), prompt/continue flows, event fan-out with listener settlement, retry classification hooks, compaction/context/execution harness pieces, and the durable run journal integration | ported |
| `coding` (tools) | `core/tools/{truncate,path-utils,file-mutation-queue,read,write,ls}.ts` | ported (read offsets/limits preserve JavaScript numeric and `Array.slice` behavior, including fractional and negative values; text reads apply UTF-8 replacement semantics; new writes and directories use Node's default modes before umask; `ls` uses Unicode collation after lowercasing, per upstream `localeCompare`) |
| `coding` (edit) | `core/tools/{edit.ts, edit-diff.ts}` — exact/fuzzy matching, overlap detection, CRLF/BOM preservation, diffs | ported |
| `coding` (search) | `core/tools/{find.ts, grep.ts}` — real fd/rg backends, gitignore respect, relativized output, match/context formatting, fractional fd limits and grep limits/context behavior, UTF-8 replacement in paths and context lines, and stopping rg at the match limit | ported (D244: find result paths keep leading/trailing whitespace, where upstream's line trim breaks such file names) |
| `coding` (bash) | `core/tools/bash.ts` + `output-accumulator.ts` — bounded-memory streaming with TextDecoder UTF-8 replacement and initial-BOM behavior, private exclusive 0600 temp-file full output, tree-kill, 128+N signal mapping, finite positive timeout validation with the exact 2,147,483.647-second cap message, timeouts, aborts, PI_* env handling | ported (D161: the port sweeps its own `pi-bash`/`pi-output`/`pi-powershell` temp files down to a byte budget instead of leaving them to OS aging; the 100 ms streamed-update throttle emits inline under its own lock, so the reader and the throttle timer cannot reorder or re-enter the sink) |
| `coding` (sandbox removal) | `core/tools/{bash,edit,write}.ts` and `core/sdk.ts`: tools use ordinary OS permissions without a built-in sandbox. The port-local Landlock launcher, write/edit guards, permission modes, `/permissions`, footer codes, and sandbox prompt section are removed | parity restored; no platform-specific read-only fallback |
| `coding` (sessions) | `core/session-manager.ts` — v3 JSONL trees, branching, compaction context, migrations, discovery/listing; `core/messages.ts` custom roles | ported (session directories/files use Node's 0777/0666 defaults before umask and new session files are created exclusively; D210: off-loop writes record failures and recover the accepted prefix instead of throwing on the UI owner loop; `FlushWrites` returns unresolved failures and `DrainWriteErrors` hands out diagnostics) |
| `coding` (prompt) | `core/system-prompt.ts` (structured sections, forced prompts, section diffing) + `core/skills.ts` + `utils/frontmatter.ts` — skill discovery (including directory and file symlinks, case-insensitive `.gitignore`/`.ignore`/`.fdignore` rules, and top-level Markdown skill files), path resolution (`~`/relative), canonical-real-path dedupe and name-collision diagnostics, plus the `available_skills` prompt block. Skill and prompt-template frontmatter preserve quoted-versus-plain scalar types for consumed fields, recognize YAML boolean variants and inline comments, use JavaScript trimming and UTF-16 length limits, and reject non-string descriptions or hints. Discovery also covers the `.agents/skills` locations (`core/package-manager.ts` `collectAncestorAgentsSkillDirs`): the user `~/.agents/skills`, and the project's `.agents/skills` from cwd up to the git repository root when the project is trusted (the rest of the package manager stays out of scope). The prompt/tool loadout is installed as a system message on each prompt and between turns (`preparePromptAndToolLoadout`), with tool snippets/guidelines filled from the registry | ported |
| `coding` (compaction) | `core/compaction/{compaction.ts, utils.ts}` — projection-aware cut points and token estimates, split-turn handling, retry-wrapped summaries with reserve-based token budgets, file-operation tracking for nested tool calls, and `core/messages.ts` `convertToLlm` | ported (manual and automatic compaction use the same projection rules as `SessionManager.Projection`. Projection-guarded overflow recovery keeps the failed assistant response and tool results in history, omits them from model context, and retries once. The refreshed projection supplies post-compaction messages and the estimated context size. Only the stock summarizer is built in; the extension is out of scope, D41) |
| `coding` (session facade) | `core/agent-session.ts` (extension-free reduction: persistence, queue tracking, auto-compaction/retry, stats, skill blocks) + `core/defaults.ts` — the activity state (`IsStreaming`/`IsCompacting`/`IsIdle`) derives from the run flag and the compaction/branch-summary abort controllers, matching upstream's `get isCompacting` | ported |
| `coding` (powershell + registry) | `core/tools/powershell.ts` (PowerShell variant of the shared shell tool: `-NoProfile -NonInteractive -ExecutionPolicy Bypass -Command` args, UTF-8 console prefix, PS> prompt, pi-powershell temp files, Windows-only exec failure), `core/tools/index.ts` (tool names, `createTool`, coding/read-only/all tool sets), `utils/shell.ts` (bash resolution incl. legacy-WSL stdin transport and Git Bash discovery, PowerShell resolution preferring pwsh, binary-output sanitizing) | ported |
| `coding` (model resolution) | `core/model-resolver.ts` — default models per provider, exact reference matching (bare id or canonical provider/model, ambiguous bare ids rejected), fuzzy partial matching preferring aliases over dated ids, colon-aware thinking-level parsing (OpenRouter-style `model:exacto:high`), scope resolution with glob matching and structured diagnostics, CLI model resolution (provider inference from `provider/model`, authenticated-provider tie-breaks for ambiguous ids, provider/model split preference over gateway ids, custom-id fallback with `:thinking` stripping), initial model priority chain, and session model restoration; `utils/shell.ts`-style glob semantics in `coding/glob.go` | ported |
| `coding` (settings) | `core/settings-manager.ts` + `core/http-dispatcher.ts`'s timeout choices/parsing (the configured timeout is applied per streamed request as a headers and body idle timer, D40/D254) — the full settings document (all upstream fields), deep merge (nested objects merge, arrays/primitives replace, undefined skipped), the four legacy migrations (queueMode, websockets, skills object, retry.maxDelayMs), ordered set-aware JSON writing, locked file storage (proper-lockfile semantics and Node default creation modes before umask) and in-memory storage, scoped managers with global/project trust, reload, overrides, recorded errors, modified-field persistence that preserves externally added keys, and the complete getter/setter surface (models, thinking levels, compaction token resolution, retry, HTTP idle timeouts, editor, shell, resources, terminal/TUI, markdown, warnings). The JSON field schema is single-sourced: the typed writer derives its key names and declaration order from the `Settings` struct's `json` tags, so a field cannot be added without becoming persistable (a manually maintained serializer had silently dropped `cacheWarming`) | ported |
| `coding` (cli + json events) | `cli/args.ts` (full flag parser: value flags, shorthands, `--` separator, @file args, unknown-flag capture for extensions, thinking-level validation diagnostics, session-name normalization, help text), `cli/auth-command.ts` + `cli/auth-check.ts` + `cli/credential-print.ts` (the `pi auth check|print-api-key|print-bearer-token` subcommands with `--json`/`--credentials`/`--no-refresh`/`--min-expiry`, credential/store-type filtering, and the ready/not_ready/invalid exit codes), and `modes/json-event.ts` (one shared event shaper for the map API and stdout serializer; streaming `message_update` events omit the outer cumulative message and inner partial, toolcall_start is enriched with id/toolName, cumulative usage is preserved, and event-specific payloads retain required zero/false/empty values; upstream-generated byte goldens cover every session event type and all assistant stream variants) | ported |
| `coding` (context files, prompt files) | `core/resource-loader.ts` context discovery + system-prompt files + `core/footer-data-provider.ts` git path resolution — the SYSTEM.md (replace) and APPEND_SYSTEM.md (append) prompt files (`resolvePromptInput`'s text-or-file sources, the trusted project's `.pi/` file winning over the agent dir's, `--system-prompt`/`--append-system-prompt` taking precedence, several appends joined with a blank line), listed in the loaded-resources Context section — AGENTS.override.md/AGENTS.md/AGENTS.MD/CLAUDE.md/CLAUDE.MD preference per directory, directory candidates skipped, BOM stripping, global agent file first then ancestors outermost-to-innermost with path dedup, and the nested-worktree shadow rule (commondir/worktree detection, bare-layout and submodule exclusions) | ported |
| `coding` (prompt templates) | `core/prompt-templates.ts` + `core/source-info.ts` — bash-style command argument parsing (including the implementation's literal-backslash quote behaviour), the full placeholder vocabulary ($N, $@, $ARGUMENTS, `${N:-default}`, `${@:-default}`, `${@:N}`, `${@:N:L}`) with non-recursive substitution, template loading from global/project/explicit paths (markdown only, symlink-to-file resolution, broken symlinks skipped), frontmatter description/argument-hint with the first-line truncation fallback, source info (user/project/temporary, top-level origin), and `/name args` expansion | ported |
| `coding` (file args + mime) | `cli/file-processor.ts`, `utils/mime.ts`, and the inline-image limits from `utils/image-resize*.ts` — @file argument expansion (path resolution, empty files skipped, missing/unreadable files erroring, BOM-stripped text wrapped in `<file name>` blocks), image sniffing (JPEG with the JPEG-2000 marker rejected, PNG with IHDR validation and animated-PNG detection, GIF, WebP, structurally validated BMP), and inline-image preparation (MIME normalization, byte-identical pass-through within the 2000x2000/4.5MB limits, conversion hints, dimension notes, upstream omission messages) and a pure-Go bilinear resizer, so an oversized image is scaled to the limits instead of being omitted, and the EXIF orientation of JPEG/WebP input applied before scaling (`utils/exif-orientation.ts`) (D26: Photon's Lanczos3 resampling and its webp/bmp decoding are not available to the port) | ported |
| `coding` (print mode) | `modes/print-mode.ts` + `modes/json-event.ts` (extension-free core) — JSON mode writing the session header (with the file's `"type":"session"` discriminator) before one JSON line per event with the cumulative `partial` stripped from `message_update`, text mode printing the final assistant message's text blocks, the error/aborted stop-reason path reporting to stderr with exit code 1, piped stdin folded into the first prompt (`readPipedStdin`), sequential prompts, prompt-failure exit handling, and signal shutdown that cancels the active operation, disposes resources, drains accepted session writes and flushes output before returning 129/143; ordinary returns also unregister and join the signal waiter | ported (either non-TTY stream selects text print mode, with explicit `-p`/`--mode json` retained; `@file` images attach only to the first prompt in both interactive and print modes; D41: extensions/runtime rebind skipped) |
| `coding` (session runtime, complete) | the remaining core surface: agent-session state/control (tools, queue modes, model/thinking management, scoped models, auto toggles, bash recording), the prompt pipeline (expansion, streaming behaviors, auth validation, deferred messages), the auto-retry loop with the post-run continuation, automatic compaction (overflow/threshold/recoverable-length, with projection-guarded recovery), branch summarization + navigateTree, session bash execution + manual compaction, cache warming (economics, safety windows, persisted usage), the model runtime + registry facade (including request-context forwarding into auth resolution), config-value resolution, provider attribution + runtime credentials, remote catalog overlay, auth.json storage + models store, HTML entity decoding + prompt/attachment and tool-result image normalization, JSONL export + the bug report (redaction, metadata, diagnostics, summary) + crash log, project trust, git branch/deprecation helpers, and slash commands | ported |
| `coding`, `cmd` (stdio RPC) | v1.3.0 `modes/rpc/{rpc-mode,rpc-types,jsonl}.ts` supported subset: prompt/steer/follow_up/abort/clear_queue, state/messages, model/thinking selection, queue modes, session stats/entries/tree/fork messages/last assistant text, session naming, `get_commands`, compaction and retry controls, bash/abort_bash, `export_html` through an injected host renderer, and session replacement (new_session/switch_session/fork/clone via `RPCModeOptions.ReplaceSession`), plus the typed subprocess client in `coding/rpcclient`; correlated JSONL responses plus shared event streaming, auth-preflight cancellation, EOF/signal teardown and accepted-save draining | partial (D235: four pending preflights, safe field validation, cancellation-aware byte pumps, no startup dialogs; extensions/UI remain deferred; see `docs/rpc.md`) |
| `coding` (out of scope) | exportToHtml (needs the interactive TUI theme resolution chain), the resource loader / sdk / agent-session services and runtime (extension mechanics, D41), the package and tools managers; RPC's implemented subset and remaining gaps are tracked above; interactive TUI status is tracked below | out of scope |
| (removed) `chord`, `chord/{delta,services,facets}`, `client`, `protocol`, `server`, `server/testing`, `telemetry`, `durable` | ported through the pinned tags, but unreachable from the built CLI (`go list -deps github.com/dat267/pier`): the framed-CBOR chord transport and server, the chord state/delta/services library, the telemetry span contracts, and the durable session/harness kernel (SQLite, JSONL and memory storage, scheduler, built-in tasks, coding tools) | removed (D236: not wired into the binary; preserved on the `unwired-packages` branch. The detailed porting entries these rows carried are historical) |
| `tui` (foundation) | `@earendil-works/pi-tui` src/utils.ts — grapheme-cluster segmentation (the UAX #29 rules the width math depends on: controls, Extend/ZWJ, SpacingMark AM vowels, Prepend, Hangul jamo and syllables, paired regional indicators, Indic linkers, and emoji ZWJ sequences), terminal-cell width (CJK/fullwidth double via the Unicode east-asian-width tables, emoji presentation including modifier and tag sequences, tab = 3, ambiguous = 1), terminal-sequence extraction (CSI/OSC/APC with BEL and ST terminators) and stripping, and the width cache | ported (the `Extended_Pictographic`, emoji-modifier and RGI ZWJ sequence tables are generated from the reference: Unicode 17.0 as exposed by Node 26's ICU 78, the runtime the differential runs against) |
| `tui` (line primitives) | src/utils.ts — `truncateToWidth` (contiguous-prefix kept-width rule, ellipsis budget, padding only when the text fit), `sliceByColumn`/`sliceWithWidth` (visible-column extraction with pending-ANSI prefixing), `extractSegments` (before/after with inherited styling at the boundary), `AnsiCodeTracker` (SGR attribute/256-color/RGB state, OSC 8 hyperlinks with their original terminator, line-end reset), `wrapTextWithAnsi` (token-based wrapping with style carry-over, long-token breaking, CJK breaking), `applyBackgroundToLine`, `normalizeTerminalOutput` (Thai/Lao AM decomposition, tab expansion) | ported (D167: a slice whose start sits on a style reset flushes the carried prefix before an in-range code, so the reset is not reordered ahead of the colour it cancels — a double-clicked word inside inline code no longer turns the rest of the line yellow) |
| `coding` (version check) | `utils/version-check.ts` — the release check: the request with its timeout and user agent, the semver valid/compare subset (prerelease ordering, build metadata ignored), and the rule that only a strictly newer version notifies | ported (D174: the check reads this module's GitHub releases rather than pi's feed or the Go module proxy, `pier update` installs the release asset, and a build whose version is not semver gets no card at all) |
| `coding` (headless stdout) | `core/output-guard.ts` — `takeOverStdout`/`restoreStdout`, the raw-stdout tail with its `ENOBUFS`/`EAGAIN` retry, and `writeRawStdout`/`waitForRawStdoutBackpressure`/`flushRawStdout` | ported in effect (D241: no `os.Stdout` patch; `printStdout`'s mutex plus Go's blocking writes give output ordering and backpressure, and headless diagnostics go to stderr directly. `cmd` writes the session-id warning to stderr in every mode) |
| `coding/interactive` (trust/crash/autocomplete helpers) | the last in-scope interactive-mode helpers — `renderProjectTrustWarningIfNeeded` (the trusted/trust-requiring-resources guard and the warning line), `promptForMissingSessionCwd` (the confirm dialog and fallback cwd), `handleFatalRuntimeError` (the error line, crash record, theme-watcher stop, mode stop and exit), `recordCrash`/`crashReportInstructions`/`suggestBugReport` (the crash-log write, the `pi -r` resume hint and the once-only bug hint), `maybeSaveImplicitProjectTrustAfterReload`, and the mode-level autocomplete assembly (`createBaseAutocompleteProvider` with the `/model`/`/thinking`/`/login` argument completions, the prompt-template and skill commands, and `setupAutocompleteProvider` with the wrapper trigger-character merging and editor installation) | ported (D131: the process-level effects and the resource-loader/extension collaborators are injected seams; also makes the input loop prefer a delivered input over cancellation) |
| `coding/interactive` (loaded resources) | `showLoadedResources` — the startup `[Context]`, `[Skills]` and `[Prompts]` sections (collapsed lists; expanded project/user/path scope groups over `getShortPath`/`buildScopeGroups`/`formatScopeGroups`), the `[Skill conflicts]`, `[Prompt conflicts]` and `[Theme conflicts]` warnings/collisions blocks, and the ctrl+o expand toggle driving the header and section expandables | ported (prompt/theme read warnings and first-wins collisions show even during quiet startup, diagnostic paths carry upstream source labels, custom themes carry user/project/temporary SourceInfo, and the port's extra Themes listing was removed (v1.2.0 lists only Context/Skills/Prompts plus out-of-scope Extensions); extensions stay out of scope, D41/D140) |
| `coding/interactive` (interactive-mode: remaining) | the last interactive-mode methods are not ported and are listed here so the scoreboard stays precise: the extension-UI context assembly and the extension shortcut/markdown-transform/custom-editor hooks (extension mechanics, D41), the remaining resource-display sections (`formatDiagnostics`/`findSourceInfoForPath`/`getDisplaySourceInfo` and the Extensions body) which depend on the package manager's `SourceInfo` (D41) — The Context, Prompts and Themes bodies are ported, Themes without per-theme `SourceInfo`, so its entries group under the local/project scope. The clipboard file-path, image and text/right-click paste flows are ported (D106) | remaining (documented) |
| `coding/interactive` (share + bug report) | `session-share.ts` and the orchestration of `bug-report.ts` — the `pi.share` trailing entry (system prompt + tool schemas), the branch export for sharing, the Radius upload with the credential/gateway/loader and abort handling, the private-gist fallback (the `gh` auth check, the gist creation and the viewer URL), and the `/bug` flow (the consent/summary prompts, the loader around the model-written summary, the bundle build, the upload with the zip fallback and the archive export) | not ported (out of scope: the port has no upload or gist transport, so `/share` and `/bug` are absent from `BuiltinSlashCommands` and the submit dispatch; the JSONL export and crash log they build on are ported) |
| `coding/interactive` (terminal color queries) | `terminal-colors.ts` plus the TuiBase query surface — the OSC 11 background-color response detection and parsing (6-digit and 12-digit hex, `rgb:`/`rgba:` slash forms), the `CSI ? 997` color-scheme report parsing, the renderer's `QueryTerminalBackgroundColor`/`QueryTerminalColorScheme` round-trips with their pending-query/timer bookkeeping, the color-scheme listener registry, the `CSI ? 2031` notification toggle and the OSC 11/report input consumers | ported (also fixes D129: custom session entries serialize their payload as `data` like upstream, and D130: the serialized branch header carries `type: "session"`) |
| `coding/interactive` (command handlers) | the remaining command handlers of `modes/interactive/interactive-mode.ts` — the `/export`/`/import` path parsing (quoted and bare arguments) with the JSONL-vs-HTML branch, the confirm flow, the missing-cwd prompt and the import error handling; `/share` and `/bug` (injected flows); `/copy` (the alt-screen selection preference, the last-assistant-text fallback, the flash vs status); `/name` (the usage warning, normalization warning and confirmation); `/session` (the full info panel: file/id, message counts, the prompt/cached/uncached token split with the hit rate, cache warming mode/status/economics, the cost breakdown per model and the cache re-billed line); `/changelog` (the reversed, link-normalized entries); `/hotkeys` (the full keybinding table with the Windows newline note); `/new`; `/debug` (the rendered-line widths plus the message JSONL written to the debug log, reachable from the renderer's global debug key as well — upstream points `ui.onDebug` at `handleDebugCommand`); `/compact`; the armin/Earendil/daxnuts easter eggs with the OpenCode Kimi K2.5 trigger; and `stop` | ported (D125: the runtime/clipboard/report collaborators are injected, D41 for the extension seams) |
| `coding/interactive` (run orchestration) | the `init`/`run` half of `modes/interactive/interactive-mode.ts` — the init orchestration (signal registration, the renderer mount, the UI start, the compact/expanded startup header with the logo and the keybinding-derived instruction lists (upstream's onboarding line is dropped: pier ships no docs tree, D176), the key/submit handler enablement, the session rebind, the initial-message render, the theme/branch watchers, the provider count and the background grammar load), the run orchestration (the offline-gated catalog refresh, the release and tmux checks — both reported on the UI loop, and the release check reading this module's own proxy feed, D41 — the startup diagnostics in order, the migrated-provider/models.json/fallback warnings, the crash notice, the Anthropic warning, the initial-message prompts and the main input loop) and the chat helpers (`clearEditor`, `showError`, `showWarning`, the new-version card with the optional hyperlink) | ported (D124: the collaborators are injected function values; the extension/resource seams stay out of scope, D41) |
| `coding/interactive` (startup + orchestration helpers) | the startup helpers of `modes/interactive/interactive-mode.ts` — `getUserInput` (the pending-input queue and the waiter callback), `rebuildChatFromMessages`, `renderCurrentSessionState` (the container/streaming/pending-tool reset), the tmux keyboard check (the extended-keys/extended-keys-format queries with the 2s timeout and the warning wording), the changelog display logic (the resumed-session skip, the fresh-install version recording — the install-telemetry ping itself is not sent, D41 — and the new-entry selection with the versioned link normalization), the markdown theme with the code-block indent setting, the available-provider footer count (scoped models first) and the Anthropic subscription-auth warning (the setting gate, the OAuth/API-key checks and the once-only guard) | ported (the terminal/process collaborators are injected; startup input is handed to the owner loop (D143); also adds `coding.GetChangelogPath` with a mutex-guarded package dir) |
| `coding/interactive` (lifecycle) | the lifecycle half of `modes/interactive/interactive-mode.ts` — the renderer mount/stop (`mountInteractiveTui`, `stopInteractiveTui` with the fullscreen transcript replay) and the mode switch (the overlay guard, the render-state capture/restore, the component/focus/terminal/cursor/clear-on-shrink preservation, the progress restore), the terminal title (session name + cwd basename), the shutdown paths (the graceful path with the theme-auto-sync disable, the input drain, the runtime dispose and the resume command; the signal path that disposes before touching the terminal), the ctrl+C double-press window, ctrl+D, the requested-shutdown hook, the emergency dead-terminal exit, the uncaught-crash terminal restore with the crash report, the ctrl+Z suspend with the Windows guard, and the signal/terminal-error/uncaught-exception handler registry | ported (D122: the process-level effects (exit, signals, terminal title, stdout/stderr) are injected so the lifecycle is testable; the extension/resource collaborators remain seams, D41) |
| `coding/interactive` (auth wiring) | the login/logout half of `modes/interactive/interactive-mode.ts` — the login provider listing (oauth/api-key filtering, the configured-status derivation, the sorted output), the stored-credential listing, the `/login` routing (the exact provider reference, the single/multi auth-type cases, the auth-type picker, the provider list), the `/logout` flow with the runtime logout and the credential-synchronization error message, the auth prompt/event mapping onto the login dialog (select/manual-code/text prompts with the abort race, auth-url/device-code/info/progress events), the OAuth and API-key dialogs (including the amazon-bedrock details and the ambient-auth info dialog), and `completeProviderAuthentication` (the deferred-selection rule, the default-model lookup with the radius fallback, the failure guidance for missing defaults/models, the status/error split and the post-refresh re-check), plus the generic `ExtensionSelectorComponent` | ported (D120: the runtime/collaborators are injected and the auth-select dialog is a seam, D41; D156: the flow runs off the UI loop and the dialog marshals its own mutations) |
| `coding/interactive` (model + session selector wiring) | the model/session half of `modes/interactive/interactive-mode.ts` — `showModelSelector` (the default-model reference, the select/select-as-default paths with the provider-count/editor-border updates and the statuses, the dispose wiring), `showModelsSelector` (the configured-pattern resolution with the no-match diagnostics, the session-vs-settings enabled ids, the scope application rules, the persist normalization to the all-enabled case, and the background catalog refresh with the 15s timeout, the cached-models fallback and the per-provider error status) and `showSessionSelector`/`handleResumeSession` (the current/all loaders, the rename flow, the session switch with the missing-cwd prompt and the resumed/cancelled statuses) plus `SessionManager.ListAllSessions` | ported (D118: the runtime/session-switch collaborators are injected; also fixes D119: the scroll-view transient-scrollbar hide delay now defaults to 1000ms like upstream, and the scoped-models selector is state-locked against the background refresh) |
| `coding/interactive` (settings wiring) | the `showSettingsSelector` half of `modes/interactive/interactive-mode.ts` — the settings snapshot assembly (the default-model reference, the current/available models, the image/steering/transport/cache-warming/thinking/theme/editor/TUI/fullscreen/warning rows from the settings manager and session) and the full change-callback table: the session mutations (auto-compact, steering/follow-up, cache warming, per-model thinking with the current-model application and the global-default revert), the settings writes with their side effects (image rows updating the tool components, skill commands rebuilding autocomplete, the HTTP idle timeout showing the formatted status (the value reaches the wire per streamed request as a headers and body idle timer, D40/D254 — the port has no dispatcher to reconfigure), theme set/preview through the controller, hide-thinking with the transcript update, cache-miss notices rebuilding the chat, hardware cursor/clear-on-shrink on the renderer, editor padding/autocomplete on both editors, output padding on the live components or the rebuild, TUI-mode switching with the rejection reset, fullscreen scrollbar/copy-on-select, warnings) | ported (D117: the session/theme/renderer/chat collaborators are injected through narrow interfaces and function values) |
| `coding/interactive` (selector wiring) | the selector half of `modes/interactive/interactive-mode.ts` — the editor-replacement selector slot (`showSelector`/`disposeActiveSelector` with the token-guarded done callback, the focus switch and the editor restore), and the selector command wiring: the thinking selector and `/thinking` command (level matching, unknown-level error, select/save-as-default), the fork-from-message selector (the last-message preselect, the runtime fork with the cancelled path and the editor text restore), the project trust selector (the trust-store decision persistence and the restart status) and the tree selector (the leaf no-op, the summarization prompt flow with the re-show on cancel, the streaming abort, the compaction guard and the navigate result statuses) | ported (D116: the fork/navigate/dialog collaborators are injected function values and the summarization dialog is a seam that skips the prompt when unwired, D41) |
| `coding/interactive` (key wiring + submit dispatch) | the `setupKeyHandlers`/`setupEditorSubmitHandler` half of `modes/interactive/interactive-mode.ts` — the key wiring (the escape handler chain: streaming queue restore, bash abort, bash-mode exit and the double-escape tree/fork action with its 500ms window, plus the app.clear/exit/suspend/thinking/model/tools/external-editor/copy/follow-up/dequeue/session action registrations and the editor onChange bash-mode detection) and the submit handler (the full slash-command dispatch chain in upstream order with the argument parsing for `/model`, `/thinking`, `/bug`, `/login`, `/compact` and friends, the `!`/`!!` bash-command path with the already-running warning, the compaction queueing, the streaming steer path and the normal submission with the bash-component flush, input callback and history recording) | ported (D115: command handlers and clipboard helpers are injected function values; D41: the extension-command branch is absent) |
| `coding/interactive` (queue + editor actions) | the queue/editor-action half of `modes/interactive/interactive-mode.ts` — the queued-message model (the session steering/follow-up queues merged with the compaction queue, the pending-message container with the truncated steering/follow-up lines and the dequeue hint, the clear/restore-into-editor flow with the abort option and the dequeue statuses, the compaction queue with its history recording and the flush paths for the retry/idle/all-extension cases plus the restore-on-error behaviour, and moving the pending bash components into the chat) and the small editor actions (the tool-output expansion toggle across the header and the expandable chat children, the thinking-block visibility toggle with the settings persistence and the assistant re-render, the editor border color for bash/thinking mode, and the thinking-level/model cycling statuses) | ported (D114: the extension-command check is always false, D41) |
| `coding/interactive` (event dispatch) | the `subscribeToAgent`/`handleEvent` half of `modes/interactive/interactive-mode.ts` — the agent/turn lifecycle (pending-tool reset, the retry-escape restore, the working indicator with the terminal progress toggle), the message lifecycle (user/custom rendering, the streaming assistant component with incremental updates and tool-call component sync, the aborted/error result synthesis with the retry-attempt message and the bug-report suggestion, the thinking-drop/cache-miss notices), the tool-execution lifecycle (start/partial/end with the pending-tool map), the entry-appended routing (custom entries, cache-warm usage), the compaction lifecycle (the abort escape handler, the aborted/error/result paths with the chat rebuild from the session context and the cost notice), the auto-retry and summarization-retry status indicators, and the `agent_settled` shutdown hook | ported (D113: collaborators are injected function values; also adds the missing `entry_appended`/`summarization_retry_*`/`agent_settled` session events and their emissions, and the status-indicator interface so retry/compaction/branch-summary indicators share the UI state) |
| `coding/interactive` (transcript rendering) | the transcript half of `modes/interactive/interactive-mode.ts` — `addMessageToChat` for every message kind (user with the spacer rule, the skill-block split into the skill component plus the user message, history population; assistant; bash execution with output/exit/cancel/truncation/full-output-path; compaction and branch summaries; custom messages gated on display; system/tool-result no-ops), `renderSessionItems`/`renderSessionEntries` (the entry projection with the compaction/branch-summary cost-notice injection, the custom/usage entry routing, the tool-call components with the abort/error result synthesis and the pending-tool map, the tool-result matching, the cache-miss injection and the footer invalidation), the cache-warming/compaction-cost/cache-miss notices with their thresholds and labels, the dropped-thinking diagnostics notice, the consecutive-status collapsing and the managed-tool status lines, and `renderInitialMessages` (D159: the panel's whole-session work runs off the UI loop, and its statistics, cache-waste accounting and usage breakdown — plus the rebuild's cache-miss accounting — are folded from per-entry records the manager keeps) | ported (D112: the extension entry/message renderers are seams that return nil, D41; the tool renderer registry is injected) |
| `coding/interactive` (extension-UI state machine) | the extension-UI surface of `modes/interactive/interactive-mode.ts` — the status-indicator lifecycle (the working/retry/compaction/branch-summary indicators, editor-border embedding with the working-status-editor detection and thinking-level border colors, the status-container fallback, kind-filtered clearing and the idle-status restore for regular mode with clear-on-shrink, the working visibility/message/indicator options and the hidden-thinking-label propagation), the extension widgets (string-array wrapping with the 10-line truncation note, above/below-editor placement, replacement disposal, the leading/empty spacers and the per-placement containers), the custom footer/header swapping (factory construction, expansion state, index-preserving header replacement), the terminal-input listener registry and the reset surface | ported (D111: extension factories are function values; the extension runner itself stays out of scope, D41) |
| `coding/interactive` (custom editor + interactive-mode helpers) | `components/custom-editor.ts` plus the helper layer of `modes/interactive/interactive-mode.ts` — the custom editor (app keybinding routing for interrupt/exit/paste-image/extension shortcuts/history, the action-handler table, and the working-status top border with the overflow label, spinner fallback and width fallbacks) and the pure helpers (expandable text, the working-status-editor and expandable guards, dead-terminal codes, the Anthropic subscription-key and unknown-model predicates, shell quoting, the resume command with the session-dir/id arguments, the llama.cpp post-login guidance, the login-provider completion dedup/auth-type merge/search text/description, and the fuzzy autocomplete item mapping) | ported (D109: the editor's top-border override is a field hook instead of a subclass override; also fixes D110: an explicit empty loader message no longer becomes "Loading...") |
| `coding/interactive` (footer + core deps) | `components/footer.ts`, `core/footer-data-provider.ts`, `utils/changelog.ts`, `core/telemetry.ts` — the footer (working directory with the git branch and session name, the token/cache/cost stats from all session entries including usage/compaction/branch-summary records, the latest cache hit rate, the subscription marker, the context percentage with its error/warning thresholds and the auto-compact and experimental markers, the model/thinking right side with the provider prefix rule and the truncation ladder, and the alphabetically sorted extension status line) plus the footer data provider (git metadata discovery, sync branch resolution with the reftable `.invalid` fallback and detached HEAD, extension statuses, provider count, branch-change subscriptions, cwd switching and disposal) and the changelog utilities (versioned link normalization with the legacy-repo/floating-ref/relative-path rules, section parsing, version comparison and new-entry selection) | ported (D107: the footer watcher polls HEAD instead of fs.watch; D108: the footer takes a narrow FooterSession interface; **D141: the builtin footer renders the user's footer-extension format instead — one dim line `3.5%/1M · <statuses> · model · cwd` — because upstream's per-render session-entry walk needs a json.Unmarshal per message per frame in Go (O(session size) per frame, 445ms on a 10k-entry session); the upstream helpers FormatTokens/FormatCwdForFooter keep their behavior** |
| `coding/interactive` (renderer composition) | `modes/interactive/tui-renderer.ts` plus the `tui.TUI` surface it returns — `createInteractiveTui` selecting the regular/fullscreen renderer with the fullscreen search styles (underline/inverse over the search-match palette), the scroll-to-end indicator (keybinding-derived shortcut over the selected background) and the injected browser/clipboard hooks, and the stable renderer reference that follows a renderer swap | ported (D105: upstream's ES Proxy becomes an explicit forwarding reference over a `TUI` interface; D106: clipboard copying is injected since the native helper is out of scope) |
| `coding/interactive` (tree selector) | `components/tree-selector.ts` — the session tree navigator: the flatten/reflatten indentation and connector/gutter algorithm (branch points shift, virtual roots, single-child chains), the filter modes (`default`/`no-tools`/`user-only`/`labeled-only`/`all`) with the search tokens, assistant tool-call-only suppression and the folded-descendant pruning, the visible-tree rebuild with nearest-visible-ancestor attachment and the ancestor/descendant navigation (fold/unfold, branch segment jumps), the horizontal viewport (gutter pinning and selected-anchor panning), the entry display text for every entry type (messages incl. tool-call formatting, custom messages, compaction, branch summaries, model/thinking changes, custom, labels, session info), the label editing flow with timestamps, and the search line and keybinding help | ported (also ports `SessionManager.GetTree()` with the resolved labels and timestamp-sorted children; fixes the `app.tree.filter.default` default key from `ctrl+f` to `ctrl+d`, D104) |
| `coding/interactive` (session-switch runtime) | `core/agent-session-runtime.ts` `switchSession`/`newSession` + `rebindCurrentSession` — opening the target session, the missing-cwd error that the resume flow can prompt on, teardown (abort, dispose, unsubscribe), a fresh `CreateAgentSession` around the opened manager, the shared `*AppSession` pointer swap that rebinds every wiring, the `SessionInfo` holder updates, the event-pump re-subscription, the footer invalidation and the transcript rebuild | ported (extension lifecycle events are no-ops, D41; `app.switchSession`/`app.sessionNew` wired to the `/resume` selector and the `app.session.new` keybinding) |
| `coding/interactive` (session selector) | `components/session-selector.ts` — the resume selector: the header (scope title, the sort/name/scope segments, the loading progress, the delete-confirmation and status lines, the path state and the key hints), the session tree (parent-path linking, subtree latest-activity ordering, the `│  `/`├─ `/`└─ ` prefixes) and the list (name/age/message-count columns, control-character stripping, the current-session/named/confirming colors, the selected-row background, scrolling, search-driven flat mode, the named filter, path display, the delete confirmation with the current-session guard and the Ctrl+Backspace alias, and rename mode), plus the component plumbing (scope/sort/name toggles, the abortable loaders with progress, the delete/rename mutations and the status messages) | ported (D100: AbortController becomes a context; D101: the status timer is injectable; D102: the session-file deleter is injectable; D103: loader results use a settle helper and now apply on the owner loop after the state mutex was retired; D158: a stacked layout below 60 columns for narrow terminals) |
| `coding/interactive` (settings selector) | `components/settings-selector.ts` — the full `/settings` list (auto-compact, steering/follow-up modes, transport, HTTP idle timeout, cache warming, hide thinking, Mermaid, cache-miss notices, changelog, quiet startup, telemetry, project trust, double-escape, tree filter, warnings, per-model thinking, TUI mode, fullscreen output/scrollbar/copy-on-select, theme; the Show-images and Image-width rows appear only when the terminal supports inline images), the search-enabled list with the callback dispatch, the warnings submenu, the theme submenu (single vs automatic mode, light/dark selection, preview callbacks, the `light/dark` setting), and the stepped per-model thinking submenu (current-model/default-model ordering, reasoning levels, `(clear override)`) | ported (D97: the settings list maps display positions back to the stored items so filtered activation mutates the real item; D98: the catalog refresh coordinator unpublishes finished/cancelled refreshes before signalling, so a later caller starts a fresh refresh) |
| `coding/interactive` (skill/setup/announcement/easter eggs) | `components/{skill-invocation-message,first-time-setup,earendil-announcement,armin}.ts` — the skill block component (collapsed one-liner with the expand hint and the expanded label plus markdown), the first-time setup dialog (the logo, theme step with preview callbacks, the analytics step, the navigation/confirm/cancel bindings), the announcement card, and the armin 31x36 XBM half-block grid with its seven effect state machines (typewriter, scanline, rain, fade, crt, glitch, dissolve), ticked on the owner loop through `tui.Animator`, and the `ARMIN SAYS HI` footer; the port also keeps the daxnuts 32x32 RGB reveal as an extra (upstream removed it at this pin, `1c1e9c0ef`), driven by the same seam | mostly ported (D94: the bundled announcement image asset is not shipped, so only the surrounding spacer renders; D95: the easter-egg randomness uses an injectable source. The first-time setup dialog is built but never shown: upstream gates it on `PI_EXPERIMENTAL=1` plus the official distribution and a missing `settings.json`, which the port does not implement.) |
| `coding/interactive` (session search, auth dialogs) | `components/{session-selector-search,oauth-selector,login-dialog}.ts` — the session search (regex `re:` mode with error handling, quoted-phrase tokenization with the unclosed-quote fallback, fuzzy token matching, the name filter, and the recent/relevance sorting with the modified-time tie-break), the auth provider selector (the login/logout modes, the conditional auth-type labels, the status indicators with the env-var formatting and the configured/other-type cases, the paged list and status header, and the fuzzy provider search), and the login dialog (the auth URL, device-code, manual-input, prompt, details, info and waiting states with their hints, the input submit/cancel flow, the browser opener hook, and the abort channel) | ported (D91: prompt promises become result channels; D92: the browser opener is injected; D93: V8 regex error wording differs; D156: the dialog posts its mutations from the login flow's goroutine) |
| `coding/interactive` (model selectors) | `components/{model-selector,scoped-models-selector}.ts` — the model selector (the snapshot load with the current/default-first sorting, the background catalog refresh with the timeout/error/success status handling and dispose cancellation, the all/scoped scope toggle, the fuzzy search over the selector search text with the default-query ranking, the current/default badges, the paged list with the scroll indicator and the model-name footer) and the scoped models selector (the enabled-ids model with the all-enabled normalization, toggle/enable-all/clear-all/provider toggles, the reorder bindings, the availability strikethrough for missing models, the session-only change notifications plus the persist callback, the dirty marker and the footer hints) | ported (D90: the model runtime is behind a narrow interface for testability) |
| `coding/interactive` (selectors) | `components/{theme-selector,show-images-selector,thinking-selector,user-message-selector,trust-selector,custom-message}.ts` — the theme and show-images select lists with the preselect/preview wiring, the thinking-level selector (level descriptions, the default marker, the search input with fuzzy filtering that rebuilds the list, the save-as-default binding and the hint lines), the fork-from-message selector (the paged message list with the cursor/metadata lines, wrap-around navigation and the empty auto-cancel), the project trust selector (the trust options with the parent-folder and session-only variants, the saved-decision formatting and the j/k navigation), and the custom-message component (the custom renderer with the error fallback, the label and markdown default rendering, the expanded/padding state) | ported |
| `coding/interactive` (bash + tool execution) | `components/tool-execution.ts`, `core/tools/render-utils.ts` and `core/tools/renderers/bash.ts` — the tool component (the default/self render shells, the tool renderer interface with the call/result renderers and the render context, the fallback call/result rendering with preview truncation, the partial/error/success backgrounds, the expansion toggle via mouse or API, the argument/execution state flags, the invalid-renderer recovery, and the self-shell render/height/mouse delegation), the shell renderers (call line, streaming tail preview with the earlier-lines hint, truncation notices, Elapsed/Took), plus the inline result-image components (a spacer and `tui.ImageComponent` per image block, gated on the terminal protocol and show-images). A manual `!` command renders through the same shell frame (D246); upstream `components/bash-execution.ts` is not ported | ported (D89: the text fallback is `tui.ImageFallback` with dimensions and the upstream bracket form) |
| `coding/interactive` (messages, status, app keybindings) | `core/keybindings.ts` (the app.* keybinding table with the Windows/WSL/macOS variants, the legacy-name migration, the config-file loading and ordering, and the manager over the TUI manager with an explicit key order) and `components/{assistant-message,status-indicator,branch-summary-message,compaction-summary-message}.ts` — the assistant transcript component (text/thinking runs, click-to-toggle thinking visibility, the length/abort/error notices, OSC 133 zone markers, markdown transformers), the status indicators (working/retry with countdown/compaction/branch-summary plus the border-aware renders and the idle status), and the branch/compaction summary components (expanded/collapsed states, the token count formatting, mouse toggling) | ported (D88: messages arrive as field values instead of the custom-message union; D263: expanded thinking keeps every Markdown text element italic, including headings and highlighted code) |
| `coding/interactive` (diff) | `modes/interactive/components/diff.ts` plus the jsdiff (`diff` v8) word diff it uses — `renderDiff` with the line parser, tab expansion, the consecutive removed/added grouping, single-line intra-line highlighting (inverse styling with leading-whitespace preservation) and the context/removed/added coloring, and a direct port of jsdiff's word diff (the extended-word-char tokenizer with whitespace merging, the Myers diff with the diagonal-pruning optimisation and path merging, `buildValues`, `join`, and the whitespace deduplication post-processing with the overlap helpers) | ported (D87: the npm package is unavailable offline, so the algorithm is ported from its source and verified against the real library; `internal/jsdiff` now holds the Myers core shared with the unified-patch generator) |

| `coding/interactive` (leaf components) | `modes/interactive/components/{keybinding-hints,countdown-timer,dynamic-border,visual-truncate,bordered-loader,markdown-transform,user-message,custom-entry}.ts` plus the theme-derived factories (`getMarkdownTheme`/`getSelectListTheme`/`getEditorTheme`/`getSettingsListTheme`/`getResolvedThemeColors`/`getThemeExportColors`/`isLightTheme`/`getLanguageFromPath`/`highlightCode`): key text/display formatting with the macOS Option rule, the key/raw hints, the countdown timer, the dynamic border, visual-line truncation (including the JS slice(-0) quirk), the bordered loader (cancellable and plain), the markdown transformer pipeline, the user-message component with OSC 133 zone markers, and the custom-entry component with the expanded state and the renderer-failure fallback box | ported (D231: syntax highlighting is built in through the `highlight` port; D85: ActiveTheme accessor; D86: style-color switch) |
| `coding/interactive` (scaffolding) | `modes/interactive/{chat-viewport,theme/theme-controller,model-search,external-editor,model-catalog-refresh}.ts` — the shared chat viewport (follow-end primary transcript + the fixed input dock with pending/status/widgets/editor/footer and their stretch rules), the settings-driven theme controller (auto light/dark settings, terminal color-scheme syncing, preview, in-memory instances, error fallback to dark), the model search-text builders, the external editor launcher (temp file, BOM/trailing-newline handling, failure status), and the model-catalog refresh coordinator (per-runtime deduplication with independent caller cancellation) plus the terminal query detection helpers | ported (D76: sequential terminal queries instead of concurrent; D77: Go waits on the child process directly) |
| `coding/interactive` (theme) | `modes/interactive/theme/{theme,theme-schema}.ts` — the theme document shape with structural validation and the missing-color summary, the color utilities (hex parsing, the 6x6x6 cube and grayscale 256-color conversion with the saturation guard, foreground/background ANSI building for truecolor and 256-color modes), variable resolution with circular/missing-reference errors, the optional-token fallbacks, the `Theme` palette (fg/bg maps, `fg`/`bg` with partial resets, chalk-equivalent bold/italic/underline/inverse/strikethrough, raw ANSI accessors, thinking-level and bash-mode border colors), built-in dark/light loading from the embedded upstream JSON (the port ships its own amber-accented palette over these — terminal-transparent except for the tool-state and user-message fills, D154), custom theme discovery/loading, the terminal light/dark detection (COLORFGBG index luminance, RGB luminance, fallback), the auto-theme setting parse/resolve, and the global theme registry with watcher | ported (D231: the highlightCode hook colors tokens through the `highlight` port; D75: the theme file watcher polls instead of fs.watch) |
| `tui` (autocomplete provider) | src/autocomplete.ts — `CombinedAutocompleteProvider`: slash-command completion with fuzzy filtering (including the skill: prefix rule), argument completion through command hooks, quoted/`@`-prefixed token extraction, path completion for relative/absolute/`~` prefixes with the root/trailing-slash/file-prefix split, directory-first ordering, symlink-to-directory resolution, completion-value quoting, `applyCompletion` for commands/attachments/arguments/paths with the directory-without-space and trailing-quote cursor rules, fd-based fuzzy file search (base-directory first then recursive, scoring with the directory bonus, depth/length/name tie-breaks, the 20-item cap, `.git` filtering, scoped queries), and the Tab file-completion trigger | ported (D72: ICU collation approximated by case-insensitive comparison; D73: command entries are a single type instead of a SlashCommand/AutocompleteItem union; D157: a trailing `.` or `..` is split off before `~` is expanded, where upstream's expansion cleans it away and completes in the parent directory) |
| `tui` (editor) | src/components/editor.ts (plus the autocomplete surface from src/autocomplete.ts) — the multi-line prompt editor: the paste-marker aware segmenter and word wrapper, the scroll-indicator borders, the word-wrapped layout with cursor mapping, the visual-line map and sticky-column vertical movement with atomic-segment snapping, grapheme-accurate insert/delete/word deletes/line deletes, the kill ring with yank/yank-pop, undo snapshots with paste-registry restore, prompt history browsing (draft restore, multi-line entries), bracketed-paste handling with the large-paste marker registry and renumbering, character jump mode, mouse cursor placement including wrapped lines, the render pipeline (padding, fake cursor, hardware cursor marker, autocomplete list and scrolling), and the autocomplete integration (trigger patterns, slash-command context, Tab/force completion, suggestion list wiring) | ported (non-forced trigger-token requests debounce 20 ms, the provider runs off the owner loop with a cancelable context, and results are posted back through the host's Post seam with a stale-result check; a host without that seam keeps the synchronous call; the CombinedAutocompleteProvider implementation lands next) |
| `tui` (alt-screen renderer) | src/tui-alt-screen.ts — the fullscreen renderer: enter/exit sequences with mouse-mode selection and capability handling, the layout-frame render pipeline (search refresh, OSC 133 stripping, search highlights, scroll-to-end indicator, overlay compositing, viewport clipping, selection painting, flashes, cursor extraction, over-wide line clamping), the screen diff with full/partial redraws and cursor positioning, the deferred-document replay on stop, viewport scrolling (keys, prompts, top/bottom), search toggle/query/navigation with reveal, mouse handling (SGR/legacy parsing, overlay and layout dispatch with layout-node skipping, capture/drag/click with double/triple-click word/line selection, auto-scroll, scrollbar hover/drag, right-click paste, OSC 8 URL activation, wheel routing with overscroll chaining), selection text extraction and OSC 52/injected clipboard copy with flashes, plus image-line detection, kitty/iTerm2 encoding and deletion, the kitty placement registry, cell-size math and the `Image` component (`tui/terminalimage.go`, `tui/imagecomponent.go`) | ported (D81 closed: kitty image caching is ported; D82: selection auto-scroll timer; D84: render serialization) |
| `tui` (alt-screen search) | src/alt-screen-search.ts — the fullscreen transcript search: the corpus builder (the ASCII fast path indexing non-space runs with linear column mapping, and the grapheme path for ANSI/wide/CJK text), whitespace-normalized case-insensitive matching with the span-to-segment projection and adjacent-segment merging, the cached search index (source/query change detection), the match key, and the search overlay component (input + result counter + navigation buttons with the keybinding-derived labels, hover state, responsive button layout, and button hit-testing) | ported (D80: the nav-direction and hover APIs use (value, ok) instead of undefined) |
| `tui` (main-screen renderer) | src/tui-main-screen.ts — the differential renderer for the main screen and scrollback: the state capture/restore, the bounded 1 MiB output writer with synchronized-output framing, the full-render paths (first render, width/height changes with the Termux height exception, clear-on-shrink) and the incremental paths (first/last changed line detection, append starts, the all-deleted-lines clearing path, viewport-relative scrolling, per-line clear + write, tail clearing with cursor restoration), the hardware-cursor positioning for IME (row deltas + absolute column), the kitty image header parsing with reserved-row blocks and deletion of changed images, the over-wide-line crash log, and the render hooks | ported (D78: rune-aware chunking instead of surrogate pairs; D79: over-wide lines report through a hook instead of throwing) |
| `tui` (latex) | src/latex.ts — the LaTeX math-to-Unicode renderer: the extracted symbol/command tables (224 symbols, negated symbols, blackboard letters, super/subscript digits, accents, named operators, limits, relation commands, spacing/font/size/ignored/plain-wrapper sets), the script formatter (unicode scripts with the `_`/`^(...)` fallback and the `=`/`+-` normalization), fraction/root formatting, the named-operator output normalization, the LaTeX parser (sequences, commands, required/optional/raw arguments, `\limits` modifiers, operator limits, environments: equation/aligned/gather/multline/split/cases/array/matrices with delimiters), and the layout engine (fraction stacking, operator limits, script stacking, matrix rows with padding, baseline joining, marker-based multi-line composition) | ported (D69/D70: capabilities injectable, lookaround-free matching) |
| `tui` (markdown renderer) | the renderer half of src/components/markdown.ts — the `Markdown` component (cache, padding, default text style with the colour/background split, the sentinel-based style-prefix reapplication), block rendering (headings with level-specific styling and the #≥3 prefix, paragraphs, code blocks with optional highlighting and the closing-fence trim, quotes with border and style reapplication, hr, html, space tokens), the inline renderer (nested strong/em/del/codespan/link/html/latex with the style-prefix trailing trim), lists (nesting, markers, preserve-markers mode, task markers, continuation indentation, loose spacing), and tables (natural/min column widths, the shrink/grow distribution with rounding fixups, cell wrapping with the mid-fragment style reset, borders) plus the OSC 8 `Hyperlink` helper and injectable terminal capabilities | ported (D68: src/latex.ts is a separate port, and `RenderLatex` now renders; unsupported syntax still falls back to the raw source; D69: capabilities are injectable; the notable divergences are D67's subset lexer and D231's forwarded syntax-highlighting hook) |
| `internal/markdown` (ported into `tui`) | the tokenizer and renderer halves of src/components/markdown.ts: a Marked-compatible block + inline lexer (`internal/markdown/block*.go`, `internal/markdown/inline.go`) producing the `Block`/`Inline` model, and the upstream-parity renderer (`internal/markdown/render.go`); plus headings (ATX, multi-line setext, levels, inline tokens), paragraphs, fenced (backtick/tilde, language) and indented code, blockquotes, ordered/unordered lists with nesting, task markers, tight/loose shapes, tables (alignment, header, rows), hr, html blocks, space runs, the strict strikethrough rule, escape/entity handling, autolinks and inline links/images, hard breaks, intraword-underscore emphasis rules, and the LaTeX block/inline extensions (including upstream's pending/streamed spans) plus `trimPartialClosingFences`. `tui/markdown.go` is the component wrapper (transform, per-width incremental line cache, off-loop preparation, padding/background); terminal primitives are injected through `markdown.Terminal` | ported (D67: Go has no `marked` dependency available, so this is a built-in subset lexer verified against upstream Marked token projections for a 79-case corpus; the renderer is pinned by the upstream render golden) |
| `internal/highlight` | the highlight.js 10.7.3 engine and its language definitions (`lib/core.js` and `lib/languages/*.js`): the mode compiler and keyword compiler, the `ResumableMultiRegex` with its per-byte dispatch and anchored context check, the `_highlight` scan loop with `SubLanguage`/`endsParent`/`returnBegin`/illegal handling, the token tree, and the HTML renderer; lookaround and backreference rules get an explicit equivalent (a `beforeBegin` guard or a `ManualBegin` matcher). The package is wired into the TUI through `coding/interactive/codehighlight.go`, so a registered language colors its tokens (D231) | ported: all 191 languages `lib/index.js` registers (the 21 pi registers eagerly plus the remaining 170). Every language has a fixture in `internal/highlight/testdata` (192 fixtures for 191 languages) and a Node-generated `.html` golden; `golden_manifest.json` is the single source of truth for both `gen_golden.mjs` and `TestGoldenHighlight`, and `TestGoldenFixturesAreListed` fails when a fixture is unlisted. `pattern_test.go` pins RE2 compatibility for the eager set |
| `tui` (fuzzy + settings list) | src/fuzzy.ts (subsequence matching with the consecutive/boundary/gap scoring, the letter-digit swap retry, exact-match bonus, and token filtering over whitespace/slashes with a stable sort) and src/components/settings-list.ts (settings rows with label/value alignment, the 36-column label clamp, value truncation, scroll indicators, the selected-item description wrapping, the optional fuzzy search input, Enter/Space value cycling, submenus with the done callback including navigate-to re-entry, mouse wheel/press/click handling with the search-row offset, and the hint line) | ported (D66: the Go matcher function is MatchFuzzy because the type and upstream function names collide) |
| `tui` (list + text + loader components) | src/components/select-list.ts (value-prefix filtering, visible-range scrolling centred on the selection, the primary/description two-column layout with column-width bounds and custom truncation, scroll indicators, wheel/press/click mouse handling with press-then-click confirmation, wrap-around keyboard navigation), src/components/truncated-text.ts (single-line truncation with padding), src/components/loader.ts (frame animation with the verbatim/coloured indicator and message updates), src/components/cancellable-loader.ts (abort on cancel with a done channel), src/components/mouse-region.ts (child-first mouse delegation), src/components/alt-screen-flash.ts (timed transient messages) | ported (D62 render-requester surface, D63 done channel instead of AbortSignal, D64 stopped flag for the animation callback) |
| `tui` (input + box + keybindings) | src/keybindings.ts (the built-in keybinding table with descriptions, the manager with user overrides, duplicate-key normalization, conflict detection, resolved bindings, and the global accessor), src/kill-ring.ts + src/undo-stack.ts (Emacs kill/yank ring with accumulate/prepend/rotate; snapshot stack), src/word-navigation.ts (backward/forward word navigation over word segments), src/components/box.ts (padding + background container with the bg-sample cache, mouse forwarding, background padding), src/components/input.ts (single-line input with the fake cursor and hardware-cursor marker, horizontal scrolling with the end-cursor column reservation, bracketed-paste buffering, kill/yank/yank-pop, undo with word coalescing, grapheme-aware movement and deletion, Kitty CSI-u printable decoding, control-character rejection, placeholder rendering, mouse cursor placement) | ported (D58 word segmentation heuristic, D59 no deep clone, D60 canonical binding order, D61 byte offsets) |
| `tui` (layout engine + scroll view) | src/layout.ts (the box-tree layout pass: leaf/scroll/vstack/hstack boxes, the render cache, clip intersection, scroll translation with follow-end, the measured-versus-allocated sizing, the line-offset rule for cursor markers beyond the allocated height, the cursor-marker-aware line offset), src/components/scroll-view.ts (scroll position/follow-end state machine, scrollTo/scrollBy/scrollToStart/scrollToEnd, the transient auto scrollbar with its hide timer, the always-mode content-width reservation, the single-child invariants), the screen painter (recursive box painting with the full-width fast path and OSC 133 zone stripping, scrollbar cell replacement preserving the target background, the scrollbar geometry with thumb sizing and offsets), and the hit-testing helpers (getLayoutBoxesAt with layer/depth ordering, getScrollViewBox, getScrollViewsAt) | ported (D57 closed: kitty image metadata and row cropping are ported) |
| `tui` (layout foundation + basic components) | src/layout-node.ts (the layout-node provider with stack/scroll nodes), src/components/stack.ts + v-stack/h-stack (the flex sizing algorithm: basis/min/max clamping, weighted grow and shrink distribution with capacity limits, gap accounting, the visible callback, the stack component with entry bookkeeping), src/components/text.ts (word-wrapped padded text with the width-aware margin reduction, tab expansion, optional background, cache) and src/components/spacer.ts | ported (upstream goldens for the sizing algorithm, stacks, and text) |
| `tui` (keys) | src/keys.ts — the `KeyId`/`Key` identifier surface, the legacy sequence tables (ANSI/SS3/PC-style function keys, shift/ctrl variants, alt+arrow), `matchesKey` for specials, arrows, function keys, and letter/digit/symbol keys across the legacy, Kitty CSI-u, and modifyOtherKeys encodings (base-layout-key fallback for non-Latin layouts with the remap guard, the raw-0x08 Windows Terminal heuristic, mode-aware `\x1b\r`/`\n` mappings), `parseKey` formatting (`formatParsedKey` with functional-codepoint normalization, shifted-letter identity, and lock-mask handling), `decodeKittyPrintable`/`decodePrintableKey` for plain/Shift CSI-u and modifyOtherKeys input, the key-event type tracking, and the Kitty protocol global state; verified against an upstream-generated golden matrix (169 inputs x 2 protocol states x 85 key ids plus parse/decode results) | ported |
| `tui` (terminal + stdin) | src/terminal.ts + src/stdin-buffer.ts — `StdinBuffer` (complete-sequence extraction for CSI/OSC/DCS/APC/SS3/meta with the SGR-mouse shape check, old-style ESC[M 6-byte events, the WezTerm ESC+ESC+CSI split, the escape vs sequence timeout, bracketed-paste re-wrapping, and the Kitty printable raw-echo dedup), `ProcessTerminal` (raw mode via x/term, bracketed paste on/off, SIGWINCH resize watcher, columns/rows with COLUMNS/LINES fallbacks, cursor/clear/title/OSC 9;4 progress with keepalive, drainInput idle protocol, the PI_TUI_WRITE_LOG output log), the Kitty keyboard protocol negotiation (flags request with the DA sentinel, modifyOtherKeys fallback, split-response buffering with the 150 ms flush timer), `resolveEscapeTimeoutMs` (PI_TUI_ESC_TIMEOUT / SSH), and the keyboard-protocol globals from keys.ts | ported (D169: timed-out SGR mouse heads reassemble across split tails and coalesced wheel bursts instead of entering the editor as text) |
| `tui` (renderer core) | src/tui.ts — Component model with the optional-method interfaces (InputHandler/MouseHandler/KeyReleaseWanter/Focusable), mouse event normalization with `dispatchMouseEvent`/`retargetMouseEvent`, `Container` (render + mouse-layout hit testing), the overlay stack (anchors/percent/margin layout, focus-order stacking, capture/non-capturing modes, handles with hide/setHidden/focus/unfocus/bounds, focus-restore state machine), `compositeTuiLine`/`compositeOverlays` (screen-relative compositing with segment resets), `applyLineResets`, cursor-marker extraction, and the render scheduler (16 ms throttle, immediate-render preemption of queued throttled frames); src/keys.ts `isKeyRelease`/`isKeyRepeat` | ported (terminal.ts, tui-main-screen/tui-alt-screen doRender, and the terminal query protocols pending) |
| `coding/interactive` (built-in tool renderers) | `core/tools/renderers/{index,read,bash,edit,write,grep,find,ls}.ts` — `withBuiltInRenderers` (extension definition falling back to the built-in one), the shared arg decoding (`str`/`replaceTabs`/`normalizeDisplayText`/`trimTrailingEmptyLines`/`renderToolPath`/`linkPath`), and each tool's call header and result body: read (the compact docs/skill/resource classification with the line range and expand hint, the language-highlighted expanded body and the truncation notices), bash/powershell (`createShellRenderers` with the `$`/`PS>` prompt, the timeout suffix, the cached collapsed visual-line preview with the earlier-lines hint, the full-output-path/truncation warnings and the elapsed/Took line), edit (the pending/success/error header backgrounds, the async file-preview diff via computeEditsDiff and the settled-result diff), write (the incremental highlight cache with the 50-line re-highlight prefix, the 10-line collapsed preview with the total-lines hint and the invalid-content error), and grep/find/ls (the pattern/path/glob/limit headers, the 15/20-line collapsed bodies and the match/result/entry-limit truncation warnings) | ported (D133: extension-registered renderer definitions stay out of scope, so the lookup always resolves to the built-in renderers; computeEditsPreview already runs off-loop and posts its result to the owner, matching upstream's asynchronous preview) |
| `internal/goal`, `coding` (goal controller), `coding/interactive` | `~/.pi/agent/extensions/goal/{state,machine,command,render,index}.ts` — the durable goal snapshot + `applyChange` validator + `foldGoal` replay (usage folding, state version), the `GoalMachine` state machine and effects (`agent_end` turn admission, `agent_before_settle` continuation/budget/abort, `goal_replace`), the `/goal` argument parser, the `Box`/`Text` cards with click-to-expand, and the controller that wires the two tools (`get_goal`/`set_goal`), the `session_start` replay, the tool-exposure sync, the transcript entry renderers and the tool-call cards | ported as a builtin internal package (upstream ships it as an installed user extension and the port has no extension host, D41; D223 records the re-port and the current-source mapping; D232 lets the agent clear its own goal; D233 makes `/goal set` overwrite a live goal without confirmation; D234 collapses the tools to `get_goal`/`set_goal`; D239 makes every pause emit the context-only `<goal_paused>` message; D242 keeps the goal active during an auto-retried transient failure; D243 notifies on every pause) |
| `main.go` + `cmd` (CLI + composition) | `main.ts`'s boot plus the `InteractiveMode` constructor — `coding/interactive/app.go` composes the ported wirings (renderer/theme, containers, editor, footer, transcript, event dispatcher, queue, selector slot, key/submit handlers, lifecycle, startup/run, selectors/settings/models/session/auth/commands/trust/autocomplete) into a runnable `App`, and the `cmd` package boots project trust for the session's own directory (the override, the trust store, the `defaultProjectTrust` setting and the pre-TUI startup prompt, `resolveProjectTrusted` minus D39's extension hook; D160: the runtime cwd is the session's, and a session switch re-points the one settings manager and re-resolves that directory's trust without a prompt), settings/auth/model-runtime/agent-session from the CLI flags (`--help`, `--version`, `-r`/`-c`, `--model`/`--provider`, `--offline`, `--tui-mode`, prompts) and runs it; startup resolves the physical cwd like Node's `process.cwd()`, so a shell `PWD` symlink does not select a different per-project session directory for `-c` or `-r`; `CreateAgentSession`'s own cwd fallback and `OpenSession`'s header-less fallback resolve the same physical directory; the tool flags (`--tools`/`--exclude-tools`/`--no-tools`/`--no-builtin-tools`) reach the session through `Args.ToolSelection`, and the startup flags (`--use-theme`, `--name`, `--approve`/`--no-approve`, `--models`, `--api-key`, `@file` arguments, and the parse diagnostics) are applied before the session is created; `resumeSession` ports `resolveSessionPath` (`--session` by path, exact id, id prefix, or globally across projects), `validateSessionIdFlags` (`--session-id` conflicts/format, create-with-id fallback), `validateForkFlags` + `SessionManager.forkFrom` (`--fork` resolves and forks into a new session in the current cwd), and `-r` opens the interactive session picker (`cli/session-picker.ts` over a main-screen renderer) | ported (D133: the app resolves tool renderers to the built-in set; D134: the app takes a pre-booted session and no-ops the grammar load/package check/extension rebind; lifecycle flags are now owner-loop state after the mutex was retired; app-owned optional queues cancel at shutdown while accepted session/settings saves drain (D199); clipboard copies and detached work inherit mode lifetime, and permanent terminal exit has a shared 2 s output grace (D200); optional admission is bounded per clipboard, theme and detached-work domain (D201); optional OSC 52 output waits off-loop for capacity and redundant title/progress hints defer under output pressure (D202); transcript width changes warm in cooperative owner-loop chunks before publishing a complete frame (D203); large built-in assistant Markdown prepares detached snapshots with owner-applied generation checks (D204); large text-only read and shell results share that optional four-job budget (D205); eligible tool workers also prepare Box padding/backgrounds for an owner-applied cache handoff (D206); pending eligible tools retain completed frames with live clock rows and displayed mouse geometry (D207); accepted pending clock updates reuse backing storage with owner revisions while unadmitted work remains retryable (D208); first-child Container changes reuse flattened storage with revision signals (D209); the picker skips the package-manager theme discovery, D41) (D175: on android the boot reads $PREFIX/etc/resolv.conf and installs a resolver fallback, because the pure-Go resolver finds no /etc/resolv.conf there) |
