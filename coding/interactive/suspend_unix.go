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

// installSIGTSTPHandler installs handler for SIGTSTP and returns the cleanup.
// The suspend raises SIGTSTP again, and that one has to reach the default action
// so the process really stops; the channel is armed again once it is resumed. A
// test seam, because the real handler stops the process.
var installSuspendSignalHandler = func(handler func()) func() {
	channel := make(chan os.Signal, 1)
	signal.Notify(channel, syscall.SIGTSTP)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-channel:
				signal.Reset(syscall.SIGTSTP)
				handler()
				signal.Notify(channel, syscall.SIGTSTP)
			}
		}
	}()
	return func() {
		signal.Stop(channel)
		close(done)
	}
}
