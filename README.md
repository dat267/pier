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
| Pin | `cd32f772` (Release v1.0.2); the applicable runtime and catalog changes from v1.0.1 are included — see `docs/PORTING.md` for the delta's ported and not-ported commits |
| Stale reference test | `packages/ai/test/faux-provider.test.ts` "estimates prompt and output tokens" still expects pre-`9e05370b2` faux serialization; this implementation follows the current source |

## Status

`docs/PORTING.md` is the authoritative per-area table: what is implemented,
partially implemented, or out of scope, file by file, plus the reference pin's
change log. `docs/DIVERGENCES.md` records this implementation's own decisions:
every numbered place it deliberately departs from the pi reference, with the
scenario and the rationale.

Every runtime package in the pi reference has a counterpart here: `ai` (all ten wire APIs, all provider factories, the nine OAuth flows, image generation, the api/images registries, overflow detection, virtual models, Anthropic workload identity federation), `agent` (the loop), `coding-agent` (the complete core: session, tools, compaction, branch summarization, retry, cache warming, model runtime/registry/composer, stores, exports, bug report, crash log, trust, utilities, and the createAgentSession assembly), `protocol`, `client`, `chord` (+delta/services/facets), `server` (+unix/testing), `telemetry`, and `durable` (the complete package: the session kernel and transaction, the three storage backends, the whole `harness/` tree with its scheduler and built-in tasks, the coding tools, and every upstream storage-conformance case replayed).

The pi `goal` extension is not carried (D191): it ships upstream as an installed user extension (`~/.pi/agent/extensions/goal`), and the port has no extension host (D41), so the port behaves like a stock pi install without it. The `pi-tui` library and the interactive coding-agent mode are implemented (`tui/*` and `coding/interactive/*`), including the theme, every component, and the interactive-mode method groups; the root `main.go` and the `cmd` package compose them into a runnable CLI. The remaining items are the documented out-of-scope set below.

Deliberately left out of scope, each with its decision recorded in code: the extension mechanics (resource loader, sdk extension surface, agent-session services/runtime, package and tools managers), the native clipboard, the kitty/iterm image transport internals, the node-specific chord bundler, the bug-report upload transport, and the optional `session-backends` sqlite driver (a host-provided storage backend imported by no runtime package) plus the `evals` Docker harness (a development tool).

## Build & test

```bash
just build   # bin/pier: pure Go (CGO_ENABLED=0), the flags the release workflow uses
just check   # gofmt + go vet + go test
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

Download the release binary — Linux x64/arm64, or Termux on Android:

```sh
base=https://github.com/dat267/pier/releases/latest/download
asset=pier-linux-amd64   # pier-linux-arm64, or pier-android-arm64 in Termux
curl -fsSL -o "$asset"        "$base/$asset"
curl -fsSL -o "$asset.sha256" "$base/$asset.sha256"
sha256sum -c "$asset.sha256"
mkdir -p ~/.local/bin
install -m 755 "$asset" ~/.local/bin/pier   # any directory on PATH
```

Windows (PowerShell):

```powershell
$base  = "https://github.com/dat267/pier/releases/latest/download"
$asset = "pier-windows-amd64.exe"   # or pier-windows-arm64.exe
$dest  = "$env:USERPROFILE\bin"     # any directory on PATH

Invoke-WebRequest "$base/$asset"        -OutFile $asset -UseBasicParsing
Invoke-WebRequest "$base/$asset.sha256" -OutFile "$asset.sha256" -UseBasicParsing
$want = (Get-Content "$asset.sha256").Split()[0].ToLower()
$got  = (Get-FileHash $asset -Algorithm SHA256).Hash.ToLower()
if ($got -ne $want) { throw "checksum mismatch for $asset" }
New-Item -ItemType Directory -Force -Path $dest | Out-Null
Move-Item -Force $asset (Join-Path $dest "pier.exe")
```

There are no macOS assets; build from source on macOS. Afterwards `pier
update` replaces the binary in place with the newest release, verifying the same
checksum; on Windows it moves the running `pier.exe` aside to `pier.exe.old`
(the image is locked) and clears that on the next update.

Or build from source:

```bash
just install                 # into $(go env GOBIN), or $(go env GOPATH)/bin
just VERSION=1.2.3 install   # stamps --version and the changelog comparison
```

`just install` prints where it landed and warns if that directory is not on
`PATH`. It is a thin wrapper over `go install .`, which you can run
directly if you prefer. The binary derives its display name from its own file
name, so a symlink named `pi` would show `pi`.

Then run `pier`. `pier --help` lists the flags; `pier --version` prints the version. The CLI supports the interactive mode, resume (`-c` continues the newest session; `-r` opens the interactive session picker; `--session` accepts a file path, a session id, an id prefix, or matches globally across projects; `--session-id` opens a matching session or creates a new one with that id; `--fork` forks a session into a new one in the current cwd), model selection (`--provider`, `--model`), `--offline`, `--tui-mode` and initial prompts. Print mode (`-p`) and JSON output (`--mode json`) are wired, MCP servers from the settings are connected directly (tools become agent tools), `/login` opens the authorization URL in the platform browser, and `pier update` replaces the binary with the newest release after verifying its checksum. The rpc mode, package manager, extensions and migrations are not wired.
