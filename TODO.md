# Reproduced audit findings

- M82 (medium): `PrepareBranchEntries` ignores `context_edit` entries, so a branch summary is built from messages the user omitted or replaced; the summary then reintroduces hidden content into the next session's context and its file lists name files from hidden tool calls. Pinned pi v1.5.0 applies the latest edit per target (d0fe21308, #11680). Evidence: `/tmp/pier-maintenance-20261102-notes.md` M82.
- M83 (low): branch summarization never sends the session thinking level, while pinned pi v1.5.0 passes `reasoning: thinkingLevel` for reasoning-capable models (269afc66a, #11482). Conversation compaction already does this in pier, so branch summaries are the gap. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M83.
