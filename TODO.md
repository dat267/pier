# Reproduced audit findings

- **A443** (low, docs) the port cites D-rows that have no entry: D1, D2, D3, D5,
  D8, D21-D29, D32, D34-D40 and D49 are named from Go source but `DECISIONS.md`
  starts at D41. D49 is added with this pass; the rest need a decision
  (reconstruct the rows or drop the numbers) because the citing comments are the
  only surviving record of their rationale.
- **A446** (low, docs) highlight: nginx variables are colored and a ruby
  variable ending the input is not truncated, where the reference's grammars
  stop matching. Resolved as D413 with
  `internal/highlight/reference_grammar_quirks_test.go`.
