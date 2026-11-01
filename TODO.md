# Reproduced audit findings

- M23 (P3): Markdown inline parsing rescans to the end for every `[`, so provider text with many unmatched brackets stalls preparation (32 KB: 1.5 s-3.1 s vs 14-24 ms upstream). Evidence and repair plan: `/tmp/pier-maintenance-round6-notes.md`, M23.
