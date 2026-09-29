package coding

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

func TestSandboxModeCodesRoundTrip(t *testing.T) {
	cases := map[string]SandboxMode{
		"RO": "read-only", "ro": "read-only", "read-only": "read-only",
		"WW": "workspace-write", "ww": "workspace-write", "workspace-write": "workspace-write",
		"FA": "full-access", "fa": "full-access", "full-access": "full-access",
	}
	for arg, want := range cases {
		got, ok := SandboxModeFromCode(arg)
		if !ok || got != want {
			t.Fatalf("SandboxModeFromCode(%q) = %q, %v (want %q)", arg, got, ok, want)
		}
	}
	if _, ok := SandboxModeFromCode("nope"); ok {
		t.Fatal("SandboxModeFromCode(nope) must not match")
	}
	if got := SandboxModeFullAccess.Code(); got != "FA" {
		t.Fatalf("full-access code = %q", got)
	}
}

func TestDefaultSandboxMode(t *testing.T) {
	if got := DefaultSandboxMode(SandboxBackendNone); got != SandboxModeReadOnly {
		t.Fatalf("no-backend default = %q (want read-only)", got)
	}
	if got := DefaultSandboxMode(SandboxBackendLandlock); got != SandboxModeWorkspaceWrite {
		t.Fatalf("landlock default = %q (want workspace-write)", got)
	}
}

func TestSandboxSetModeFallsBackToReadOnly(t *testing.T) {
	s := &Sandbox{backend: SandboxBackendNone, workspace: "/ws"}
	mode, warning := s.SetMode(SandboxModeWorkspaceWrite)
	if mode != SandboxModeReadOnly || warning == "" {
		t.Fatalf("SetMode(workspace-write) = %q, %q (want read-only + warning)", mode, warning)
	}
	if s.Mode() != SandboxModeReadOnly {
		t.Fatalf("active mode = %q", s.Mode())
	}
	// full-access is an explicit toggle and always applies.
	if mode, warning := s.SetMode(SandboxModeFullAccess); mode != SandboxModeFullAccess || warning != "" {
		t.Fatalf("SetMode(full-access) = %q, %q", mode, warning)
	}
}

func TestSandboxNilIsFullAccess(t *testing.T) {
	var s *Sandbox
	if s.Mode() != SandboxModeFullAccess {
		t.Fatalf("nil sandbox mode = %q", s.Mode())
	}
	argv, err := s.WrapArgv([]string{"bash", "-c", "true"})
	if err != nil || len(argv) != 3 {
		t.Fatalf("nil WrapArgv = %v, %v", argv, err)
	}
	if err := s.CheckPath("/etc/passwd"); err != nil {
		t.Fatalf("nil CheckPath = %v", err)
	}
}

func TestSandboxWritableRoots(t *testing.T) {
	home := "/home/u"
	roots := SandboxWritableRoots("/ws", home)
	for _, want := range []string{"/ws", "/tmp", "/var/tmp", "/dev", "/proc", "/sys", home + "/go", home + "/.pi"} {
		if !contains(roots, want) {
			t.Fatalf("writable roots missing %q: %v", want, roots)
		}
	}
	ro := (&Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendLandlock, workspace: "/ws", home: home}).WritableRoots()
	if len(ro) != 1 || ro[0] != "/dev/null" {
		t.Fatalf("read-only roots = %v (want [/dev/null])", ro)
	}
}

func TestInspectSandboxPath(t *testing.T) {
	ws := t.TempDir()
	home := t.TempDir()
	allow := SandboxWritableRoots(ws, home)
	if reason := InspectSandboxPath(filepath.Join(ws, "a", "b.txt"), ws, allow); reason != "" {
		t.Fatalf("inside workspace blocked: %q", reason)
	}
	if reason := InspectSandboxPath("relative.txt", ws, allow); reason != "" {
		t.Fatalf("workspace-relative path blocked: %q", reason)
	}
	outside := "/etc/pier-sandbox-denied/x.txt" // not in any allowlist
	if reason := InspectSandboxPath(outside, ws, allow); reason == "" {
		t.Fatal("outside path must be blocked")
	}
	// A symlink inside the workspace pointing outside must not escape.
	if err := os.Symlink("/etc", filepath.Join(ws, "link")); err == nil {
		if reason := InspectSandboxPath(filepath.Join(ws, "link", "x.txt"), ws, allow); reason == "" {
			t.Fatal("symlink escape must be blocked")
		}
	}
}

