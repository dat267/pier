# Reproduced audit findings

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, OpenRouter catalog, and Google Vertex metadata-token endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.
- M52 (low): MCP timeout conversion treats a finite `timeout` whose seconds-to-milliseconds product is `+Inf` as MaxInt64, which the timer adapter clamps to 1 ms; pinned upstream disables the timeout when the product is non-finite. A local `1e308`-second config probe showed `timeoutMs=9223372036854775807`. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M52.

