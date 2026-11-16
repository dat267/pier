# Reproduced, unresolved findings

## `go test ./coding ./cmd` failed once and did not reproduce

- Severity: low until it repeats; it may be a resource-limited environment effect rather than a test bug.
- Failure: one `go test -count=1 ./coding ./cmd` run printed `FAIL` for `./coding` while `./cmd` passed. The harness truncated the output before the failing test name, so the case is unknown.
- Evidence: five later runs were green, including three repeats of the same two-package command and two `go test -count=1 ./...` runs, so it is not deterministic. The audit summary from an earlier session records one similar truncated `./coding` failure that also did not reproduce in nine runs.
- Repair plan: the next occurrence needs the full log, `go test -count=1 ./coding -json` or `-run` bisection, and the test name recorded here. `internal/leakcheck` runs every package's `TestMain`, so a leaked goroutine would name itself with `GODEBUG=tracebackancestors=20`.
