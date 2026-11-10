package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

func noticeLines(t *testing.T, message string) int {
	t.Helper()
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	chat := &tui.Container{}
	w := &RunWiring{Chat: chat, Display: &DisplayOptions{OutputPad: 0}}
	w.ShowChatWarning(message)
	if len(chat.Children) == 0 {
		return 0
	}
	return len(chat.Children[len(chat.Children)-1].Render(80))
}

// A chat notice must stay a notice. A goal's blocked reason is model-supplied
// free text with no length limit in set_goal's schema, and it reaches this path
// through NotifyEffect; 100 KB on one line drew 1,284 chat lines and 15,000
// short lines drew 15,001. The full text stays in the goal state and the
// transcript cards, which goalPreview bounds (D427, D428).
func TestChatNoticeIsBounded(t *testing.T) {
	cases := map[string]string{
		"one long line": strings.Repeat("x", 100000),
		"many lines":    strings.Repeat("abcdef\n", 15000),
	}
	for name, message := range cases {
		if lines := noticeLines(t, message); lines > 8 {
			t.Fatalf("%s: notice rendered %d chat lines, want <= 8", name, lines)
		}
	}
}

// A short reason is shown as written, so the bound does not mangle ordinary
// notices.
func TestChatNoticeKeepsAShortMessage(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	chat := &tui.Container{}
	w := &RunWiring{Chat: chat, Display: &DisplayOptions{OutputPad: 0}}
	w.ShowChatWarning("Goal paused: the build is broken")
	rendered := ""
	for _, child := range chat.Children {
		rendered += strings.Join(child.Render(80), "\n")
	}
	if !strings.Contains(rendered, "the build is broken") {
		t.Fatalf("short notice was mangled: %q", rendered)
	}
}
