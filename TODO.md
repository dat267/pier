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
