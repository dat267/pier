package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// The custom message box writes the label, then the body. The body goes through
// tui.SanitizeUntrustedText (D342: model- and extension-generated session text
// renders as data), but the label was interpolated raw:
//
//	label := theme.Fg("customMessageLabel", "\x1b[1m["+c.message.CustomType+"]\x1b[22m")
//
// customType comes from an extension's sendMessage call and is persisted in the
// session file, so a value carrying an escape sequence executed a terminal
// control function (an OSC 0 title set, a mode change) in the middle of the
// frame, and the reference does the same. Treat the label as data like the body.
func TestTranscriptCustomMessageLabelIsSanitized(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.MarkdownTheme = nil
	message := coding.CreateCustomMessage("notice\x1b]0;pwned\x07", []byte(`"body"`), true, nil, 1000)
	renderer.AddMessageToChat(message, false)
	rendered := strings.Join(renderChat(t, renderer.Chat), "\n")

	if strings.Contains(rendered, "\x1b]0;pwned") {
		t.Fatalf("the customType control sequence reached the terminal: %q", rendered)
	}
	// The label itself is still visible, just without the sequence.
	if !strings.Contains(rendered, "[notice") {
		t.Fatalf("the label disappeared; rendered:\n%s", rendered)
	}
}
