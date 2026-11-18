package ai

import (
	"context"
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

type setupCancelStreams struct{}

func (setupCancelStreams) Stream(model *Model, _ TranscriptContext, _ *StreamOptions) *AssistantMessageEventStream {
	stream := NewAssistantMessageEventStream()
	message := &AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: StopStop}
	stream.End(&message)
	return stream
}

func (s setupCancelStreams) StreamSimple(model *Model, context TranscriptContext, options *SimpleStreamOptions) *AssistantMessageEventStream {
	return s.Stream(model, context, &options.StreamOptions)
}

// A request canceled while auth resolves is an abort. Upstream's applyAuth calls
// signal.throwIfAborted, and the port reports its canonical abort text instead
// of Go's "context canceled" (D256).
func TestModelsStreamSetupFailureOnCancelReportsAbort(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	models := CreateModels(nil)
	models.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "p", Name: "provider",
		Auth: ProviderAuth{OAuth: &OAuthAuth{
			Name: "test",
			Refresh: func(_ *OAuthCredential, _ context.Context) (*OAuthCredential, error) {
				cancel()
				return &OAuthCredential{
					Access: "a2", Refresh: "r2", Expires: time.Now().Add(time.Hour).UnixMilli()}, nil
			},
			ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
				return &ModelAuth{APIKey: credential.Access}, nil
			},
		}},
		Models: []*Model{{ID: "m", API: APIOpenAICompletions, Provider: "p"}},
		Single: setupCancelStreams{},
	}))
	if _, err := models.credentials.Modify("p", func(*Credential) (*Credential, error) {
		return &Credential{Type: CredentialOAuth, OAuth: &OAuthCredential{OAuthCredentials{
			Access: "a1", Refresh: "r1", Expires: 0,
		}}}, nil
	}, context.Background()); err != nil {
		t.Fatal(err)
	}
	model := models.GetModel("p", "m")
	if model == nil {
		t.Fatal("model missing")
	}
	message, _ := models.Stream(model, Context{}, &ModelsStreamOptions{
		Ctx: ctx,
	}).Result(context.Background())
	if message.StopReason != StopError || message.ErrorMessage == nil ||
		*message.ErrorMessage != RequestAbortedMessage {
		t.Fatalf("message = %+v; want %q", message, RequestAbortedMessage)
	}
}

// pi v1.4.0 (4a080252), models.ts createProvider publishes a fetched overlay
// through context.publish's Update closure. Go must synchronize that assignment
// with concurrent public GetModels calls, just as it does restored overlays.
func TestDynamicProviderRefreshSynchronizesCatalogReads(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	provider := CreateProvider(CreateProviderOptions{
		ID:     "test-dynamic",
		Single: capturingStreams{},
		FetchModels: func(*RefreshModelsContext) ([]*Model, error) {
			close(started)
			<-release
			return []*Model{{ID: "fresh", Provider: "test-dynamic"}}, nil
		},
	})
	refreshDone := make(chan error, 1)
	readDone := make(chan struct{})
	go func() {
		refreshDone <- provider.RefreshModels(&RefreshModelsContext{
			Ctx:          context.Background(),
			AllowNetwork: true,
			Publish: func(publication ModelsPublication) bool {
				publication.Update()
				return true
			},
		})
	}()
	<-started
	go func() {
		provider.GetModels()
		close(readDone)
	}()
	close(release)
	if err := <-refreshDone; err != nil {
		t.Fatal(err)
	}
	<-readDone
	models := provider.GetModels()
	if len(models) != 1 || models[0].ID != "fresh" {
		t.Fatalf("refreshed catalog = %+v; want fresh model", models)
	}
}

// pi v1.4.0 (4a080252), models.ts createProvider invokes context.publish
// outside model-access guards; its synchronous Update closure restores models.
// A host publication callback may read the provider's catalog after updating it.
func TestStoredProviderPublicationCanReadModels(t *testing.T) {
	provider := CreateProvider(CreateProviderOptions{
		ID:     "test-dynamic",
		Single: capturingStreams{},
		FetchModels: func(*RefreshModelsContext) ([]*Model, error) {
			t.Error("offline restore must not fetch")
			return nil, nil
		},
	})
	done := make(chan []*Model, 1)
	go func() {
		var published []*Model
		provider.RefreshModels(&RefreshModelsContext{
			Ctx: context.Background(),
			Stored: &ModelsStoreEntry{Models: []*Model{
				{ID: "restored", Provider: "test-dynamic"},
			}},
			Publish: func(publication ModelsPublication) bool {
				publication.Update()
				published = provider.GetModels()
				return true
			},
		})
		done <- published
	}()
	select {
	case models := <-done:
		if len(models) != 1 || models[0].ID != "restored" {
			t.Fatalf("published catalog = %+v; want restored model", models)
		}
	case <-time.After(time.Second):
		t.Fatal("publication callback deadlocked reading the provider's catalog")
	}
}
