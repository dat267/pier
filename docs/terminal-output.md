
## D202. Optional terminal output is admitted before enqueueing

D198's paint watermarks do not cover direct terminal writes. The direct-write
inventory distinguishes producers rather than imposing a lossy cap on the
terminal's generic FIFO:

- Differential main/fullscreen paints, cursor/screen commands, alternate-screen
  transitions and scrollback replay retain lossless committed FIFO admission.
  Renderer paints remain scheduled through D198; a large frame can overshoot.
- Bracketed paste, keyboard negotiation, color queries/subscriptions, cursor
  restoration, progress clear and graceful-exit hints are essential protocol or
  lifecycle output. They do not wait for optional admission. Queries with reply
  deadlines cannot be delayed behind optional output without changing semantics.
- Window-title updates and active-progress refreshes/keepalives are redundant
  metadata. At 256 KiB of committed queued plus in-flight output, they defer
  before enqueueing. There is one newest-pending slot per kind, not a mutation
  of any committed FIFO entry. The writer admits deferred hints after recovery
  to 128 KiB or less, without requiring another input event. Progress clear
  invalidates pending active hints, and stopped ticker generations cannot
  reintroduce an active indicator. Permanent shutdown discards uncommitted hints.
- Interactive clipboard OSC 52 fallback previously wrote raw stdout from its
  worker, bypassing terminal ordering, backlog measurement and exit policy.
  `CopyTextToClipboardWithOSC52` injects the fallback sink, while retaining the
  platform helper order and existing 100,000-byte encoded-payload limit. The
  app captures its stable terminal sink at construction. ProcessTerminal uses
  `WriteOptionalContext`; custom terminals retain their own Write behavior.

`WriteOptionalContext` is an off-loop API. It waits before accepting a complete
packet until committed bytes plus packet length fit within 256 KiB and no
renderer frame is open. Cancellation, output finalization or permanent shutdown
release waiting producers without dropping accepted output. Packets larger than
256 KiB are rejected rather than split through escape sequences or frames.
Accepted packets retain FIFO ordering. Admission is not a delivery confirmation;
D200 still governs display output remaining at exit. D201 bounds app clipboard
jobs (including waiting producers) to four; this API does not bound arbitrary
SDK callers that spawn their own waiting goroutines.

A title remains one atomic existing value: an oversized title on an idle
terminal is allowed to overshoot instead of truncating or deadlocking. Deferred
metadata memory is therefore bounded by one title value plus one fixed progress
sequence, not by an absolute title-byte limit. Generic `Write`, standalone
clipboard APIs and standalone fullscreen selection defaults retain compatibility;
this is not a global hard byte cap or an admission rule for arbitrary SDK writes.
No saved session/settings JSON or provider payload changes.

Blocked-writer regression: 64 repeated same-title/progress updates plus 10
virtual keepalive ticks formerly added 1,296 committed bytes behind a 262,144-byte
in-flight prefix. Now they add zero committed bytes while stalled, retain only
newest metadata, and recover without replaying active progress after clear.
Tests also observe waiting/canceled optional admission, responsive essential
writes, atomic frame/FIFO recovery, oversized-title progress, injected clipboard
fallback errors, and app-owned OSC 52 routing. Existing D198 owner-loop
blocked-console input tests and the SSA UI-blocking gate remain active.
