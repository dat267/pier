# Reproduced audit findings

- M106 (low): the `/model` selector never shows a model's display name next to its id, so a renamed override (`minimax/minimax-m3` named "MiniMax M3 [rasmus@openrouter, parasail]") is indistinguishable from the plain entry. Pinned pi v1.5.0 shows the name only when it adds words the id does not (model-search.ts `getDistinctModelName`), hiding redundant names like "Claude 3" for `claude-3`.
