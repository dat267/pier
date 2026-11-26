# TODO

Reproduced, unresolved findings go here, each with its reproduction, why it is not
fixed, and the tests that must stay green; the file is empty when none are open.

## An unknown content block type drops the whole message (low)

Reproduction: `ai.UnmarshalMessage` on

```json
{"role":"user","content":[{"type":"text","text":"keep me"},{"type":"futureBlock","v":1}],"timestamp":1}
```

returns `ai: unknown content block type "futureBlock"`. Every caller treats that error
as "skip this message" (`SessionEntryToContextMessages`, `UnmarshalMessages`,
`branchsummary.go`, `GetUserMessagesForForking`, the transcript's dropped-thinking scan),
so the whole user message — including its valid `text` block — disappears from the
model's context with no warning.

Reference: `session-manager.ts` loads with a plain `JSON.parse` and no discriminator
check, so both blocks survive and the message reaches the provider. The two projects
model exactly the same four block types (`text`, `thinking`, `image`, `toolCall`), so no
supported provider produces an unknown one.

Reachable only from a **newer pier**: `LoadEntriesFromFileBuffered` disables the fast
scan for an older header version but has no check for a *future* one, so a session
written by a later release loads as if it were current.

Why not fixed here: matching the reference needs an opaque passthrough `Content`
implementation that keeps the block's bytes and its `type` discriminator, which touches
`ContentList.MarshalJSON`, `contentKind`, and every `switch` over `Content`. That is a
design change, not a repair, and it needs its own review.

The alternative — erroring only when *no* block decodes, and skipping just the unknown
one — is smaller but would silently rewrite the transcript on the next save.

Tests that must stay green: `TestContentBlockRoundTrip`,
`TestUserContentListRejectsToolCall` (a *known* type in the wrong list must still error),
`TestMessageCacheAppendKeepsEarlierProjection`, and the session round-trip fixtures.

## An unreadable context file is skipped without a warning (low)

Reproduction: create `AGENTS.md` with mode `0000` in the cwd and load the project
context. `LoadContextFileFromDir` stats the file successfully, fails to read it, and
`continue`s to the next candidate, so the file silently vanishes from the prompt.

Reference: `resource-loader.ts` prints
`console.error(chalk.yellow("Warning: Could not read <path>: <err>"))` from two call
sites, `loadContextFileFromDir` (`:228`) and `resolvePromptInput` (`:206`). Pier skips
silently at both of its ports (`coding/contextfiles.go`, `coding/promptfiles.go`
`ResolvePromptInput`), so a user with a permissions mistake or a file replaced mid-read
gets no diagnostic where pi names the file and the error.

The read-error branch is narrow: `os.Stat` follows symlinks and `IsRegular` rejects a
FIFO or a directory before any read, so a dangling symlink or a pipe is handled earlier.
The remaining trigger is permissions that allow `stat` but deny `read`, or a race on file
replacement.

Why not fixed here: it is consistent across both ported call sites, so fixing one would
leave the other inconsistent. The warning is also colored stderr, which has to route
through D381's supports-color decision rather than a bare `fmt.Fprintln`. Doing it
properly means one small helper used by both sites plus a test per site.

Tests that must stay green: `TestLoadContextFileFromDirPreference`,
`TestLoadProjectContextFilesLayering`, `TestLoadContextFileFromDirSkipsDirectories`, and
`TestResolvePromptInputReadsExistingPaths`.
