package ai

import (
	"context"
	"testing"
)

// Port of coding-agent's withVirtualModels provider wrapper.

type fakeProviderStreams struct{}

func (fakeProviderStreams) Stream(model *Model, _ TranscriptContext, _ *StreamOptions) *AssistantMessageEventStream {
	return ErrorStreamForModel(model, ErrCodeStream, "physical")
}

func (fakeProviderStreams) StreamSimple(model *Model, _ TranscriptContext, _ *SimpleStreamOptions) *AssistantMessageEventStream {
	return ErrorStreamForModel(model, ErrCodeStream, "physical")
}

func TestWrapProviderWithVirtualModels(t *testing.T) {
	const virtualAPI = "pi-virtual"
	physical := &Model{ID: "sonnet", Provider: "p", API: "messages"}
	hidden := &Model{ID: "auto", Provider: "p", API: "messages"}
	provider := CreateProvider(CreateProviderOptions{
		ID: "p", Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "p"}},
		Models: []*Model{physical, hidden}, Single: fakeProviderStreams{},
	})
	virtual := &Model{ID: "auto", Name: "Auto", Provider: "p", API: virtualAPI}
	wrapped := WrapProviderWithVirtualModels("p", provider, virtualAPI, []*Model{virtual})

	models := wrapped.GetModels()
	if len(models) != 2 || models[0].ID != "sonnet" || models[1].ID != "auto" || models[1].API != virtualAPI {
		t.Fatalf("models = %+v", models)
	}
	// Filtering keeps the virtual models regardless of credentials.
	filtered := wrapped.FilterModels([]*Model{physical, hidden, virtual}, nil)
	if len(filtered) != 2 || filtered[0].ID != "sonnet" || filtered[1].ID != "auto" || filtered[1].API != virtualAPI {
		t.Fatalf("filtered = %+v", filtered)
	}
	// Streaming a virtual model fails until it is routed.
	stream := wrapped.StreamSimple(virtual, TranscriptContext{}, &SimpleStreamOptions{})
	event, ok := stream.Next(context.Background())
	if !ok || event.Type != EventError || event.Error == nil {
		t.Fatalf("event = %+v, %v", event, ok)
	}
	// A physical model still reaches the wrapped implementation.
	physicalStream := wrapped.StreamSimple(physical, TranscriptContext{}, &SimpleStreamOptions{})
	physicalEvent, _ := physicalStream.Next(context.Background())
	if physicalEvent.Type == EventError && physicalEvent.Error != nil && physicalEvent.Error.ErrorMessage != nil &&
		*physicalEvent.Error.ErrorMessage != "physical" {
		t.Fatalf("event = %+v", physicalEvent)
	}

	// A nil provider lists only the virtual models.
	only := WrapProviderWithVirtualModels("vp", nil, virtualAPI, []*Model{virtual})
	if models := only.GetModels(); len(models) != 1 || models[0].ID != "auto" {
		t.Fatalf("models = %+v", models)
	}
}
