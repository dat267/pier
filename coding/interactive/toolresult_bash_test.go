package interactive

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// D205: core/tools/renderers/bash.ts builds expanded Text synchronously.
func TestLargeBashResultPreparesOffLoop(t *testing.T) {
	InitTheme("dark", false)
	for _, name := range []string{"bash", "powershell"} {
		for _, expanded := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expanded=%t", name, expanded), func(t *testing.T) {
				c := NewToolExecutionComponent(name, "call", map[string]any{"command": "printf output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
				var work func() func()
				c.SetResultPreparation(&tui.MarkdownPreparation{Submit: func(run func() func()) bool { work = run; return true }})
				c.SetExpanded(expanded)
				result := &SortToolResultContent{Content: []ToolResultContent{{Type: "text", Text: strings.Repeat("plain output\n", 6000)}}}
				c.UpdateResult(result, false)
				pending := c.Render(80)
				if work == nil {
					t.Fatal("large bash output ran inline")
				}
				apply := work()
				if !reflect.DeepEqual(c.Render(80), pending) {
					t.Fatal("worker changed attached display")
				}
				apply()
				sync := NewToolExecutionComponent(name, "call", map[string]any{"command": "printf output"}, ToolExecutionOptions{}, WithBuiltInRenderers(name, nil), nil, "/tmp")
				sync.SetExpanded(expanded)
				sync.UpdateResult(result, false)
				if !reflect.DeepEqual(c.Render(80), sync.Render(80)) {
					t.Fatal("prepared bash differs from synchronous output")
				}
			})
		}
	}
}
