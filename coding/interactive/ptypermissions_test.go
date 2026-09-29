//go:build linux

package interactive

import (
	"strings"
	"testing"
	"time"
)

// TestPTYPermissionsCommand drives the real `/permissions` command through a
// pty: the bare form reports the default (workspace-write on a Landlock host),
// a code switches the mode, and the footer follows.
func TestPTYPermissionsCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	session := startPier(t)
	if !session.waitForOutput("v0.0.0", 10*time.Second) {
		t.Fatalf("pier did not start:\n%.600s", stripAnsiForLog(session.output()))
	}

	// Bare /permissions reports the active mode. On a Landlock host the default
	// is workspace-write, whose detail names the backend.
	session.typeAndSubmit("/permissions")
	if !session.waitForWrappedOutput("kernel-enforced workspace (Landlock)", 5*time.Second) {
		t.Fatalf("bare /permissions did not report workspace-write:\n%.800s", stripAnsiForLog(session.output()))
	}

	// RO switches to read-only; the detail is distinctive.
	session.typeAndSubmit("/permissions RO")
	if !session.waitForWrappedOutput("read-only (writes denied", 5*time.Second) {
		t.Fatalf("/permissions RO did not take:\n%.800s", stripAnsiForLog(session.output()))
	}

	// FA switches to full access.
	session.typeAndSubmit("/permissions FA")
	if !session.waitForWrappedOutput("unrestricted (all writes allowed)", 5*time.Second) {
		t.Fatalf("/permissions FA did not take:\n%.800s", stripAnsiForLog(session.output()))
	}

	// An unknown mode is rejected, not silently accepted.
	session.typeAndSubmit("/permissions XX")
	if !session.waitForWrappedOutput("Unknown permission mode", 5*time.Second) {
		t.Fatalf("/permissions XX was not rejected:\n%.800s", stripAnsiForLog(session.output()))
	}

	if strings.Contains(unwrapped(session.output()), "panic") {
		t.Fatalf("the app panicked:\n%.800s", stripAnsiForLog(session.output()))
	}
}
