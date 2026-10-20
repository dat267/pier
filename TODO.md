# Reproduced audit findings

Audit started 2026-10-20 against workspace `075f84a` and the local reference
checkout pi **v1.3.0**, `71023aec1546d6d677a65669c5665c0927ce0dd0`.
Implementation code is unchanged. Entries below require an observed reproducer;
documented intentional divergences and unconfirmed suspicions are excluded.

## 1. Concurrent terminal pushes both enter an EventStream [P1]

- **Location:** `ai/eventstream.go`, `EventStream.Push`.
- **Observed:** two concurrent terminal pushes produce two events (`[2 1]`
  in the recorded run), although the first completion should close admission.
  The result is fixed by one event while consumers can receive another terminal
  event afterward. `Push` releases `mu` after checking `done`, then runs the
  predicate without reserving admission or checking completion again.
- **Reference:** pi v1.3.0 `packages/ai/src/utils/event-stream.ts`, `push`:
  its synchronous `done` check/transition admits only one terminal push.
- **Reproduce:** save this program outside the repository and run `go run`:

  ```go
  package main
  import ("context"; "fmt"; "sync"; "github.com/dat267/pier/ai")
  func main() {
      entered, release := make(chan struct{}, 2), make(chan struct{})
      s := ai.NewEventStream(func(v int) bool {
          entered <- struct{}{}; <-release; return true
      }, func(v int) int { return v })
      var wg sync.WaitGroup
      for _, v := range []int{1, 2} {
          wg.Add(1)
          go func() { defer wg.Done(); s.Push(v) }()
      }
      <-entered; <-entered; close(release); wg.Wait()
      fmt.Println(s.Events(context.Background())) // two terminal events
  }
  ```

  The two-predicate barrier demonstrates the current admission gap; a future
  regression test must also allow a correct implementation to serialize
  admission without waiting for a second predicate invocation. Preserve the
  existing callback reentrancy tests when fixing this.

## 2. Overflow regexes disagree with JavaScript whitespace and line rules [P2]

- **Location:** `ai/overflow.go`, `overflowPatterns` and
  `cerebrasBodylessOverflowPattern`.
- **Impact:** false negatives skip overflow recovery; false positives can
  compact/retry responses that upstream does not classify as overflow.
- **Reference:** pi v1.3.0 `packages/ai/src/utils/overflow.ts`,
  `isContextOverflow` and its JavaScript regex literals. These differences are
  not listed as intentional divergences.
- **Reproduce:** call `ai.IsContextOverflow` with an `AssistantMessage` having
  `StopReason: ai.StopError`, the provider below and a pointer to the indicated
  error string; pass context window `0`. Run the same inputs through upstream
  `isContextOverflow({provider, stopReason: "error", errorMessage}, 0)`:

  | Provider | Error string (Go/JS escapes) | Upstream | Pier |
  |---|---|---|---|
  | `cerebras` | `400\u00a0status code\ufeff(no body)` | `true` | `false` |
  | `openai` | `exceeds maximum context length\u00a0(200000)` | `true` | `false` |
  | `google` | `input token count\rexceeds the maximum` | `false` | `true` |
  | `google` | `input token count\u2028exceeds the maximum` | `false` | `true` |
  | `anthropic` | `prompt iſ too long` (`ſ` = U+017F) | `false` | `true` |

  RE2 `\s` excludes several JS whitespace characters; RE2 `.` includes CR and
  U+2028/U+2029; RE2 case folding also admits long-s where JS `/i` without `/u`
  does not. Changing only the whitespace class does not address all cases.

## 3. Raw JSON bypasses JavaScript string/object canonicalization [P2]

- **Location:** `ai/json.go`, `MarshalJSON`/`MarshalIndentJSON`; nested raw
  fields such as `ai.ToolCall.Arguments`, custom messages and entry details.
