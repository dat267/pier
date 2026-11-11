//go:build linux

package interactive

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

// Probe with the developer's real session files (copied): same names, sizes and
// list ordering as the failing run.
func TestZZPTYDeleteRealSessions(t *testing.T) {
	bin := pierBinary(t)
	agentDir := t.TempDir()
	project := "/home/dat/repos/pier"
	if err := coding.NewProjectTrustStore(agentDir).Set(project, new(true)); err != nil {
		t.Fatal(err)
	}
	sessionDir := coding.DefaultSessionDir(project, agentDir)
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sources, err := filepath.Glob("/home/dat/.pi/agent/sessions/--home-dat-repos-pier--/*.jsonl")
	if err != nil || len(sources) < 2 {
		t.Skipf("no real sessions: %v", err)
	}
	for _, src := range sources {
		copyFile(t, src, filepath.Join(sessionDir, filepath.Base(src)))
	}
	// Start on the smallest session so the picker has others to delete.
	current := ""
	for _, src := range sources {
		info, err := os.Stat(src)
		if err != nil {
			t.Fatal(err)
		}
		if current == "" || info.Size() < 60_000_000 {
			current = filepath.Join(sessionDir, filepath.Base(src))
		}
	}
	t.Logf("current=%s", filepath.Base(current))

	master, slave, err := openPty()
	if err != nil {
		t.Skipf("pty: %v", err)
	}
	slaveFile := os.NewFile(uintptr(slave), "slave")
	masterFile := os.NewFile(uintptr(master), "master")
	cmd := exec.Command(bin, "--offline", "--session", current)
	cmd.Dir = project
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slaveFile, slaveFile, slaveFile
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "PI_CODING_AGENT_DIR="+agentDir, "GOTRACEBACK=all")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Skipf("start: %v", err)
	}
	defer func() { _ = cmd.Process.Kill() }()
	_ = slaveFile.Close()

	session := &ptySession{cmd: cmd, master: masterFile, done: make(chan struct{})}
	go func() {
		defer close(session.done)
		buf := make([]byte, 65536)
		for {
			n, err := masterFile.Read(buf)
			if n > 0 {
				session.mu.Lock()
				session.buf.Write(buf[:n])
				session.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	defer masterFile.Close()

	if !session.waitForOutput("ctrl+o", 120*time.Second) {
		t.Fatalf("did not start")
	}
	session.master.Write([]byte("/resume\r"))
	if !session.waitForOutput("Resume Session", 60*time.Second) {
		t.Fatalf("picker did not open")
	}
	// Wait until the picker stops loading, logging how long that took.
	start := time.Now()
	for time.Since(start) < 60*time.Second {
		if !strings.Contains(stripAnsiForProbe(session.output()), "Loading ...") {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Logf("picker finished loading after %v", time.Since(start))
	time.Sleep(300 * time.Millisecond)
	session.master.Write([]byte("\x04"))
	time.Sleep(800 * time.Millisecond)
	session.master.Write([]byte("\r"))
	time.Sleep(4 * time.Second)

	out := session.output()
	for _, needle := range []string{"Delete session?", "moved to trash", "Session deleted", "Failed to delete", "Cannot delete"} {
		if strings.Contains(out, needle) {
			t.Logf("saw %q", needle)
		}
	}
	remaining, _ := filepath.Glob(filepath.Join(sessionDir, "*.jsonl"))
	t.Logf("files left: %d of %d", len(remaining), len(sources))
}

func stripAnsiForProbe(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape:
			if (r >= 0x40 && r <= 0x7e) || r == ']' {
				if r == ']' {
					continue
				}
				inEscape = false
			}
		case r == '\r':
			continue
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}
