# Reproduced audit findings

## A472 a model-supplied blocked reason becomes an unbounded chat notice

- Severity: medium (UI cost; user-visible transcript blow-up, same class as A471).
- Measured (`RunWiring.ShowChatWarning`, then `Render(80)`):

  | blocked_reason | chat lines |
  |---|---|
  | 100 KB on one line | 1,284 |
  | 15,000 short lines | 15,001 |

  100 KB single line: 11 ms; 15,000 lines: 7 ms.
- Cause: `goalUpdate`'s blocked branch (`internal/goal/machine.go:355`) trims
  `blocked_reason` and stores it unbounded; `pauseNotice` embeds it in
  `NotifyEffect`, `GoalController.notice` forwards it, and
  `RunWiring.ShowChatWarning` sanitizes escape sequences but adds every line to
  the chat. `set_goal`'s schema gives `blocked_reason` no length limit.
- Reachable: a model calling `set_goal status=blocked` after three rounds with a
  long reason. Also the get_goal result card and the durable goal entry render
  the same text through `goalPreview`, which D427 already bounds.
- Fix: bound the notice at the point it becomes a chat line, so every notice
  path inherits it, and keep the untruncated text in the goal state (the card
  and the model-facing messages already carry it).
- Verification: a test asserting a 100 KB one-line reason and a 15,000-line
  reason each add a bounded number of chat lines, while a short reason is
  unchanged.
