package interactive

import (
	"strings"
	"testing"
)

// BenchmarkOwnerLoopContent measures individual input/event/paint phases, not
// background queues. Fixtures are sized to expose content-shaped work.
func BenchmarkOwnerLoopContent(b *testing.B) {
	payload := strings.Repeat("some output line with words to wrap around terminal width\n", 18000)
	b.Run("paste-1MB", func(b *testing.B) {
		app, cleanup := newTestAppB(b)
		defer cleanup()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			app.defaultEditor.SetText("")
			app.defaultEditor.HandleInput("\x1b[200~" + payload + "\x1b[201~")
			app.defaultEditor.Render(80)
		}
	})
	b.Run("tool-update-1MB", func(b *testing.B) {
		component := NewToolExecutionComponent("read", "call", map[string]any{"file": "x"}, ToolExecutionOptions{}, tickBenchDefinition(payload), nil, "/tmp")
		component.SetArgsComplete()
		result := skipTextResult(payload)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			component.UpdateResult(result, true)
			component.Render(80)
		}
	})
	b.Run("transcript-resize", func(b *testing.B) {
		app, cleanup := newTestAppB(b)
		defer cleanup()
		buildScrollTranscriptN(b, app, 500)
		app.documentContainer.Render(80)
		document := app.documentContainer.Children[2].(*resizeDocument)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			width := 81 + i%2
			app.documentContainer.Render(width)
			b.StopTimer()
			for document.targetWidth != 0 {
				app.documentContainer.Render(width)
			}
			b.StartTimer()
		}
	})
}
