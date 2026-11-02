# Reproduced audit findings

- M85 (low): `SetActiveToolsByName` rebuilds the tool list in the order the caller passes, so re-enabling the same tools in a different order changes the declared tool order and rewrites the system prompt's tool list, invalidating prompt caches. Pinned pi v1.5.0 keeps the current order for tools that stay active and appends newly active ones (b9e3eef6c, #11495). Evidence: `/tmp/pier-maintenance-20261102-notes.md` M85.
