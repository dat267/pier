# Reproduced audit findings

## High

- CR-129: Provider HTTP redirects can forward bearer credentials to a different origin (confirmed for the same host on a different port), unlike upstream fetch's credential stripping. Tightening redirect credential policy awaits approval.

## Medium

- CR-127: Trust-store read/parse/lock errors are swallowed as “no decision”; with `defaultProjectTrust: "always"`, a malformed `trust.json` can turn a stored denial into a trusted project. Upstream errors instead; changing trust-failure handling awaits approval.
- CR-138: A 71-byte PNG with unallocatable dimensions panics in ProcessImage's decoder instead of returning an image-omission result. Reproduced; repair parked at the audit deadline.

## Low

- CR-117: Highlighter multiline anchors use RE2's LF-only boundaries instead of JavaScript's CR/LF/LS/PS boundaries, so comments, directives, and headings can consume later lines or include the CR of CRLF files.
