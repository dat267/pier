# Reproduced audit findings

- M131 (medium): `LoginDialogComponent.ShowAuth` opens the provider-supplied authorization URL with the platform handler without checking its scheme, so a hostile or broken OAuth backend can return a `file:` or custom-scheme URL that launches a local program on login. It also shows and copies the raw URL. Pinned pi v1.5.0 opens only a normalized http(s) target and displays `webUrl(url) ?? replaceControlCharacters(url)`.
