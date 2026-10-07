package interactive

import (
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// BenchmarkToolResult measures a single built-in result, including formatting
// in UpdateResult. Admission uses a sink, not an end-to-end latency measurement.
func BenchmarkToolResult(b *testing.B) {
	InitTheme("dark", false)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output with words and spaces\n", 30000)}}}
	for _, name := range []string{"read", "bash"} {
		for _, mode := range []string{"cold", "admission"} {
			b.Run(name+"/"+mode, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					c := NewToolExecutionComponent(name, "call", map[string]any{"path": "output.txt", "command": "printf output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
					if mode == "admission" {
						c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(func() func()) bool { return true }})
					}
					c.SetExpanded(true)
					c.UpdateResult(result, false)
					c.Render(80)
				}
			})
		}
	}
}
