# Reproduced audit findings

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, OpenRouter catalog, and Google Vertex metadata-token endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.
- M49 (low): remote-catalog freshness uses unchecked `now - checkedAt` int64 arithmetic; a persisted minimum timestamp wraps the age negative, making a very stale catalog appear fresh and suppressing refresh. A local synthetic catalog probe with `checkedAt=MinInt64` issued zero requests. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M49.

