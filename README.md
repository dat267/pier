# pier, a Go coding agent

A from-scratch Go implementation of an AI coding agent with a terminal UI,
developed against Mario Zechner's [pi](https://github.com/earendil-works/pi)
(`@earendil-works/pi-ai`, `pi-agent-core`, `pi-coding-agent`) as its strongest
reference. pi is a reference, not a specification: this code follows its
structure and behavior closely, and where it departs, the departure is a
deliberate feature of this implementation. The development conventions live in
`AGENTS.md`.

## Reference pin

| What | Value |
|---|---|
| Repository | https://github.com/earendil-works/pi (cloned at `./pi`, gitignored) |
| Pin | `71023aec` (Release v1.3.0), the latest released tag. Remaining gaps and out-of-scope areas are tracked in `docs/PORTING.md` |

## Status

`docs/PORTING.md` is the authoritative per-area table: what is implemented,
partially implemented, or out of scope, file by file, plus the reference pin's
change log. `docs/DIVERGENCES.md` records this implementation's own decisions:
every numbered place it deliberately departs from the pi reference, with the
scenario and the rationale.

Every runtime package in the pi reference that the CLI actually reaches has a counterpart here: `ai` (all ten wire APIs, all provider factories, the nine OAuth flows, image generation, the api/images registries, overflow detection, virtual models, Anthropic workload identity federation), `agent` (the loop), and `coding-agent` (the complete core: session, tools, compaction, branch summarization, retry, cache warming, model runtime/registry/composer, stores, exports, bug report, crash log, trust, utilities, and the createAgentSession assembly).

The goal workflow is re-ported as a built-in feature (D223), despite shipping
upstream as a user extension. It lives in `internal/goal` and the session
controller because pier has no extension host (D41); this does not add general
extension support. The `pi-tui` library and interactive coding-agent mode are
implemented (`tui/*` and `coding/interactive/*`), including the theme, every
component, and the interactive-mode method groups. The root `main.go` and `cmd`
package compose them into a runnable CLI. The remaining items are the
documented out-of-scope set below.

Like stock pi, pier has no built-in filesystem sandbox (D177). Shell commands,
write, and edit run with the OS user's permissions, including access outside the
workspace. There is no `/permissions` command, sandbox prompt section, or
RO/WW/FA footer indicator. Use external confinement when needed; restrictions
inherited from the launching process still apply.

Deliberately left out of scope, each with its decision recorded in code: the extension mechanics (resource loader, sdk extension surface, agent-session services/runtime, package and tools managers), the native (Bun FFI) clipboard (text copy/read and image read use platform commands instead, D106), Photon's Lanczos3 resampling and its WebP/BMP decoding (the port has a bilinear resizer and stdlib png/jpeg/gif, D26), the node-specific chord bundler, the bug-report upload transport, and the optional `session-backends` sqlite driver (a host-provided storage backend imported by no runtime package) plus the `evals` Docker harness (a development tool). The `chord` library, its framed-CBOR `client`/`protocol`/`server` transport, `telemetry`, and `durable` were ported but are unreachable from the CLI, so they were removed and are preserved on the `unwired-packages` branch (D236).

## Build & test

```bash
just build   # bin/pier: pure Go (CGO_ENABLED=0), the flags the release workflow uses
just check   # gofmt + go vet + go test
just reference # is ./pi at the pinned tag, and how far has upstream moved past it
just --list  # the other recipes (install, test-race, cross, clean)
```

Plain Go works too. The CLI is the module root — `main.go` is a thin wrapper
over the `cmd` package, which holds the boot — so `go install .` works:

```bash
go build -o bin/pier .
go test ./...
```

CI runs `go test -race -count=2 -timeout 300s ./...`, but **the race detector cannot run on
android/arm64** — Go rejects it outright (`-race is not supported on
android/arm64`) — so `just test-race` only fails on Termux. That check is
CI-only, and it is the only one that cannot be reproduced locally.

## Install the CLI

For Linux x64, replace the asset with `pier-linux-arm64` on ARM64 or
`pier-android-arm64` in Termux:

```sh
asset=pier-linux-amd64; base=https://github.com/dat267/pier/releases/latest/download; curl -fsSLo "$asset" "$base/$asset" && curl -fsSLo "$asset.sha256" "$base/$asset.sha256" && sha256sum -c "$asset.sha256" && mkdir -p "$HOME/.local/bin" && install -m 755 "$asset" "$HOME/.local/bin/pier"
```

Add `$HOME/.local/bin` to `PATH` if needed. Windows PowerShell, use
`pier-windows-arm64.exe` on ARM64:

```powershell
$ErrorActionPreference='Stop'; $base='https://github.com/dat267/pier/releases/latest/download'; $asset='pier-windows-amd64.exe'; $dest=Join-Path $HOME 'bin'; Invoke-WebRequest "$base/$asset" -OutFile $asset -UseBasicParsing; Invoke-WebRequest "$base/$asset.sha256" -OutFile "$asset.sha256" -UseBasicParsing; $want=(Get-Content "$asset.sha256").Split()[0].ToLower(); $got=(Get-FileHash $asset -Algorithm SHA256).Hash.ToLower(); if ($got -ne $want) { throw "checksum mismatch for $asset" }; New-Item -ItemType Directory -Force -Path $dest | Out-Null; Move-Item -Force $asset (Join-Path $dest 'pier.exe')
```

Set `$dest` to a directory on `PATH` if `$HOME\bin` is not already there.
There are no macOS assets; build from source on macOS. Afterwards `pier
update` replaces the binary in place with the newest release, verifying the same
checksum; on Windows it moves the running `pier.exe` aside to `pier.exe.old`
(the image is locked) and clears that on the next update.

Build from source with `just install`, or use `just VERSION=1.2.3 install` to stamp `--version` and the changelog comparison.

`just install` prints where it landed and warns if that directory is not on
`PATH`. It is a thin wrapper over `go install .`, which you can run
directly if you prefer. The binary derives its display name from its own file
name, so a symlink named `pi` would show `pi`.

Then run `pier`. `pier --help` lists the flags; `pier --version` prints the version. The CLI supports the interactive mode, resume (`-c` continues the newest session; `-r` opens the interactive session picker; `--session` accepts a file path, a session id, an id prefix, or matches globally across projects; `--session-id` opens a matching session or creates a new one with that id; `--fork` forks a session into a new one in the current cwd), model selection (`--provider`, `--model`), `--offline`, `--tui-mode` and initial prompts. Print mode (`-p`) and JSON output (`--mode json`) are wired, MCP servers from the settings are connected directly (tools become agent tools), `/login` opens the authorization URL in the platform browser, and `pier update` replaces the binary with the newest release after verifying its checksum. Stdio RPC (`--mode rpc`) supports prompting, steering, abort, queues, state/messages, model/thinking selection, session introspection, compaction/retry and bash controls, session replacement, and event streaming; a typed subprocess client lives in `coding/rpcclient`. See [the RPC guide](docs/rpc.md) for the supported subset and remaining gaps. The package manager, extensions and migrations are not wired.
