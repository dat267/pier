# pier, a Go coding agent

pier is a terminal-based AI coding agent and a from-scratch Go port of
[Mario Zechner's pi](https://github.com/earendil-works/pi).

## Reference and scope

The reference pin is pi v1.4.0 (`4a080252`). See [CHANGES.md](CHANGES.md)
for coverage and [ARCHITECTURE.md](ARCHITECTURE.md) for deliberate differences.
The extension system is out of scope; `/goal` is built in.

There is no built-in filesystem sandbox. Shell, write, and edit tools run with
your OS permissions, including outside the workspace. Use external confinement.

## Build and test

Requires Go 1.27 or later.

```sh
just build       # creates bin/pier
just check       # formatting, vet, and tests
./bin/pier --help
```

Without `just`, use `go build -o bin/pier .` and `go test -count=1 ./...`.

## Install

Install from source with `just install`, or download a release asset and its
`.sha256` file from [GitHub Releases](https://github.com/dat267/pier/releases).
Verify the checksum with `sha256sum` or `Get-FileHash` before installing.
Assets cover Linux (amd64 and arm64), Android (arm64), Windows (amd64 and
arm64), and macOS (amd64 and arm64). Run `pier update` to update an existing
installation on each platform. macOS releases are not notarized. The arm64
binary is ad-hoc signed; the amd64 binary is unsigned. A browser-downloaded
copy may need `xattr -d com.apple.quarantine` before it runs.

## Use

Run `pier` for interactive mode. Use `pier -c` to continue the newest session,
`pier -r` to choose a session, `pier -p` for print mode, or `pier --mode json`
for JSON events. `--model`, `--provider`, `--offline`, and `--help` are also
available. MCP servers configured in settings provide agent tools, and `/login`
opens the provider authorization page in your browser.

The built-in `/goal` command tracks multi-round objectives in session history.
The agent can inspect and update goals with the `get_goal` and `set_goal` tools.
Stdio RPC (`pier --mode rpc`) and its supported commands are documented in
[the RPC contract](ARCHITECTURE.md#stdio-rpc).

## Design and contributor docs

Packages follow pi's layout. The interactive UI has a single owner loop, session
projections supply model context, and persisted JSON preserves JavaScript key
ordering. See [architecture and performance](ARCHITECTURE.md),
[ARCHITECTURE.md](ARCHITECTURE.md), and
[ARCHITECTURE.md](ARCHITECTURE.md). Development guidance is in
[AGENTS.md](AGENTS.md).
