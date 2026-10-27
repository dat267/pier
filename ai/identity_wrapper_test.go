package ai

import "testing"

// Reference: pi v1.4.0 utils/identity.ts withClientIdentity and
// test/identity.test.ts (adc5fa9f8/04a93bad8). The outer wrapper wins over
// earlier wrappers and registry defaults; a request still wins over all.
type readOnlyRegistry struct{ ModelRegistry }

func TestWithClientIdentityMutability(t *testing.T) {
	models := CreateModels(nil)
	wrapped := WithClientIdentity(WithClientIdentity(models, &ClientIdentity{UserAgent: "short"}), &ClientIdentity{})
	mutable, ok := wrapped.(MutableModelRegistry)
	if !ok {
		t.Fatal("mutable registry became read-only")
	}
	model := &Model{ID: "m", Provider: "p"}
	provider := CreateProvider(CreateProviderOptions{ID: "p", Models: []*Model{model}})
	mutable.SetProvider(provider)
	if wrapped.GetProvider("p") != provider || wrapped.GetModel("p", "m") != model || models.GetProvider("p") != provider {
		t.Fatal("wrapper copied or hid registry state")
	}
	mutable.DeleteProvider("p")
	if models.GetProvider("p") != nil {
		t.Fatal("delete was not delegated")
	}
	mutable.SetProvider(provider)
	mutable.ClearProviders()
	if len(models.GetProviders()) != 0 {
		t.Fatal("clear was not delegated")
	}
	readOnly := WithClientIdentity(readOnlyRegistry{models}, &ClientIdentity{})
	if _, ok := readOnly.(MutableModelRegistry); ok {
		t.Fatal("read-only registry gained mutation methods")
	}
}

func TestWithClientIdentityPrecedence(t *testing.T) {
	base := &ClientIdentity{UserAgent: "short", Attribution: PIAttribution, Version: "base"}
	models := CreateModels(&CreateModelsOptions{Identity: base})
	model := &Model{ID: "m", Provider: "p", API: APIOpenAICompletions}
	var captured *ClientIdentity
	models.SetProvider(CreateProvider(CreateProviderOptions{
		ID: "p", Models: []*Model{model},
		Auth: ProviderAuth{APIKey: &ApiKeyAuth{Name: "p", Resolve: func(AuthResolveInput) (*AuthResult, error) {
			return &AuthResult{Auth: ModelAuth{APIKey: "key"}}, nil
		}}},
		Single: identityStreams{capture: func(_ *Model, options *StreamOptions) { captured = options.Identity }},
	}))
	inner := &ClientIdentity{Version: "inner", Attribution: &ClientAttribution{Name: "inner", URL: "https://inner.example"}}
	outer := &ClientIdentity{Attribution: &ClientAttribution{Name: "outer", URL: "https://outer.example"}}
	wrapped := WithClientIdentity(WithClientIdentity(models, inner), outer)
	if _, err := wrapped.CompleteSimple(model, Context{}, nil); err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.UserAgent != "short" || captured.Version != "inner" || captured.Attribution != outer.Attribution {
		t.Fatalf("wrapped identity = %+v", captured)
	}
	request := &ClientIdentity{AttributionSet: true, VersionSet: true}
	options := &ModelsSimpleStreamOptions{Identity: request}
	if _, err := wrapped.CompleteSimple(model, Context{}, options); err != nil {
		t.Fatal(err)
	}
	if captured.Attribution != nil || captured.Version != "" || !captured.VersionSet || captured.UserAgent != "short" {
		t.Fatalf("request identity = %+v", captured)
	}
	if options.Identity != request || request.UserAgent != "" || outer.Version != "" || base.Version != "base" {
		t.Fatal("wrapper changed caller-owned options or identities")
	}
}
