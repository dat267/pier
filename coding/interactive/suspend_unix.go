//go:build !windows

package interactive

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/dat267/pier/tui"
)

// suspendProcess stops the process with SIGTSTP and restarts the UI when it is
// resumed (upstream handleCtrlZ's SIGCONT handler). The process keeps a live
// signal subscription while stopped, so fg resumes it and the UI repaints.
func suspendProcess(ui tui.TUI) {
	resumed := make(chan os.Signal, 1)
	signal.Notify(resumed, syscall.SIGCONT)
	go func() {
		<-resumed
		signal.Stop(resumed)
		if ui != nil {
			ui.Start()
			ui.RequestRender(true)
		}
	}()
	// pid 0 addresses the whole process group, matching upstream's
	// process.kill(0, "SIGTSTP").
	if err := syscall.Kill(0, syscall.SIGTSTP); err != nil {
		signal.Stop(resumed)
	}
}
