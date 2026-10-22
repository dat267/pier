# Reproduced audit findings

## Compaction audit of `48081fe`

Reference: pi **v1.3.0** (`71023aec1546d6d677a65669c5665c0927ce0dd0`).
All five findings below reproduced on three consecutive runs. No production
fixes have been applied. The failing probes live under
`coding/testdata/compaction-audit/` so they do not break the normal test gate.
See that directory's `README.md` for the reproduction command.

1. **High: resumed overflow prevents the next prompt.**
   `coding/agent_session_compaction.go`'s `entryIDForMessage` only consults the
   live message-to-entry map. SDK resume installs messages with `SetMessages`
   (`coding/sdk.go`) without filling that map. A resumed session ending in a
   context-overflow error fails its next `PromptText` with
   `Cannot persist recovery omission because a projected message has no source entry`.
   Upstream `_findPersistedMessageEntryId` falls back to branch/projection
   provenance. Probe: `TestAuditCompactionRecoversResumedOverflow`.

2. **High: the between-turn threshold check ignores new tool-result tokens.**
   `coding/agent_session.go`'s `maybeAutoCompact` calls `CheckCompaction` on the
   last assistant; without context edits, that uses its usage directly rather
   than estimating trailing messages. With 610 reported tokens and an 800-token
   tool result, the next request is sent without compaction in a 1000-token
   window (reserve 100). Upstream `_compactBeforeNextAssistantResponse` checks
   the complete projection. Probe: `TestAuditCompactionCountsNewToolResults`.

3. **Medium: retained pre-compaction usage remains trusted.**
   `coding/compaction.go`'s `EstimateProjectedContextTokens` compares entry
   positions in replay order, where the latest compaction precedes its retained
   messages, instead of chronological branch order. A short summary and two
   short retained messages still report 900100 tokens after compaction.
   The same fixture through upstream `estimateProjectedContextTokens` reports
   12 tokens with no usage-backed estimate. This also affects preparation's
   `TokensBefore`. Probe: `TestAuditCompactionInvalidatesRetainedUsage`.

4. **Medium: compaction drops configured provider environment.**
   `coding/compaction.go`'s `runSummarization` never copies `CompactionOptions.Env`
   into stream options. A configured `AWS_REGION` is absent from the summary
   request, even though auth resolution supplies environment in both session
   compaction paths. Providers using scoped environment can fail or use process
   defaults instead. Upstream `createSummarizationOptions` forwards `env`.
   Probe: `TestAuditCompactionForwardsProviderEnvironment`.

5. **Medium: manual and automatic compaction drop the selected reasoning level.**
   `runManualCompaction` (`coding/agent_session.go`) and `RunAutoCompaction`
   (`coding/agent_session_compaction.go`) construct `CompactionOptions` without
   `ThinkingLevel`. With a reasoning-capable model set to `high`, both summary
   requests carry an empty reasoning level. Upstream `_getSummarizationRequestAuth`
   and `_runDefaultCompaction` forward the selected level.
   Probe: `TestAuditCompactionKeepsSelectedReasoning`.

## Earlier audit

All 13 findings from the 2026-10-20 audit of `075f84a` against the same reference
have regression-tested fixes. See `docs/PORTING.md` for the resolution summary.

Add only observed reproductions here, excluding documented intentional
divergences and unconfirmed suspicions. Remove each finding once its fix is
verified.