- **Observed:** the shared encoder normalizes some numeric spellings but
  preserves escaped string spellings, numeric property insertion order and
  duplicate members from `json.RawMessage`. `JSON.parse` followed by
  `JSON.stringify` does not. This breaks the documented session/wire byte
  parity, including imported tool arguments that bypass `ParseToolCallArguments`.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/core/session-manager.ts`,
  `loadEntriesFromFile`/`forkFrom`; `packages/agent/src/proxy.ts` request body
  `JSON.stringify`. D253 covers integer precision, not these string/object
  differences; D2's unpaired-surrogate exception does not apply to these inputs.
- **Reproduce:** for each raw JSON below, run
  `ai.MarshalJSON(json.RawMessage(raw))` and compare with
  `JSON.stringify(JSON.parse(raw))` in Node:

  | Input | Pier | Upstream |
  |---|---|---|
  | `{"text":"\u0061\/\u4f60\u597d"}` | unchanged | `{"text":"a/你好"}` |
  | `{"10":1,"2":2,"a":3}` | unchanged | `{"2":2,"10":1,"a":3}` |
  | `{"x":1,"x":2}` | unchanged | `{"x":2}` |

  All three calls succeeded without an encoding error. Preserve ordinary
  non-index insertion order and the documented native-integer behavior when
  addressing this; decoding into an unordered Go map is not a parity fix.

## 4. Tool validation errors identify the wrong failing value [P2]

- **Location:** `ai/validation.go`, `ValidateToolArguments` error construction.
- **Impact:** tool failures shown to the model point at a valid root object
  instead of explaining the argument it must repair. Nested schema failures
  can collapse to `Unknown validation error` as well.
- **Reference:** pi v1.3.0 `packages/ai/src/utils/validation.ts`,
  `formatValidationPath` and the `validator.Errors(args)` formatter. This is
  independent of intentionally unsupported validation keywords (D6) and the
  documented JSON syntax-error wording difference (D221).
- **Reproduce:** use tool `t` with parameters
  `{"type":"object","properties":{"count":{"type":"integer"}},"required":["count"]}`.
  Call `ai.ValidateToolArguments` with `ToolCall{Name:"t", Arguments:...}`;
  upstream equivalent is `validateToolArguments(tool, {name:"t", arguments:...})`.

  | Arguments | Upstream error lines | Pier error lines |
  |---|---|---|
  | `{"count":"two"}` | `- count: must be integer` | `- root: Expected object` |
  | `{}` | `- count: must have required properties count` | `- root: Expected object` followed by `- count: required property missing` |

  The surrounding `Validation failed for tool "t"` and received-arguments
  sections match in these reproductions; the root diagnostic is false.

## 5. Finished Agent runs retain parent-context registrations [P2]

- **Location:** `agent/agent.go`, `runWithLifecycle` deferred cleanup.
- **Observed:** three successful prompts with a never-canceled parent context
  register three cancellation handlers and release zero, even though the agent
  is idle. The stored `context.WithCancel` cancel function is called by `Abort`
  but is not released when a normal run finishes. A standard cancelable parent
  similarly retains child contexts until it is eventually canceled; a custom
  `Done`-only context can retain propagation goroutines.
- **Reference:** pi v1.3.0 `packages/agent/src/agent.ts`, `runWithLifecycle` /
  `finishRun` clear runtime-owned run state after awaited listeners settle.
  This is a Go resource-lifetime defect, not an assertion that upstream aborts
  a successful run. Keep successful settlement's abort status unchanged.
- **Reproduce:** use the following observing parent with `Agent.PromptText`:

  ```go
  type parent struct {
      context.Context
      done chan struct{}
      registered, stopped atomic.Int32
  }
  func (p *parent) Done() <-chan struct{} { return p.done }
  func (p *parent) AfterFunc(f func()) func() bool {
      p.registered.Add(1)
      return func() bool { p.stopped.Add(1); return true }
  }
  ```

  Initialize it with `Context: context.Background(), done: make(chan struct{})`.
  Construct `agent.NewAgent` with model `{ID:"m", Provider:"p",
  API:ai.APIOpenAIResponses}` and a fake `StreamFn` returning an
  `ai.NewAssistantMessageEventStream()` already pushed with a successful
  `EventDone` message containing `ai.TextContent{Text:"ok"}`. Run three
  `a.PromptText(p, "hello")` calls synchronously. All return nil; the observed
  output is `registered=3 stopped=0 streaming=false`. Finished runs should not
  keep their parent registrations alive. This reproducer uses no real network
  request, sleeps, reflection or race detector.

## 6. Fuzzy model resolution selects a different alias ordering [P2]

- **Location:** `coding/modelresolver.go`, `bestFuzzyMatch`.
- **Observed:** Go bytewise descending ID order differs from upstream's
  descending `localeCompare`, so an ambiguous substring can silently select a
  different model even within one provider.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/core/model-resolver.ts`,
  `bestFuzzyMatch`: `.sort((a, b) => b.id.localeCompare(a.id))[0]`.
