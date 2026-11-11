package interactive

import (
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
