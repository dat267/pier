package ai

import "testing"

// The request effort comes from the model's thinkingLevelMap when the selected
// level is mapped, and an unmapped level sends its own name
// (openai responses stream.ts). GitHub Copilot omits the reasoning block
// entirely when thinking is off.
func TestOpenAIResponsesReasoningEffortMapping(t *testing.T) {
	mapped := "xhigh"
	model := &Model{
		ID: "gpt-5", Name: "GPT-5", API: APIOpenAIResponses, Provider: "openai",
		BaseURL: "https://api.openai.com/v1", Input: []string{"text"}, Reasoning: true,
		ThinkingLevelMap: ThinkingLevelMap{ThinkHigh: &mapped},
	}
	compat := GetOpenAIResponsesCompat(model)
	context := TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}
	params, err := BuildOpenAIResponsesParams(model, context, &OpenAIResponsesOptions{ReasoningEffort: ThinkHigh}, &compat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if params.Reasoning == nil || params.Reasoning.Effort != "xhigh" {
		t.Fatalf("reasoning = %+v, want the mapped effort xhigh", params.Reasoning)
	}

	copilot := *model
	copilot.Provider = "github-copilot"
	offCompat := GetOpenAIResponsesCompat(&copilot)
	params, err = BuildOpenAIResponsesParams(&copilot, context, nil, &offCompat, nil)
	if err != nil {
		t.Fatal(err)
	}
	if params.Reasoning != nil {
		t.Fatalf("copilot reasoning at off = %+v, want omitted", params.Reasoning)
	}
}
