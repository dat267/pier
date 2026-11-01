# Reproduced audit findings

- M35 (medium): classifier endpoints read unbounded successful JSON response bodies into memory, accepting a synthetic 20 MiB response with only an empty answers array. Bound choice and protocol impact need product decision. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M35.