- **Reproduce:** construct two `ai.Model` values with `Provider:"p"` and the
  IDs in each row. Resolve
  `ModelReferenceRequest{Text:"model", Fuzzy:true, ThinkingSuffix:"none"}`
  with `usable` returning true. Compare upstream
  `resolveModelReference({text:"model", fuzzy:true, thinkingSuffix:"none"},
  models, () => true)`:

  | Model IDs | Upstream selected ID | Pier selected ID |
  |---|---|---|
  | `model-2`, `model_1` | `model-2` | `model_1` |
  | `model-Z`, `model-z` | `model-Z` | `model-z` |

  Both results have status `resolved`; this is not merely a display-order
  mismatch. Confirm the intended locale policy before choosing a comparator.

## 7. Quoted frontmatter scalars retain YAML escape syntax [P2]

- **Location:** `coding/skills.go`, `ParseFrontmatter` quoted-value handling;
  consumers include skill descriptions and prompt-template argument hints.
- **Observed:** the parser removes surrounding quotes but does not decode
  doubled single quotes or double-quoted escapes. These are flat scalar values,
  not nested YAML structures excluded under D10.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/utils/frontmatter.ts`,
  `parseFrontmatter`, and `core/prompt-templates.ts` `loadTemplateFromFile`.
- **Reproduce:** surround each frontmatter line below with opening/closing
  `---` lines and append `body`; compare `coding.ParseFrontmatter(input)` with
  upstream `parseFrontmatter(input)` on the same input bytes:

  | Frontmatter line | Upstream decoded value | Pier decoded value |
  |---|---|---|
  | `description: 'Don''t repeat'` | `Don't repeat` | `Don''t repeat` |
  | `description: "Line\nBreak"` | `Line`, newline, `Break` | literal `Line\nBreak` |
  | `argument-hint: "Use \"quoted\" text"` | `Use "quoted" text` | literal `Use \"quoted\" text` |

  Reproduction used a shared JSONL input fixture read by both Go and Node to
  avoid differences introduced by the host language's string escaping.

## 8. The read tool rejects every empty text file with EOF [P2]

- **Location:** `coding/mime.go`, `DetectSupportedImageMimeTypeFromFile`, and
  its caller `coding/tools_fs.go`, `CreateReadTool.Execute`.
- **Observed:** MIME sniffing treats a zero-byte read with `io.EOF` as a file
  error, so reading an existing empty file fails rather than returning empty
  text. CLI `@file` processing has a separate empty-file skip and is unaffected.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/utils/mime.ts`,
  `detectSupportedImageMimeTypeFromFile`, accepts `bytesRead === 0`; and
  `core/tools/read.ts` takes the normal text-file path.
- **Reproduce:** create an empty `empty.txt` in a temporary directory:

  ```go
  mime, err := coding.DetectSupportedImageMimeTypeFromFile(path)
  // Actual: mime == "", err == io.EOF. Expected: "", nil.
  args, _ := json.Marshal(map[string]string{"path": path})
  result, err := coding.CreateReadTool("/tmp", nil).
      Execute("r", args, context.Background(), nil)
  // Actual: zero result, EOF. Expected: one text block containing "".
  ```

  Running the pinned source with its own source resolver confirms the expected
  result, without falling through to stale package dist files:

  ```sh
  node --experimental-strip-types \
    --import ./pi/packages/coding-agent/src/experimental/source-resolver.ts \
    --input-type=module -e '
  import {createReadTool} from "./pi/packages/coding-agent/src/core/tools/read.ts";
  console.log(await createReadTool("/tmp").execute("r", {path: "/tmp/empty.txt"}));'
  ```

  Upstream returns `content: [{type:"text", text:""}]` and
  `structuredContent: ""`.

## 9. Supplying only a read-tool model callback disables default resizing [P2]

- **Location:** `coding/tools_fs.go`, `ReadToolOptions.AutoResizeImages` and
  `CreateReadTool`'s `options == nil || options.AutoResizeImages` defaulting.
- **Observed:** adding an unrelated `Model` callback changes the image resize
  default from true to false. The option is documented as default true, but
  its plain Go bool cannot distinguish omitted from explicitly false.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/core/tools/read.ts`,
  `createReadToolDefinition`: `options?.autoResizeImages ?? true`. Model-aware
  read tools are a supported port API and are also exercised by existing tests.
