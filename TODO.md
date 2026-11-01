# Reproduced audit findings

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, OpenRouter catalog, and Google Vertex metadata-token endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.
- M48 (low): MCP OAuth `SaveTokens` multiplies `expires_in` into milliseconds, converts to `int64`, then adds the current time without bounds; a large finite lifetime wraps the stored `TokensExpireAt` into the past. A local provider probe with `expires_in=1e16` produced `-9223370243279553631`. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M48.

