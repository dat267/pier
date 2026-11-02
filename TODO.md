# Reproduced audit findings

- M70 (medium): the goal prompt framers embed the objective and blocker text without escaping `</...>` delimiters, so a goal whose objective or reason contains `</untrusted_objective>` (or `</goal_paused>`, `</goal_blocked>`) closes the wrapper early and injects lines that read as harness instructions in the next round. Evidence: `/tmp/pier-maintenance-20261102-notes.md` M70.
