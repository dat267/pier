# Reproduced audit findings

- M97 (low): bare autolinks do not stop at CJK punctuation. Pinned pi v1.5.0's `MarkdownTokenizer.url` truncates a bare URL at the first position matching `cjkPunctuationRegex` (any punctuation after a CJK character, or a fullwidth ，．：；！？（）［］｛｝“”‘’…—) and re-tokenizes the prefix, so `https://example.com/テスト。次` links only up to the `。`. Pier's autolink matches the full run and includes the punctuation in the link.
