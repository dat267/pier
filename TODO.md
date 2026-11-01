# Reproduced audit findings

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, and OpenRouter catalog endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through, but pier already diverges for unsafe OSC in D290; extending that security policy needs an ANSI allowlist/compatibility decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.

