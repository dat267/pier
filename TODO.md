# Reproduced audit findings

## Medium

- CR-138: A 71-byte PNG with unallocatable dimensions panics in ProcessImage's decoder instead of returning an image-omission result. Reproduced; repair parked at the audit deadline.

## Low

- CR-117: Highlighter multiline anchors use RE2's LF-only boundaries instead of JavaScript's CR/LF/LS/PS boundaries, so comments, directives, and headings can consume later lines or include the CR of CRLF files.
