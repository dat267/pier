# Stdio RPC

`pier --mode rpc` runs the coding agent as a local subprocess controlled by
JSONL on stdin. Responses and session events share stdout; diagnostics use
stderr. The command and event shapes follow **pi v1.3.0** for the supported
subset below. This is not the framed-CBOR chord transport (removed from the
tree; D236), and it is not an MCP server.

RPC runs the enabled agent tools with the same permissions as the CLI. It is
not an authenticated network service. Do not expose it directly to untrusted
clients.

## Start and inspect

```bash
printf '%s\n' '{"id":"state","type":"get_state"}' \
  '{"id":"messages","type":"get_messages"}' |
  pier --offline --mode rpc --no-mcp --no-session
```

A host that submits prompts must **keep stdin open while work is running**.
Closing stdin shuts down the session and cancels active work. There is no
handshake or session-header record. Normal CLI model, credential, tool, trust,
and session-path flags apply. `--continue` and explicit `--session` file paths
can restore a session at startup. RPC never opens a trust dialog, a session
picker, or a cross-project fork confirmation. Use `--fork` or an explicit
session file path instead of a global session-id match that needs confirmation.
`--resume` and `@file` arguments are rejected. Attach images through commands.

## Supported commands

Every command accepts an optional string `id`. Responses repeat it when
present, including an empty string. Omitted ids remain omitted.

- `prompt`: `message` (string), optional `images`, optional `streamingBehavior`
  (`"steer"` or `"followUp"`).
- `steer`, `follow_up`: `message`, optional `images`. These queue input, even
  while idle; they do not start an idle agent. Skills and prompt templates
  expand before queueing.
- `abort`: cancels the current prompt, including auth preflight, and responds
  after the admitted prompt workers settle.
- `clear_queue`: returns `{ "steering": [...], "followUp": [...] }`.
- `get_state`: returns model/thinking selection, activity and queue modes,
  session identity, auto-compaction status, and message/queue counts. Optional
  model, session-file, and session-name fields are omitted when absent.
- `get_messages`: returns `{ "messages": [...] }`, with role-tagged messages.
- `get_available_models`: returns `{ "models": [...] }` from the authenticated
  availability snapshot.
- `set_model`: `provider`, `modelId`; returns the selected model.
- `cycle_model`: returns `{ "model": ..., "thinkingLevel": ..., "isScoped": ... }`
  or `null` when no cycle is available.
- `get_available_thinking_levels`: returns `{ "levels": [...] }`.
- `set_thinking_level`: `level`; the session clamps a valid level to the model's
  capabilities.
- `cycle_thinking_level`: returns `{ "level": ... }` or `null`.
- `set_steering_mode`, `set_follow_up_mode`: `mode` (`"all"` or
  `"one-at-a-time"`).
- `get_last_assistant_text`: returns `{ "text": "..." }`, or an empty object
  when there is no assistant text.
- `get_session_stats`: returns the camelCase session counters, token totals,
  cost, and context usage when a model window is known.
- `export_html`: optional `outputPath`; renders the current session branch with
  the same exporter as `/export` and returns `{ "path": ... }`. An in-memory
  session fails with `Cannot export in-memory session to HTML`; a persisted
  session with no file yet fails with `Nothing to export yet - start a
  conversation first`.
- `get_fork_messages`: returns `{ "messages": [{ "entryId": ..., "text": ... }] }`
  for the user messages that can seed a fork.
- `get_entries`: returns `{ "entries": [...], "leafId": ... }`. An optional
  `since` entry id returns the entries after it; an unknown id fails with
  `Entry not found: <id>`.
- `get_tree`: returns `{ "tree": [...], "leafId": ... }` with resolved labels.
- `set_session_name`: `name` (trimmed); an empty name fails with
  `Session name cannot be empty`.
- `get_commands`: returns `{ "commands": [...] }` for prompt templates and
  skills, each with `name`, optional `description`, `source`, and `sourceInfo`.
- `compact`: optional `customInstructions`; returns the compaction result
  (`summary`, `firstKeptEntryId`, `tokensBefore`, optional `estimatedTokensAfter`,
  `usage`, `details`).
