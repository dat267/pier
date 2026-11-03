# Reproduced audit findings

- M143 (low, performance, open): `ai.MarshalJSON` is now about 33–36 ms and 288k allocations for a 4,000-message conversation (790 KB), down from 90–100 ms and 838k (scalar fast paths account for the latest reduction); Node `JSON.stringify` takes 1.1 ms. The remaining cost is 8,001 nested canonicalizations from per-message `MarshalJSON` plus byte-parity transforms. Closing it needs a fragment-marshaling path and a decision on whether wire/persisted bytes may skip JS key-order parity; realistic sizes cost only a few ms today. See D350.