func TestCheckPathPerMode(t *testing.T) {
	ws := t.TempDir()
	home := t.TempDir()
	outside := "/etc/pier-sandbox-denied/x.txt" // not in any allowlist
	ro := &Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendLandlock, workspace: ws, home: home}
	if err := ro.CheckPath(outside); err == nil {
		t.Fatal("read-only must block writes")
	}
	ww := &Sandbox{mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock, workspace: ws, home: home}
	if err := ww.CheckPath(filepath.Join(ws, "ok.txt")); err != nil {
		t.Fatalf("workspace-write blocked an in-workspace path: %v", err)
	}
	if err := ww.CheckPath(outside); err == nil {
		t.Fatal("workspace-write must block outside paths")
	}
	if err := (&Sandbox{mode: SandboxModeFullAccess}).CheckPath(outside); err != nil {
		t.Fatalf("full-access blocked: %v", err)
	}
}

func TestSandboxPromptNote(t *testing.T) {
	note := SandboxPromptNote(SandboxModeReadOnly, SandboxBackendLandlock, "/ws", "/home/u")
	if !strings.Contains(note, "mode: read-only") || !strings.Contains(note, "/ws") {
		t.Fatalf("read-only note = %q", note)
	}
	full := SandboxPromptNote(SandboxModeFullAccess, SandboxBackendNone, "/ws", "/home/u")
	if !strings.Contains(full, "DISABLED") {
		t.Fatalf("full-access note = %q", full)
	}
}

func TestSandboxWrapArgv(t *testing.T) {
	command := []string{"bash", "-c", "true"}
	// full-access returns the argv unchanged.
	full, err := (&Sandbox{mode: SandboxModeFullAccess}).WrapArgv(command)
	if err != nil || strings.Join(full, " ") != strings.Join(command, " ") {
		t.Fatalf("full-access WrapArgv = %v, %v", full, err)
	}
	// A confined mode with no backend fails closed.
	ww := &Sandbox{mode: SandboxModeWorkspaceWrite, backend: SandboxBackendNone, workspace: "/ws"}
	if _, err := ww.WrapArgv(command); err == nil {
		t.Fatal("workspace-write without a backend must fail closed")
	}
	// With a backend, the argv is launcher-wrapped.
	ok := &Sandbox{mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock, workspace: "/ws", home: "/home/u"}
	wrapped, err := ok.WrapArgv(command)
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapped) < 2 || wrapped[1] != SandboxLauncherSubcommand {
		t.Fatalf("wrapped argv = %v", wrapped)
	}
	if wrapped[len(wrapped)-len(command)-1] != "--" {
		t.Fatalf("wrapped argv missing separator: %v", wrapped)
	}
}

