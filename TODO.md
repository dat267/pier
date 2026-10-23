# Reproduced audit findings

**Pathological emphasis runs differ from Marked (v1.4.0).** Well-formed
emphasis now matches Marked, including the flanking rules and the CJK extension
upstream applies with `marked-cjk-friendly` (`b2d0da6fe`, #10154): the 95-case
token corpus and a 26-case matrix agree, and 391 of 400 random emphasis-heavy
strings do too. The rest are malformed delimiter runs where Marked's regex-based
scan picks a different boundary, for example `：__1_` (Marked renders `_` literal
then em(1); pier leaves the text literal) and `**__(___*` (pier opens a strong
where Marked does not). Matching those needs Marked's exact `emStrong` regex
alternatives, which RE2 cannot express, so the scan reimplements their intent.

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
