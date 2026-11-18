package interactive

import (
	"context"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
	"strings"
	"testing"
)

// TestUserMessageSanitizesStoredText pins the transcript against terminal
// control injection from a session file (D447). Reference: pi v1.5.0 f9a26365
// modes/interactive/components/user-message.ts:47 passes its text straight to
// Markdown, which forwards escape sequences, while the port sanitizes untrusted
// terminal text (D342, D422). A resumed session renders its stored user messages
// through this component, so a shared or planted file chooses those bytes.
func TestUserMessageSanitizesStoredText(t *testing.T) {
	InitTheme("dark", false)
	component := NewUserMessageComponent("hi \x1b[2J\x1b]52;c;cGF3bmVk\x07 there", nil, 0, nil)
	for _, line := range component.Render(80) {
		if strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x1b]52") {
			t.Fatalf("the user message rendered a stored sequence raw: %q", line)
		}
	}
	component.SetText("changed \x1b[2J")
	for _, line := range component.Render(80) {
		if strings.Contains(line, "\x1b[2J") {
			t.Fatalf("SetText rendered a stored sequence raw: %q", line)
		}
	}
}

// The fallback box for a custom entry whose renderer panicked interpolates the
// entry's customType and the recovered panic text straight into the frame. Both
// are session data: customType is persisted in the session file, and a panic
// message can quote the input that caused it. Every neighbouring fallback
// sanitizes (assistantmessage.go's error block, D342).
func TestCustomEntryFallbackSanitizesItsText(t *testing.T) {
	SetRegisteredThemes(nil)
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	InitTheme("dark", false)

	renderer := func(CustomEntry, EntryRenderOptions, *Theme) tui.Component {
		panic("boom\x1b]0;pwned\x07")
	}
	component := NewCustomEntryComponent(CustomEntry{CustomType: "note\x1b]0;typed\x07"}, renderer)
	rendered := strings.Join(component.Render(100), "\n")
	for _, sequence := range []string{"\x1b]0;pwned", "\x1b]0;typed"} {
		if strings.Contains(rendered, sequence) {
			t.Fatalf("the fallback let %q reach the terminal: %q", sequence, rendered)
		}
	}
	if !strings.Contains(rendered, "renderer failed") {
		t.Fatalf("the fallback did not report the failure: %q", rendered)
	}
}

const storedNamePayload = "\x1b[2J\x1b]52;c;cGF3bmVk\x07name"

// TestStoredSessionNameIsSanitized pins every place the port shows or seeds a
// stored session name (D448). Reference: pi v1.5.0 f9a26365
// modes/interactive/interactive-mode.ts:6776 and :6791 interpolate the name into
// the transcript, and session-selector.ts:818 seeds the rename editor with it,
// while the port sanitizes untrusted terminal text (D342, D422).
func TestStoredSessionNameIsSanitized(t *testing.T) {
	t.Run("name command", func(t *testing.T) {
		InitTheme("dark", false)
		wiring, _, _ := newCommandTestWiring(t)
		wiring.SessionInfo.AppendSessionInfo(storedNamePayload)
		wiring.HandleNameCommand("/name")
		assertNoStoredSequence(t, strings.Join(wiring.Chat.Render(120), "\n"))
	})

	t.Run("rename prompt", func(t *testing.T) {
		InitTheme("dark", false)
		loader := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
			return []coding.SessionInfo{{Path: "/tmp/x.jsonl", Name: storedNamePayload}}, nil
		}
		component := NewSessionSelectorComponent(loader, loader,
			func(string) {}, func() {}, func() {}, func() {},
			SessionSelectorOptions{Keybindings: NewAppKeybindingsManager(nil, "").KeybindingsManager, Post: (&postQueue{}).Post})
		component.WaitForPendingLoads()
		component.enterRenameMode("/tmp/x.jsonl", storedNamePayload)
		assertNoStoredSequence(t, strings.Join(component.Render(120), "\n"))
	})
}

func assertNoStoredSequence(t *testing.T, rendered string) {
	t.Helper()
	if strings.Contains(rendered, "\x1b[2J") || strings.Contains(rendered, "\x1b]52") {
		t.Fatalf("rendered a stored session name raw: %q", rendered)
	}
}
