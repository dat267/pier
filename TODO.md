# Reproduced audit findings

- M77 (low): a session switch, branch navigation, or compaction rebuild clears the chat while a ctrl+o/ctrl+t sweep is still draining, but `QueueController` keeps its deferred cards, so the loop rebuilds discarded components and repaints the replacement transcript once per chunk (100 cards: 11 extra beats). Evidence: `/tmp/pier-maintenance-20261102-notes.md` M77.
