# Reproduced, unresolved findings

## A replaced session keeps the first project's model scope

- Severity: low. The model cycle scope and its diagnostics belong to the cwd a session was resolved in.
- Evidence: `cmd/rpcmode.go:84` (`rpcSessionReplacer`) rebinds the project settings to the replacement's cwd but copies `base.ScopedModels` from the initial `run`, and `coding/sdk.go` uses `options.ScopedModels` as given. Upstream builds the scale scope inside the per-session factory, after the settings rebind, so a `/resume` or `/fork` into another project resolves that project's `enabledModels` and reports its `--models` diagnostics again (`model-scope-session-replacement.test.ts`, v1.6.0).
- Fix shape: when the scope came from settings rather than from `--models`, re-run `scopedModelsFor` in the replacer after `settings.RebindProject` and pass the new scope (and its diagnostics) into `CreateAgentSession`.
- Reproduce: start RPC with `--models` patterns, `switch_session` to a session whose project sets different `enabledModels`, then `get_available_models`; the cycle scope is still the first project's.
