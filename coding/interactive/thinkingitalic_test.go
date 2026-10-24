package interactive

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
)

// D263: pi v1.4.0 components/assistant-message.ts uses defaultTextStyle.italic,
// but tui/components/markdown.ts bypasses it for headings and code. Pier keeps
// every expanded thinking element italic without changing answer Markdown.
func TestThinkingMarkdownKeepsStyledTextItalic(t *testing.T) {
	newRendererTestTheme(t)
	theme := GetMarkdownTheme()
	for _, width := range []int{80, 20} {
		component := NewAssistantMessageComponent(&ai.AssistantMessage{
			Content: ai.ContentList{
				ai.ThinkingContent{Thinking: "# Heading\n\n**Bold** and `inline` with [link](https://example.com).\n\n- list item\n\n> quoted text\n\n```go\nreturn 42\n```"},
				ai.TextContent{Text: "Answer"},
			}, StopReason: ai.StopStop,
		}, false, &theme, "", 1, nil)
		lines := component.Render(width)
		for _, word := range []string{"Heading", "Bold", "inline", "link", "list", "quoted", "return", "42"} {
			assertRenderedItalic(t, lines, word, true)
		}
		assertRenderedItalic(t, lines, "Answer", false)
	}
}

// D263 also applies to growing thinking blocks without dropping the existing
// streaming Markdown cache or changing collapsed thinking behavior.
func TestThinkingItalicSurvivesStreamingAndVisibility(t *testing.T) {
	newRendererTestTheme(t)
	theme := GetMarkdownTheme()
	component := NewAssistantMessageComponent(nil, false, &theme, "Thinking...", 1, nil)
	body := "# Heading\n\nFirst `inline`\n\n```go\nreturn 42\n```"
	for _, text := range []string{body, body + "\n\n## Another heading\n\n**Bold** finish"} {
		message := &ai.AssistantMessage{Content: ai.ContentList{ai.ThinkingContent{Thinking: text}}, StopReason: ai.StopPending}
		component.UpdateContent(message, true)
		fresh := NewAssistantMessageComponent(nil, false, &theme, "Thinking...", 1, nil)
		fresh.UpdateContent(message, true)
		if got, want := strings.Join(component.Render(24), "\n"), strings.Join(fresh.Render(24), "\n"); got != want {
			t.Fatalf("streamed thinking differs from fresh render:\n got %q\nwant %q", got, want)
		}
		assertRenderedItalic(t, component.Render(24), "inline", true)
		component.SetHideThinkingBlock(true)
		assertRenderedItalic(t, component.Render(24), "Thinking...", true)
		component.SetHideThinkingBlock(false)
		assertRenderedItalic(t, component.Render(24), "Heading", true)
	}
}

func TestThinkingItalicRespectsDisabledStyles(t *testing.T) {
	newRendererTestTheme(t)
	SetStyleColorsEnabled(false)
	defer SetStyleColorsEnabled(true)
	theme := GetMarkdownTheme()
	component := NewAssistantMessageComponent(&ai.AssistantMessage{
		Content: ai.ContentList{ai.ThinkingContent{Thinking: "# Heading\n\n`inline`"}}, StopReason: ai.StopStop,
	}, false, &theme, "", 0, nil)
	if lines := component.Render(40); strings.Contains(strings.Join(lines, "\n"), "\x1b[3m") {
		t.Fatalf("italic emitted while styles are disabled: %q", lines)
	}
}

// Inspect terminal state, rather than only testing for an italic opener: an
// opener before a nested reset does not make the following text italic.
func assertRenderedItalic(t *testing.T, lines []string, word string, want bool) {
	t.Helper()
	found := false
	for _, line := range lines {
		var plain strings.Builder
		var styles []bool
		italic := false
		for i := 0; i < len(line); {
			if code, n := tui.ExtractANSICode(line, i); n > 0 {
				if strings.HasPrefix(code, "\x1b[") && strings.HasSuffix(code, "m") {
					parameters := strings.Split(code[2:len(code)-1], ";")
					for j := 0; j < len(parameters); j++ {
						value, _ := strconv.Atoi(parameters[j])
						switch value {
						case 0, 23:
							italic = false
						case 3:
							italic = true
						case 38, 48, 58:
							if j+1 < len(parameters) {
								switch parameters[j+1] {
								case "5":
									j += 2
								case "2":
									j += 4
								}
							}
						}
					}
				}
				i += n
				continue
			}
			plain.WriteByte(line[i])
			styles = append(styles, italic)
			i++
		}
		if index := strings.Index(plain.String(), word); index >= 0 {
			found = true
			for _, italic := range styles[index : index+len(word)] {
				if italic != want {
					t.Fatalf("%q italic = %v, want %v in %q", word, italic, want, line)
				}
			}
		}
		if italic {
			t.Fatalf("italic leaked beyond rendered line: %q", line)
		}
	}
	if !found {
		t.Fatalf("%q missing from rendered lines: %q", word, lines)
	}
}
