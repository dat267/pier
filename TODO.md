# Reproduced audit findings

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, OpenRouter catalog, and Google Vertex metadata-token endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.
- M47 (low): an extreme positive `FederationTokenCache.RefreshBackoff` overflows `now.Unix()+RefreshBackoff`, causing an expired cached token to be reused. A local token endpoint with `expires_in=1` and `RefreshBackoff=MaxInt64` returned the same token on the next call instead of exchanging again. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M47.

