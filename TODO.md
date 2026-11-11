# Reproduced audit findings

## A504 — medium: the editor's autocomplete provider runs on the owner loop

- Category and affected paths: responsiveness in `tui/editor.go`
  `startAutocompleteRequestLocked`, reached from `(*tui.Renderer).
  HandleTerminalInput`; the provider is `coding/interactive`'s
  `CombinedAutocompleteProvider`, whose file suggestions walk the tree and may
  exec `fd` (`walkDirectoryWithFd`).
- Scenario: type `@` (or a path fragment) in the prompt of a large repository.
  Expected: the suggestion walk runs off the owner loop and its result is posted
  back, as the session-file work does (D441, D442). Actual: the editor hands the
  request to its host's `Post`, which is `editorHostAdapter.Post` →
  `ui.Post` → the owner loop, so the walk and any `fd` subprocess block the loop.
  Only a debounce limits it.
- Evidence: `tui/editor.go:2157-2174` posts `startAutocompleteRequestLocked` when
  the host implements `EditorPoster`; `coding/interactive/app.go:1622` is that
  implementation, and it posts to the UI. The synchronous branch is the test
  fallback, so the distinction is production-only. Found once A493's
  pointer-receiver descent let the gate see `(*tui.Renderer).HandleTerminalInput`.
- Authority: `AGENTS.md` UI-ownership rule; `internal/uiblock`'s invariant.
- Root cause: `Post` was chosen as "owner-loop handoff" so editor state stays on
  one goroutine; the *provider call* inherited that, though it only needs copies
  of the buffer and cursor.
- Repair plan: split the request from the apply: hand the provider call to the
  autocomplete queue with copies of the lines, cursor and prefix, and post the
  result back to the loop via the existing token check (`autocompleteStartToken`).
  The editor already discards stale results, so the handoff has a seam.
- Verification owed: a test that the provider call does not run on the posting
  goroutine (fake queue + handshake), plus the autocomplete suites and `just check`.
