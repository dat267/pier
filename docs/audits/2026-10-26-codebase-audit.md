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

## CR-022, P3: Mistral finalized parallel calls in Go map order (fixed)

Authority: the same reference's `toolBlocksByKey.values()` preserves first-seen
order. Pier ranged over a Go map when emitting `toolcall_end`, so parallel calls
ended in nondeterministic order. Finalization now walks persisted content in
first-seen order without introducing another ordering structure.

Replay:

```sh
go test -count=30 ./ai -run '^TestMistralToolCallsEndInFirstSeenOrder$'
```

Observed red order: `call8 call5 call0 call4 call3 call7 call2 call6 call1`;
expected: `call8 call3 call5 call0 call7 call2 call6 call1 call4`. The test uses
nonmonotonic provider indexes, so sorting provider indexes would not pass.
A three-call source-corpus probe also failed across 20 repetitions; contiguous
single-call streaming remains a control.

Verification: focused test passes 30 repetitions; all ai tests, `just check`,
`just cross` (all six targets), `just reference` pass.

## CR-023, P3: Mistral omitted the default object argument delta (fixed)

Authority: `consumeChatStream` in the same pinned reference uses strings
verbatim and `JSON.stringify(arguments || {})` for non-string arguments.
Absent and null arguments therefore emit `{}`. Pier instead emitted an empty
delta, so stream clients observed different tool arguments. The argument delta
now defaults to `{}` while explicitly present string arguments remain verbatim.

Replay:

```sh
go test -count=1 ./ai -run '^TestMistralDefaultsAbsentToolArgumentsToObjectDelta$'
```

Observed red: absent and null arguments each emitted `""`, want `"{}"`.
Empty-string and object argument controls passed before and after.

Verification: focused test, all ai tests, `just check`, `just cross` (six
targets), `just reference` pass. The draft 22-scenario source corpus now matches
in 20 repetitions and regenerates byte-identically. CR-021 and CR-022 corpus
reproductions also pass after their repairs. `TODO.md` has no confirmed
unresolved findings; this does not establish full codebase coverage.
