package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/coding"
)

// TestSessionListSanitizesStoredCwd pins the picker against terminal control
// injection from a session file (D445). Reference: pi v1.5.0 f9a26365
// modes/interactive/components/session-selector.ts:466 renders
// `shortenHomePath(session.cwd)` verbatim, while the port sanitizes untrusted
// terminal text (D342, D422). SessionInfo.Cwd is the header's cwd
// (coding/session_manager.go), so a shared or imported session file controls it,
// and the all-projects scope renders it in every row.
func TestSessionListSanitizesStoredCwd(t *testing.T) {
	InitTheme("dark", false)
	sessions := []coding.SessionInfo{{
		Path:         "/tmp/attacker/s1.jsonl",
		ID:           "s1",
		Cwd:          "/tmp/\x1b[2J\x1b]52;c;cGF3bmVk\x07project",
		Name:         "named",
		FirstMessage: "hi",
		Modified:     time.Unix(1700000000, 0),
	}}
	list := NewSessionList(sessions, true, SortRecent, NameFilterAll, nil, "")
	lines := list.Render(120)
	for _, line := range lines {
		if strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x1b]52") {
			t.Fatalf("the picker rendered a stored cwd unsanitized: %q", line)
		}
	}
}
