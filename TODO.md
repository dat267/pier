# Reproduced audit findings

## A476 the unknown-action reply echoes an unbounded action string

- Severity: low (unbounded reply to the model; inconsistent with the sibling path).
- Measured: `set_goal` with a 100,000-character `status` that fails the enum
  check produces a 100,057-character reply, the whole value quoted back.
- Cause: `goalUpdate` (`internal/goal/machine.go:293`) builds
  `Unknown action %s ...` from `jsonQuote(rawAction)` with no bound.
- The same file already bounds the identical echo in the command path:
  `goal/command.go:54` uses `TruncateObjective(trimmed, 20)` for
  `Unknown subcommand ...`, which yields `"zzzzzzzzzzzzzzzzzzz…"`.
- Reachable: `set_goal` rejects an unknown status in `coding/goalcontroller.go`
  before dispatching, so the tool path is bounded by the model's argument
  length only; the machine path is reachable directly and from any future
  caller.
- Fix: bound the echoed action with `TruncateObjective`, matching the command
  path's own convention.
- Verification: a test asserting a huge action yields a short reply while a
  normal typo is still quoted in full.
