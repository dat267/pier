package ai

import (
	"encoding/json"
	"testing"
)

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
