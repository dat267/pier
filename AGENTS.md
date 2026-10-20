# AGENTS.md

Read this first. `docs/PORTING.md` tracks per-area status, `docs/DIVERGENCES.md`
the intentional differences, plus `docs/architecture.md`, `docs/locks.md`,
`docs/rpc.md` and `docs/terminal-output.md`.

Scan the `<available_skills>` block before starting a task and read a matching
`SKILL.md` first (test-first, debugging, guardrails); loading it after the first
attempt is too late. Relative paths resolve against that directory.

## What this is

A from-scratch Go port of Mario Zechner's **pi**
(`github.com/earendil-works/pi`) and a sibling of its TypeScript monorepo, not a
fork. Module `github.com/dat267/pier`, Go 1.27, offline-cached modules only
(`go.mod` is authoritative). The reference checkout is `./pi` (gitignored),
pinned to `v1.3.0` (`71023aec`): `just reference` fails at another tag, and
`just reference-fetch` refreshes tags without moving it. Diff through its
sources (`node --experimental-strip-types`, `FORCE_COLOR=1` for chalk parity),
and say which tag a review read.

## Reference rules

1. **pi is the reference, not the spec.** Read the TypeScript; never infer
   behavior. Cite the reference file in each commit. A departure is a deliberate
   feature, not an approximation to correct silently.
2. **JSON is byte-parity.** Sessions, transcripts and wire payloads round-trip
   identically: key names, key order, no HTML escaping. Use `ai.MarshalJSON`,
   never `encoding/json.Marshal`, for persisted or sent data. JS insertion order
   is tracked explicitly where it matters.
3. **JS semantics where pi relies on them**: object order, `JSON.stringify`,
   truthiness at API boundaries. Departures get a numbered **D-row** comment with
   the reproducing scenario and an entry in `docs/DIVERGENCES.md`.
4. **Every behavior change is test-observed**; Go-only tests cite upstream.

## Layout

`main.go` wraps `cmd`; packages mirror upstream `packages/`: `ai`, `agent`,
`coding`, `coding/interactive`, `tui`, `coding/rpcclient`. Port-local:
`internal/offloop`, `internal/uiblock`, `internal/goal` (D223),
`internal/markdown`, `internal/highlight`; `scripts` generates catalogs.

## Build, test, run

```bash
go build ./... && go vet ./... && gofmt -l .   # the local gate
go test -count=1 ./...
```

CI adds `go test -race -count=2 -timeout 300s ./...` with `GOTRACEBACK=all`; do
not run the race detector locally except to reproduce a CI failure, and treat a
test that fails only under `-race` as a bug. `just` wraps the gate; install with
`pier update`, never `go install github.com/dat267/pier@latest`.

CI runs one pinned Ubuntu runner, then cross-builds and cross-`vet`s Windows,
Linux/arm64, Android/arm64 and macOS, so OS-only `syscall` code cannot break
`go install`. PTY tests reuse `PIER_TEST_BIN`, else `./bin/pier`, else build
once; `-short` skips them and SSA.

Keep fixtures sized to the property (60k appends cost 44 s under `-race`); use
`allocatedBytesPerRun`/`AllocsPerRun`, not `testing.Benchmark` calls; swap
`deviceCodeSleep`/`oauthRetrySleep` instead of sleeping; never spawn the editor
(stub `externalEditorRunner`). Auth: `~/.pi/agent/auth.json` (or
`PI_CODING_AGENT_DIR`); `--offline` skips the catalog refresh.

## Concurrency and the interactive mode

The UI is a single-writer loop (D143; upstream is single-threaded). Producers
only enqueue: `sessionEventQueue` is lossless plus latest-wins partials, and
blocking work runs off-loop via `internal/offloop`/`RunWork`. D199-D209 own
shutdown draining, clipboard and detached-work ownership, admission bounds,
OSC 52 and hint deferral, cooperative resize warming, and 64 KiB Markdown/tool
rendering with owner-applied generation checks, retained frames and flattened
containers. The loop select has a `ctx.Done()` arm, pre-paint drains cap at 64
events (D197), and assistant partials carry a UI-local stream generation (D196).
Heavy handler, render and watcher work runs off-loop or is chunked, and
`TestStreamingDeltaCostIndependentOfTranscriptSize` bounds streaming deltas.

**Optimize only what is reproducible**: a red test, a benchmark or a concrete
report first; speculative caches, thresholds, fast paths and off-loop plumbing get
reverted. Do not assemble the session projection by hand, re-scan entries per
message, or run a value-shaped loop on the UI thread.

`interactive-mode.ts` is one ~4000-line class, split into `*Wiring` types in
`coding/interactive/app.go`. `coding/session_projection.go` is one
branch-version-cached walk over branch, compaction window, context settings and
messages; `LatestCompaction`, `ContextSignature` and `CacheMissFor` avoid
decoding it, and large sessions attach a trailing window.

## Conventions and gotchas

- `SessionConfig.Control` carries the model runtime, settings, tool registry and
  toggles; `NewAgentSession` installs it (or an empty block), so no method guards
  a half-built session. An empty block means auto-compaction and retry are off.
- UI state belongs to the owner loop; workers enqueue events or use `UI.Post`.
  Narrow locks remain for handoff, terminal I/O, logging and core state
  (`docs/locks.md`). Never run user code under a retained mutex: snapshot under,
  deliver outside. `internal/uiblock` enforces the non-blocking loop.
- Tests pin `TMPDIR`; a stub that reports through a file writes `out.part` then
  `mv`s it, so a polling reader never sees an empty `out`.
- Use `strings.Builder` whenever iteration count scales with input;
  `TestMarkdownRenderScalesWithInputSize` bounds allocations.
- `ptyflow_test.go` covers the in-process flows (no PTY); `internal/uiblock`
  checks the blocking invariant statically; other PTY tests drive the real
  binary; `ptywatch_test.go` keeps `TestMutexBlockedDetectorHasTeeth`.
- Go 1.27: function literals passed as arguments need an explicit result type.
  RE2: no lookaround, `-` first in a class, no `\s` inside `[...]`. `filepath.Rel`
  returns `"."` for equal paths (Node `""`); `path.Clean` drops trailing slashes;
  normalize nil vs empty slices when comparing renders.
- Seams are wired (`SetCustomThemesDir`, `tui.SetKeybindings`,
  `SetClipboardCopier`, `coding.SetPackageDir`); screens use `RenderNow`. Goldens
  in `*/testdata/` are `label => JSON` lines generated from upstream with Node.

## Out of scope (documented)

Native (Bun FFI) clipboard (platform commands instead, D106); Photon's Lanczos3
and WebP/BMP decoding (bilinear resizer, stdlib codecs, D26); extensions and the
package manager; a full stdio RPC (`docs/rpc.md`, D235); Mermaid (inert).
