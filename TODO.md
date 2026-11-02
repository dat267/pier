# Reproduced audit findings

- M88 (medium): `MatchFuzzy` scores only the greedy leftmost alignment, so a query can be scattered across an earlier unrelated part of the text (`opus` via "openRouter") and the real contiguous match ranks last. Pinned pi v1.5.0 also scores every contiguous occurrence and keeps the best alignment (8c03fef4d, #11220), which changes model/session/tree search ordering. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M88.
