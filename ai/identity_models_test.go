package ai

import "testing"

// Reference: pi v1.4.0 models.ts applyAuth and test/identity.test.ts
// (adc5fa9f8/04a93bad8). Models defaults are merged below each request.
type identityStreams struct {
	capture func(*Model, *StreamOptions)
}

func (s identityStreams) Stream(model *Model, _ TranscriptContext, options *StreamOptions) *AssistantMessageEventStream {
	s.capture(model, options)
	return doneStream()
}

func (s identityStreams) StreamSimple(model *Model, _ TranscriptContext, options *SimpleStreamOptions) *AssistantMessageEventStream {
	s.capture(model, &options.StreamOptions)
	return doneStream()
}

func (s identityStreams) FetchDeferred(model *Model, _ *DeferredHandle, options *StreamOptions) *AssistantMessageEventStream {
	s.capture(model, options)
	return doneStream()
}

func (s identityStreams) CancelDeferred(model *Model, _ *DeferredHandle, options *StreamOptions) error {
	s.capture(model, options)
	return nil
}

func TestModelsIdentityAcrossRequestMethods(t *testing.T) {
	models := CreateModels(&CreateModelsOptions{Identity: &ClientIdentity{UserAgent: "short"}})
	model := &Model{ID: "m", Provider: "p", API: APIOpenAICompletions}
	var captured *ClientIdentity
	var capturedModel *Model
	models.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "p", Models: []*Model{model},
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "p", Resolve: func(AuthResolveInput) (*AuthResult, error) {
			return &AuthResult{Auth: ModelAuth{APIKey: "key", BaseURL: "https://resolved.example"}}, nil
		}}},
		Single: identityStreams{capture: func(requestModel *Model, options *StreamOptions) {
			captured, capturedModel = options.Identity, requestModel
		}},
	}))
	for _, method := range []string{"complete", "simple", "deferred", "cancel"} {
		t.Run(method, func(t *testing.T) {
			captured = nil
			var err error
			switch method {
			case "complete":
				_, err = models.Complete(model, Context{}, nil)
			case "simple":
				_, err = models.CompleteSimple(model, Context{}, nil)
			case "deferred":
				_, err = models.FetchDeferred(model, &DeferredHandle{}, nil)
			case "cancel":
				err = models.CancelDeferred(model, &DeferredHandle{}, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			if captured == nil || FormatUserAgent(captured) != "pi" {
				t.Fatalf("identity = %+v", captured)
			}
			if capturedModel.BaseURL != "https://resolved.example" || model.BaseURL != "" {
				t.Fatalf("request model = %+v; source = %+v", capturedModel, model)
			}
		})
	}
}

func TestModelsDefaultIdentity(t *testing.T) {
	defaults := &ClientIdentity{UserAgent: "short", Attribution: PIAttribution, Version: "base"}
	models := CreateModels(&CreateModelsOptions{Identity: defaults})
	var captured *ClientIdentity
	model := &Model{ID: "m", Provider: "p", API: APIOpenAICompletions}
	models.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "p", Models: []*Model{model},
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "p", Resolve: func(AuthResolveInput) (*AuthResult, error) {
			return &AuthResult{Auth: ModelAuth{APIKey: "key"}}, nil
		}}},
		Single: capturingStreams{streamSimple: func(_ *Model, _ TranscriptContext, options *SimpleStreamOptions) *AssistantMessageEventStream {
			captured = options.Identity
			return doneStream()
		}},
	}))
	if _, err := models.CompleteSimple(model, Context{}, nil); err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.UserAgent != "short" || captured.Version != "base" || captured.Attribution != PIAttribution {
		t.Fatalf("default identity = %+v", captured)
	}
	request := &ClientIdentity{VersionSet: true, AttributionSet: true}
	if _, err := models.CompleteSimple(model, Context{}, &ModelsSimpleStreamOptions{
		Identity: request}); err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.UserAgent != "short" || captured.Version != "" || !captured.VersionSet || captured.Attribution != nil || !captured.AttributionSet {
		t.Fatalf("request identity = %+v", captured)
	}
	if defaults.Attribution != PIAttribution || defaults.Version != "base" || request.UserAgent != "" {
		t.Fatal("request merge changed caller-owned identities")
	}
}
