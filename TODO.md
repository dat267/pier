# Reproduced audit findings

- M94 (medium): the system prompt text is still v1.4.0's. Pinned pi v1.5.0 changed the preamble ("You are a coding assistant running inside pi... Help users inspect files, run commands, and modify code."), the tools trailer ("Additional tools may be available.") and the skills guidance ("Resolve relative paths against the skill's directory..."). Every session prompt differs from the pin. Evidence: `coding/testdata/system-prompt/golden.json` regenerated from v1.5.0.
