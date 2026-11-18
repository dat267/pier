package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) agent-loop.ts createToolResultMessage builds
// the object as { role, toolCallId, toolName, content, details, usage, isError,
// durationMs?, timestamp }, and agent-session.ts then assigns
// `message.nestedCalls = summary.calls`, which JavaScript orders after every
// existing key. Pier put nestedCalls between usage and isError, so a result
// with nested calls persisted in a different byte order.
func TestToolResultMessagePutsNestedCallsAfterTimestamp(t *testing.T) {
	message := &ToolResultMessage{
		ToolCallID: "call-1",
		ToolName:   "bash",
		Content:    UserContentList{TextContent{Text: "ok"}},
		NestedCalls: &NestedToolCalls{
			Calls:    []NestedToolCallRecord{{ID: "n1", Name: "read", Status: "ok"}},
			Complete: true,
		},
		IsError:   false,
		Timestamp: 1700000000000,
	}
	encoded, err := MarshalMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	json := string(encoded)
	nested := strings.Index(json, `"nestedCalls"`)
	timestamp := strings.Index(json, `"timestamp"`)
	if nested < 0 || timestamp < 0 {
		t.Fatalf("missing keys: %s", json)
	}
	if nested < timestamp {
		t.Fatalf("nestedCalls before timestamp: %s", json)
	}
}

// Upstream interpolates names into "${name}" template literals, so a quote or
// backslash in a model-supplied tool name is copied, not Go-escaped as %q
// would. constrained-sampling.ts also ends the wrapped inference error with a
// period (pi v1.5.0 packages/ai/src/api/constrained-sampling.ts).
func TestToolMessagesInterpolateNamesLikeTemplateLiterals(t *testing.T) {
	tool := Tool{
		Name:       `say "hi"\now`,
		Parameters: json.RawMessage(`{"type":"array"}`),
		ConstrainedSampling: ConstrainedSamplingValue{
			Set: true, Config: &ConstrainedSamplingConfig{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /a/"}},
		},
	}
	_, err := ResolveGrammarConstrainedSampling(tool, true)
	want := `Tool "say "hi"\now" cannot use grammar constrained sampling: grammar constrained sampling requires an object parameter schema.`
	if err == nil || err.Error() != want {
		t.Fatalf("grammar error = %v\nwant %s", err, want)
	}

	// utils/validation.ts: `Invalid arguments for tool "${toolCall.name}": ...`
	_, err = ValidateToolArguments(tool, ToolCall{Name: tool.Name, ArgumentsError: "bad"})
	if want := `Invalid arguments for tool "say "hi"\now": bad`; err == nil || err.Error() != want {
		t.Fatalf("validation error = %v\nwant %s", err, want)
	}
}
