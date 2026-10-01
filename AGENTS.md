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
- Dependencies are offline-cached only: `golang.org/x/text`, `x/term`, `x/sys`,
  and `x/tools` (`internal/uiblock`'s SSA analysis).
- Upstream checkout lives in the workspace at `./pi` (gitignored): clone
  `https://github.com/earendil-works/pi` there at tag `v1.0.0`
  (`a13d35a74`; see the README's pin table). The released tags are the
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
go test -race -count=2 -timeout 300s ./...   # the completion gate (all packages)

# the CLI (binary derives its display name from the file name)
go build -o bin/pier .
./bin/pier --help
./bin/pier -r                       # resume the newest session
```

A `justfile` wraps the commands above (`just`, `just check`, `just --list`;
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

The gate runs with `GOTRACEBACK=all` in CI so a hung test prints every
goroutine. The PTY watchdogs reuse a prebuilt binary via `PIER_TEST_BIN`
(CI builds `.` first); locally they fall back to `./bin/pier`. The test job
builds only linux/amd64, so a second CI job cross-builds and cross-`vet`s the
port for Windows, macOS (both arches), the BSDs, Solaris, DragonFly and AIX —
`vet` also compiles every `_test.go`. That matrix is the guard against
`syscall` code that exists on only some OSes (`SysProcAttr{Setpgid}`,
`syscall.Kill`, `Stat_t.Ctim`) reaching a user's `go install`.

Keep the suite quick — the race detector slows every path 5-10x, and the gate
runs each test twice (`-p 8` overlaps the test-binary builds; the whole gate is
~50 s, `-race -count=1` ~30 s on four cores):

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
- `-short` skips the PTY watchdogs (they wait on real rendering).
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
`PI_CODING_AGENT_DIR`). `/login` is wired, but the CLI does not install a
browser opener (`interactive.SetBrowserOpener`), so the OAuth browser flow is a
no-op; seed credentials with upstream pi or by hand. `--offline` skips the
catalog refresh.

## Concurrency architecture

The interactive mode is a single-writer UI loop, not mutex-guarded shared state
(upstream is single-threaded; every Go-side lock was invented to bridge that
gap, and **D143** records the divergence). Producers only enqueue:
`sessionEventQueue` has a lossless channel and a latest-wins partial channel,
and blocking work (a turn, session writes, settings persists, theme loads, the
transcript pre-render, clipboard) runs off the loop through `internal/offloop`
queues or `RunWiring.RunWork`. Every consumer select has a `ctx.Done()` arm; a
producer parked on a full channel is released by cancellation or `Close`.

Stages 2 (rendering) and 3 (input and signals) are also on the loop. Slow phases
over `PIER_STALL_MS` (default 100; `0` disables) and keystroke round trips over
`PIER_INPUT_LAT_MS` (default 50) append to `<agentDir>/pier-stall.log` and
`<agentDir>/pier-input-latency.log` with goroutine dumps. The full design and the
performance measurements are in `docs/architecture.md`.

## Architecture of the interactive mode

Upstream `interactive-mode.ts` is one ~4000-line class; the port splits it into
narrow wirings in `coding/interactive` (`app.go` is the composition layer, and
each `*Wiring` has a constructor next to its struct). The session projection
lives in one module (`coding/session_projection.go`): it resolves the branch
path, the compaction window, the context settings and the messages in one walk,
caches by branch version, and is the single answer for `CurrentSystemMessage`,
`LatestCompaction` and `ContextSignature`. Cache-miss notices are O(1)
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
- **Locking (hard-won).** `tui.Container` methods lock `Container.mu`
  (session-event goroutines mutate the chat/document containers while the
  render timer renders them; upstream is single-threaded). Locks always nest
  parent→child; callbacks/invalidate fan-out is snapshotted under the lock and
  delivered outside it. Never call out to user code / callbacks while holding
  a mutex; the callback may re-enter the same object or trigger shutdown.
  `Render` runs under the render lock and takes component locks; input runs
  under the render lock but not the renderer lock. Known-good patterns:
  snapshot listeners/handlers under the lock, deliver outside it; collect
  emissions and flush after unlock; run `OnSubmit`/`OnData`/`OnPaste` and
  selector callbacks outside component locks. D136–D139 are the concrete bugs
  this caused (editor submit, model selector, terminal↔screen scroll, stdin
  buffer exit). A re-entrancy/lock-order audit is worth re-running after any
  new locking code.
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
- **PTY watchdogs.** `coding/interactive/ptywatch_test.go` drives the real
  binary through the D136–D139 flows (editor submit, model selector,
  fullscreen scroll, exit) inside a pty, then sends `SIGQUIT` and fails when
  the stack dump shows any goroutine parked in `sync.runtime_SemacquireMutex`.
  The tests use `$PIER_TEST_BIN`, else `./bin/pier`, else they build once into
  a temp dir (and skip if that fails), so the package gate stays fast. CI
  prebuilds the binary and sets `PIER_TEST_BIN`. `TestMutexBlockedDetectorHasTeeth`
  proves the parser still catches a genuine mutex deadlock.

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
an entry in `docs/DIVERGENCES.md` (range **D1–D176**). Prefer a D-row over
silently approximating the reference.

## Out of scope (documented)

Native clipboard, kitty/iterm image transport internals beyond the
line-detection helpers, extension mechanics, the package manager, and the rpc
mode. Mermaid rendering needs `grok-mermaid` and is inert.

