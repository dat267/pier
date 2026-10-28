# Reproduced audit findings

## Low

- CR-117: Highlighter multiline anchors use RE2's LF-only boundaries instead of JavaScript's CR/LF/LS/PS boundaries, so comments, directives, and headings can consume later lines or include the CR of CRLF files.
- CR-122: Stylus selector lookahead accepts end-of-input even though upstream requires a following character, coloring incomplete selectors that upstream leaves plain.
