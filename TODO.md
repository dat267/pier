# Reproduced audit findings

## P2

- **A418 (M261)**: The selection copy runs its clipboard callback and `Flash` on a goroutine (`go copyTextToClipboard` in `handleSelectionMouseEventLocked`), so a concurrent second copy or an owner-loop frame can mutate `AltScreenFlashContainer.entries` (unlocked `Flash`/`AnimationFrame`/`Dispose`) and `nextID` together. Upstream's `copyTextToClipboard` is an async function whose continuation resumes on the single-threaded event loop, so it is safe there; the port's `go` needs the flash marshalled through `Renderer.Post`.
