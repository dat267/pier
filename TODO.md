# Reproduced audit findings

- M109 (low): the user-message picker ignores PageUp/PageDown and does not capture fullscreen viewport keys, unlike pinned pi v1.5.0's user-message-selector (`getSelectPageIndex`, `capturesViewportInput`).
