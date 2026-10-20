# AGENTS.md

Read this first. Deep dives: `docs/PORTING.md` (per-area status, pin),
`docs/DIVERGENCES.md` (intentional differences), `docs/architecture.md`,
`docs/locks.md`, `docs/rpc.md`, `docs/terminal-output.md`.

Scan the `<available_skills>` block before starting a task and read a matching
`SKILL.md` first (test-first, debugging, guardrails); loading it after the first
attempt is too late. Relative paths resolve against that directory.

## What this is

A from-scratch Go port of Mario Zechner's **pi**
(`github.com/earendil-works/pi`), a sibling of its TypeScript monorepo, not a
fork. Module `github.com/dat267/pier`, Go 1.27, offline-cached modules only
(`go.mod` is authoritative). The reference checkout is `./pi` (gitignored),
pinned to `v1.3.0` (`71023aec`); `just reference` fails at another tag and
`just reference-fetch` refreshes tags without moving it. Diff against those
sources, not an installed bundle, and say which tag a review read.

## Reference rules

1. **pi is the reference, not the spec.** Read the TypeScript; never infer
   behavior. Cite the reference file in each commit. A departure is a deliberate
   feature, not an approximation to correct silently.
2. **JSON is byte-parity.** Sessions, transcripts and wire payloads round-trip
   identically, including key order and HTML escaping: persist and send through
   `ai.MarshalJSON`, never `encoding/json.Marshal`. JS insertion order matters.
3. **JS semantics where pi relies on them**: object order, `JSON.stringify`,
   truthiness at API boundaries. Departures get a **D-row** comment with the
   reproducing scenario and an entry in `docs/DIVERGENCES.md`.
4. **Every behavior change is test-observed**; Go-only tests cite upstream.

## Layout

Packages mirror upstream `packages/`: `ai`, `agent`, `coding`,
`coding/interactive`, `tui`, `coding/rpcclient`, with `main.go` wrapping `cmd`.
Port-local helpers live in `internal/`, `scripts/` generates catalogs, and
`docs/` holds the status and design documents.

## Build, test, run

```bash
go build ./... && go vet ./... && gofmt -l .   # the local gate
go test -count=1 ./...
```

CI runs the same gate with the race detector, twice, and with `GOTRACEBACK=all`.
Treat a test that fails only under `-race` as a bug, but leave the race gate to
CI. `just` wraps the gate; install with `pier update`, never `go install`.

CI also cross-builds and cross-`vet`s Windows, Linux/arm64, Android/arm64 and
macOS on one runner, so OS-only `syscall` code cannot break `go install`. PTY
tests reuse a prebuilt binary or build one, and `-short` skips them.

Keep the suite quick: size fixtures to the property under test, use allocation
counters instead of benchmark loops in ordinary tests, fake sleeps instead of
sleeping, and never spawn the developer's editor from a test.

## Concurrency and the interactive mode

The UI is a single-writer loop; upstream is single-threaded and every Go lock
bridges that gap (D143). Producers only enqueue, and blocking work (turns,
session writes, settings, themes, transcript pre-rendering, clipboard) runs
off-loop. Ownership, shutdown draining, admission bounds, terminal output and
tool-frame lifecycle are documented row by row in `docs/DIVERGENCES.md` and
`docs/locks.md`; workers never mutate attached UI state, never run user code
under a retained mutex, and callbacks are snapshotted under the lock and
delivered outside it.

**Optimize only what is reproducible**: a red test, a benchmark or a concrete
report first; speculative caches, thresholds, fast paths and off-loop plumbing
get reverted. Do not assemble the session projection by hand, re-scan entries
per message, or run a value-shaped loop on the UI thread.

`interactive-mode.ts` is one large class; the port splits it into wirings
composed in `coding/interactive/app.go`, and the session projection lives in one
module cached by branch version (`docs/architecture.md`).

## Conventions and gotchas

- A session always has its collaborators: `NewAgentSession` installs the control
  block, so no method guards a half-built session; optional fields stay optional
  and an empty block disables auto-compaction and retry.
- UI state belongs to the owner loop; workers post events or use `UI.Post`.
  `internal/uiblock` enforces the non-blocking loop; audit ownership and callback
  re-entrancy when adding a worker, handoff or lock.
- Tests pin `TMPDIR`; a file-reporting stub writes a temp name then renames, so a
  polling reader never sees an empty file.
- Use `strings.Builder` whenever iteration count scales with input.
- Go/JS parity traps: `filepath.Rel` returns `"."` for equal paths (Node `""`),
  `path.Clean` drops trailing slashes, and renders compare nil vs empty slices.
  Go 1.27 needs an explicit result type on some function-literal arguments; RE2
  has no lookaround and no `\s` inside `[...]`.
- Goldens in `*/testdata/` are `label => JSON` lines generated from upstream with
  Node; Go replays the same scripted input.

## Out of scope (documented)

Native (Bun FFI) clipboard; Photon's Lanczos3/WebP/BMP decoding; extensions and
the package manager; a full stdio RPC milestone; Mermaid rendering. See
`docs/PORTING.md` and `docs/DIVERGENCES.md` for the rationale.
