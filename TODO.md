# Reproduced audit findings

- M84 (medium): compaction, manual `/compact` and branch summaries pre-resolve the provider credential and pass it as `options.apiKey`. When the session uses the SDK stream function, the runtime resolves auth again and takes its API-key path instead of the stored OAuth login, so providers whose API-key path differs from their OAuth path fail every summary while prompts work. Pinned pi v1.5.0 marks the SDK stream function and skips pre-resolution (6fed3cfb9, #11556). Evidence: `/tmp/pier-maintenance-20261102-notes.md` M84.
