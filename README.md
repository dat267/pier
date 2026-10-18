# pier, a Go coding agent

A from-scratch Go implementation of an AI coding agent with a terminal UI that
ports the core features of Mario Zechner's
[pi](https://github.com/earendil-works/pi). The code follows its structure and
behavior closely, and where it departs, the departure is a deliberate feature.
Development conventions are in `AGENTS.md`.

## Reference pin

| What | Value |
|---|---|
| Repository | https://github.com/earendil-works/pi (cloned at `./pi`, gitignored) |
| Pin | `71023aec` (Release v1.3.0), the latest released tag |

Per-area status and the pin's change log are in `docs/PORTING.md`; every
deliberate departure is a numbered D-row in `docs/DIVERGENCES.md`.

## Status

The reference packages the CLI reaches all have counterparts: `ai` (ten wire
APIs, provider factories, the nine OAuth flows, image generation, overflow
detection, virtual models), `agent` (the loop), `coding` (session, tools,
compaction, branch summarization, retry, cache warming, model runtime, stores,
exports, bug report, crash log, trust), `tui` (the renderer, layout, markdown,
highlighting, images) and `mcp`. The interactive mode is `coding/interactive`;
`cmd` and the root `main.go` boot the CLI. The goal workflow is built in
(D223) rather than loaded as an extension, because pier has no extension host
(D41).

Out of scope, each with its decision recorded in code: extension mechanics
(resource loader, sdk surface, agent-session services/runtime, package and
tools managers), the native Bun-FFI clipboard (text, file-path and image
clipboard use platform commands, D106), Photon's Lanczos3 resampling and its
WebP/BMP decoding (a bilinear resizer and stdlib png/jpeg/gif instead, D26),
the chord node bundler, the bug-report upload transport, `session-backends`,
and the `evals` harness. `chord`, `client`, `protocol`, `server`, `telemetry`
and `durable` were ported but unreachable from the CLI, so they were removed
(D236) and preserved on the `unwired-packages` branch.

There is no built-in filesystem sandbox (D177): shell, write and edit run with
the OS user's permissions, including outside the workspace. Use external
confinement.

## Architecture

- **Reference-shaped packages.** `ai`, `agent`, `coding`, `tui` and `mcp` mirror
  upstream `packages/`; port-local code lives under `internal/`. Upstream's
  ~4000-line `interactive-mode.ts` becomes narrow, injectable wirings under
  `coding/interactive`, composed by `app.go`. Each `*Wiring` owns its own
  field wiring, so the composition root cannot silently skip a seam
  (`TestAppWiringCompleteness`).
- **Single-writer UI loop.** Input dispatch, painting and all UI mutation share
  one owner goroutine; producers enqueue events or post work (D143). Blocking
  work (turns, session writes, settings persists, theme loads, transcript
  pre-render, clipboard) runs off-loop through `internal/offloop`. Shutdown
  drains accepted mandatory saves without a deadline while optional work is
  cancelled (D199). UI-state mutexes are retired (D146/D147); the retained
  handoff and I/O locks are inventoried in `docs/locks.md`.
- **One session projection.** `coding/session_projection.go` resolves the branch
  path, compaction window, context settings and messages in a single walk and
  caches by branch version. Callers pass the projection through; assembling it
  by hand or re-scanning entries per message is a defect.
- **JSON byte-parity.** Session files, transcripts and wire payloads round-trip
  exactly and use `ai.MarshalJSON` (JavaScript key order, no HTML escaping).
  Where pi relies on JS semantics Go lacks, the divergence is a numbered D-row.
- **Differential renderer.** `tui` diffs frames against a layout engine and
  scroll view; oversized content prepares off-loop on private snapshots and is
  adopted on-owner after a generation check. The rationale and measurements are
  in `docs/architecture.md`.

## Build & test

```bash
just build      # bin/pier: pure Go (CGO_ENABLED=0), the release flags
just check      # gofmt + go vet + go test
just reference  # is ./pi at the pinned tag, and how far past it has moved
just --list     # install, test-race, cross, clean
```

Plain Go works too: `main.go` is a thin wrapper over `cmd`, so `go build -o
bin/pier .` and `go test ./...` are enough. CI runs
`go test -race -count=2 -timeout 300s ./...`; the race detector cannot run on
`android/arm64`, so that one check is CI-only.

## Install

Linux x64 (use `pier-linux-arm64`, or `pier-android-arm64` in Termux):

```sh
asset=pier-linux-amd64; base=https://github.com/dat267/pier/releases/latest/download; curl -fsSLo "$asset" "$base/$asset" && curl -fsSLo "$asset.sha256" "$base/$asset.sha256" && sha256sum -c "$asset.sha256" && mkdir -p "$HOME/.local/bin" && install -m 755 "$asset" "$HOME/.local/bin/pier"
```

Windows PowerShell (use `pier-windows-arm64.exe` on ARM64):

```powershell
$ErrorActionPreference='Stop'; $base='https://github.com/dat267/pier/releases/latest/download'; $asset='pier-windows-amd64.exe'; $dest=Join-Path $HOME 'bin'; Invoke-WebRequest "$base/$asset" -OutFile $asset -UseBasicParsing; Invoke-WebRequest "$base/$asset.sha256" -OutFile "$asset.sha256" -UseBasicParsing; $want=(Get-Content "$asset.sha256").Split()[0].ToLower(); $got=(Get-FileHash $asset -Algorithm SHA256).Hash.ToLower(); if ($got -ne $want) { throw "checksum mismatch for $asset" }; New-Item -ItemType Directory -Force -Path $dest | Out-Null; Move-Item -Force $asset (Join-Path $dest 'pier.exe')
```

There are no macOS assets; build from source there. `pier update` replaces the
binary with the newest release and verifies the same checksum (on Windows it
moves the running image aside as `pier.exe.old` first). Build from source with
`just install`, or `just VERSION=1.2.3 install` to stamp `--version`; the binary
derives its display name from its own file name, so a symlink named `pi` shows
`pi`.

## Using it

`pier --help` lists the flags. The CLI supports the interactive mode, resume
(`-c` newest; `-r` picker; `--session` by path, id, prefix or global match;
`--session-id`; `--fork`), model selection (`--provider`, `--model`),
`--offline`, `--tui-mode` and initial prompts. Print mode (`-p`) and JSON output
(`--mode json`) are wired. MCP servers from the settings connect directly and
their tools become agent tools. `/login` opens the authorization URL in the
platform browser. Stdio RPC (`--mode rpc`) supports prompting, steering, abort,
queues, state and messages, model and thinking selection, session
introspection, compaction, retry, bash control, session replacement and event
streaming, with a typed subprocess client in `coding/rpcclient`; see
[docs/rpc.md](docs/rpc.md).
