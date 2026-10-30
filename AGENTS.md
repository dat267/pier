# AGENTS.md

Project rules for coding agents. For user help see [README.md](README.md); use
`git log` for release and change history; design, D-rows and lock inventory are
in [ARCHITECTURE.md](ARCHITECTURE.md). `TODO.md` contains only reproduced,
unresolved findings.

## Upstream parity

- `./pi` is the pinned upstream implementation (v1.4.0, `4a080252`); pier is a
  near-identical Go port. Read its TypeScript to preserve behavior, not an
  installed bundle. `just reference` verifies the pin.
- Treat JSON as byte-parity: persist and send through `ai.MarshalJSON`, never
  `encoding/json.Marshal`. Preserve JavaScript key order, stringify behavior and
  truthiness where upstream depends on them.
- Every behavior change needs a test citing the upstream source. Cite the
  upstream file/commit in the commit message, using `type(scope): summary`;
  document intentional differences with a reproducing D-row comment and an
  entry in `ARCHITECTURE.md`.

## Structure

Packages mirror upstream: `ai`, `agent`, `coding` (including `interactive`),
`tui`, `mcp`, and `coding/rpcclient`. `ai`/`agent` must not import `coding` or
`tui`; `tui` must not import `coding`. The root `main.go` only calls
`cmd.Execute`; helpers live in `internal/`.

## Build and verification

```sh
just check                                  # gofmt, vet, tests
just cross                                  # cross-build and vet CI targets
go test -count=1 ./coding -run TestName
go test -short ./...                       # skips slower real-binary PTY tests
```

Go 1.27 is required; CGO is off. CI runs the race gate and cross-builds Windows,
Linux/arm64, Android/arm64 and macOS. Treat race-only failures as bugs. PTY tests
use `$PIER_TEST_BIN`, then `bin/pier`, then a temporary build.

Keep tests quick: small fixtures, allocation checks instead of loops, fake
sleeps, and never launch the developer's editor. Tests must use temporary agent
state (`t.TempDir`/`t.Setenv`), not the real agent directory. Package `TestMain`s
pin relevant environment and golden timezone; test file-reporting stubs should
write then rename so readers never see partial files.

## UI ownership

The interactive UI is single-writer. Producers enqueue; workers do blocking
work off-loop and never mutate attached UI state. Audit ownership, shutdown and
callback re-entrancy when adding workers, handoffs or locks. Never run user code
under a retained mutex; snapshot callbacks under lock and deliver after unlock.
See the lock inventory and D-rows in `ARCHITECTURE.md`.

Optimize only with a reproducible test, benchmark or report. Avoid speculative
caches and fast paths, hand-built session projections, per-message rescans, or
value-shaped loops on the UI thread.

## Gotchas

- `NewAgentSession` installs its collaborators; an empty control block disables
auto-compaction and retry.
- `filepath.Rel` returns `"."` for equal paths (Node returns `""`); `path.Clean`
  drops trailing slashes; render tests distinguish nil from empty slices.
- Go 1.27 may require explicit result types on function-literal arguments. RE2
  has no lookaround and no `\s` inside character classes.
- Catalog JSON is generated, not hand-edited. Regenerate in a scratch copy of
  `./pi`: run upstream `generate-models.ts --strict --data-only`, then
  `scripts/gen_catalog.py`, `scripts/gen_image_catalog.py`, and
  `scripts/gen_classifier_catalog.py <scratch-pi> openai-decisions` for
  `ai/classifier_catalog.json` and
  `scripts/gen_classifier_catalog.py <scratch-pi> typesafe-system-one` for
  `ai/typesafe_classifier_catalog.json` and
  `scripts/gen_classifier_catalog.py <scratch-pi> cloudflare-workers-ai-system-one`
  for `ai/cloudflare_classifier_catalog.json`; the upstream generator fetches
  models.dev and rewrites its checkout.
- Upstream goldens in `*/testdata/` are Node-generated; regenerate as each
  fixture instructs and never hand-edit them.

## Out of scope

Native (Bun FFI) clipboard, Photon image formats, extensions/package manager,
full stdio RPC, and Mermaid rendering. See `ARCHITECTURE.md` for rationale.
