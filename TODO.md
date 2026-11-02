# Reproduced audit findings

- M61 (low): `TestModelArgumentCompletionEmptyPrefixReusesCachedScopeItems` samples process-wide TotalAlloc in the shared test binary; a full race-shuffled run measured 321,680 bytes against its 225,280-byte threshold while focused race count=5 passed. Isolate the measurement without lowering the budget. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M61.

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, OpenRouter catalog, and Google Vertex metadata-token endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.

