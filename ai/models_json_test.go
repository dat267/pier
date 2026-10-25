package ai

import (
	"encoding/json"
	"reflect"
	"testing"
)

// pi v1.4.0 (4a080252), types.ts Model.compat and
// coding-agent/src/core/remote-catalog-provider.ts parseCatalog retain the
// enclosing API's compatibility object without guessing from its keys.
func TestModelJSONCompatUsesAPI(t *testing.T) {
	no := false
	for _, test := range []struct {
		api    Api
		compat string
		want   *ModelCompat
	}{
		{APIOpenAICompletions, `{"supportsReasoningEffort":false}`, &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{SupportsReasoningEffort: &no}}},
		{APIAnthropicMessages, `{"supportsTemperature":false}`, &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsTemperature: &no}}},
		{APIBedrockConverse, `{"supportsStrictMode":false}`, &ModelCompat{Bedrock: &BedrockCompat{SupportsStrictMode: &no}}},
		{APIMistralConversations, `{"supportsMidConvoSystemMessages":false}`, &ModelCompat{MistralConversations: &MistralConversationsCompat{SupportsMidConvoSystemMessages: &no}}},
		{APIOpenAIResponses, `{"supportsMidConvoSystemMessages":false}`, &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &no}}},
		{APIAzureOpenAIResponses, `{"supportsMidConvoSystemMessages":false}`, &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &no}}},
		{APIOpenAICodexResponses, `{"supportsMidConvoSystemMessages":false}`, &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &no}}},
	} {
		t.Run(string(test.api), func(t *testing.T) {
			var model Model
			if err := json.Unmarshal([]byte(`{"compat":`+test.compat+`,"api":"`+string(test.api)+`"}`), &model); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(model.Compat, test.want) {
				t.Fatalf("compat = %+v, want %+v", model.Compat, test.want)
			}
		})
	}
}
