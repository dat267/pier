# 2026-10-27 code review

## CR-056: P2 branch forks discarded bookmarks (fixed)

Reference: read-only pi v1.4.0 (`4a080252`),
`packages/coding-agent/src/core/session-manager.ts`, `createBranchedSession`.
The reference recreates resolved labels for retained targets after filtering
label entries out of the selected path. Pier filtered them out but never
recreated them, so RPC fork/clone lost every bookmark.

Public-manager reproduction printed `before label="bookmark"` and
`after label="" error=<nil>`, while the pinned TypeScript retained the label.
`TestBranchedSessionPreservesBookmarks` failed with `bookmark = "", want
"bookmark"` in both memory and file cases before the repair. It now verifies
recreated IDs, original timestamps, parent chaining and persistence on reopen.
The retained-line migration test now expects the recreated label as the pin
does; its exact retained-message byte assertion is unchanged.

The repair collects resolved labels in JavaScript Map insertion order and
recreates only labels whose targets survive the fork. A differential public
manager probe covered an off-path update, deleting and re-adding a label,
multiple retained targets and an excluded target; Node and Go output matched.

Verification: `go test -count=1 ./coding` passed, including branch, migration
and RPC callers. Temporary red logs and reference/control probes are under
`/tmp/pier-5am-audit/`; these are local evidence, not permanent fixtures.
