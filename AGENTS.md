# AGENTS.md

Project rules for coding agents. For user help see [README.md](README.md); use
`git log` for release and change history; design, the stdio RPC contract and the
lock inventory are in [ARCHITECTURE.md](ARCHITECTURE.md), and D-rows (deliberate
divergences) are in [DECISIONS.md](DECISIONS.md). `TODO.md` contains only
reproduced, unresolved findings.

## Upstream parity

- `./pi` is the pinned upstream implementation (v1.5.0, `f9a26365`); pier is a
  near-identical Go port. Read its TypeScript to preserve behavior, not an
  installed bundle. `just reference` verifies the pin. D-rows and test comments
  cite the revision a behavior was verified against, so older ones still name
  v1.4.0 on purpose.
- Treat JSON as byte-parity: persist and send final bytes through
  `ai.MarshalJSON`, not `encoding/json.Marshal`. `encoding/json` remains
  appropriate for internal parsing, round-trips and `MarshalJSON` method
  internals, but not for final persisted or wire bytes. Preserve JavaScript key
  order, stringify behavior and truthiness where upstream depends on them.
- Parity never outranks responsiveness. When an upstream behavior would add a
  measured stall, a per-keystroke or per-frame cost, or worse UX, keep the port
  fast and pleasant, and record the difference as a D-row instead of copying it.
  A cache or fast path needs a measured baseline, not a guess.
- When the port is measurably slower than the pinned reference for the same
  input (a benchmark, a profile, or a supplied workload), iterate on the Go code
  until a repeatable, same-workload benchmark shows it beats the reference while
  preserving behavior. Pin behavior with tests. Do not keep a patch that only
  wins on a pathological input while regressing representative workloads; keep
  optimizing or revert it. Record the baseline, reference result, final result,
  and approach in a D-row.
- Every behavior change needs a test citing the upstream source. Cite the
  upstream file/commit in the commit message, using `type(scope): summary`;
  document intentional differences with a reproducing D-row comment and an
  entry in `DECISIONS.md`, inserted in numeric order and naming its test.

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

Go 1.27 is required. Builds and releases set `CGO_ENABLED=0`; the race gate
needs cgo enabled (`just test-race` and CI). CI runs the race gate on linux/amd64
and cross-builds `windows/{amd64,arm64}`, `linux/arm64`, `android/arm64`, and
`darwin/{amd64,arm64}`. Treat race-only failures as bugs. PTY tests use
`$PIER_TEST_BIN`, then `bin/pier`, then a temporary build.

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
See the lock inventory in `ARCHITECTURE.md` and the D-rows in `DECISIONS.md`.

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
  `./pi`: from `<scratch-pi>/packages/ai`, run
  `node catalog/generate.ts --strict --data-only`. Then, from this repo, run:

  ```sh
  python3 scripts/gen_catalog.py <scratch-pi> > ai/models_catalog.json
  python3 scripts/gen_image_catalog.py <scratch-pi> > ai/image_catalog.json
  python3 scripts/gen_classifier_catalog.py <scratch-pi> openai-decisions \
    > ai/classifier_catalog.json
  python3 scripts/gen_classifier_catalog.py <scratch-pi> typesafe-system-one \
    > ai/typesafe_classifier_catalog.json
  python3 scripts/gen_classifier_catalog.py <scratch-pi> cloudflare-workers-ai-system-one \
    > ai/cloudflare_classifier_catalog.json
  ```

  The upstream generator fetches models.dev and rewrites its checkout.
- Upstream goldens in `*/testdata/` are Node-generated; regenerate as each
  fixture instructs and never hand-edit them.

## Out of scope

Native (Bun FFI) clipboard, Photon image formats, extension mechanics and the
package manager, full upstream stdio-RPC compatibility (the supported subset is
documented in `ARCHITECTURE.md`), and Mermaid rendering. See `DECISIONS.md`
for rationale.
