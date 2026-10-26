package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// D223's installed goal extension, index.ts set_goal.execute, trims objectives
// with JavaScript String.trim. Source read SHA-256:
// 5b0d421772a0b3c37f6486092acf9301aeaf7bc2aa288574dff47ba1ce3d662e.
// Stock pi v1.4.0 has no goal tool. D234 changes tool actions, not objective
// whitespace: BOM-only is empty, and U+0085 is not JavaScript whitespace.
func TestSessionGoalObjectiveUsesJavaScriptTrim(t *testing.T) {
	for _, fixture := range []struct {
		name, objective, want string
		reject                bool
	}{
		{"bom-only", "\uFEFF", "", true},
		{"bom-boundaries", "\uFEFFfix\uFEFF", "fix", false},
		{"next-line-control", "\u0085fix\u0085", "\u0085fix\u0085", false},
		{"ascii-control", " fix ", "fix", false},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			manager, dir := newTestSession(t)
			session, err := NewAgentSession(&SessionConfig{Cwd: dir, Sessions: manager, StreamFn: stubStreamFn, Goal: true})
			if err != nil {
				t.Fatal(err)
			}
			tool := session.GetToolDefinition("set_goal")
			if tool == nil {
				t.Fatal("set_goal not registered")
			}
			params, err := ai.MarshalJSON(map[string]string{"objective": fixture.objective})
			if err != nil {
				t.Fatal(err)
			}
			result, err := tool.Execute("call", json.RawMessage(params), context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.IsError != fixture.reject {
				t.Fatalf("IsError = %v, want %v: %+v", result.IsError, fixture.reject, result.Content)
			}
			snapshot := session.Goal.Snapshot().Goal
			if fixture.reject {
				if snapshot != nil {
					t.Fatalf("blank objective created a goal: %+v", snapshot)
				}
			} else if snapshot == nil || snapshot.Objective != fixture.want {
				t.Fatalf("goal = %+v, want objective %q", snapshot, fixture.want)
			}
		})
	}
}
