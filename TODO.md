# Reproduced audit findings

- M71 (low): summarization prompts embed the conversation and the previous summary inside `<conversation>`/`<previous-summary>` without escaping, so message text (or a chained summary) containing `</conversation>` closes the block early and injects lines ahead of the summarization instructions; same boundary as M70. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M71.
