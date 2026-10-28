# Reproduced audit findings

## Medium

- CR-127: Trust-store read/parse/lock errors are swallowed as “no decision”; with `defaultProjectTrust: "always"`, a malformed `trust.json` can turn a stored denial into a trusted project. Upstream errors instead; changing trust-failure handling awaits approval.

## Low

- CR-117: Highlighter multiline anchors use RE2's LF-only boundaries instead of JavaScript's CR/LF/LS/PS boundaries, so comments, directives, and headings can consume later lines or include the CR of CRLF files.
