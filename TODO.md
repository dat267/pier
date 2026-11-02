# Reproduced audit findings

- M69 (low): `RuntimeCredentials` guards its runtime-override map with no mutex, so `SetRuntimeAPIKey`/`RemoveRuntimeAPIKey` racing `Read`/`List`/`HasRuntimeAPIKey` is a Go data race (SDK callers can set a key while a turn resolves credentials). Evidence: `/tmp/pier-maintenance-20261102-notes.md` M69.
