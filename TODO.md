# Reproduced audit findings

- **A442** (low) agent: a `toolcall_end` payload replaces the accumulated tool
  call instead of merging into it (`Object.assign` upstream), so a server that
  omits `id` or `name` erases them and the call cannot be dispatched. Full
  record: 2026-11-09-agent-mcp-audit. Tests:
  `agent/proxy_toolcall_patch_test.go`.
