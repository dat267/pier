package interactive

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// D357: the streaming sanitizer keeps the clean-prefix fast path; this
// measures the end-to-end assistant update plus paint for 100 KiB in 16-byte
// deltas.
func BenchmarkAssistantStream100KB(b *testing.B) {
	app, cleanup := newTestAppB(b)
	defer cleanup()
	streaming := &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending}
	app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageStart, Agent: agentEvent("message_start", streaming)})
	var body strings.Builder
	update := func() {
		app.events.HandleEvent(&coding.SessionEvent{Type: coding.SessionMessageUpdate, Agent: agentEvent("message_update", &ai.AssistantMessage{API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending, Content: ai.ContentList{ai.TextContent{Text: body.String()}}})})
		app.ui.RenderNow(true)
	}
	const total = 100 << 10
	delta := strings.Repeat("x", 16)
	b.ReportAllocs()
	b.SetBytes(total)
	for b.Loop() {
		for written := 0; written < total; written += len(delta) {
			body.WriteString(delta)
			update()
		}
	}
}

func TestJoinThinkingBlocksSingleBlockDoesNotCopy(t *testing.T) {
	blocks := []string{"one thinking block"}
	if got := joinThinkingBlocks(blocks); got != blocks[0] {
		t.Fatalf("join = %q, want %q", got, blocks[0])
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = joinThinkingBlocks(blocks) }); allocs != 0 {
		t.Fatalf("single-block join allocates %.1f times", allocs)
	}
}

func TestAssistantWithoutTransformersUsesNoTransform(t *testing.T) {
	_, cleanup := newTestAppB(t)
	defer cleanup()
	message := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m", StopReason: ai.StopPending,
		Content: ai.ContentList{ai.TextContent{Text: "hello"}},
	}
	component := NewAssistantMessageComponent(message, false, nil, "", 0, nil)
	for _, child := range component.blockComponents {
		if markdown, ok := child.(*tui.Markdown); ok && markdown.Options.Transform != nil {
			t.Fatal("identity transform installed without transformers")
		}
	}
}

func TestAssistantBlockComponentMapsAreReused(t *testing.T) {
	_, cleanup := newTestAppB(t)
	defer cleanup()
	message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "a"}}}
	component := NewAssistantMessageComponent(message, false, nil, "", 0, nil)
	first := component.blockComponents
	message.Content = ai.ContentList{ai.TextContent{Text: "b"}}
	component.UpdateContent(message, true)
	second := component.blockComponents
	message.Content = ai.ContentList{ai.TextContent{Text: "c"}}
	component.UpdateContent(message, true)
	third := component.blockComponents
	pointer := func(m map[assistantBlockKey]tui.Component) uintptr { return reflect.ValueOf(m).Pointer() }
	if pointer(first) == pointer(second) || pointer(third) != pointer(first) {
		t.Fatalf("block component maps were not reused")
	}
}
