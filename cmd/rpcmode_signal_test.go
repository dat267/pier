package cmd

import (
	"context"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// pi v1.5.2 (b4afdddf), modes/rpc/rpc-mode.ts registerSignalHandlers: the first
// signal starts the shutdown, and a repeated one takes the `signalCount > 1`
// branch and exits at once, because a shutdown can wait on work such as an MCP
// token refresh. SIGHUP asks for 129 and SIGTERM for 143.
func TestRPCSignalWatcherForcesARepeatedSignal(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for signal handoff goroutines")
	}
	signals := make(chan os.Signal, 2)
	stop := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	var cancelled atomic.Int32
	forced := make(chan int, 2)
	done, exitCode := watchRPCSignals(signals, func() {
		cancelled.Add(1)
		cancel()
	}, func(code int) { forced <- code }, stop)

	signals <- syscall.SIGTERM
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the first signal did not start the shutdown")
	}
	// A repeated signal during the shutdown forces the exit with the same code.
	signals <- syscall.SIGTERM
	select {
	case code := <-forced:
		if code != 143 {
			t.Fatalf("forced exit code = %d, want 143", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the repeated signal did not force the exit")
	}
	close(stop)
	<-done
	if code := exitCode(); code != 143 {
		t.Fatalf("exit code = %d, want 143", code)
	}
	if got := cancelled.Load(); got != 1 {
		t.Fatalf("cancel called %d times, want once", got)
	}
}

func TestRPCSignalWatcherMapsHangupTo129(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for signal handoff goroutines")
	}
	signals := make(chan os.Signal, 2)
	stop := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done, exitCode := watchRPCSignals(signals, cancel, func(int) {}, stop)
	signals <- syscall.SIGHUP
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the hangup did not start the shutdown")
	}
	close(stop)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the signal did not finish the watcher")
	}
	if code := exitCode(); code != 129 {
		t.Fatalf("exit code = %d, want 129", code)
	}
}
