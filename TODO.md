# Reproduced audit findings

**A pipe-free table header does not start a table (v1.4.0).** `internal/markdown`
requires a `|` in the header row (`tryTable`, `looksLikeTableStart`), but Marked,
as upstream reads it, accepts a pipe-free header when the delimiter row has a
pipe: upstream `2ed3118b0`'s own case renders `["$$", "x^2", "", "Value",
"| --- |", "1"]` as a table, and pier renders it as text. Independent of the
math fix that surfaced it; needs the Marked table-start rule ported with its own
regression cases.

No unresolved findings otherwise. The compaction audits of `48081fe` and `5918595`
recorded eight findings against pi v1.3.0
(`71023aec1546d6d677a65669c5665c0927ce0dd0`): resumed overflow blocking the
next prompt, the between-turn threshold ignoring new tool-result tokens,
retained usage trusted after compaction, compaction dropping provider
environment and the selected reasoning level, failed overflow compaction
replaying the omitted response, compaction auth resolution escaping the request
context, and UTF-8 file-list ordering. All eight now have red-first regression
tests in `coding/compaction_findings_test.go`; each probe was promoted from
`coding/testdata/compaction-audit/` and observed failing before its fix landed.

## Earlier audit

All 13 findings from the 2026-10-20 audit of `075f84a` against the same reference
have regression-tested fixes. See `docs/PORTING.md` for the resolution summary.

Add only observed reproductions here, excluding documented intentional
divergences and unconfirmed suspicions. Remove each finding once its fix is
verified.
