# Reproduced audit findings

- **A443** (low, docs) the port cites D-rows that have no entry: D1, D2, D3, D5,
  D8, D21-D29, D32, D34-D40 and D49 are named from Go source but `DECISIONS.md`
  starts at D41. D49 is added with this pass; the rest need a decision
  (reconstruct the rows or drop the numbers) because the citing comments are the
  only surviving record of their rationale.
- **A450** (low) highlight: the working nginx variable variant added relevance,
  so `highlightAuto` preferred nginx for perl/bash/processing snippets. Resolved
  under D413. Test: `internal/highlight/autodetect_nginx_score_test.go`.
