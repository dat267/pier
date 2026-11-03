package interactive

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateArgsSkipsUnchangedRawMessage(t *testing.T) {
	_, cleanup := newTestAppB(t)
	defer cleanup()
	raw := json.RawMessage(`{"path":"a.txt","content":"body"}`)
	component := NewToolExecutionComponent("write", "id", raw, ToolExecutionOptions{}, nil, nil, "")
	component.UpdateArgs(raw)
	if component.argsPending {
		t.Fatal("equal raw arguments marked the display pending")
	}
	component.UpdateArgs(json.RawMessage(`{"path":"b.txt","content":"body"}`))
	if !component.argsPending {
		t.Fatal("changed raw arguments did not mark the display pending")
	}
}

func BenchmarkToolArgsRepeat(b *testing.B) {
	_, cleanup := newTestAppB(b)
	defer cleanup()
	raw := json.RawMessage(`{"path":"a.txt","content":"` + strings.Repeat("x", 1<<20) + `"}`)
	component := NewToolExecutionComponent("write", "id", raw, ToolExecutionOptions{}, nil, nil, "")
	b.ReportAllocs()
	for b.Loop() {
		for range 100 {
			component.UpdateArgs(raw)
			_ = component.Render(80)
		}
	}
}
