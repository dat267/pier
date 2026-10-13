//go:build windows

package interactive

import "github.com/dat267/pier/tui"

// suspendProcess is a no-op on Windows: handleCtrlZ returns before it (there is
// no SIGTSTP).
func suspendProcess(ui tui.TUI) {}
