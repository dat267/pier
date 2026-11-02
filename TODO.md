# Reproduced audit findings

- M141 (low, parity): word-diff equality trims with Go's `strings.TrimSpace`, but jsdiff 8.0.4 `WordDiff.equals` uses JavaScript `trim()` (`\s`). A U+FEFF-prefixed word is therefore removed+added instead of kept; U+0085 is handled the other way. `DiffWords("a", "\uFEFFa")` yields two changes where pinned jsdiff yields one keep.
- M142 (low, performance): `diffWordTokenize` runs two regexes per token part and concatenates strings, costing about 6.7–7.1 ms and 5,250 allocations per 2,000-word word-diff; pinned jsdiff 8.0.4 measures 0.96–1.19 ms on the same input.
