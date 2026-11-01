# Reproduced audit findings

- M35 (medium): classifier, MCP OAuth, MCP HTTP JSON, and OpenRouter catalog endpoints read unbounded successful response bodies into memory; synthetic 20 MiB responses parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.
- M37 (medium): arbitrary CSI terminal controls in provider errors and assistant text pass through the final TUI output boundary. Synthetic `ESC[2J` reaches terminal output unchanged, allowing screen clearing/cursor manipulation. Upstream also passes it through. A focused untrusted-text sanitizer could preserve renderer-generated output but changes model/provider text behavior; compatibility decision pending. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M37.
- M44 (low): the bash-preview allocation assertion measures process-wide allocations with `testing.AllocsPerRun`; after asynchronous interactive tests it intermittently counts unrelated allocations and fails despite passing in isolation. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M44.

