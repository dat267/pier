package interactive

import (
	"context"
	"encoding/json"
	"fmt"
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
	if err := onlyThemeEscapes(rendered); err != nil {
		t.Fatal(err)
	}
}

// onlyThemeEscapes walks one render's escapes. A hyperlink must be framed: the
// renderer opens it around text it chose and closes it, so opens and closes
// alternate across the whole render (a link may span wrapped lines) and none is
// left open. Text that ends a frame inside a link of its own would otherwise
// make everything after it clickable.
func onlyThemeEscapes(rendered []string) error {
	linkOpen := false
	for _, line := range rendered {
		for pos := 0; pos < len(line); {
			index := strings.IndexByte(line[pos:], 0x1b)
			if index < 0 {
				break
			}
			at := pos + index
			rest := line[at+1:]
			if strings.HasPrefix(rest, "[") {
				sgr := sgrPrefix(rest)
				if sgr == "" || !strings.HasSuffix(sgr, "m") {
					return fmt.Errorf("rendered a non-theme escape sequence %q in %q", escapeAt(rest), line)
				}
				pos = at + 1 + len(sgr)
				continue
			}
			if strings.HasPrefix(rest, "]") || strings.HasPrefix(rest, "_pi:") {
				body, consumed, ok := oscBody(rest)
				if !ok {
					return fmt.Errorf("rendered an unterminated escape sequence %q in %q", escapeAt(rest), line)
				}
				pos = at + 1 + consumed
				switch {
				case strings.HasPrefix(body, "]133;"):
				case strings.HasPrefix(body, "_pi:"):
				case strings.HasPrefix(body, "]8;"):
					uri := body[strings.IndexByte(body, ';')+1:]
					uri = strings.TrimPrefix(uri, ";")
					switch {
					case uri == "":
						if !linkOpen {
							return fmt.Errorf("rendered a closing hyperlink with none open in %q", line)
						}
						linkOpen = false
					case linkOpen:
						return fmt.Errorf("rendered a nested hyperlink in %q", line)
					default:
						linkOpen = true
					}
				default:
					return fmt.Errorf("rendered a non-theme OSC sequence %q in %q", body, line)
				}
				continue
			}
			return fmt.Errorf("rendered a non-theme escape sequence %q in %q", escapeAt(rest), line)
		}
	}
	if linkOpen {
		return fmt.Errorf("rendered output ends inside an open hyperlink")
	}
	return nil
}

