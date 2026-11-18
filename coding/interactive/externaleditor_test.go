package interactive

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dat267/pier/tui"
)

// ---- Chat viewport ----

func (s *staticLines) Render(width int) []string { return s.lines }

func (s *staticLines) Invalidate() {}

// stubExternalEditorRunner replaces the external-editor runner for the rest of
// the test. Every test that reaches EditInExternalEditor must use it: the real
// runner spawns the developer's $EDITOR with this process's console and
// environment attached, and on Windows a command that cannot be executed makes
// the shell fall back to the temp prompt file's association — running the suite
// opened the developer's editor that way (AGENTS.md, TestMain).
func stubExternalEditorRunner(t *testing.T, stub func(*exec.Cmd) error) {
	t.Helper()
	saved := externalEditorRunner
	externalEditorRunner = stub
	t.Cleanup(func() { externalEditorRunner = saved })
}

// TestExternalEditor verifies the editor invocation. The runner is stubbed, so
// the test asserts the handover — the prompt file exists and holds the prompt
// when the editor runs, and the edited contents come back — without spawning
// anything.
func TestExternalEditor(t *testing.T) {
	commandLine := ""
	promptPath := ""
	handedOver := ""
	stubExternalEditorRunner(t, func(command *exec.Cmd) error {
		commandLine = strings.Join(command.Args, " ")
		promptPath = command.Args[len(command.Args)-1]
		data, err := os.ReadFile(promptPath)
		if err != nil {
			return err
		}
		handedOver = string(data)
		return os.WriteFile(promptPath, []byte("edited"), 0o644)
	})

	result := EditInExternalEditor(ExternalEditorOptions{Command: "my-editor --wait", Content: "original"})
	if result.Status != "complete" || result.Content != "edited" {
		t.Fatalf("result = %+v (handed over %q)", result, handedOver)
	}
	if handedOver != "original" {
		t.Errorf("editor received %q, want the prompt content", handedOver)
	}
	// The configured command, with its arguments, is what reaches the editor.
	if !strings.Contains(commandLine, "my-editor --wait") {
		t.Errorf("editor command = %q, want the configured command", commandLine)
	}
	// It edits a temp file, and that file does not outlive the call.
	if promptPath == "" {
		t.Fatal("no prompt file was passed to the editor")
	}
	if _, err := os.Stat(filepath.Dir(promptPath)); !os.IsNotExist(err) {
		t.Errorf("temp prompt directory %q outlived the call (%v)", filepath.Dir(promptPath), err)
	}

	// The editor's trailing newline is trimmed and a BOM stripped.
	stubExternalEditorRunner(t, func(command *exec.Cmd) error {
		return os.WriteFile(command.Args[len(command.Args)-1], []byte("\uFEFFcontent\n"), 0o644)
	})
	result = EditInExternalEditor(ExternalEditorOptions{Command: "my-editor", Content: "x"})
	if result.Status != "complete" || result.Content != "content" {
		t.Fatalf("result = %+v", result)
	}

	// A failing editor reports failure.
	stubExternalEditorRunner(t, func(*exec.Cmd) error { return errors.New("editor failed") })
	result = EditInExternalEditor(ExternalEditorOptions{Command: "my-editor", Content: "x"})
	if result.Status != "failed" {
		t.Fatalf("result = %+v", result)
	}
}

// TestExternalEditorDefaultRunnerFailsSafe pins the production default: with no
// stub in place, a command that cannot be executed reports failure instead of
// reaching the shell. This is the one external-editor case a test may run for
// real — a command that does not exist opens nothing — and it is what stops the
// seam from silently disabling the feature.
func TestExternalEditorDefaultRunnerFailsSafe(t *testing.T) {
	result := EditInExternalEditor(ExternalEditorOptions{
		Command: filepath.Join(t.TempDir(), "no-such-editor"),
		Content: "x",
	})
	if result.Status != "failed" {
		t.Fatalf("result = %+v, want failed", result)
	}
}

// TestAmbientEditorEnvIsPinned guards TestMain's pin from being dropped, which
// would put the developer's real editor back in reach of a test run.
func TestAmbientEditorEnvIsPinned(t *testing.T) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if got := os.Getenv(key); !strings.Contains(got, "pier-test-no-external-editor") {
			t.Errorf("%s = %q, want the pinned no-op path (see TestMain)", key, got)
		}
	}
}

// ---- Catalog refresh ----

type fakeThemeUI struct {
	mu             sync.Mutex
	invalidates    int
	renders        int
	notifications  bool
	listener       func(TerminalTheme)
	bgListener     func(tui.RgbColor)
	bgProbes       int
	systemProbes   int
	terminalColors tui.TerminalColors
	// queryGate, when non-nil, holds QueryTerminalColors until it is closed;
	// queryStarted is closed as soon as the query begins.
	queryGate    chan struct{}
	queryStarted chan struct{}
}

func (f *fakeThemeUI) Invalidate() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.invalidates++
}

func (f *fakeThemeUI) RequestRender() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renders++
}

func (f *fakeThemeUI) SetTerminalColorSchemeNotifications(enabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notifications = enabled
}

func (f *fakeThemeUI) OnTerminalColorSchemeChange(listener func(theme TerminalTheme)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listener = listener
	return func() {}
}

func (f *fakeThemeUI) OnTerminalBackgroundColorChange(listener func(color tui.RgbColor)) func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bgListener = listener
	return func() {}
}

func (f *fakeThemeUI) RequestTerminalBackgroundColor() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bgProbes++
}

func (f *fakeThemeUI) QueryTerminalColors(timeoutMS int, onLateReply func(tui.TerminalColors)) tui.TerminalColors {
	f.mu.Lock()
	f.systemProbes++
	gate := f.queryGate
	started := f.queryStarted
	colors := f.terminalColors
	f.mu.Unlock()
	if started != nil {
		close(started)
	}
	if gate != nil {
		<-gate
	}
	return colors
}

func (f *fakeThemeUI) BeginTerminalColors() (tui.TerminalColorWait, bool) {
	f.mu.Lock()
	f.systemProbes++
	gate := f.queryGate
	started := f.queryStarted
	colors := f.terminalColors
	f.mu.Unlock()
	if started != nil {
		close(started)
	}
	return func(timeoutMS int, onLateReply func(tui.TerminalColors)) tui.TerminalColors {
		if gate != nil {
			<-gate
		}
		return colors
	}, true
}

func (f *fakeThemeSettings) GetThemeSetting() *string { return f.setting }

func (f *fakeThemeSettings) SetTheme(theme string) {
	value := theme
	f.setting = &value
	f.setCalls = append(f.setCalls, theme)
}

func (f *fakeThemeSettings) Flush() { f.flushes++ }

func (f *fakeDetector) QueryTerminalBackgroundColor(timeoutMs int) (RgbColor, bool) {
	f.rgbCalls++
	return f.rgb, f.hasRGB
}

func (f *fakeDetector) QueryTerminalColorScheme(timeoutMs int) (TerminalTheme, bool) {
	f.schemeCalls++
	return f.scheme, f.hasScheme
}
