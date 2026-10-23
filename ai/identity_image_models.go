package ai

// ImageModelRegistry is the image counterpart of ModelRegistry. The Go port
// keeps image and text registries separate; upstream Models exposes both.
type ImageModelRegistry interface {
	GetProviders() []*ImagesProvider
	GetProvider(string) *ImagesProvider
	GetModels(string) []*ImagesModel
	GetModel(string, string) *ImagesModel
	Refresh(string) error
	GetAuth(string, *AuthResolutionOverrides) (*AuthResult, error)
	GenerateImages(*ImagesModel, ImagesContext, *ImagesOptions) *AssistantImages
}

type MutableImageModelRegistry interface {
	ImageModelRegistry
	SetProvider(*ImagesProvider)
	DeleteProvider(string)
	ClearProviders()
}

// WithImagesClientIdentity is WithClientIdentity for the separate image
// registry. The result remains mutable only if the source is mutable.
func WithImagesClientIdentity(models ImageModelRegistry, identity *ClientIdentity) ImageModelRegistry {
	wrapped := &identityImageModels{ImageModelRegistry: models, identity: identity}
	if mutable, ok := models.(MutableImageModelRegistry); ok {
		return &mutableIdentityImageModels{identityImageModels: wrapped, mutable: mutable}
	}
	return wrapped
}

type mutableIdentityImageModels struct {
	*identityImageModels
	mutable MutableImageModelRegistry
}

func (m *mutableIdentityImageModels) SetProvider(provider *ImagesProvider) {
	m.mutable.SetProvider(provider)
}
func (m *mutableIdentityImageModels) DeleteProvider(id string) { m.mutable.DeleteProvider(id) }
func (m *mutableIdentityImageModels) ClearProviders()          { m.mutable.ClearProviders() }

type identityImageModels struct {
	ImageModelRegistry
	identity *ClientIdentity
}

func (m *identityImageModels) GenerateImages(model *ImagesModel, transcript ImagesContext, options *ImagesOptions) *AssistantImages {
	var merged ImagesOptions
	if options != nil {
		merged = *options
	}
	merged.Identity = MergeClientIdentity(m.identity, merged.Identity)
	return m.ImageModelRegistry.GenerateImages(model, transcript, &merged)
}
