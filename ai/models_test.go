package ai

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.5.2 (b4afdddf) serializes a model with JSON.stringify, which
// escapes only the characters JSON requires; a compat string such as a
// chat-template kwarg keeps "<", ">" and "&" as written. Pier encoded the compat
// arm with encoding/json, which writes them as \u003c, \u003e and \u0026.
func TestModelCompatJSONKeepsHTMLCharactersLiteral(t *testing.T) {
	kwargs := map[string]json.RawMessage{"prefix": json.RawMessage(`"<b>a & b</b>"`)}
	model := Model{
		ID: "m", Name: "M", API: APIOpenAICompletions, Provider: "p",
		BaseURL: "https://example.test", Input: []string{"text"},
		Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{ChatTemplateKwargs: kwargs}},
	}
	encoded, err := model.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"<b>a & b</b>"`) {
		t.Fatalf("compat string was escaped: %s", encoded)
	}
	if strings.Contains(string(encoded), `\u003c`) {
		t.Fatalf("compat string used HTML escaping: %s", encoded)
	}
}

// pi v1.4.0 (4a080252), models.ts Models.getModels directly delegates to
// each provider's getter; that getter can also read the registry's GetProvider.
func TestModelsGetterCanReadRegistry(t *testing.T) {
	for _, id := range []string{"test-reentrant", ""} {
		t.Run("provider="+id, func(t *testing.T) {
			registry := CreateModels(nil)
			registry.SetProvider(CreateProvider(CreateProviderOptions{
				ID:     "test-reentrant",
				Single: capturingStreams{},
				ModelsGetter: func() []*Model {
					if registry.GetProvider("test-reentrant") == nil {
						return nil
					}
					return []*Model{{ID: "model", Provider: "test-reentrant"}}
				},
			}))
			done := make(chan []*Model, 1)
			go func() { done <- registry.GetModels(id) }()
			select {
			case models := <-done:
				if len(models) != 1 || models[0].ID != "model" {
					t.Fatalf("models = %+v; want model", models)
				}
			case <-time.After(time.Second):
				t.Fatal("model getter deadlocked reading its registry")
			}
		})
	}
}

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
		// A models.json supportsMessageThreads entry overrides the threads
		// default (api/anthropic-messages.ts getAnthropicCompat).
		{APIAnthropicMessages, `{"supportsMessageThreads":false}`, &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsMessageThreads: &no}}},
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