- `set_auto_compaction`, `set_auto_retry`: `enabled` (boolean).
- `abort_retry`: cancels an in-flight auto-retry.
- `bash`: `command`, optional `excludeFromContext`; runs it in the session cwd
  and returns the bash result (`output`, optional `exitCode`, `cancelled`,
  `truncated`, optional `fullOutputPath`), streaming `bash_execution_update`
  events.
- `abort_bash`: cancels a running `bash` command.
- `new_session`: optional `parentSession`; starts a fresh session in the
  current session directory and returns `{ "cancelled": false }`.
- `switch_session`: `sessionPath`; opens that session file and returns
  `{ "cancelled": false }`.
- `fork`: `entryId` and optional `position` (`"before"` default, or `"at"`).
  `before` requires a user message, branches at its parent and returns
  `{ "text": ..., "cancelled": false }`; `at` keeps the entry and omits `text`.
- `clone`: forks at the current leaf and returns `{ "cancelled": false }`. An
  empty session fails with `Cannot clone session: no current entry selected`.

The replacement commands run through the host's session factory. `new_session`,
`switch_session`, `fork` and `clone` fail with `Session replacement is not
available in this build` when no factory is supplied (a Go caller of
`coding.RunRPCMode` must set `RPCModeOptions.ReplaceSession`). A replacement
aborts in-flight work and disposes the old session first, then subscribes the
new one, so its events and state queries use the replacement.

Model, thinking, queue-mode, compaction and retry toggles are session-local;
`set_auto_retry` writes the same persisted retry setting the run loop reads.
Images use the upstream shape:

```json
{"type":"image","data":"<base64>","mimeType":"image/png"}
```

## Acceptance and events

```json
{"id":"turn-1","type":"prompt","message":"Explain this repository"}
{"id":"turn-1","type":"response","command":"prompt","success":true,"data":{"disposition":"started"}}
```

`prompt` responds once after successful preflight, not after the model finishes.
Its disposition is `started` or `queued`. Extensions are absent, so `handled`
is never produced. Sending another prompt during a run without an explicit
`streamingBehavior` fails. `steer` and `follow_up` return `queued`.

`bash` and `compact` run off the read loop and respond when they finish. The
loop keeps reading while they run, so `abort` and `abort_bash` can cancel them.

Session events stream independently of responses using the same event shaper
as JSON print mode. Wait for `agent_settled` to observe final completion;
`agent_end` may be followed by a retry or another queued turn. Once a prompt
has been accepted, later model failures appear in events, not as a second
response for the same command.

```json
{"id":"bad","type":"response","command":"set_model","success":false,"error":"Model not found: missing/model"}
```

Unknown commands return a correlated `Unknown command: <type>` failure.
Malformed JSON returns a failure with `command: "parse"`; the connection stays
open. Invalid fields retain a valid command type and id. Records are delimited
only by LF, with CRLF and a final unterminated record accepted. Payload strings
may contain U+2028/U+2029; do not split records on those characters. Records
larger than 64 KiB are supported.

## Bounds and shutdown

D235 admits at most four pending prompt preflights in addition to an accepted
turn. Additional requests receive a visible failure and may be retried.
Preflight admission stays in input order while abort and state queries remain
readable. Output uses one unbuffered byte handoff: a slow consumer backpressures
producers rather than accumulating an unbounded output queue.

EOF, cancellation, and stdout failures stop work and dispose the session.
Accepted session writes drain without a deadline. SIGTERM/SIGHUP return
143/129 after host cleanup; ordinary EOF returns 0 unless I/O or persistence
failed. Pending display output may be discarded during shutdown, never accepted
session saves.

An OS read or write that cannot be interrupted may outlive owner teardown in a
byte-only pump until the OS releases it or the process exits. The pumps never
mutate session state. A Go caller of `coding.RunRPCMode` must reserve its streams
for that run and must not reuse a canceled output while an in-flight write may
still be blocked.

## Not implemented in this milestone

- Extensions, extension UI requests/responses, and extension commands.
- A typed subprocess RPC client library.

The underlying CLI and session APIs may support operations that are not yet
exposed through RPC. Unsupported commands fail explicitly; this milestone does
not claim complete upstream RPC compatibility.
