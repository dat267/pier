# Reproduced audit findings

## A471 `goalPreview` caps a goal card by line count only, so one long line renders in full

- Severity: medium (UI cost; user-visible transcript blow-up).
- Measured through the real renderer (`goalSetRenderers.RenderCall` then
  `Render(80)` in `coding/interactive`):

  | objective | rendered lines |
  |---|---|
  | 100,000 short lines | 8 |
  | 1 MB on a single line | 12,502 |
  | 20 lines of 200 KB each | 12,503 |

  1 MB single-line render: 82 ms. The 100,000-short-line case shows the cap
  working; any line longer than the cap itself is passed through whole.
- Cause: `goalPreview` (`internal/goal/render.go:238`) splits on newlines,
  keeps `goalPreviewLines` lines, and returns each kept line unchanged, so the
  column bound is unbounded. `tui.Text.Render` then wraps that line to the
  transcript width, producing one output line per width band.
- Reachable: `set_goal`'s `objective` argument is free text with no length
  limit in the tool schema (`coding/goalcontroller.go:694`); the card is drawn
  from the call's arguments, and again from the durable goal entry, and again
  by `get_goal`'s result card (`RenderGetGoalRenderResult`).
- Fix: bound each kept line to a character budget in `goalPreview` and report
  the elided remainder in the existing hidden-count hint, so a wide line
  collapses like extra lines do.
- Verification: a test that renders a 1 MB single-line objective, a 20x200 KB
  objective and 100,000 short lines, asserting the rendered line count stays
  bounded while the full text is still reachable when expanded.
