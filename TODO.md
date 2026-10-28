# Reproduced audit findings

## Low

- CR-117: Highlighter multiline anchors use RE2's LF-only boundaries instead of JavaScript's CR/LF/LS/PS boundaries, so comments, directives, and headings can consume later lines or include the CR of CRLF files.
- CR-121: INI highlighting's rejected key lookahead consumes bare key text instead of letting the root illegal rule discard the highlight, so truncated/invalid input keeps token colors upstream removes.
- CR-122: Stylus selector lookahead accepts end-of-input even though upstream requires a following character, coloring incomplete selectors that upstream leaves plain.
