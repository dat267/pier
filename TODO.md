# Reproduced, unresolved findings

## TypeScript highlighting is slower than the reference (2026-11-23)

The first 30 KB of `pi/packages/coding-agent/src/core/agent-session.ts`
highlights as TypeScript in about 12.1 ms in the port and 9.9 ms in the pinned
highlight.js (warmed, `ignoreIllegals: true`), after D521, D522 and D523 (19.9
ms before D522). The dispatch now decides most positions without Go's regexp;
what remains is the regexp calls for rules the D523 walk leaves undecided
(line anchors, the dot, `\A`) and the non-dispatch scan. Pruning each
submatcher's leading alternation was measured and dropped: 1.7% on this file,
within noise elsewhere.
