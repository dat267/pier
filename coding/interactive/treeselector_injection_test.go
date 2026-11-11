package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
)

// TestTreeRenderSanitizesStoredRowFields pins the tree against terminal control
// injection from a session file (D446). Reference: pi v1.5.0 f9a26365
// modes/interactive/components/tree-selector.ts:747 renders a label verbatim,
// while the port sanitizes untrusted terminal text (D342, D422). A label is
// stored in the JSONL, so a shared or planted session file controls it.
func TestTreeRenderSanitizesStoredRowFields(t *testing.T) {
	InitTheme("dark", false)
	payload := "\x1b[2J\x1b]52;c;cGF3bmVk\x07"
	session := coding.NewSessionManager(t.TempDir(), nil)
	session.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	branch := session.GetBranch("")
	if len(branch) == 0 {
		t.Fatal("no entry to label")
	}
	label := payload + "labelled"
	if _, err := session.AppendLabelChange(branch[0].ID, &label); err != nil {
		t.Fatal(err)
	}

	selector := NewTreeSelectorComponent(session.GetTree(), session.GetLeafID(), 40, func(string) {}, func() {}, TreeSelectorOptions{})
	for _, line := range selector.Render(120) {
		if strings.Contains(line, "\x1b[2J") || strings.Contains(line, "\x1b]52") {
			t.Fatalf("the tree rendered a stored label unsanitized: %q", line)
		}
	}
}
