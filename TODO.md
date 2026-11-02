# Reproduced audit findings

- M119 (low): the edit tool's prompt guidelines are v1.4.0's four bullets; pinned pi v1.5.0 replaces them with two ("Use edit for targeted changes. Batch changes to one file in a single call." and "Each edits[].oldText must exactly and uniquely match the original file. Keep matches short and non-overlapping; merge nearby changes without including large unchanged regions."), so the system prompt rules differ.
- M120 (low): the grep tool normalizes lone carriage returns to newlines, so a file with classic-Mac line endings is split into lines that ripgrep never reported; pinned pi v1.5.0 only normalizes CRLF (grep.ts).
