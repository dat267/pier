package ai

import "testing"

// Reference: pi v1.4.0 utils/identity.ts withClientIdentity.generateImages.
type readOnlyImageRegistry struct{ ImageModelRegistry }

func TestWithImagesClientIdentityMutability(t *testing.T) {
	models := CreateImagesModels(nil)
	wrapped := WithImagesClientIdentity(WithImagesClientIdentity(models, &ClientIdentity{}), &ClientIdentity{})
	mutable, ok := wrapped.(MutableImageModelRegistry)
	if !ok {
		t.Fatal("mutable image registry became read-only")
	}
	model := &ImagesModel{ID: "m", Provider: "p"}
	provider := CreateImagesProvider(ImageProviderOptions{ID: "p", Models: []*ImagesModel{model}, API: OpenRouterImages{}})
	mutable.SetProvider(provider)
	if wrapped.GetProvider("p") != provider || wrapped.GetModel("p", "m") != model || models.GetProvider("p") != provider {
		t.Fatal("image wrapper copied or hid registry state")
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
	readOnly := WithImagesClientIdentity(readOnlyImageRegistry{models}, &ClientIdentity{})
	if _, ok := readOnly.(MutableImageModelRegistry); ok {
		t.Fatal("read-only image registry gained mutation methods")
	}
}

func TestWithImagesClientIdentityPrecedence(t *testing.T) {
	models := CreateImagesModels(&CreateModelsOptions{Identity: &ClientIdentity{UserAgent: "short"}})
	model := &ImagesModel{ID: "m", Provider: "p", API: KnownImagesApiOpenRouter}
	var captured *ClientIdentity
	models.SetProvider(CreateImagesProvider(ImageProviderOptions{
		ID: "p", Models: []*ImagesModel{model},
		API: identityImages{capture: func(identity *ClientIdentity) { captured = identity }},
	}))
	wrapped := WithImagesClientIdentity(WithImagesClientIdentity(models, &ClientIdentity{Version: "inner"}), &ClientIdentity{Attribution: PIAttribution})
	wrapped.GenerateImages(model, ImagesContext{}, nil)
	if captured == nil || captured.UserAgent != "short" || captured.Version != "inner" || captured.Attribution != PIAttribution {
		t.Fatalf("wrapped identity = %+v", captured)
	}
	request := &ClientIdentity{AttributionSet: true, VersionSet: true}
	options := &ImagesOptions{Identity: request}
	wrapped.GenerateImages(model, ImagesContext{}, options)
	if captured.Attribution != nil || captured.Version != "" || !captured.VersionSet || captured.UserAgent != "short" {
		t.Fatalf("request identity = %+v", captured)
	}
	if options.Identity != request || request.UserAgent != "" {
		t.Fatal("wrapper changed the caller's options")
	}
}
