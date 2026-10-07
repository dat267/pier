# AGENTS.md

Guidance for agents working in this repository. Read this first; the
authoritative per-area port status is `docs/PORTING.md`, the divergence log is
`docs/DIVERGENCES.md`, the architecture and performance notes are
`docs/architecture.md`, the lock inventory is `docs/locks.md`, and the README
covers install and usage.

## What this is

A from-scratch Go implementation of an AI coding agent with a terminal UI,
developed against Mario Zechner's **pi** (the reference:
`https://github.com/earendil-works/pi`) as its strongest reference. It is a
sibling of the TypeScript monorepo, not a fork: the Go code follows the
reference packages closely, file for file.

- Module: `github.com/dat267/pier`, Go `1.27`.
- Dependencies are offline-cached only. Direct dependencies: `golang.org/x/text`,
  `x/term`, `x/sys`, `x/tools`
  (`internal/uiblock`'s SSA analysis), and `modernc.org/sqlite` (the durable
  SQLite backend). `go.mod` is authoritative for versions and indirect
  dependencies.
- Upstream checkout lives in the workspace at `./pi` (gitignored): clone
  `https://github.com/earendil-works/pi` there at tag `v1.0.2`
  (`cd32f772`; see the README's pin table). The released tags are the
  reference: an installed bundle drifts and can sit on a divergent upstream
  line. Diff checks go through the `./pi` sources (`node
  --experimental-strip-types`, `FORCE_COLOR=1` for chalk parity).
- License: MIT (see `LICENSE`).

## Reference rules

1. **pi is the reference, not the spec.** A commit cites the reference file it
   follows. Never infer behavior; read the TypeScript source. Where this
   implementation departs, the departure is a deliberate feature, not an
   approximation to be silently corrected.
2. **JSON is byte-parity.** Session files, transcripts and wire payloads must
   round-trip identically: same key names, same key order, no HTML escaping.
   Use `ai.MarshalJSON`, never `encoding/json.Marshal`, for anything persisted
   or sent. JS object insertion order is tracked explicitly where it matters.
3. **JS semantics where pi relies on them.** Object insertion order,
   `JSON.stringify` behavior, truthiness at API boundaries. Divergences get a
   numbered **D-row** in a code comment and an entry in `docs/DIVERGENCES.md`.
4. **Every behavior change is test-observed.** Tests mirror upstream `test/`
   files where they exist; Go-only tests assert the upstream-derived
   expectation and cite the upstream source in a comment.

## Layout

Packages mirror upstream `packages/`:

| Go package | Upstream | Notes |
|---|---|---|
| `ai` | `packages/ai` | models, wire APIs, providers, OAuth, images |
| `agent` | `packages/agent` | the agent loop and stateful Agent |
| `coding` | `packages/coding-agent` core | session, tools, compaction, settings, model runtime |
| `coding/interactive` | `packages/coding-agent` interactive mode | components, theme, the mode wirings |
| `tui` | `packages/pi-tui` | terminal abstraction, renderer, components |
| `main.go`, `cmd` | `main.ts` | the CLI entrypoint (a thin root main over the `cmd` package) |
| `chord`, `client`, `protocol`, `server`, `telemetry`, `durable` | same | supporting packages |
| `internal/offloop`, `internal/uiblock` | — | port-local helpers: the off-loop work queues and the UI-blocking analyzer |
| `scripts` | — | catalog generators |
| `docs` | — | port status (`PORTING.md`), the divergence log (`DIVERGENCES.md`), architecture notes (`architecture.md`) and the lock inventory (`locks.md`) |

## Build, test, run

```bash
go build ./...                     # everything
go vet ./...                       # must be clean
gofmt -l .                         # must be empty
                               # the local gate: fast, no race detector
go test -count=1 ./...             # CI adds -race -count=2 -timeout 300s

# the CLI (binary derives its display name from the file name)
go build -o bin/pier .
./bin/pier --help
./bin/pier -c                       # continue the newest session
./bin/pier -r                       # open the session picker
```

**Local gate vs CI gate.** Develop against `go build ./...`, `go vet ./...`,
`gofmt -l .` and `go test -count=1 ./...` — do not run the race detector
locally; it adds substantial overhead, and the full gate
(`go test -race -count=2 -timeout 300s ./...`) runs in CI on every push.
A test that only fails
under `-race` (timing, scheduling, GC) is still a bug: reproduce it locally
with `-race -count=<n> -run <Test>` when a CI run points at it, fix it, and
leave the race gate to CI.

A `justfile` wraps the commands above (`just`, `just check` — the local gate,
`just test-race` for the rare local race reproduction — and `just --list`;
`just VERSION=1.2.3 install` stamps the version `make install` used to, and the
recipe names match the old Makefile targets). `install` has a `[windows]`
(PowerShell) variant so it does not need a POSIX shell on PATH; the other
recipes still run through the global bash shell. The build stays plain Go, so
the gate runs the `go` commands directly.

The module root is the command: `main.go` is a thin wrapper over
`cmd.Execute`, and the `cmd` package holds the flags, boot and session
resolution. That layout is the one `github.com/dat267/min` uses; installing this
port is `pier update` (its release asset), not
`go install github.com/dat267/pier@latest`, which would publish the module to
proxy.golang.org.

The full gate (CI) runs with `GOTRACEBACK=all` so a hung test prints every
goroutine, and with `-race -count=2`. The real-binary PTY tests reuse a prebuilt
binary via `PIER_TEST_BIN` (CI builds `.` first); locally they fall back to
`./bin/pier`, then build once into a temporary directory if needed. One pinned
Ubuntu 24.04 runner executes the native gates, then cross-builds and cross-`vet`s
Windows (both arches), Linux/arm64, Android/arm64 and macOS (both arches).
`vet` also compiles every `_test.go`. Keeping the targets on one runner avoids
matrix jobs failing before execution when GitHub cannot acquire hosted runners.
These checks guard against
`syscall` code that exists on only some OSes (`SysProcAttr{Setpgid}`,
`syscall.Kill`, `Stat_t.Ctim`) reaching a user's `go install`.

Keep the suite quick. CI adds race-detector overhead and runs each test twice;
measure changes rather than assuming a fixed slowdown or wall-time budget.
The same rules apply to the local `-count=1` loop:

- Size fixtures to the *property*, not to production scale. The projection tests
  assert ratios (window vs session, warm memo vs cold), so a few hundred
  message pairs discriminate as well as a few thousand; at 60k appends one test
  cost 44 s under `-race` on its own. Do not grow a fixture back without
  re-measuring the package.
- Avoid `testing.Benchmark` *calls* in ordinary tests: `testing.Benchmark`
  has a hard one-second floor per measurement, so two scaling tests paid ~5 s
  of pure timer wait. Use `allocatedBytesPerRun` (`coding/allocmeasure_test.go`)
  for bytes and `testing.AllocsPerRun` for counts. Proper `func BenchmarkX`
  declarations are fine — they only run under `-bench` and never execute in
  the plain test run (`coding/session_load_bench_test.go`).
- Never sleep a spec-mandated interval in a test. RFC 8628's device-code poll
  interval is a whole second, which made the OAuth tests spend ~25 s asleep;
  they swap `deviceCodeSleep` (`ai/oauthpkce.go`) for a millisecond instead.
  Copilot and Kimi retry tests replace `oauthRetrySleep` to record and assert
  requested backoff delays without sleeping through them.
- `-short` skips real-binary PTY tests and whole-module SSA analysis. It is
  useful for a targeted loop, not a substitute for the full gate.
- **A test must never spawn the developer's editor.** `EditInExternalEditor`
  hands this process's console (`os.Stdin`/`os.Stdout`) and its environment to
  whatever `VISUAL`/`EDITOR` resolves to — correct when a user pressed ctrl+g,
  wrong inside a test run. Windows makes it worse: the spawn is `cmd /c
  <command> <tempfile>`, and a command that cannot be executed (the test handed
  it a POSIX `editor.sh`) hands the temp file to the shell's association
  fallback, which is how running the suite opened VS Code on random temp paths.
  Tests stub `externalEditorRunner` (`stubExternalEditorRunner`), and `TestMain`
  pins `VISUAL`/`EDITOR` to a path that does not exist so an accident fails
  instead of opening an editor.

The interactive mode reads credentials from `~/.pi/agent/auth.json` (or
`PI_CODING_AGENT_DIR`). `/login` is wired and the CLI installs a browser
opener (`installBrowserOpener` → `coding.OpenBrowser`, the port of
`utils/open-browser.ts`: `open` on macOS, `rundll32 url.dll,FileProtocolHandler`
on Windows, `xdg-open` elsewhere, never a shell), so the OAuth browser flow
opens the authorization URL. `--offline` skips the catalog refresh.

## Concurrency architecture

The interactive mode is a single-writer UI loop, not mutex-guarded shared state
(upstream is single-threaded; every Go-side lock was invented to bridge that
gap, and **D143** records the divergence). Producers only enqueue:
`sessionEventQueue` has a lossless channel and a latest-wins partial channel,
and blocking work (a turn, session writes, settings persists, theme loads, the
transcript pre-render, clipboard) runs off the loop through `internal/offloop`
queues or `RunWiring.RunWork`. D199 separates mandatory session/settings queue
work from optional theme, transcript warming and keyboard clipboard reads:
shutdown cancels optional contexts and drops waiting optional tasks, then drains
accepted saves without a deadline. Running optional work may finish later but
must discard canceled results, including callbacks already posted to the UI.
D200 extends ownership to all interactive clipboard copy paths and explicit
detached work. The forwarding UI reference gates posted results after teardown.
Permanent terminal teardown then shares a 2 s output grace, after accepted saves
have drained; a timeout can lose final display output, never accepted saves.
Temporary stops remain lossless and unbounded, and the resume hint uses the
terminal FIFO only while output grace remains.
D201 bounds normal optional admission: four clipboard requests, four parallel
explicit detached jobs, and one running plus one newest-pending theme load.
Busy rejections remain visible; accepted saves still have lossless admission.
D202 admits optional OSC 52 packets off-loop within 256 KiB committed output,
and defers redundant title/progress hints until recovery to 128 KiB. Essential
protocol output and committed frames remain lossless during normal operation.
The direct-output inventory and exceptions are in `docs/terminal-output.md`.
D203 warms transcript width changes cooperatively: at most 64 direct children
per Render call, retaining the previous complete frame until ready. The resize
wrapper remains owner-loop state; it never warms attached components on workers.
D204 prepares built-in assistant Markdown of 64 KiB or more on private worker
snapshots, with four total admitted jobs and owner-applied generation checks.
Standalone renderers and custom transformers retain their synchronous defaults.
D205 uses that same four-job budget for text-only expanded read and both shell
result modes at 64 KiB. Workers capture content/theme/metadata; elapsed timers
and result-generation application stay on-owner. Custom callbacks remain
synchronous, and detached tool warming never submits nested work.
D206 finishes eligible tool Box padding/backgrounds in the same private worker
job and adopts its cache on-owner after the generation check. Header snapshots
and elapsed clocks stay on-owner; original mouse callbacks are not replaced.
D207 retains the completed tool frame while replacements prepare, avoiding
owner-side repainting of old raw output after invalidation. Clock rows remain
live; resize hit testing uses displayed geometry. D208 leaves unadmitted/busy
work unversioned so skipping parents retry; accepted pending work uses owner
revisions and updates clock rows in place without copying the complete frame.
D209 reuses flattened Container storage for first-child changes as well as tail
changes, preserving revision/change-offset signals and clearing removed string
slots on shrink. Invalidation and capacity growth still rebuild on-owner.
Tool frame lifecycle state is owned by `coding/interactive/toolframes.go`:
callers capture safe input and delegate rendering/revisions/hit testing, never
inspect pending/ready or prepared/displayed state. The executor remains the
owner-applied preparation seam; no attached state is accessed by workers.
The main run-loop select has a `ctx.Done()` arm;
pre-paint event drains are non-blocking and capped at 64 events, giving both
ready event classes service (D197). Assistant partials carry a UI-local stream
generation so delayed updates cannot overwrite the next message (D196).
Blocking session-event and terminal-input sends are released by run-context
cancellation or queue/input closure. Standalone startup screens instead end
when their selection is settled.

Stages 2 (rendering) and 3 (input and signals) are also on the loop. Slow phases
over `PIER_STALL_MS` (default 100; `0` disables) and keystroke round trips over
`PIER_INPUT_LAT_MS` (default 50) append to `<agentDir>/pier-stall.log` and
`<agentDir>/pier-input-latency.log`, respectively. Stall records include
goroutine dumps; input-latency records split the read, paint and write delays.
The full design and performance measurements are in `docs/architecture.md`.

## Architecture of the interactive mode

Upstream `interactive-mode.ts` is one ~4000-line class; the port splits it into
narrow wirings in `coding/interactive` (`app.go` is the composition layer, and
each `*Wiring` has a constructor next to its struct). The session projection
lives in one module (`coding/session_projection.go`): it resolves the branch
path, the compaction window, the context settings and the messages in one walk,
caches by branch version, and supplies `CurrentSystemMessage`.
`LatestCompaction` and `ContextSignature` inspect the current branch without
decoding the full message projection. Cache-miss notices are O(1)
(`SessionManager.CacheMissFor`); large sessions attach a trailing window and
materialize the rest per loop beat.

Do not assemble the projection by hand, re-scan session entries per message, or
run a value-shaped loop on the UI thread. The deep dives and their measurements
are in `docs/architecture.md`.

## Conventions and gotchas

- **A session always has its collaborators.** `SessionConfig.Control` carries
  the model runtime, settings manager, tool registry and toggles;
  `NewAgentSession` installs it (or an empty block), so no session method guards
  against a half-built session. The fields inside the block stay optional
  (`ModelRuntime` nil means no auth/catalog, `Settings` nil means no live
  settings), and an empty block means auto-compaction and auto-retry are off.
- **UI ownership and retained locks.** Renderer, container, editor, screen,
  selector and input-buffer state belongs to the owner loop. UI-state mutexes
  and the renderer's internal timer were retired (D146/D147); do not restore
  shared mutation from producers. Workers enqueue events or use `UI.Post` to
  apply changes on the owner. Narrow locks remain for handoff, terminal I/O,
  logging and cross-goroutine core state; `docs/locks.md` is authoritative for
  the UI lock inventory. Never invoke user code or callbacks while holding a
  retained mutex: snapshot under the lock, deliver outside it. D136–D139
  document the historical deadlocks; `internal/uiblock` enforces the current
  non-blocking UI-loop invariant. Audit ownership and callback re-entrancy
  whenever adding a worker, handoff or lock.
- **Tests never write into the shared temp dir.** `os.TempDir()` follows
  `TMPDIR`, so a test that exercises temp-file code paths pins it
  (`t.Setenv("TMPDIR", t.TempDir())`). Two leaks came from ignoring this: 492
  empty `pi-agent-dir*` directories, and 215 `pi-typing-probe-*.log` files.
- **Render cost scales with content.** The loop is one goroutine, so a
  value-shaped loop on it is a freeze rather than a slowdown. `renderInlineTokens`
  accumulated a rendered message into a string with `result += ...` while looping
  once per token, so every append copied the whole accumulation: one 1 MB message
  allocated 46 GB and took 11.5 s on the loop (100 KB: 113 ms, which is what a
  captured "render" phase of 126 ms was). Use `strings.Builder` whenever the
  iteration count scales with the input, and read a GC-dominated CPU profile on
  the render path as this class — `TestMarkdownRenderScalesWithInputSize` bounds
  the allocation volume so a regression cannot come back unnoticed.
- **Interactive regression tests.** D192 retired the real-binary D136–D139
  mutex-deadlock watchdog flows. `coding/interactive/ptyflow_test.go` now
  covers submit, model selection, scroll and shutdown in process; despite its
  name, it does not spawn a PTY. `internal/uiblock` checks the blocking-work
  invariant statically. Other PTY tests still drive the real binary for
  startup, trust, compaction and typing behavior. They use `$PIER_TEST_BIN`,
  else `./bin/pier`, else build once into a temporary directory. CI prebuilds
  the binary and sets `PIER_TEST_BIN`. `ptywatch_test.go` retains the shared
  PTY helpers and `TestMutexBlockedDetectorHasTeeth`, which deliberately
  deadlocks a helper process to prove the stack parser detects it.

- **Lock inventory** (retained and retired locks, and the retirement invariant)
  is in `docs/locks.md`: no user code under a lock; snapshot under and deliver
  outside.

- **Go 1.27 quirk.** Function literals passed as arguments need an explicit
  result type when the parameter's function type has one.
- **RE2 regex** (no lookaround/backreferences). Put `-` first in a character
  class; `\s` is invalid inside `[...]`.
- **JSON struct tags** are required for field-name parity with upstream.
- **`range` is a keyword**; avoid it as an identifier.
- **`filepath.Rel`** returns `"."` for equal paths (Node returns `""`);
  `path.Clean` drops trailing slashes (`path.posix.normalize` keeps them).
- **Nil vs empty slices**: normalize in render comparisons.
- **Test seams already wired**: `SetCustomThemesDir`, `SetRegisteredThemes`,
  `tui.SetKeybindings`, `SetSessionFileDeleter`, `SetClipboardCopier`,
  `SetClipboardReader`, `SetBrowserOpener`, `coding.SetPackageDir`. The
  interactive test screens call `DisableAutoRender()` (now a no-op: every
  renderer is caller-driven since D146) so tests drive rendering with
  `RenderNow`.
- **Goldens**: `tui/testdata/`, `coding/interactive/testdata/`,
  `coding/testdata/` hold `label => JSON` lines generated by driving the
  upstream TypeScript with Node; Go replays the same scripted input.

## Divergences

Where the reference relies on JSON/JS semantics Go has no equivalent for, where
a reference behavior is a defect, or wherever this implementation makes a
deliberate choice of its own, it departs from the reference: a numbered **D-row**
in a code comment at the point of divergence, with the reproducing scenario, and
an entry in `docs/DIVERGENCES.md` (the authoritative list and numbering).
Prefer a D-row over silently approximating the reference.

## Out of scope (documented)

Native clipboard, kitty/iterm image transport internals beyond the
line-detection helpers, extension mechanics, the package manager, and the rpc
mode. Mermaid rendering needs `grok-mermaid` and is inert.