- **Reproduce:** write a PNG with dimensions 2001 × 1, then read it with each
  tool below. Base64-decode the returned `ai.ImageContent.Data` and inspect
  `image.DecodeConfig(...).Width`:

  ```go
  coding.CreateReadTool(dir, nil) // actual returned width: 2000
  coding.CreateReadTool(dir, &coding.ReadToolOptions{
      Model: func() *ai.Model { return nil },
  }) // actual returned width: 2001; expected default: 2000
  ```

  Upstream `createReadToolDefinition(dir, {}).execute(...)` returned a 2000 × 1
  image and the corresponding dimension note for the same fixture. This is
  separate from D26's intentional resampling-algorithm differences. Preserve
  an explicit way to disable resizing when correcting option presence. The
  default SDK construction already supplies the settings flag explicitly, so
  this finding affects callers constructing options directly.

## 10. Whitespace-only edit targets get a false duplicate-match error [P2]

- **Location:** `coding/editdiff.go`, `countOccurrences` used by
  `ApplyEditsToNormalizedContent`.
- **Observed:** fuzzy normalization can turn a nonempty whitespace target into
  `""`. `strings.Count(content, "")` counts boundaries, while upstream's
  `.split("").length - 1` has different semantics. A unique exact whitespace
  match can therefore be rejected before replacement.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/core/tools/edit-diff.ts`,
  `countOccurrences` and `applyEditsToNormalizedContent`. This affects whether
  an edit succeeds, not the intentional diff-formatting differences in D9.
- **Reproduce:** call
  `coding.ApplyEditsToNormalizedContent(content, []coding.Edit{{OldText:" ",
  NewText:"X"}}, "f.txt")`; compare upstream
  `applyEditsToNormalizedContent(content, [{oldText:" ", newText:"X"}], "f.txt")`:

  | Content | Upstream new content | Pier |
  |---|---|---|
  | `a ` | `aX` | rejects: `Found 2 occurrences` |
  | ` a` | `Xa` | rejects: `Found 3 occurrences` |
  | `ab ` | `abX` | rejects: `Found 3 occurrences` |

  Each input contains exactly one ASCII space. All upstream calls succeeded.

## 11. Reading an Agent state snapshot can crash during tool execution [P1]

- **Location:** `agent/agent.go`, `State` and `processEvents` updates to
  `PendingToolCalls`.
- **Observed:** `State` copies the struct under `mu`, then returns the same
  mutable map used by tool-start/tool-end events. A caller iterating that map
  after the method returns can terminate the process with
  `fatal error: concurrent map iteration and map write`. This occurred in two
  successive standalone reproductions without `-race`.
- **Reference:** pi v1.3.0 `packages/agent/src/agent.ts`, `state` getter and
  `processEvents`. JavaScript's single-thread execution does not expose this
  concurrent-map failure; Go's locks must bridge that runtime difference.
  `State` is documented as returning a snapshot, and its retained lock does
  not protect the map after return.
- **Reproduce:** set `runtime.GOMAXPROCS(4)`; create an `agent.Agent` with:
  1. A fake model and 1,000 no-op tools `t0` through `t999`, each using
     parameters `{"type":"object","properties":{}}`. Each `Execute` calls
     `runtime.Gosched()` and returns one text block `"ok"`.
  2. A fake `StreamFn` whose first response is one `StopToolUse` message with
     1,000 calls (`c0`…`c999`, the corresponding tool names, arguments `{}`).
     Its next response is a `StopStop` message containing text `"ok"`. Return
     each response through a new stream pushed with `ai.EventDone`.
  3. Run one `a.PromptText(context.Background(), "go")` in a goroutine. Until
     it returns, inspect the public state from the caller goroutine:

     ```go
     for range a.State().PendingToolCalls {
         runtime.Gosched()
     }
     ```

  The process exits with status 2 from `internal/runtime/maps.(*Iter).Next`.
  No provider request, external tool, timer or race detector is involved.
  Resolve ownership of the pending-call map specifically; copying the entire
  transcript on every state read would introduce an unrelated scaling cost.

## 12. Agent failure-lifecycle listener errors are swallowed [P2]

- **Location:** `agent/agent.go`, `handleRunFailure` ignores every
  `processEvents` error; `runWithLifecycle` then returns nil.
- **Observed:** when a listener rejects the initial event and again rejects
  the failure lifecycle, upstream rejects the prompt with the listener error.
  Pier reports successful method completion instead. Hosts cannot detect that
  their persistence/event callback failed during failure handling.
- **Reference:** pi v1.3.0 `packages/agent/src/agent.ts`, `runWithLifecycle`
  awaits `handleRunFailure`, which awaits each failure event's listeners.
- **Reproduce:** create an agent with an unused fake `StreamFn` and subscribe:

  ```go
  a.Subscribe(func(agent.AgentEvent, context.Context) error {
      return errors.New("listener failed")
  })
  err := a.PromptText(context.Background(), "x")
  // Actual: err == nil; provider stream is never called.
  ```

  Upstream reproduction using its source resolver:

  ```js
  const a = new Agent({streamFn: () => { throw Error("must not run"); }});
  a.subscribe(() => { throw Error("listener failed"); });
  await a.prompt("x"); // rejects with "listener failed"
  ```

  Ordinary first-stage run failures should still become transcript failure
  events; this finding concerns an additional error while delivering those
  failure events, which must not be reported as successful completion.

## 13. Settings saves never recover stale lock artifacts [P2]

- **Location:** `coding/settingsmanager.go`, `acquireLockWithRetry` /
  `FileSettingsStorage.WithLock`.
- **Observed:** an existing `.lock` artifact blocks every save after ten
  attempts, regardless of its age. The Go implementation uses an exclusive
  regular-file creation and performs no stale-lock recovery; the upstream
  `proper-lockfile` lock is a directory with stale detection. A crash can leave
  settings persistence broken until the lock is removed manually.
- **Reference:** pi v1.3.0 `packages/coding-agent/src/core/settings-manager.ts`,
  `FileSettingsStorage.acquireLockSyncWithRetry` using
  `lockfile.lockSync(path, {realpath:false})`. `docs/PORTING.md` claims
  proper-lockfile semantics for settings. D32 describes the separate auth
  backend, not an intentional settings-lock exception.
- **Reproduce:** in a temporary `dir`, create `settings.json` containing `{}`
  and a directory `settings.json.lock`. Set the lock directory's modification
  time to one minute ago with `os.Chtimes`. Then call:

  ```go
  s := coding.NewFileSettingsStorage(dir, dir)
  err := s.WithLock(coding.SettingsScopeGlobal, func(*string) *string {
      next := `{"theme":"light"}`
      return &next
  })
  // Actual: open .../settings.json.lock: file exists.
  ```

  On the identical fixture, upstream
  `new FileSettingsStorage(dir, dir).withLock("global", () =>
  '{"theme":"light"}')` reclaimed the stale directory and saved successfully.
  Repeat the setup before rerunning either side. A stale regular lock file
  created by Pier also blocks Pier indefinitely; test both artifact types when
  resolving the persistence/interoperability policy.

