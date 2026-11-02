package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// M37: assistant text, thinking, and provider error text are model/provider
// output, i.e. untrusted. A screen-clear or cursor-movement sequence from them
// must never reach the terminal, while the renderer's own styling still does.
const (
	unsafeClearScreen = "\x1b[2J"
	unsafeCursorHome  = "\x1b[H"
	unsafeClipboard   = "\x1b]52;c;c2VjcmV0\x07"
)

func TestAssistantTextCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	message := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "hello" + unsafeClearScreen + "world" + unsafeCursorHome + unsafeClipboard}},
	}
	component := NewAssistantMessageComponent(message, false, nil, "", 0, nil)
	rendered := strings.Join(component.Render(80), "\n")
	for _, sequence := range []string{unsafeClearScreen, unsafeCursorHome, unsafeClipboard} {
		if strings.Contains(rendered, sequence) {
			t.Fatalf("untrusted control %q reached output: %q", sequence, rendered)
		}
	}
	if text := coding.StripAnsi(rendered); !strings.Contains(text, "helloworld") {
		t.Fatalf("assistant text lost: %q", text)
	}
}

func TestAssistantThinkingCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	message := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{
			ai.ThinkingContent{Thinking: "hmm" + unsafeClearScreen + "more"},
			ai.TextContent{Text: "answer"},
		},
	}
	component := NewAssistantMessageComponent(message, false, nil, "", 0, nil)
	rendered := strings.Join(component.Render(80), "\n")
	if strings.Contains(rendered, unsafeClearScreen) {
		t.Fatalf("thinking control reached output: %q", rendered)
	}
	if text := coding.StripAnsi(rendered); !strings.Contains(text, "hm") || !strings.Contains(text, "more") {
		t.Fatalf("thinking text lost: %q", text)
	}
}

func TestAssistantErrorMessageCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	errorMessage := "provider said" + unsafeClearScreen + "no"
	message := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		StopReason: ai.StopError, ErrorMessage: &errorMessage,
	}
	component := NewAssistantMessageComponent(message, false, nil, "", 0, nil)
	rendered := strings.Join(component.Render(80), "\n")
	if strings.Contains(rendered, unsafeClearScreen) {
		t.Fatalf("error control reached output: %q", rendered)
	}
	if text := coding.StripAnsi(rendered); !strings.Contains(text, "provider saidno") {
		t.Fatalf("error text lost: %q", text)
	}
}

func TestChatErrorCannotEmitTerminalControls(t *testing.T) {
	newRendererTestTheme(t)
	chat := &tui.Container{}
	wiring := &RunWiring{Chat: chat, Display: &DisplayOptions{}}
	wiring.ShowChatError("failed" + unsafeClearScreen + "hard")
	rendered := strings.Join(chat.Render(80), "\n")
	if strings.Contains(rendered, unsafeClearScreen) {
		t.Fatalf("chat error control reached output: %q", rendered)
	}
	if text := coding.StripAnsi(rendered); !strings.Contains(text, "failedhard") {
		t.Fatalf("chat error text lost: %q", text)
	}
}

func TestStatusCannotEmitTerminalControls(t *testing.T) {
	theme := newRendererTestTheme(t)
	renderer := NewTranscriptRenderer(&tui.Container{}, nil, nil, nil, nil)
	renderer.ShowStatus("working" + unsafeClearScreen + "now")
	rendered := strings.Join(renderer.Chat.Render(80), "\n")
	if strings.Contains(rendered, unsafeClearScreen) {
		t.Fatalf("status control reached output: %q", rendered)
	}
	if text := coding.StripAnsi(rendered); !strings.Contains(text, "workingnow") {
		t.Fatalf("status text lost: %q", text)
	}
	_ = theme
}
