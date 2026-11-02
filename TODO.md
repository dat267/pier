# Reproduced audit findings

No open findings.
- M143 (low, performance): `ai.MarshalJSON` takes about 90–100 ms and 838k allocations for a 4,000-message conversation (790 KB), while Node `JSON.stringify` takes 1.07–1.10 ms. Encoding alone is 40 ms because every message type's `MarshalJSON` calls the package helper, and each of the 8,001 nested calls re-runs canonicalization; the canonical pass alone is 55 ms. Plain Go structs of the same size encode in 1.3 ms, so the gap is the custom-marshaler chain plus the byte-parity canonicalizer, not encoding/json itself. At realistic session sizes the absolute cost is a few ms; a fix means removing nested canonicalization and rewriting the canonicalizer, and deciding whether request/persisted bytes may skip JS key-order parity.
