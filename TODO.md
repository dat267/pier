# Reproduced audit findings

- M32 (low): OAuth discovery requests raw dot-segment paths that the pin's WHATWG URL normalizes, so an issuer with `/a/../b` finds no metadata. Evidence and plan: `/tmp/pier-maintenance-round15-notes.md` M32.

