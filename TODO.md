# Reproduced audit findings

- M72 (low): `ConvertToLlm` wraps a stored compaction/branch summary in `<summary>` tags without escaping, so a summary containing `</summary>` closes the block early on every later request and injects text as harness context. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M72.
