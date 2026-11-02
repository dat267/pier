# Reproduced audit findings

- M129 (low): `budgetStopReason` computes `CreatedAt + TimeLimitMs`, which overflows for a large model-supplied time limit and turns the check negative, so a goal created with a practically unlimited budget pauses on its first settle with "Time limit reached."
