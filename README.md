# pier, a Go coding agent

pier is a terminal-based AI coding agent and a from-scratch Go port of
[Mario Zechner's pi](https://github.com/earendil-works/pi). It is pinned to pi
v1.4.0 (`4a080252`); use `git log` for release/change history and
[ARCHITECTURE.md](ARCHITECTURE.md) for design and deliberate differences.

There is no built-in filesystem sandbox: shell, write and edit tools use your OS
permissions, including outside the workspace. Use external confinement.

## Build and install

Requires Go 1.27+.

```sh
just build       # creates bin/pier
just check       # format, vet, tests
./bin/pier --help
```

Install with `just install` or download a release and its `.sha256` file from
[GitHub Releases](https://github.com/dat267/pier/releases). Verify the checksum
with `sha256sum` or `Get-FileHash`. Assets cover Linux (amd64/arm64), Android
(arm64), Windows (amd64/arm64), and macOS (amd64/arm64). Run `pier update` to
update. macOS binaries are not notarized: arm64 is ad-hoc signed; amd64 is
unsigned. A downloaded binary may require `xattr -d com.apple.quarantine`.

## Use

Run `pier` for interactive mode. `pier -c` continues the latest session,
`pier -r` selects a session, `pier -p` prints, and `pier --mode json` emits JSON
events. Options include `--model`, `--provider`, `--offline` and `--help`.
Configure MCP servers in settings to add tools; `/login` opens provider
authorization in a browser.

`/goal` tracks multi-round objectives in session history. The agent can manage
them with `get_goal` and `set_goal`. Stdio RPC (`pier --mode rpc`) is documented
in [the RPC contract](ARCHITECTURE.md#stdio-rpc).

## Development

Packages follow pi's layout. The UI uses a single owner loop; session projections
build model context, and persisted JSON preserves JavaScript key order. See
[AGENTS.md](AGENTS.md) for contributor rules, [ARCHITECTURE.md](ARCHITECTURE.md)
for design details; use `git log` for coverage and change history.
