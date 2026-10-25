---
name: code-review
description: Manually audit Pier codebase-wide for parity gaps, logic flaws, concurrency bugs, security vulnerabilities, and measured performance problems. Record reproduced findings in TODO.md, then implement and verify their fixes.
disable-model-invocation: true
---

# Code review

Run only when explicitly invoked with `/skill:code-review`. Default workflow:
inspect the whole first-party codebase, reproduce findings, plan in `TODO.md`,
then execute the repairs. Optional instructions after the command can narrow
the scope, set a time budget, request audit-only, or resume existing findings.
Honor those limits. Do not interpret invocation as permission to commit, push,
release, install dependencies, or modify the reference checkout.

All repository paths below are relative to the repository root, not this skill.

## 1. Establish the baseline

- Read `AGENTS.md`, `docs/PORTING.md`, `docs/DIVERGENCES.md`,
  `docs/architecture.md`, `docs/locks.md`, `docs/rpc.md`,
  `docs/terminal-output.md`, and existing `TODO.md`. Load the TDD and systematic
  debugging skills if available; otherwise follow the red-first loop below.
- Record `git status --short`, the current commit, and `just reference`.
  Derive the pin from the current documentation, never a hardcoded version.
  Preserve existing edits. Do not reset, stash, or overwrite someone else's work.
- Treat `./pi` as read-only. If the pin is missing, dirty, or mismatched, report
  that limitation and do not claim parity verification. Continue independent
  logic, concurrency, security, and performance checks where possible.
- Read the local gate recipes and CI configuration. Run `go vet ./...` and
  `go test -short ./...` as baseline checks. Record pre-existing failures;
  a new failure must be distinguished from them. Use cached modules only.
- Use a session goal, if supported, to track scope, evidence, repairs, and
  verification. A bounded run is a partial audit, not proof of global safety.

## 2. Inventory and inspect systematically

Create a concise coverage ledger in `docs/audits/<date>-codebase-audit.md`, using
an unambiguous suffix if that path already exists. Include the audited commit,
reference tag/commit, dirty-tree limitations, and entries for each area and each
of the five review lenses below: inspected, not applicable with reason, or not
yet inspected. Record exact files/functions read, relevant checks, confirmed
finding IDs, and remaining coverage. Do not mark an area reviewed from a search
result alone. Keep unconfirmed leads here, not in `TODO.md`, and label them.

Inventory first-party production code, tests, scripts, build/release workflows,
and configuration. Cover `ai`, `agent`, `coding`, `coding/interactive`,
`coding/rpcclient`, `tui`, `mcp`, `cmd`, `internal`, and root entry points as they
exist. Inspect platform-specific files and generated-data consumers. Exclude
vendor/dependency bodies, generated catalogs, build output, and `./pi` from
first-party scanning; read pinned upstream sources separately for parity.

Work in bounded slices until coverage is complete or the user's budget is
exhausted. Prioritize trust boundaries, persistence, cancellation/shutdown,
UI ownership, and recent changes, but do not stop at the first bug or review
only recent diffs. Trace values through callers, callees, and error paths;
searches and analyzer warnings are leads, not findings. If delegating, use
non-overlapping read-only slices and reconcile evidence before editing.

### Review lenses

1. **Port parity:** Locate and read the corresponding pinned TypeScript source.
   Compare supported observable behavior, defaults, missing/null/empty values,
   JS truthiness and UTF-16 semantics, error behavior, path/URL rules, JSON
   bytes and key order, streaming transitions, and persistence round-trips.
   Check documented divergences and out-of-scope features first. A difference
   alone is not a bug. Do not restore excluded features or emulate declined
   diagnostics. A bug shared with upstream is not a parity finding.
2. **Logic:** Trace state machines and invariants, boundary conditions,
   ordering, stale snapshots, retry/compaction transitions, partial I/O,
   resource cleanup, cancellation, and error propagation. Include malformed
   external input only where that input can reach the production boundary.
3. **Concurrency:** Establish the owner of every shared state touched by a
   reviewed path. Trace locks and lock order, callbacks/reentrancy, channel
   admission, cancellation, worker handoffs, shutdown draining, and lifetimes.
   Check the single-writer UI invariant and blocking work on its loop. Prove
   a feasible interleaving; a mutex or goroutine by itself is not suspicious.
   Leave the full race gate to CI per `AGENTS.md`; static review cannot prove
   race freedom, and no race-detector run means no claim that it passed.
4. **Security:** State the attacker, input, trust boundary, and affected asset.
   Trace path traversal/symlinks, command or argument injection, credential
   leakage, auth/token validation, HTTP redirects/SSRF, untrusted MCP/provider
   data, file permissions, terminal control injection, resource exhaustion,
   project trust, and update/download integrity. Account for intentional tool
   capabilities and user-granted authority. Use synthetic secrets, local
   servers, and disposable directories; never exploit real systems or print
   real credentials. Do not add network scanners or fetch advisory databases
   without authorization. Security fixes that deliberately differ from pi
   need an explicit documented divergence, not silent parity claims.
