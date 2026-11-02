# Reproduced audit findings

- M74 (medium): opening an existing 0-byte session file and writing anything fails with `session append failed (...): open ...: file exists`; the header is never written and the file stays 0 bytes, so every turn is lost with only a stderr line. `OpenSession` resets the write state via `NewSession`, then rewrites through the exclusive-create claim against the file that already exists. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M74.
