# Reproduced audit findings

- M66 (low): the /tree search re-decodes and re-lowercases every entry on every keystroke (`ApplyFilter` → `decodeTreeMessage`/`getSearchableText`), costing ~20ms and 4.8MB per pass at 3,000 entries (22 allocations per node); no per-node caching. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M66.
