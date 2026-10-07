package interactive

import (
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/tui"
)

// Isolate one live clock-row update while replacement work remains pending.
func BenchmarkPendingToolClockOwner(b *testing.B) {
	InitTheme("dark", false)
	oldNow := shellNow
	defer func() { shellNow = oldNow }()
	now := int64(1000)
	shellNow = func() time.Time { return time.UnixMilli(now) }
	c := NewToolExecutionComponent("bash", "call", map[string]any{"command": "output"}, ToolExecutionOptions{}, WithBuiltInRenderers("bash", nil), nil, "/tmp")
	var work func() func()
	c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
	c.MarkExecutionStarted()
	c.SetExpanded(true)
	result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output with words and spaces\n", 30000)}}}
	c.UpdateResult(result, true)
	c.Render(80)
	work()()
	c.Render(80)
	c.UpdateResult(result, true)
	c.Render(80)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		now += 100
		c.AnimationTick()
		c.Render(80)
	}
}
