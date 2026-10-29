# Reproduced audit findings

- CR-173 (P3): streamable-HTTP `MaxRetries: 0` silently selects five retries, so callers cannot disable reconnect; API needs an explicit unset/zero distinction.
- CR-175 (P2): session JSONL files are created mode 0666 (typically 0644) in directories mode 0777 (typically 0755), exposing conversation/tool output to other local users when parent paths are searchable; hardening would intentionally diverge from pi and needs approval.
- CR-176 (P2): D161's output-budget sweep can unlink a still-open, in-progress tool output file when a new log pushes existing scratch above 256 MiB. Same-process concurrent outputs are now protected; another pier process sharing the temp directory has a separate active-file registry and can still evict it, leaving a stale path. Pinned pi v1.4.0 (`packages/coding-agent/src/core/bash-executor.ts` and `core/tools/output-accumulator.ts`) does not sweep these files. Cross-process protection remains open.