// TestSandboxLauncherEnforcesReadOnly drives the real Linux Landlock path
// through a re-exec of the test binary as `pier __sandbox-exec`.
func TestSandboxLauncherEnforcesReadOnly(t *testing.T) {
	if DetectSandboxBackend() != SandboxBackendLandlock {
		t.Skip("no Landlock backend on this host")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	allow := t.TempDir()
	deniedDir := t.TempDir()
	allowedFile := filepath.Join(allow, "ok.txt")
	deniedFile := filepath.Join(deniedDir, "denied.txt")
	script := "echo ok > " + allowedFile + "; echo no > " + deniedFile

	cmd := exec.Command(os.Args[0], "-test.run=TestSandboxLauncherHelper", "--",
		"--allow", allow, "--", "sh", "-c", script)
	cmd.Env = append(os.Environ(), "GO_WANT_SANDBOX_HELPER=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("launcher must fail when the confined command writes outside the allowlist; output: %s", out)
	}
	if data, readErr := os.ReadFile(allowedFile); readErr != nil || strings.TrimSpace(string(data)) != "ok" {
		t.Fatalf("allowed write did not succeed: %q, %v", data, readErr)
	}
	if _, statErr := os.Stat(deniedFile); statErr == nil {
		t.Fatal("confined write outside the allowlist must be denied")
	}
}

func TestSandboxLauncherHelper(t *testing.T) {
	if os.Getenv("GO_WANT_SANDBOX_HELPER") != "1" {
		return
	}
	index := 0
	for i, arg := range os.Args {
		if arg == "--" {
			index = i + 1
			break
		}
	}
	os.Exit(RunSandboxLauncher(append([]string{SandboxLauncherSubcommand}, os.Args[index:]...)))
}

func TestBashToolSandboxFailsClosedWithoutBackend(t *testing.T) {
	ws := t.TempDir()
	tool := CreateBashTool(ws, &BashToolOptions{Sandbox: &Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendNone, workspace: ws}})
	if _, err := tool.Execute("c", json.RawMessage(`{"command":"echo hi"}`), context.Background(), nil); err == nil {
		t.Fatal("read-only without a backend must fail closed")
	}
}

func TestBashToolFullAccessRuns(t *testing.T) {
	tool := CreateBashTool(t.TempDir(), &BashToolOptions{Sandbox: &Sandbox{mode: SandboxModeFullAccess}})
	result, err := tool.Execute("c", json.RawMessage(`{"command":"echo hi"}`), context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].(ai.TextContent).Text, "hi") {
		t.Fatalf("full-access bash result = %+v", result.Content)
	}
}

func TestWriteToolSandboxGating(t *testing.T) {
	ws := t.TempDir()
	ww := &Sandbox{mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock, workspace: ws, home: "/home/u"}
	tool := CreateWriteTool(ws, ww)
	if _, err := tool.Execute("c", json.RawMessage(`{"path":"ok.txt","content":"x"}`), context.Background(), nil); err != nil {
		t.Fatalf("in-workspace write blocked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ws, "ok.txt")); err != nil {
		t.Fatal(err)
	}
	outside := CreateWriteTool(ws, ww)
	if _, err := outside.Execute("c", json.RawMessage(`{"path":"/etc/pier-sandbox-test.txt","content":"x"}`), context.Background(), nil); err == nil {
		t.Fatal("outside write must be blocked")
	}
	ro := CreateWriteTool(ws, &Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendLandlock, workspace: ws, home: "/home/u"})
	if _, err := ro.Execute("c", json.RawMessage(`{"path":"ok2.txt","content":"x"}`), context.Background(), nil); err == nil {
		t.Fatal("read-only write must be blocked")
	}
}

func TestSessionSandboxModeAndPromptSection(t *testing.T) {
	session := newControlSession(t, nil, nil)
	session.control.Sandbox = &Sandbox{mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock, workspace: session.Cwd, home: "/home/u"}
	if session.SandboxMode() != SandboxModeWorkspaceWrite || session.SandboxBackend() != SandboxBackendLandlock {
		t.Fatalf("mode = %q backend = %q", session.SandboxMode(), session.SandboxBackend())
	}
	options, ok := session.resolvedSystemPromptOptions()
	if !ok {
		t.Fatal("no prompt options")
	}
	if note := options.Sections["sandbox"]; !strings.Contains(note, "mode: workspace-write") {
		t.Fatalf("sandbox prompt section = %q", note)
	}
	effective, warning := session.SetSandboxMode(SandboxModeReadOnly)
	if effective != SandboxModeReadOnly || warning != "" {
		t.Fatalf("SetSandboxMode = %q, %q", effective, warning)
	}
	options, _ = session.resolvedSystemPromptOptions()
	if note := options.Sections["sandbox"]; !strings.Contains(note, "mode: read-only") {
		t.Fatalf("sandbox prompt section after switch = %q", note)
	}
}
