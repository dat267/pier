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
	for _, want := range []string{"/ws", "/tmp", "/var/tmp", "/dev", "/proc", "/sys",
		home + "/go", home + "/.bun", home + "/.gradle", home + "/.m2",
		home + "/.sdkman", home + "/.local/share/uv", home + "/.local/share/fnm",
		home + "/.local/share/containers", home + "/Android/Sdk", home + "/.pi"} {
		if !contains(roots, want) {
			t.Fatalf("writable roots missing %q: %v", want, roots)
		}
	}
	ro := (&Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendLandlock, workspace: "/ws", home: home}).Policy().Writable
	if len(ro) != 1 || ro[0] != "/dev/null" {
		t.Fatalf("read-only roots = %v (want [/dev/null])", ro)
	}
}

// TestSandboxPolicyIsSingleSourceOfTruth pins the one policy all three surfaces
// derive from: the kernel launcher argv, the in-process write/edit check and the
// prompt note agree on the writable set, including the podman runtime dirs.
func TestSandboxPolicyIsSingleSourceOfTruth(t *testing.T) {
	runtime := t.TempDir()
	containers := filepath.Join(runtime, "containers")
	libpod := filepath.Join(runtime, "libpod")
	ws := t.TempDir()
	s := &Sandbox{
		mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock,
		workspace: ws, home: "/home/u",
		runtimeDirs: []string{containers, libpod},
	}
	policy := s.Policy()
	for _, want := range append([]string{ws, "/tmp", "/home/u/.pi"}, containers, libpod) {
		if !contains(policy.Writable, want) {
			t.Fatalf("policy writable roots missing %q: %v", want, policy.Writable)
		}
	}
	// The in-process check must agree with the kernel grant: a write/edit target
	// under the runtime dirs is allowed, not just bash.
	if err := policy.CheckPath(filepath.Join(containers, "storage", "db.sql")); err != nil {
		t.Fatalf("policy CheckPath denied a kernel-granted path: %v", err)
	}
	// The launcher argv carries every writable root.
	argv, err := policy.WrapArgv([]string{"bash", "-c", "true"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, "\x00")
	for _, want := range policy.Writable {
		if !strings.Contains(joined, want) {
			t.Fatalf("launcher argv missing writable root %q: %v", want, argv)
		}
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

func TestSandboxPolicyPromptNote(t *testing.T) {
	// full-access names the disabled state.
	full := (&Sandbox{mode: SandboxModeFullAccess, backend: SandboxBackendNone}).Policy().PromptNote()
	if !strings.Contains(full, "DISABLED") {
		t.Fatalf("full-access note = %q", full)
	}
	// read-only advertises only what it can write, not the workspace allowlist.
	ro := (&Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendLandlock, workspace: "/ws", home: "/home/u"}).Policy().PromptNote()
	if !strings.Contains(ro, "mode: read-only") || !strings.Contains(ro, "/dev/null") {
		t.Fatalf("read-only note = %q", ro)
	}
	if strings.Contains(ro, "/ws") || strings.Contains(ro, "~/.pi") {
		t.Fatalf("read-only note advertised a non-writable path: %q", ro)
	}
	// workspace-write derives its prose from the same writable set, including
	// the podman runtime dirs.
	runtime := t.TempDir()
	containers := filepath.Join(runtime, "containers")
	libpod := filepath.Join(runtime, "libpod")
	ww := (&Sandbox{
		mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock,
		workspace: "/ws", home: "/home/u", runtimeDirs: []string{containers, libpod},
	}).Policy().PromptNote()
	for _, want := range []string{"/ws", "~/.pi", containers, libpod} {
		if !strings.Contains(ww, want) {
			t.Fatalf("workspace-write note missing %q: %q", want, ww)
		}
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
	// The package-manager caches reach the launcher argv, not just the list.
	joined := strings.Join(wrapped, "\x00")
	for _, want := range []string{"/home/u/.bun", "/home/u/.gradle", "/home/u/.m2", "/home/u/.sdkman", "/home/u/.local/share/uv", "/home/u/.local/share/fnm", "/home/u/.local/share/containers", "/home/u/Android/Sdk"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("launcher argv missing %q: %v", want, wrapped)
		}
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

func TestSandboxRuntimeDirRoots(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1234")
	roots := SandboxRuntimeDirRoots()
	want := []string{"/run/user/1234/containers", "/run/user/1234/libpod"}
	if strings.Join(roots, ",") != strings.Join(want, ",") {
		t.Fatalf("roots = %v (want %v)", roots, want)
	}
}

// TestSandboxWrapArgvCreatesPodmanRuntimeDirs covers the podman grant: the two
// runtime roots are created (a confined process cannot mkdir under
// XDG_RUNTIME_DIR) and reach the launcher argv, while the parent directory is
// never granted.
func TestSandboxWrapArgvCreatesPodmanRuntimeDirs(t *testing.T) {
	runtime := t.TempDir()
	containers := filepath.Join(runtime, "containers")
	libpod := filepath.Join(runtime, "libpod")
	s := &Sandbox{
		mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock,
		workspace: t.TempDir(), home: "/home/u",
		runtimeDirs: []string{containers, libpod},
	}
	argv, err := s.WrapArgv([]string{"bash", "-c", "true"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(argv, "\x00")
	for _, want := range []string{containers, libpod} {
		if !strings.Contains(joined, want) {
			t.Fatalf("launcher argv missing %q: %v", want, argv)
		}
		if info, err := os.Stat(want); err != nil || !info.IsDir() {
			t.Fatalf("runtime dir %q was not created: %v", want, err)
		}
	}
	// The parent itself must never be granted, or the session IPC sockets
	// become replaceable.
	for _, arg := range argv {
		if arg == runtime {
			t.Fatalf("the XDG_RUNTIME_DIR parent was granted: %v", argv)
		}
	}
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

// TestBashToolSandboxConfinesCommand drives the real Landlock path through the
// bash tool: os.Executable() is this test binary, whose TestMain dispatches the
// __sandbox-exec subcommand. The missing home exercises the skip for
// allowlist entries that do not exist.
func TestBashToolSandboxConfinesCommand(t *testing.T) {
	if DetectSandboxBackend() != SandboxBackendLandlock {
		t.Skip("no Landlock backend on this host")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	ws := t.TempDir()
	sandbox := &Sandbox{mode: SandboxModeWorkspaceWrite, backend: SandboxBackendLandlock, workspace: ws, home: filepath.Join(ws, "no-such-home")}
	tool := CreateBashTool(ws, &BashToolOptions{Sandbox: sandbox})
	result, err := tool.Execute("c", json.RawMessage(`{"command":"echo hi > ok.txt && cat ok.txt"}`), context.Background(), nil)
	if err != nil {
		t.Fatalf("in-workspace write through the sandbox failed: %v", err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].(ai.TextContent).Text, "hi") {
		t.Fatalf("result = %+v", result.Content)
	}
	if _, err := tool.Execute("c", json.RawMessage(`{"command":"echo no > /etc/pier-sbx-bash.txt"}`), context.Background(), nil); err == nil {
		t.Fatal("out-of-allowlist write through the sandbox must be denied")
	}
}

// TestBashToolSandboxReadOnlyDeniesWrites drives read-only mode, whose only
// writable root is the /dev/null file: reads work, writes anywhere else fail.
func TestBashToolSandboxReadOnlyDeniesWrites(t *testing.T) {
	if DetectSandboxBackend() != SandboxBackendLandlock {
		t.Skip("no Landlock backend on this host")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh on PATH")
	}
	ws := t.TempDir()
	sandbox := &Sandbox{mode: SandboxModeReadOnly, backend: SandboxBackendLandlock, workspace: ws, home: filepath.Join(ws, "no-such-home")}
	tool := CreateBashTool(ws, &BashToolOptions{Sandbox: sandbox})
	result, err := tool.Execute("c", json.RawMessage(`{"command":"cat /etc/hostname >/dev/null && echo read-ok"}`), context.Background(), nil)
	if err != nil {
		t.Fatalf("read-only read failed: %v", err)
	}
	if len(result.Content) == 0 || !strings.Contains(result.Content[0].(ai.TextContent).Text, "read-ok") {
		t.Fatalf("result = %+v", result.Content)
	}
	if _, err := tool.Execute("c", json.RawMessage(`{"command":"echo x > /tmp/pier-sbx-ro-test.txt"}`), context.Background(), nil); err == nil {
		t.Fatal("read-only must deny writes")
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
