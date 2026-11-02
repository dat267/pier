# Reproduced audit findings

- M107 (low): session and tree pickers shorten a path to `~` whenever it shares the home directory's byte prefix, so a sibling such as `/home/bobby/...` renders as `~bby/...`. Pinned pi v1.5.0's `shortenHomePath` requires the path to be inside home (`isPathWithin`) and uses a relative path for the remainder.