5. **Performance:** Trace input-scaled loops, repeated scans/copies, allocation
   growth, I/O under locks, unbounded queues, UI blocking, and render/stream
   paths. Require a concrete workload and a measured baseline, an allocation
   test, or a reproducible stall. An apparent expensive operation alone is not
   a bottleneck. Do not add speculative caches, thresholds, workers, or fast
   paths. Use bounded benchmarks and profiles only when evidence warrants them.

Use existing locally available analyzers when useful. Do not install tools or
change configuration merely to produce more warnings. Report unavailable
checks accurately; analyzer silence is not proof of correctness.

## 3. Reproduce and plan before repairing

For each candidate, establish a reachable scenario, actual result, expected
result and its authority, root cause, and user impact. Minimize the case and
include a matching control. Reject scenarios that depend only on impossible
internal state, intentional divergences, or unsupported features.

For parity, run equivalent upstream-source and Go probes with identical inputs.
Generate fixtures by executing the pinned source, not by hand-writing expected
output or using an installed bundle. For logic, security, or concurrency, use a
safe public-boundary reproduction or deterministic schedule. For performance,
record workload size, command, relevant measurements, and why they matter.
If a candidate cannot be reproduced, keep it explicitly unconfirmed in the
ledger and do not patch it or put it in `TODO.md`.

Add each confirmed unresolved finding to `TODO.md` before production edits.
Preserve unrelated findings and deduplicate existing ones. Use this structure:

```markdown
## <ID>: <severity> <concrete failure>
- Category and affected paths/functions:
- Scenario, expected behavior, actual behavior, and impact:
- Evidence: replay command, decisive output, matching control:
- Authority: pinned reference file/function/commit or local invariant:
- Root cause (including trust boundary or interleaving where relevant):
- Repair plan: regression test, minimal change, affected callers/platforms:
- Verification: focused test and applicable repository gates:
```

Prioritize by reachable impact, not warning count. Preserve stable IDs while
findings are open. Keep evidence replayable in repository tests or a documented
probe; a temporary path alone is not durable evidence. If no confirmed
unresolved findings remain, use `No confirmed unresolved findings.` rather than
inventing tasks. Audit-only stops after the plan and coverage report.

## 4. Execute confirmed repairs

By default, continue after planning without asking for permission for each
ordinary fix. Ask before a repair changes a deliberate divergence, public
contract, trust policy, or introduces destructive actions outside this scope.
Keep blocked confirmed findings in `TODO.md` with the exact unresolved decision.

For each finding, in priority order:

1. Add the smallest regression test at the supported boundary. Run it and
   observe the intended failure before editing production code. A compile
   error or broken harness is not the required red signal. For a concurrency
   bug, use controlled handshakes rather than sleeps; for performance, retain
   the pre-change measurement and use a stable regression check where feasible,
   not a flaky wall-clock threshold in ordinary tests.
2. Change only what the evidence demands. Preserve JSON byte parity with
   `ai.MarshalJSON`, ownership and package boundaries, callback delivery outside
   locks, and platform behavior. Do not refactor unrelated code or weaken tests
   to make them pass. Keep fixtures small; never hand-edit generated goldens.
3. Run the failing test again and observe it pass, then run adjacent tests and
   the matching controls. Confirm the original reproduction is resolved and no
   intended behavior was lost. Record before/after performance measurements
   for an optimization.
4. Update authoritative docs if behavior/status changed. Add a D-row comment
   and `docs/DIVERGENCES.md` entry only for an intentional departure. Remove the
   finding from `TODO.md` only after its repair is verified; keep its resolution
   and evidence in the audit ledger. Do not erase unrelated or blocked work.

Finish the inventory rather than treating one repaired slice as a codebase-wide
audit. When time or context is limited, leave exact remaining areas and replay
commands in the ledger and report partial coverage. Do not label an incomplete
audit complete merely because its confirmed findings are fixed.

## 5. Verify and hand off

- Run focused and affected-package tests, `just check`, `just cross`, and
  `just reference`. Do not silently skip failures or run the full race gate
  locally contrary to repository guidance. Request CI verification for it.
- Regenerate changed upstream fixtures to temporary files and compare bytes.
  Verify `./pi` is unchanged. Inspect `git diff` and new files for scope,
  accidental secrets, unrelated edits, and `git diff --check` failures.
- Update the ledger with actual commands, pass/fail results, unresolved
  findings, and coverage limits. Never claim a check ran unless it did, and
  preserve failed-command exit status when filtering logs.
- Report fixed findings, remaining `TODO.md` entries, ledger path, verification,
  and any required next action concisely. Static analysis plus passing tests
  cannot certify the absence of bugs, vulnerabilities, or races.
- Leave changes uncommitted unless the user explicitly authorized committing.
  If authorized, use `type(scope): summary` and cite the pinned reference
  paths/commit where applicable. Never push or release without authorization.
