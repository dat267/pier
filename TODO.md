# Reproduced audit findings

- CR-173 (P3): streamable-HTTP `MaxRetries: 0` silently selects five retries, so callers cannot disable reconnect; API needs an explicit unset/zero distinction.
