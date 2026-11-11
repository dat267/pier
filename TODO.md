# Reproduced audit findings

## Handover (2026-11-12)

The race hunt through `coding` ended at `b6048249`. Each fix from it has a
test, and D438 records the run-lifecycle ones. Three findings remain open. They
are unreproduced or shared with upstream, so treat them as leads: reproduce one
before fixing it, and give it a D-row if the fix departs from upstream.

- **Overlapping manual compactions** (shared with upstream). Two
  `CompactSession` calls both run, and the first to finish sets
  `compactionCancel` to nil while the second is still running. `IsCompacting`
  then reports false, so a prompt can start during the second compaction. Start
  at `coding/agent_session.go` `CompactSession` and its `cleanup`. The chaos
  tests run concurrent compactions and have not shown damage to the transcript.
- **Run start inside `CompactSession`** (shared with upstream). A run can start
  between `s.Abort(ctx)` and the `compactionCancel` claim, so the compaction
  rewrites the transcript under a live run. Upstream has the same `await` gap.
- **Stale model snapshot** (not reproduced). `ModelRuntime.updateModelSnapshot`
  reads `models.GetModels` before taking `r.mu`. A credential sync
  (`synchronizeCredentialState`) that overlaps a `Refresh` could publish the
  older list last. The next update corrects it. If it is fixed, number the
  snapshots by start order, as `McpManager.rebuildTools` does.

### Ways to find more

- Bug classes seen so far:
  - An unlocked `IsStreaming`/`IsIdle` check followed by an act under the run
    lock.
  - Take-then-fail: a path drains state, then loses the run start
    (`errRunAlreadyActive`) and drops what it took.
  - A stale snapshot published last: overlapping rebuilds, or read-modify-writes
    of the active tool set.
  - Plain fields written off the loop with no lock.
- To hunt, add the suspect writer or reader to `heavySessionOps` in
  `coding/agent_session_chaos_test.go` and run it with `-race -count=40`. To
  prove a timing bug, insert a temporary `time.Sleep` in the suspected window.
  The test must fail with the sleep in place and pass once the fix is in.
- UI ownership: `handleTreeSelection` (`coding/interactive/interactivemode_selectors.go`)
  runs off the loop and calls `FlushCompactionQueue`, which takes the
  loop-owned compaction queue there. The compaction-end path now takes the
  queue on the loop (`TakeCompactionQueue`). The tree path has not been checked
  yet.
