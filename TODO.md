# Reproduced, unresolved findings

## One unreproduced `./coding` failure (2026-11-22)

One `go test -count=1 ./coding ./coding/interactive` run reported `FAIL
github.com/dat267/pier/coding 20.861s` while `./coding/interactive` passed. The
failure was not captured with its test name, and thirteen later runs of the same
packages, the same pair, and a `-race` subset all passed. It recurred once more
the same day, again with `./coding/interactive` green and again without the test
name (25 more runs passed, including five `-shuffle=on`).

`just test` now reprints every `--- FAIL:` block after the run, so the next
occurrence names its test without a rerun. If it names a test this repository
shares with `./coding/interactive`, check first whether it depends on wall-clock
timing or on process-global state (`leakcheck` and `just test-isolated` both hunt
for the second class).

## TypeScript highlighting is twice as slow as the reference (2026-11-23)

The first 30 KB of `pi/packages/coding-agent/src/core/agent-session.ts`
highlights as TypeScript in about 19.9 ms in the port and 9.9 ms in the pinned
highlight.js (warmed, `ignoreIllegals: true`), after D521. The profile is the
per-byte dispatch's anchored submatcher (`execDispatch`, `sub.prefixedRe`),
mostly Go NFA setup on rules that fail within a few bytes: TypeScript's
method-definition rule `[a-zA-Z_]\w*\([^()]*(...)*\)\s*\{` on every
identifier, and RE_STARTERS on bytes that begin `throw`/`return`. A
syntax-tree prefilter before the call broke even (D521); a compiled one, or
splitting rules so a submatcher only holds those that can match after the
identifier run, is the next thing to measure.
