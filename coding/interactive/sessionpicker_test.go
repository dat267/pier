package interactive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// scriptedTerminal records the renderer's input pump so tests can deliver
// keystrokes after the picker settles its loads.
type scriptedTerminal struct {
	fakeRendererTerminal
	onInput func(string)
	started chan struct{}
}

func (s *scriptedTerminal) Start(onInput func(string), onResize func()) {
	s.onInput = onInput
	close(s.started)
}

func TestSelectSessionSelects(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	sessions := []coding.SessionInfo{
		{Path: "/s/a.jsonl", FirstMessage: "a", Modified: time.Now()},
		{Path: "/s/b.jsonl", FirstMessage: "b", Modified: time.Now().Add(-time.Hour)},
	}
	loader := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
		return sessions, nil
	}

	terminal := &scriptedTerminal{width: 80, height: 24, started: make(chan struct{})}
	type result struct {
		path string
	}
	done := make(chan result, 1)
	go func() {
		path := SelectSession(SelectSessionOptions{
			CurrentLoader: loader,
			AllLoader:     loader,
			Terminal:      terminal,
		})
		done <- result{path: path}
	}()

	time.Sleep(200 * time.Millisecond)
	select {
	case <-terminal.started:
	case <-time.After(5 * time.Second):
		t.Fatal("picker never started the terminal input pump")
	}
	time.Sleep(200 * time.Millisecond) // let the loads settle and render
	terminal.onInput("\r")

	select {
	case got := <-done:
		if got.path != "/s/a.jsonl" {
			t.Fatalf("selected %q, want /s/a.jsonl", got.path)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SelectSession did not return after selection")
	}
}

func TestSelectSessionCancel(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	loader := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
		return nil, nil
	}

	terminal := &scriptedTerminal{width: 80, height: 24, started: make(chan struct{})}
	done := make(chan string, 1)
	go func() {
		done <- SelectSession(SelectSessionOptions{
			CurrentLoader: loader,
			AllLoader:     loader,
			Terminal:      terminal,
		})
	}()

	time.Sleep(200 * time.Millisecond)
	select {
	case <-terminal.started:
	case <-time.After(5 * time.Second):
		t.Fatal("picker never started the terminal input pump")
	}
	time.Sleep(200 * time.Millisecond) // let the loads settle and render
	terminal.onInput("\x1b")

	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("cancelled picker returned %q, want empty", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SelectSession did not return after cancel")
	}
}

// pi v1.4.0 cli/session-picker.ts creates the app KeybindingsManager before
// constructing the selector, loading custom definitions and user bindings.
func TestSelectSessionLoadsConfiguredApplicationKeybindings(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	agentDir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", agentDir)
	if err := os.WriteFile(filepath.Join(agentDir, "keybindings.json"), []byte(`{"app.session.toggleSort":"ctrl+x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tui.SetKeybindings(tui.NewKeybindingsManager(tui.TUIKeybindings, nil))
	t.Cleanup(func() { tui.SetKeybindings(tui.NewKeybindingsManager(tui.TUIKeybindings, nil)) })

	loader := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
		return []coding.SessionInfo{{Path: "/s/a.jsonl", FirstMessage: "a", Modified: time.Now()}}, nil
	}
	terminal := &scriptedTerminal{width: 80, height: 24, started: make(chan struct{})}
	done := make(chan string, 1)
	go func() {
		done <- SelectSession(SelectSessionOptions{CurrentLoader: loader, AllLoader: loader, Terminal: terminal})
	}()

	select {
	case <-terminal.started:
	case <-time.After(5 * time.Second):
		t.Fatal("picker never started the terminal input pump")
	}
	time.Sleep(200 * time.Millisecond)
	terminal.onInput("\r")
	select {
	case got := <-done:
		if got != "/s/a.jsonl" {
			t.Fatalf("selected %q, want /s/a.jsonl", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SelectSession did not return after selection")
	}

	if got := tui.GetKeybindings().GetKeys("app.session.toggleSort"); len(got) != 1 || got[0] != "ctrl+x" {
		t.Fatalf("resume picker sort binding = %v, want configured ctrl+x", got)
	}
}
