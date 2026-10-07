package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// Isolate owner publication after D205 has completed detached formatting.
// Worker cost and fixture/component construction are outside the timer.
func BenchmarkToolCompletedFrameOwner(b *testing.B) {
	InitTheme("dark", false)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output with words and spaces\n", 30000)}}}
	for _, name := range []string{"read", "bash"} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				c := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "printf output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
				var work func() func()
				c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
				c.SetExpanded(true)
				c.UpdateResult(result, false)
				c.Render(80)
				apply := work()
				b.StartTimer()
				apply()
				c.Render(80)
			}
		})
	}
}
