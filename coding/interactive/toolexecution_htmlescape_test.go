package interactive

import (
	"strings"
	"testing"
)

// tool-execution.ts renders expanded args with JSON.stringify(args, null, 2),
// which does not HTML-escape.
func TestToolExecutionFormatDoesNotHTMLEscapeArgs(t *testing.T) {
	useBuiltinPierThemeForTest(t)
	InitTheme("dark", false)
	component := NewToolExecutionComponent("custom", "call-1",
		map[string]any{"text": "a < b & c > d"}, ToolExecutionOptions{},
		WithBuiltInRenderers("custom", nil), nil, "/tmp")
	got := component.formatToolExecution()
	if strings.Contains(got, `\u003c`) || strings.Contains(got, `\u0026`) || strings.Contains(got, `\u003e`) {
		t.Fatalf("formatted execution HTML-escaped: %q", got)
	}
	if !strings.Contains(got, "a < b & c > d") {
		t.Fatalf("formatted execution = %q", got)
	}
}
