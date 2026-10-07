package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Measure invalidation/admission while a prior complete D206 frame exists.
// Initial detached preparation and fixture construction are outside the timer.
func BenchmarkToolRetainedFrameOwner(b *testing.B) {
	InitTheme("dark", false)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output with words and spaces\n", 30000)}}}
	next := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("new output with words and spaces\n", 30000)}}}
	for _, name := range []string{"read", "bash"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				c := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
				var work func() func()
				c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
				c.SetExpanded(true)
				c.UpdateResult(result, false)
				c.Render(80)
				work()()
				c.Render(80)
				b.StartTimer()
				c.UpdateResult(next, false)
				c.Render(80)
			}
		})
	}
}
