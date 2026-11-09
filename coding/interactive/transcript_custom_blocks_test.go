package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// The reference's custom-message component accepts string or block content and
// joins the text blocks (pi v1.5.0 f9a26365
// modes/interactive/components/custom-message.ts rebuild, which the port's
// CustomMessagePayload.Blocks already models):
//
//	text = typeof this.message.content === "string"
//		? this.message.content
//		: this.message.content.filter((c) => c.type === "text").map((c) => c.text).join("\n");
//
// A block-array message is the same message the model reads through
// convertCustomMessage's customContentToBlocks, so the transcript must show
// that text instead of an empty box.
func TestTranscriptCustomMessageRendersTextBlocks(t *testing.T) {
	renderer, _ := newTranscriptTestRenderer(t, false)
	renderer.MarkdownTheme = nil
	message := coding.CreateCustomMessage("notice",
		[]byte(`[{"type":"text","text":"first block"},{"type":"text","text":"second block"}]`), true, nil, 1000)
	renderer.AddMessageToChat(message, false)
	rendered := strings.Join(renderChat(t, renderer.Chat), "\n")
	for _, want := range []string{"first block", "second block"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("transcript lost %q; rendered:\n%s", want, rendered)
		}
	}
}
