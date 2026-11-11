package interactive

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// The components here render text that comes from a session file, which a
// shared, imported or planted file controls (D445, D446, D447, D448). Every one
// of those findings had the same shape: a stored string reached the terminal
// without passing through the port's rule for untrusted text (D342, D422), and
// each was found by hand because nothing checked the invariant itself. This is
// that check: a component may emit SGR styling, OSC 8 hyperlinks, the OSC 133
// shell-integration zones and pier's own metadata markers, and nothing else.
//
// A new component that renders stored text gets a subtest here. Reverting any of
// the four fixes fails the subtest for its component.

// assertOnlyThemeEscapes fails when rendered output carries an escape sequence
// other than the ones the renderer itself produces: SGR (`ESC[…m`), an OSC 8
// hyperlink, the OSC 133 shell-integration zones, or a pier metadata marker
// (`ESC_pi:…BEL`).
func assertOnlyThemeEscapes(t *testing.T, rendered []string) {
	t.Helper()
	for _, line := range rendered {
		for index := 0; index < len(line); index++ {
			if line[index] != 0x1b {
				continue
			}
			rest := line[index+1:]
			switch {
			case strings.HasPrefix(rest, "[") && strings.HasSuffix(sgrPrefix(rest), "m"):
				continue
			case strings.HasPrefix(rest, "]8;"), strings.HasPrefix(rest, "]133;"):
				continue
			case strings.HasPrefix(rest, "_pi:"):
				continue
			default:
				t.Fatalf("rendered a non-theme escape sequence %q in %q", escapeAt(rest), line)
			}
		}
	}
}

// sgrPrefix returns a CSI sequence's parameters and final byte, so the caller can
// check the final byte without parsing the whole line.
func sgrPrefix(rest string) string {
	end := strings.IndexFunc(rest[1:], func(r rune) bool {
		return r >= 0x40 && r <= 0x7e
	})
	if end < 0 {
		return ""
	}
	return rest[1 : end+2]
}

func escapeAt(rest string) string {
	end := strings.IndexFunc(rest, func(r rune) bool {
		return r >= 0x40 && r <= 0x7e
	})
	if end < 0 {
		return rest[:1]
	}
	return rest[:end+1]
}

const storedPayload = "hi \x1b[2J\x1b]52;c;cGF3bmVk\x07 there"

func TestStoredSessionTextRendersOnlyThemeEscapes(t *testing.T) {
	InitTheme("dark", false)

	t.Run("user message", func(t *testing.T) {
		component := NewUserMessageComponent(storedPayload, nil, 0, nil)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("session list", func(t *testing.T) {
		list := NewSessionList([]coding.SessionInfo{{
			Path: "/tmp/" + storedPayload, ID: "s1", Cwd: "/tmp/" + storedPayload,
			Name: storedPayload, FirstMessage: storedPayload, Modified: time.Unix(1700000000, 0),
		}}, true, SortRecent, NameFilterAll, nil, "")
		assertOnlyThemeEscapes(t, list.Render(120))
	})

	t.Run("tree", func(t *testing.T) {
		session := coding.NewSessionManager(t.TempDir(), nil)
		session.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: storedPayload}})
		label := storedPayload
		if branch := session.GetBranch(""); len(branch) > 0 {
			if _, err := session.AppendLabelChange(branch[0].ID, &label); err != nil {
				t.Fatal(err)
			}
		}
		selector := NewTreeSelectorComponent(session.GetTree(), session.GetLeafID(), 40,
			func(string) {}, func() {}, TreeSelectorOptions{})
		assertOnlyThemeEscapes(t, selector.Render(120))
	})

	t.Run("tool result", func(t *testing.T) {
		component := NewToolExecutionComponent("unknown-tool", "call-1", map[string]any{"x": storedPayload},
			ToolExecutionOptions{}, nil, nil, "")
		component.SetArgsComplete()
		component.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: storedPayload}}}, false)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("name command", func(t *testing.T) {
		wiring, _, _ := newCommandTestWiring(t)
		wiring.SessionInfo.AppendSessionInfo(storedPayload)
		wiring.HandleNameCommand("/name")
		assertOnlyThemeEscapes(t, wiring.Chat.Render(120))
	})

	t.Run("rename prompt", func(t *testing.T) {
		loader := func(SessionListProgress, context.Context) ([]coding.SessionInfo, error) {
			return []coding.SessionInfo{{Path: "/tmp/x.jsonl", Name: storedPayload}}, nil
		}
		component := NewSessionSelectorComponent(loader, loader,
			func(string) {}, func() {}, func() {}, func() {},
			SessionSelectorOptions{Keybindings: NewAppKeybindingsManager(nil, "").KeybindingsManager, Post: (&postQueue{}).Post})
		component.WaitForPendingLoads()
		component.enterRenameMode("/tmp/x.jsonl", storedPayload)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("mcp call header", func(t *testing.T) {
		// An argument key is decoded from the call's JSON, so an escape sequence in
		// it is a real escape byte; the value is re-serialized and therefore
		// JSON-escaped. Both are rendered into the same line.
		line := formatMcpCall("server/tool", json.RawMessage(`{"\u001b[2Jkey":"\u001b[2Jvalue"}`), ActiveTheme())
		assertOnlyThemeEscapes(t, []string{line})
	})

	t.Run("status", func(t *testing.T) {
		renderer := &TranscriptRenderer{Chat: &tui.Container{}, Display: &DisplayOptions{}}
		renderer.ShowStatus(storedPayload)
		assertOnlyThemeEscapes(t, renderer.Chat.Render(120))
	})
}
