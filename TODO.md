# Reproduced audit findings

- M86 (medium): the exported HTML template walks the session tree recursively (`sortChildren`, `mapNodes`, `markActive`), so exporting a long session with thousands of entries overflows the browser's call stack and the page renders nothing. Pinned pi v1.5.0 rewrote those walks iteratively (template.js, v1.4.0..v1.5.0). Evidence: `/tmp/pier-maintenance-20261102-notes.md` M86.
- M87 (low): the exported HTML renders later system messages as generic `[system]` rows and hides system prompt sections, tool additions and removals, while pinned pi v1.5.0 shows the leading system prompt and each later update with its changed sections. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M87.
