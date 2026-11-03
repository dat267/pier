package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), api/openai-completions.ts emits
// appendGrammarToolInputJsonDelta's JSON wrapper/escapes, not raw custom input.
func TestOpenAICompletionsCustomDeltasFormTheToolArguments(t *testing.T) {
	server := sseServer(t, []string{
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","custom":{"name":"expression","input":"print(\""}}]}}]}` + "\n\n",
		"data: " + `{"choices":[{"delta":{"tool_calls":[{"index":0,"custom":{"input":"hi\")\n"}}]}}]}` + "\n\n",
		"data: " + `{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}` + "\n\ndata: [DONE]\n\n",
	}, nil)
	defer server.Close()
	model := testOpenAIModel()
	model.BaseURL = server.URL
	model.Compat = &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{SupportsOpenAIGrammarTools: new(true)}}
	transcript := NormalizeContext(Context{Tools: []Tool{{
		Name:       "expression",
		Parameters: json.RawMessage(`{"type":"object","properties":{"payload":{"type":"string"}},"required":["payload"]}`),
		ConstrainedSampling: ConstrainedSamplingValue{Set: true, Config: &ConstrainedSamplingConfig{
			Type: "grammar", Variants: map[string]string{"openai_regex": ".+"},
		}},
	}}})
	stream := StreamOpenAICompletions(model, transcript, &OpenAICompletionsOptions{APIKey: "test-key"})
	var deltas strings.Builder
	for _, event := range stream.Events(context.Background()) {
		if event.Type == EventToolcallDelta {
			deltas.WriteString(event.Delta)
		}
	}
	const want = `{"payload":"print(\"hi\")\n"}`
	if got := deltas.String(); got != want {
		t.Errorf("tool-call delta JSON = %q, want %q", got, want)
	}
	message, err := stream.Result(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if message.StopReason != StopToolUse || len(message.Content) != 1 {
		t.Fatalf("message = %+v, want one finished custom call", message)
	}
	call, ok := message.Content[0].(ToolCall)
	if !ok || string(call.Arguments) != want {
		t.Fatalf("call = %+v, want arguments %s", message.Content[0], want)
	}
}
