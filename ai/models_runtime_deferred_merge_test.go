package ai

import (
	"context"
	"strings"
	"testing"
)

// A provider whose implementation is not a DeferredStreamer reports the
// provider error instead of calling a method on a nil interface
// (upstream fetchDeferred checks the implementation first).
func TestProviderFetchDeferredWithoutSupportReturnsAnError(t *testing.T) {
	provider := CreateProvider(CreateProviderOptions{
		ID: "plain", Name: "Plain",
		Models: []*Model{{ID: "m", API: APIOpenAICompletions, Provider: "plain"}},
		Single: funcStreams{},
	})
	model := provider.GetModelsOfType("chat")[0]
	stream := provider.FetchDeferred(model, &DeferredHandle{Provider: "plain", ModelID: "m", ID: "r1"}, nil)
	message, err := stream.Result(context.Background())
	if err == nil && message != nil && message.ErrorMessage != nil &&
		strings.Contains(*message.ErrorMessage, "does not support deferred responses") {
		return
	}
	if err != nil && strings.Contains(err.Error(), "does not support deferred responses") {
		return
	}
	if message != nil && message.StopReason != StopError {
		t.Fatalf("message = %+v, err = %v; want the deferred-support error", message, err)
	}
	if message == nil && err == nil {
		t.Fatal("no error reported for an unsupported deferred fetch")
	}
}

// A restored model replaces the baseline model at the same position, including
// index zero, instead of being appended as a duplicate.
func TestProviderRefreshReplacesTheFirstModel(t *testing.T) {
	provider := CreateProvider(CreateProviderOptions{
		ID: "p", Name: "p",
		Models: []*Model{
			{ID: "first", API: APIOpenAICompletions, Provider: "p", Name: "base-first"},
			{ID: "second", API: APIOpenAICompletions, Provider: "p", Name: "base-second"},
		},
		Single: funcStreams{},
	})
	if provider.RefreshModels == nil {
		t.Skip("provider has no refresh path")
	}
	err := provider.RefreshModels(&RefreshModelsContext{
		Stored: &ModelsStoreEntry{Models: []*Model{
			{ID: "first", API: APIOpenAICompletions, Provider: "p", Name: "restored-first"},
		}},
		Publish: func(publication ModelsPublication) bool {
			publication.Update()
			return true
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	models := provider.GetModelsOfType("chat")
	if len(models) != 2 {
		t.Fatalf("models = %d, want the baseline's two with the first replaced", len(models))
	}
	if models[0].Name != "restored-first" {
		t.Fatalf("first model = %+v, want the restored entry to replace it", models[0])
	}
}
