package interactive

import (
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/tui"
	"strings"
	"testing"
)

func BenchmarkSingleAssistantAdmission(b *testing.B) {
	text := strings.Repeat("# Heading\n\nParagraph with **bold** and `code` plus several words.\n\n", 16000)
	InitTheme("dark", false)
	message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: text}}}
	// Isolate owner admission from worker completion; the worker's full render
	// cost remains BenchmarkSingleAssistantCold, not hidden by this measurement.
	preparation := &tui.MarkdownPreparation{Submit: func(func() func()) bool { return true }, RequestRender: func() {}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		component := NewAssistantMessageComponent(message, false, nil, "", 1, nil)
		component.SetMarkdownPreparation(preparation)
		component.Render(80)
	}
}

// Isolate a single cold child: D203's 64-child resize bound cannot limit it.
func BenchmarkSingleAssistantCold(b *testing.B) {
	text := strings.Repeat("# Heading\n\nParagraph with **bold** and `code` plus several words.\n\n", 16000)
	InitTheme("dark", false)
	message := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: text}}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		component := NewAssistantMessageComponent(message, false, nil, "", 1, nil)
		component.Render(80)
	}
}
