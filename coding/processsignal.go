package coding

import "syscall"

// ProcessSignalName renders a process signal with Node's names, which upstream
// prints for a child that exited by signal (rpc-client.ts and
// external-editor.ts). Go's syscall.Signal.String is platform prose
// ("terminated"), so the common signals are mapped explicitly; anything else
// falls back to Go's name.
func ProcessSignalName(signal syscall.Signal) string {
	// Node names every signal SIG<NAME>. Go's Signal.String is prose ("terminated",
	// "user defined signal 1"), so the name is assembled from the platform's own
	// constants; the per-platform sets live in processsignal_unix.go and
	// processsignal_other.go because several (SIGUSR1, SIGCHLD, SIGPWR) do not exist
	// everywhere the port cross-builds for.
	if name, ok := platformSignalNames[signal]; ok {
		return name
	}
	return signal.String()
}
