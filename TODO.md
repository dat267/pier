# Reproduced audit findings

- M128 (low): `coerceRevision` truncates a fractional float and overflows an out-of-range one into `int`: a model sending `revision: 1.5` matches revision 1 and can complete or block the goal without confirming the exact revision, and `revision: 1e300` reports an absurd negative revision in the stale-ref reply.
