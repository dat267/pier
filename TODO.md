# Reproduced audit findings

- M60 (low): finite positive `fullscreenWheelScrollLines=1e308` wraps to `MinInt` when JSON settings decode, so SettingsManager clamps it to 1 instead of the upstream upper limit 100. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M60.


- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, OpenRouter catalog, and Google Vertex metadata-token endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.

