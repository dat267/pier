# Reproduced audit findings

- M67 (low): `resumeSession` applies `--continue` before `--session`/`--resume`, but pi v1.4.0 `createSessionManager` checks fork → session → resume → continue → session-id, so `pier -c --session <id>` silently resumes the newest session instead of the requested one and appends to it. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M67.
- M68 (medium): `--no-session` is ignored when a resume flag is present, so `pier --no-session -c` opens the newest session with persistence enabled and `pier --no-session --session-id X` creates a stored session file, where pi v1.4.0 `createSessionManager` returns an in-memory manager first. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M68.
