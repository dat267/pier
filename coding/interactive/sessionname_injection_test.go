package interactive

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

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
