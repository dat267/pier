# Reproduced audit findings

- M112 (low): the tree selector does not capture fullscreen viewport keys, so PageUp/PageDown scroll the transcript instead of moving the tree selection while the tree is focused (pi v1.5.0 tree-selector.ts `capturesViewportInput` while no label input is open).
- M113 (low): `splitProviderPrefix` splits `provider/model` at the first slash, so a provider whose id contains a slash (a URL-derived id) never resolves; pinned pi v1.5.0 matches known provider ids longest-first.
- M114 (low): the default OpenAI model is `gpt-5.5`; pinned pi v1.5.0 selects `gpt-6.1-sol` (model-resolver.ts `defaultModelPerProvider`), which decides the model for a scoped or fresh OpenAI provider.
