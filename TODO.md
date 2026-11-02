# Reproduced audit findings

- M65 (medium): ctrl+o rebuilds every expandable transcript card synchronously, so the next paint re-renders the whole transcript (measured 137ms for 600 bash cards with 200-line outputs) instead of only what is visible; ctrl+t already defers to a window plus chunked beats. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M65.
