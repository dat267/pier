# Reproduced audit findings

- M35 (medium): classifier and MCP OAuth metadata endpoints read unbounded successful JSON response bodies into memory; synthetic 20 MiB bodies parse successfully. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.