// oscBody returns the body of an OSC or metadata sequence and how many bytes it
// occupies after the ESC, terminated by BEL or ST.
func oscBody(rest string) (string, int, bool) {
	for index := 1; index < len(rest); index++ {
		switch {
		case rest[index] == 0x07:
			return rest[:index], index + 1, true
		case rest[index] == 0x1b && index+1 < len(rest) && rest[index+1] == '\\':
			return rest[:index], index + 2, true
		}
	}
	return "", 0, false
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

	t.Run("built-in tool frames", func(t *testing.T) {
		// A resumed session replays every stored tool call through its renderer, so
		// each renderer is a terminal sink for the call's arguments and result.
		// These cases guard the end-to-end invariant rather than one mechanism:
		// removing the arg sanitizer alone does not leak here, because the frame's
		// width truncation also drops non-SGR sequences.
		cases := map[string]map[string]any{
			"bash":      {"command": storedPayload},
			"read":      {"path": storedPayload},
			"grep":      {"pattern": storedPayload, "path": storedPayload},
			"find":      {"pattern": storedPayload, "path": storedPayload},
			"ls":        {"path": storedPayload},
			"write":     {"path": storedPayload, "content": storedPayload},
			"edit":      {"path": storedPayload, "oldText": storedPayload, "newText": storedPayload},
			"web_fetch": {"url": storedPayload},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				frame := newToolFrame(name, "call-"+name, args, toolFrameOptions{})
				frame.SetArgsComplete()
				frame.UpdateResult(&SortToolResultContent{Content: []ToolResultContent{
					{Type: "text", Text: storedPayload},
				}}, false)
				assertOnlyThemeEscapes(t, frame.Render(120))
			})
		}
	})

	t.Run("auth url", func(t *testing.T) {
		// The sign-in URL block prints the authorization server's URL as both the
		// link target and its label.
		component := NewAuthUrlComponent(nil, nil, "https://evil.example/"+storedPayload)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("login dialog", func(t *testing.T) {
		// The authorization server supplies the instructions, the device user code
		// and an info message (D460).
		component := NewLoginDialogComponent(nil, nil, "xai", nil, "", "")
		component.ShowAuth("https://x.ai/authorize", storedPayload)
		assertOnlyThemeEscapes(t, component.Render(120))
		component.ShowDeviceCode("https://x.ai/device", storedPayload)
		assertOnlyThemeEscapes(t, component.Render(120))
		component.ShowInfo(storedPayload, []ai.AuthInfoLink{{URL: "https://x.ai/help", Label: storedPayload}}, false)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("assistant markdown", func(t *testing.T) {
		// Assistant text goes through the markdown renderer, a different path from
		// the plain text components above.
		message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{
			Text: "# " + storedPayload + "\n\n| a | b |\n| - | - |\n| " + storedPayload + " | x |\n",
		}}}
		component := NewAssistantMessageComponent(message, false, nil, "Thinking", 0, nil)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("compaction summary", func(t *testing.T) {
		component := NewCompactionSummaryMessageComponent(storedPayload, 1000, nil, 0)
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("model selector", func(t *testing.T) {
		catalog := []*ai.Model{{Provider: storedPayload, ID: storedPayload, Name: storedPayload}}
		component := NewModelSelectorComponent(nil, func(func()) {}, catalog[0],
			&fakeModelRuntime{models: catalog}, nil, func(*ai.Model) {}, func() {}, "", nil, nil)
		// The constructor starts a background catalog refresh, and leakcheck fails
		// a package that leaves it running.
		defer component.Dispose()
		assertOnlyThemeEscapes(t, component.Render(120))
	})

	t.Run("status", func(t *testing.T) {
		renderer := &TranscriptRenderer{Chat: &tui.Container{}, Display: &DisplayOptions{}}
		renderer.ShowStatus(storedPayload)
		assertOnlyThemeEscapes(t, renderer.Chat.Render(120))
	})
}

// TestEscapeWalkerRejectsInjectedFraming keeps the invariant above from
// passing vacuously: the walker must accept the renderer's own escapes and
// reject framing that untrusted text would have to supply itself.
func TestEscapeWalkerRejectsInjectedFraming(t *testing.T) {
	allowed := map[string][]string{
		"sgr":       {"\x1b[31mred\x1b[39m"},
		"hyperlink": {"\x1b]8;;https://a\x1b\\one", "two\x1b]8;;\x1b\\"},
		"osc 133":   {"\x1b]133;A\x07prompt"},
		"pier meta": {"\x1b_pi:x\x07text"},
	}
	for name, rendered := range allowed {
		if err := onlyThemeEscapes(rendered); err != nil {
			t.Errorf("%s: rejected renderer output: %v", name, err)
		}
	}
	rejected := map[string][]string{
		"osc 52":       {"\x1b]52;c;cGF3bmVk\x07"},
		"csi erase":    {"\x1b[2J"},
		"unterminated": {"\x1b]8;;https://evil"},
		"left open":    {"\x1b]8;;https://evil\x07rest of the frame"},
		"nested":       {"\x1b]8;;https://a\x07\x1b]8;;https://b\x07"},
		"stray close":  {"text\x1b]8;;\x07"},
		"bare st":      {"text\x1b\\"},
	}
	for name, rendered := range rejected {
		if err := onlyThemeEscapes(rendered); err == nil {
			t.Errorf("%s: accepted injected framing %q", name, rendered)
		}
	}
}
