# Reproduced audit findings

- M105 (medium): the main (non-fullscreen) screen writes the transcript's OSC 133 prompt markers to the terminal. The terminal reads `133;A` as a new shell prompt, which ends the program-status sequence (OSC 7501), so the working/idle indicator is cleared as soon as a user message renders. Pinned pi v1.5.0 strips the markers in `tui-main-screen.ts` before writing.
