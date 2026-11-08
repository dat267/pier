# Reproduced audit findings

## P3

- **A432**: `coding/testdata/session-roundtrip/generate.mjs` is not deterministic: two runs produce different bytes, because the pinned `SessionManager` stamps `new Date().toISOString()` and generates entry ids from `randomUUID()`/`uuidv7()`. Regenerating the fixture rewrites every id and timestamp, so a reviewer cannot separate a real change from noise and the fixture cannot be used to re-derive its own expectation. Fix: freeze the clock and the random source in the generator (patched `Date` and a deterministic `crypto` shim) so successive runs are byte-identical, then regenerate once.
