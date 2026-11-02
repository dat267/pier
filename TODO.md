# Reproduced audit findings

- M108 (medium): every streamed argument delta for a tool call rebuilds the tool card (`UpdateArgs` calls `updateDisplay`), so a large call rendered at frame rate runs its renderer dozens of times per second; pinned pi v1.5.0 defers the display update to the next render (`argsPending`, #9265).
