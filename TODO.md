# Reproduced audit findings

- M115 (low): `CombineUsage` drops `totalFromProvider`, so a combined usage (nested calls, aggregated summaries) serializes without it; pinned pi v1.5.0 sets it to true only when every part was provider-reported (usage-totals.ts).
- M116 (low): session cwd filters compare resolved paths with `!=`, so on Windows a case-only difference hides a session from the current project's list and from `--continue`; pinned pi v1.5.0 uses `pathsEqual` (session-manager.ts).
- M117 (low): the trust store looks decisions up by exact key and writes one key, so on Windows a decision saved with different casing is missed and duplicates can accumulate; pinned pi v1.5.0 matches keys with `pathsEqual` and replaces every variant on write (trust-manager.ts).
- M118 (low): the `/import` and `/resume` descriptions are v1.4.0's wording ("Import and resume a session from a JSONL file", "Resume a different session"); pinned pi v1.5.0 rewords both.
