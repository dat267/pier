# Reproduced, unresolved findings

## TypeScript highlighting is slower than the reference (2026-11-23)

The first 30 KB of `pi/packages/coding-agent/src/core/agent-session.ts`
highlights as TypeScript in about 15.7 ms in the port and 9.9 ms in the pinned
highlight.js (warmed, `ignoreIllegals: true`), after D521 and D522 (19.9 ms
before D522). The remaining profile is the per-byte dispatch's anchored
submatcher calls that do match or that the D522 prefilter cannot rule out,
chiefly RE_STARTERS (a 39-way operator alternation plus `\b(?:case|return|
throw)\b`) on letters that begin those words. Pruning each submatcher's
leading alternation down to the alternatives that can start with its byte was
measured and dropped: 1.7% on this file, within noise elsewhere. The cost is
Go's NFA (`machine.add`/`step`), chosen because the anchored submatchers search
the whole remaining input; a bound on how far a rule can read, which would let
Go use its backtracker on a short window, is the next thing to measure.
