# Reproduced, unresolved findings

## TypeScript highlighting is slower than the reference (2026-11-23)

The first 30 KB of `pi/packages/coding-agent/src/core/agent-session.ts`
highlights as TypeScript in about 10.5 ms in the port and 9.9 ms in the pinned
highlight.js (warmed, `ignoreIllegals: true`), after D521, D522 and D523 (19.9
ms before D522). The D523 walk is now half the profile; its class lookup
(`prefilterClassHas`, a linear scan of the ranges) alone is 8%, so compiling
each walked rule into nodes with an ASCII bitmap is the next thing to measure.
