# Reproduced audit findings

- M110 (low): a read-tool preview whose highlighting fails returns raw lines without the code-block color. Pinned pi v1.5.0 unified the two highlight helpers on the markdown fallback (`mdCodeBlock`), and its read renderer uses it.
