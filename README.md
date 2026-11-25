# pier, a Go coding agent

pier is a terminal-based AI coding agent and a from-scratch Go port of
[Mario Zechner's pi](https://github.com/earendil-works/pi). It is pinned to pi
v1.6.0 (`faac5858`); use `git log` for release/change history,
[ARCHITECTURE.md](ARCHITECTURE.md) for design and [DECISIONS.md](DECISIONS.md)
for deliberate differences.

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

A shell call the agent starts without a `timeout` stops after 10 minutes, so a
hung command hands control back to the agent with its output so far.

A tool call that outlives a minute is moved to the background instead of
freezing the turn: the agent gets a handle, the call keeps running with its output
in a temporary file, and it is told once when the call ends. The footer shows how
many calls are still running in the background. `/features` sets the threshold, or
turns it off.

`/goal` tracks multi-round objectives in session history. The agent can manage
them with `get_goal` and `set_goal`. `/features` lists the pier-only feature
toggles, and `/tools` turns each of the agent's tools on or off for the session;
typing there filters the list. `/loop [interval] <prompt>` repeats a prompt on a
cron-style interval for as long as the session is open (rounded to one that
divides its hour or day evenly, or to whole days, and it says when it rounds), and
`/loop <prompt>` with no interval lets the agent pace the next run itself. A run
that comes due while the agent is busy waits for the current turn instead of
interrupting it. The footer shows how many loops are scheduled, `/loop list` shows
them, and `/loop stop [id]` cancels one, or all of them.
Stdio RPC (`pier --mode rpc`) is documented in
[the RPC contract](ARCHITECTURE.md#stdio-rpc).

## Development

Packages follow pi's layout. [AGENTS.md](AGENTS.md) holds the contributor
rules, [ARCHITECTURE.md](ARCHITECTURE.md) the design and
[DECISIONS.md](DECISIONS.md) the D-rows; use `git log` for change history.
