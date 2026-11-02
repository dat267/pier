# Reproduced audit findings

- M67 (low): `resumeSession` applies `--continue` before `--session`/`--resume`, but pi v1.4.0 `createSessionManager` checks fork → session → resume → continue → session-id, so `pier -c --session <id>` silently resumes the newest session instead of the requested one and appends to it. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M67.
