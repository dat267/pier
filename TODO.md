# Reproduced audit findings

- M75 (low): `TestApiKeyLoginRunsOffTheUILoop` waits for the login dialog through the shared 2-second `waitForCondition`, so the CI gate shape (`go test -race -count=2 ./...`) can fail it under machine load; observed once at the 2.02s timeout while every isolated focused run passed. Use the hang-guard budget (`waitForConditionWithin`) instead of treating the wait as a latency assertion. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M75.
