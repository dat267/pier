# Reproduced audit findings

No unresolved findings. The pathological emphasis runs recorded against pi
v1.4.0's Markdown pipeline are fixed, including `：__1_` and `**__(___*`. Red-first
regressions in `internal/markdown/emphasis_test.go` cover failed-run retries,
text-predecessor tracking, mixed-marker masking, nested underscore openers,
JavaScript whitespace and supplementary-character handling. Two deterministic
50,002-case comparisons against Marked 18.0.11 with marked-cjk-friendly 0.1.2
matched after the fixes (one emphasis-only pool, one expanded Unicode pool).

The compaction audits of `48081fe` and `5918595`
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
