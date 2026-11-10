// Package leakcheck fails a test binary that leaves goroutines running.
//
// A package's TestMain calls Run instead of m.Run. Goroutines alive before the
// tests started are ignored; any other goroutine still running once the tests
// finish (after a grace period for ones that are already winding down) is a
// leak: a worker a Dispose did not stop, a watcher nobody closed, a reader
// blocked on a pipe nobody will write. Each is reported with its stack.
//
// Background runtime and standard-library goroutines that outlive any one test
// by design (signal delivery, idle HTTP keep-alive connections) are ignored.
package leakcheck

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// grace is how long Run waits for goroutines that are already ending.
var grace = 3 * time.Second

// ignoredFrames are functions whose goroutines are not leaks. Each runs for
// the life of the process on purpose, or belongs to an idle connection pool
// the standard library owns.
var ignoredFrames = []string{
	"os/signal.signal_recv",
	"os/signal.loop",
	"net/http.(*persistConn).readLoop",
	"net/http.(*persistConn).writeLoop",
	"created by net/http/internal/http2.(*Transport).newClientConn",
	"testing.(*M).startAlarm",
	"testing.runFuzzTests",
	"runtime.ensureSigM",
	"runtime/trace.Start",
	// The MCP stdio transport's signal forwarder lives for the process.
	"pier/mcp.installExitHook.func1",
}

// Run runs the tests and returns their exit code, or 1 when they passed but
// left goroutines running. Set PIER_LEAKCHECK=off to skip the check, or
// PIER_LEAKCHECK=warn to report leaks without failing.
func Run(m *testing.M) int {
	mode := os.Getenv("PIER_LEAKCHECK")
	before := goroutineIDs(stacks())
	code := m.Run()
	if code != 0 || mode == "off" {
		return code
	}
	leaked := waitForLeaks(before, grace)
	if len(leaked) == 0 {
		return code
	}
	fmt.Fprintf(os.Stderr, "leakcheck: %d goroutine(s) still running after the tests:\n\n%s\n", len(leaked), strings.Join(leaked, "\n\n"))
	if mode == "warn" {
		return code
	}
	return 1
}

// waitForLeaks returns the stacks of goroutines not in before that are still
// running when the grace period ends (or nil as soon as none are).
func waitForLeaks(before map[string]bool, within time.Duration) []string {
	deadline := time.Now().Add(within)
	for {
		leaked := leaks(before)
		if len(leaked) == 0 || time.Now().After(deadline) {
			return leaked
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func leaks(before map[string]bool) []string {
	var out []string
	for _, stack := range splitStacks(stacks()) {
		id := goroutineID(stack)
		if before[id] || isIgnored(stack) || isCurrent(stack) {
			continue
		}
		out = append(out, stack)
	}
	return out
}

func stacks() []byte {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}

func splitStacks(all []byte) []string {
	var out []string
	for _, block := range bytes.Split(all, []byte("\n\n")) {
		if text := strings.TrimSpace(string(block)); strings.HasPrefix(text, "goroutine ") {
			out = append(out, text)
		}
	}
	return out
}

func goroutineIDs(all []byte) map[string]bool {
	ids := map[string]bool{}
	for _, stack := range splitStacks(all) {
		ids[goroutineID(stack)] = true
	}
	return ids
}

// goroutineID is the number in a stack's "goroutine 12 [running]:" header.
func goroutineID(stack string) string {
	header, _, _ := strings.Cut(stack, " [")
	return strings.TrimPrefix(header, "goroutine ")
}

// isCurrent reports the goroutine running the check itself.
func isCurrent(stack string) bool {
	return strings.Contains(stack, "pier/internal/leakcheck.leaks")
}

func isIgnored(stack string) bool {
	for _, frame := range ignoredFrames {
		if strings.Contains(stack, frame) {
			return true
		}
	}
	return false
}
