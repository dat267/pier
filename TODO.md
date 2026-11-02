# Reproduced audit findings

- M121 (low): `ShortenPath` (tool-card paths) replaces the home byte prefix, so a sibling such as `/home/bobby` renders as `~bby`; pinned pi v1.5.0 render-utils uses `shortenHomePath`, which requires the path to be inside home.
