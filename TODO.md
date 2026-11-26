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

Why not fixed here, and why a literal port would be wrong: upstream's `console.error`
writes straight to stderr, and nothing redirects console in interactive mode
(`modes/interactive/interactive-mode.ts` has no console patch), so pi's own warning
corrupts the TUI frame it is drawn over. Pier has a collected-diagnostics display for
skills, prompts and themes (`loadedresources.go`, upstream's `themeDiagnostics`), but
upstream does not route context-file failures through it either. A faithful port would
therefore reproduce a screen-corrupting write.

The right fix is to collect context-file read failures as resource diagnostics and show
them in that existing section, which is a behaviour improvement over the reference rather
than a parity repair, so it needs a D-row. It also has to reach both loaders, and
`coding/contextfiles.go` has no output channel to thread one through: adding a stderr
write inside the resource loader would put I/O in a library layer that callers like
`resourceOptions.contextFiles` do not expect.

Tests that must stay green: `TestLoadContextFileFromDirPreference`,
`TestLoadProjectContextFilesLayering`, `TestLoadContextFileFromDirSkipsDirectories`, and
`TestResolvePromptInputReadsExistingPaths`.
