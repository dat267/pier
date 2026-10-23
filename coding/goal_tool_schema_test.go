package coding

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// D234: set_goal is a port-local tool. A provider/harness can expose every
// property as required even without strict schema conversion, as happened in
// the reported objective+status retry loop. Unused arguments must accept null
// in that projection as well as in strict Responses schemas.
func TestGoalToolAcceptsNullForInactiveRequiredArguments(t *testing.T) {
	for _, strict := range []bool{false, true} {
		name := "non-strict"
		if strict {
			name = "strict"
		}
		t.Run(name, func(t *testing.T) {
			controller, _ := newGoalControllerTest(t)
			tool := findGoalTool(t, controller, "set_goal")
			tools, err := ai.ConvertResponsesTools([]ai.Tool{{
				Name: tool.Name, Parameters: tool.Parameters,
			}}, &ai.ConvertResponsesToolsOptions{Strict: &strict, SupportsStrictMode: true})
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]json.RawMessage
			if err := json.Unmarshal(tools[0].Parameters, &schema); err != nil {
				t.Fatal(err)
			}
			// Model the all-properties-required tool declaration seen by the
			// caller. Do not make property values nullable in this projection.
			schema["required"] = json.RawMessage(`["objective","status","blocked_reason"]`)
			parameters, err := ai.MarshalJSON(schema)
			if err != nil {
				t.Fatal(err)
			}
			wireTool := ai.Tool{Name: tool.Name, Parameters: parameters}
			for _, arguments := range []string{
				`{"objective":"fix the goal tool","status":null,"blocked_reason":null}`,
				`{"objective":null,"status":"complete","blocked_reason":null}`,
			} {
				validated, err := ai.ValidateToolArguments(wireTool, ai.ToolCall{
					Name: tool.Name, Arguments: json.RawMessage(arguments),
				})
				if err != nil {
					t.Fatalf("wire arguments %s: %v", arguments, err)
				}
				// Also exercise the normal agent validation against the tool's
				// original schema before handing the arguments to the executor.
				validated, err = ai.ValidateToolArguments(ai.Tool{
					Name: tool.Name, Parameters: tool.Parameters,
				}, ai.ToolCall{Name: tool.Name, Arguments: validated})
				if err != nil {
					t.Fatal(err)
				}
				result, err := tool.Execute("id", validated, context.Background(), func(agent.AgentToolResult) {})
				if err != nil || result.IsError {
					t.Fatalf("execute %s: result=%+v err=%v", arguments, result, err)
				}
			}
			if view := controller.Snapshot().Goal; view == nil || view.Phase != "complete" {
				t.Fatalf("goal was not completed: %+v", view)
			}
			for _, arguments := range []string{
				`{"objective":"another task","status":"clear","blocked_reason":null}`,
				`{"objective":null,"status":null,"blocked_reason":null}`,
			} {
				result, err := tool.Execute("id", json.RawMessage(arguments), context.Background(), func(agent.AgentToolResult) {})
				if err != nil || !result.IsError {
					t.Fatalf("invalid action %s: result=%+v err=%v", arguments, result, err)
				}
				if view := controller.Snapshot().Goal; view == nil || view.Phase != "complete" {
					t.Fatalf("invalid action changed the goal: %+v", view)
				}
			}
		})
	}
}
