# Reproduced audit findings

- M73 (medium): opening a session by path and then cloning/forking it calls `EnsurePrivateSessionDir` on the file's parent, silently chmodding a user-chosen directory to 0700 (reproduced: 755 → 700) or failing with `chmod ... operation not permitted` when pier does not own it; D289 only promises hardening for pier's own session directories. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M73.
