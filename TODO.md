# Reproduced audit findings

- CR-173 (P3): streamable-HTTP `MaxRetries: 0` silently selects five retries, so callers cannot disable reconnect; API needs an explicit unset/zero distinction.
- CR-175 (P2): session JSONL files are created mode 0666 (typically 0644) in directories mode 0777 (typically 0755), exposing conversation/tool output to other local users when parent paths are searchable; hardening would intentionally diverge from pi and needs approval.
