package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) api/azure-openai-responses.ts declares
// `AzureOpenAIResponsesOptions` without a `serviceTier` field and never reads
// one, so the azure request body never carries `service_tier`. The port embeds
// the shared OpenAIResponsesOptions in AzureOpenAIResponsesOptions, so a caller
// that set ServiceTier made the shared builder send a field the reference does
// not (A453).
func TestAzureResponsesOmitsServiceTier(t *testing.T) {
	temp := 0.3
	options := func() *OpenAIResponsesOptions {
		return &OpenAIResponsesOptions{
			StreamOptions: StreamOptions{MaxTokens: new(1000), Temperature: &temp},
			ServiceTier:   "flex",
		}
	}
	context := NormalizeContext(Context{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}})
	body := func(t *testing.T, api Api) string {
		t.Helper()
		model := &Model{ID: "gpt-4.1", Name: "gpt-4.1", API: api, Provider: "azure",
			BaseURL: "https://invalid.test/v1", Input: []string{"text"},
			ContextWindow: 128000, MaxTokens: 8192}
		params, err := BuildOpenAIResponsesParams(model, context, options(), nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := MarshalJSON(params)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}

	azure := body(t, APIAzureOpenAIResponses)
	if strings.Contains(azure, "service_tier") {
		t.Errorf("azure body carries service_tier: %s", azure)
	}
	// Control: the openai-responses adapter does forward the tier.
	openai := body(t, APIOpenAIResponses)
	if !strings.Contains(openai, `"service_tier":"flex"`) {
		t.Errorf("openai body lost service_tier: %s", openai)
	}
	// The rest of the body is unchanged: only the tier field goes away.
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(azure), &decoded); err != nil {
		t.Fatal(err)
	}
	if string(decoded["max_output_tokens"]) != "1000" {
		t.Errorf("azure max_output_tokens = %s, want 1000; body %s", decoded["max_output_tokens"], azure)
	}
}
