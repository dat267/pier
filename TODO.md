# Reproduced audit findings

- M64 (medium): tool call cards interpolate model-supplied tool names, paths, commands, patterns and file bodies into display text without sanitizing them, so a crafted argument (`ESC[2J`) clears the screen; the M37 sanitizer covered assistant/provider text only. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M64.
