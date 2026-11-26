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
