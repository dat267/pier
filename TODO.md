# Reproduced audit findings

- M100 (low): PageUp/PageDown do not move the selection in select lists or settings lists, and the editor does not forward them to an open autocomplete menu. Pinned pi v1.5.0 adds `getSelectPageIndex` (page keys move by `maxVisible`, clamped to the list bounds) and forwards `tui.select.pageUp`/`pageDown` to the autocomplete list, so `/model`, settings and completion menus page through long lists.
