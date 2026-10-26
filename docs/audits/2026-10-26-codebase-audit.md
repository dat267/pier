# Codebase audit, 2026-10-26

Continuation of `2026-10-25-codebase-audit.md`. Reference read: pi v1.4.0
(`4a080252f`), read-only. Baseline `11fa77a`, clean worktree and reference;
`GOPROXY=off GOSUMDB=off just check` and `just reference` pass. User budget:
until 17:00 workspace local time (+07). This remains a partial review, not a
certification of parity, security or race freedom. Inventory and inspection
notes are held outside the repository; this ledger records finding evidence.

## CR-021, P2: Mistral resumed already-ended content blocks (fixed)

Authority: `packages/ai/src/api/mistral-conversations.ts`, `consumeChatStream`,
clears `currentBlock` whenever tool arguments arrive, even for an existing call.
Pier inferred active state from the last persisted content block. If a tool call
started, text/thinking followed, and the call then continued, the content block
ended twice, or subsequent text extended it without a new start. Persisted
`middle` and `after` were merged instead of separate blocks.

Repair: `ai/mistral_conversations.go` tracks `contentOpen` in the existing stream
state. Finishing a block clears it; later text/thinking opens a new block.

Replay:

```sh
go test -count=1 ./ai -run '^TestMistralContinuedToolCallClosesTextBlock$'
```

Observed red: 11 events, no second `text_start`; expected and now observed:
12 events with two distinct text blocks. A source-executed draft corpus also
reproduced duplicate `thinking_end`; straight text, thinking-to-text, empty
content deltas and contiguous tool-call arguments were matching controls. The
corpus is not yet committed while independent findings in it remain unresolved.

Verification: focused test and all `TestMistral` tests pass; `go test -count=1
./ai`, `just check`, `just cross` (all six targets), `just reference` pass.
The CI race gate was not run locally.
