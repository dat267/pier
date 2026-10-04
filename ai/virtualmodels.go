package ai

import "fmt"

// Port of the provider-wrapper half of coding-agent's core/virtual-models.ts:
// withVirtualModels adds virtual catalog entries to a provider's catalog and
// fails their stream if they were not routed first. The helper lives in `ai`
// because a provider's model accessor and stream dispatch are package-private.

// WrapProviderWithVirtualModels returns a provider whose catalog lists
// virtualModels after the physical models, hiding a physical model with the
// same id, and whose virtual (virtualAPI) models fail when streamed before
// routing. A nil provider yields a keyless provider that only lists the
// virtual models (upstream withVirtualModels).
func WrapProviderWithVirtualModels(providerID string, provider *Provider, virtualAPI string, virtualModels []*Model) *Provider {
	if provider == nil {
		return CreateProvider(CreateProviderOptions{
			ID:   providerID,
			Name: providerID,
			Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "Virtual model",
				Resolve: func(AuthResolveInput) (*AuthResult, error) {
					return &AuthResult{Source: "virtual"}, nil
				}}},
			ModelsGetter: func() []*Model { return virtualModels },
			Single:       unroutedStreams{},
		})
	}
	virtualIDs := make(map[string]bool, len(virtualModels))
	for _, model := range virtualModels {
		virtualIDs[model.ID] = true
	}
	physical := func(models []*Model) []*Model {
		out := make([]*Model, 0, len(models))
		for _, model := range models {
			if model.API == virtualAPI || virtualIDs[model.ID] {
				continue
			}
			out = append(out, model)
		}
		return out
	}
	virtual := func(models []*Model) []*Model {
		out := []*Model{}
		for _, model := range models {
			if model.API == virtualAPI {
				out = append(out, model)
			}
		}
		return out
	}
	clone := *provider
	base := provider.getModels
	clone.getModels = func() []*Model {
		return append(physical(base()), virtualModels...)
	}
	originalFilter := provider.FilterModels
	clone.FilterModels = func(models []*Model, credential *Credential) []*Model {
		real := physical(models)
		if originalFilter != nil {
			real = originalFilter(real, credential)
		}
		return append(real, virtual(models)...)
	}
	originalStreams := provider.streamsFor
	clone.streamsFor = func(model *Model) ProviderStreams {
		if model.API == virtualAPI {
			return unroutedStreams{}
		}
		return originalStreams(model)
	}
	return &clone
}

// unroutedStreams fails every stream: a virtual model must be routed first.
type unroutedStreams struct{}

func (unroutedStreams) Stream(model *Model, _ TranscriptContext, _ *StreamOptions) *AssistantMessageEventStream {
	return unroutedStream(model)
}

func (unroutedStreams) StreamSimple(model *Model, _ TranscriptContext, _ *SimpleStreamOptions) *AssistantMessageEventStream {
	return unroutedStream(model)
}

func unroutedStream(model *Model) *AssistantMessageEventStream {
	return ErrorStreamForModel(model, ErrCodeStream,
		fmt.Sprintf("Virtual model %s/%s must be routed before streaming", model.Provider, model.ID))
}
