package ai

import "testing"

type identityImages struct{ capture func(*ClientIdentity) }

func (s identityImages) GenerateImages(_ *ImagesModel, _ ImagesContext, options *ImagesOptions) *AssistantImages {
	s.capture(options.Identity)
	return &AssistantImages{StopReason: ImagesStopStop}
}

// Reference: pi v1.4.0 models.ts generateImages/applyAuth. This port keeps
// image providers in a separate registry but shares CreateModelsOptions.
func TestImagesModelsDefaultIdentity(t *testing.T) {
	for _, resolved := range []bool{false, true} {
		defaults := &ClientIdentity{UserAgent: "short", Attribution: PIAttribution}
		models := CreateImagesModels(&CreateModelsOptions{Identity: defaults})
		model := &ImagesModel{ID: "m", Provider: "p", API: KnownImagesApiOpenRouter}
		var captured *ClientIdentity
		auth := ProviderAuth{}
		if resolved {
			auth.APIKey = &ApiKeyAuth{Name: "p", Resolve: func(AuthResolveInput) (*AuthResult, error) {
				return &AuthResult{Auth: ModelAuth{APIKey: "key"}}, nil
			}}
		}
		models.SetProvider(CreateImagesProvider(ImageProviderOptions{
			ID: "p", Models: []*ImagesModel{model}, Auth: auth,
			API: identityImages{capture: func(identity *ClientIdentity) { captured = identity }},
		}))
		models.GenerateImages(model, ImagesContext{}, nil)
		if captured == nil || captured.UserAgent != "short" || captured.Attribution != PIAttribution {
			t.Fatalf("resolved %t: default identity = %+v", resolved, captured)
		}
		request := &ClientIdentity{AttributionSet: true}
		models.GenerateImages(model, ImagesContext{}, &ImagesOptions{Identity: request})
		if captured == nil || captured.UserAgent != "short" || captured.Attribution != nil || !captured.AttributionSet {
			t.Fatalf("resolved %t: request identity = %+v", resolved, captured)
		}
		if defaults.Attribution != PIAttribution || request.UserAgent != "" {
			t.Fatal("image request changed caller-owned identities")
		}
	}
}
