# Reproduced audit findings

- M139 (perf): `GetContextUsage` and `ProjectedEntries` re-walk and re-estimate the whole compaction window on every call, and the footer is invalidated on every session event (including each streaming update). Measured at 40,000 entries: 9.6 ms per `GetContextUsage` and 4.6 ms per `ProjectedEntries`, so a long session pays that per paint while streaming. The resolved window and its token estimate depend only on the branch version, which the projection cache already tracks.
