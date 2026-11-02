# Reproduced audit findings

- M76 (low): the no-models and no-model-selected help names `docs/providers.md` and `docs/models.md`, but pier ships no `docs/` tree (D176 already avoids naming it in the system prompt), so a new user is pointed at files that do not exist. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M76.
