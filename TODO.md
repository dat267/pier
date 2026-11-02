# Reproduced audit findings

No open findings.
- M143 (low, performance): `ai.MarshalJSON` took about 90–100 ms and 838k allocations for a 4,000-message conversation (790 KB), while Node `JSON.stringify` takes 1.07–1.10 ms. The canonicalizer now scans bytes instead of building decoder tokens: its pass is 55 ms → 11 ms and 456k → 148k allocations, and end-to-end `MarshalJSON` is about 38 ms and 304k allocations. Still open: the remaining cost is 8,001 nested canonicalizations from each message type's `MarshalJSON` plus the byte-parity transforms. Closing it needs a fragment-marshaling path and a decision on whether wire/persisted bytes may skip JS key-order parity; realistic sizes cost only a few ms today.
