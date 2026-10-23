# AGENTS.md

Read this first. Deep dives: `docs/PORTING.md` (authoritative per-area status,
pin), `docs/DIVERGENCES.md` (intentional differences), `docs/architecture.md`,
`docs/locks.md`, `docs/rpc.md`, `docs/terminal-output.md`. `README.md` is the
user-facing summary, not the port status.

## What this is

A from-scratch Go port of Mario Zechner's **pi**
(`github.com/earendil-works/pi`), a sibling of its TypeScript monorepo, not a
fork. Module `github.com/dat267/pier`, Go 1.27, offline-cached modules only
(`go.mod` is authoritative). The reference checkout is `./pi` (gitignored),
pinned to `v1.3.0` (`71023aec`); `just reference` fails at another tag and
`just reference-fetch` refreshes tags without moving it. Treat `./pi` as
read-only. Diff against those sources, not an installed bundle, and say which
tag a review read.

## Reference rules

1. **pi is the reference, not the spec.** Read the TypeScript; never infer
   behavior. Cite the reference file (and upstream commit where one exists) in
   each commit, and use `type(scope): summary` messages (`fix(coding): …`). A
   departure is a deliberate feature, not an approximation to correct silently.
2. **JSON is byte-parity.** Sessions, transcripts and wire payloads round-trip
   identically, including key order and HTML escaping: persist and send through
   `ai.MarshalJSON`, never `encoding/json.Marshal`. JS insertion order matters.
3. **JS semantics where pi relies on them**: object order, `JSON.stringify`,
   truthiness at API boundaries. Departures get a **D-row** comment with the
   reproducing scenario and an entry in `docs/DIVERGENCES.md`.
4. **Every behavior change is test-observed**; Go-only tests cite upstream.

## Layout

Packages mirror upstream: `ai` (providers and wire), `agent` (loop), `coding`
(session, tools, settings) with `coding/interactive` (UI), `tui` (renderer and
markdown), `mcp`, and `coding/rpcclient`. `ai` and `agent` must not import
`coding` or `tui`, and `tui` must not import `coding`; `coding` composes them.
The root `main.go` only calls `cmd.Execute`; boot, flags and modes live in
`cmd`. Helpers are in `internal/`, catalog generators in `scripts/`.

## Build, test, run

```bash
just check                                  # gofmt -l, go vet, go test (local gate)
just cross                                  # cross-build + vet the CI matrix
go test -count=1 ./coding -run TestName     # one package / one test
go test -short ./...                        # skips the slower real-binary PTY tests
```

`just build` writes `bin/pier`; `just install` wraps local `go install`.
Released installs and updates use `pier update`, not `go install ...@latest`
(the module is not published that way). CGO is off everywhere.

CI runs the same gate with `-race -count=2 -timeout 300s` and `GOTRACEBACK=all`,
then cross-builds and vets windows (amd64/arm64), linux/arm64, android/arm64 and
darwin (amd64/arm64) on one runner. Treat a test that fails only under `-race` as
a bug, but leave the race gate to CI. OS-specific code needs build tags and must
survive `just cross`.

Real-binary PTY tests use `$PIER_TEST_BIN`, then `bin/pier`, then build a temp
binary; set `PIER_TEST_BIN` to avoid rebuilding. `-short` skips the slow ones.

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
- Package `TestMain`s pin `HOME`, `PI_CODING_AGENT_DIR`, `VISUAL`/`EDITOR`, and
  (in `coding/interactive`) the golden timezone; use `t.Setenv`/`t.TempDir` and
  never write the real agent directory. Tests pin `TMPDIR`; a file-reporting stub
  writes a temp name then renames, so a polling reader never sees an empty file.
- `ai/models_catalog.json` and `ai/image_catalog.json` are generated from the
  same upstream data: regenerate with `python3 scripts/gen_catalog.py ./pi >
  ai/models_catalog.json` and `python3 scripts/gen_image_catalog.py ./pi >
  ai/image_catalog.json` after running upstream's `generate-models.ts --strict
  --data-only`; never hand-edit.
- `TODO.md` holds only reproduced, unresolved findings; remove each one when its
  fix lands, not speculative work items.
- Use `strings.Builder` whenever iteration count scales with input.
- Go/JS parity traps: `filepath.Rel` returns `"."` for equal paths (Node `""`),
  `path.Clean` drops trailing slashes, and renders compare nil vs empty slices.
  Go 1.27 needs an explicit result type on some function-literal arguments; RE2
  has no lookaround and no `\s` inside `[...]`.
- Upstream goldens in `*/testdata/` (often `label => JSON` lines) are generated
  with Node; use the regeneration command in the test header and never hand-edit.

## Out of scope (documented)

Native (Bun FFI) clipboard; Photon's Lanczos3/WebP/BMP decoding; extensions and
the package manager; a full stdio RPC milestone; Mermaid rendering. See
`docs/PORTING.md` and `docs/DIVERGENCES.md` for the rationale.
