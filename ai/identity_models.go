package ai

import "context"

// ModelRegistry is the supported read/request surface of Models. It permits
// identity views without copying a registry's locks, providers or auth state.
type ModelRegistry interface {
	GetProviders() []*Provider
	GetProvider(string) *Provider
	GetModels(string) []*Model
	GetModel(string, string) *Model
	Refresh(*ModelsRefreshOptions) ModelsRefreshResult
	CheckAuth(string, context.Context) (*AuthCheck, error)
	GetAvailable(string, context.Context) ([]*Model, error)
	GetAuth(string, *AuthResolutionOverrides) (*AuthResult, error)
	GetAuthForModel(*Model, *AuthResolutionOverrides) (*AuthResult, error)
	Login(string, AuthType, *AuthInteraction) (*Credential, error)
	Logout(string, context.Context) error
	Stream(*Model, Context, *ModelsStreamOptions) *AssistantMessageEventStream
	Complete(*Model, Context, *ModelsStreamOptions) (*AssistantMessage, error)
	StreamSimple(*Model, Context, *ModelsSimpleStreamOptions) *AssistantMessageEventStream
	CompleteSimple(*Model, Context, *ModelsSimpleStreamOptions) (*AssistantMessage, error)
	StreamDeferred(*Model, *DeferredHandle, *ModelsStreamOptions) *AssistantMessageEventStream
	FetchDeferred(*Model, *DeferredHandle, *ModelsStreamOptions) (*AssistantMessage, error)
	CancelDeferred(*Model, *DeferredHandle, *ModelsStreamOptions) error
}

// MutableModelRegistry preserves the three mutation methods of upstream's
// MutableModels. A read-only ModelRegistry does not expose them.
type MutableModelRegistry interface {
	ModelRegistry
	SetProvider(*Provider)
	DeleteProvider(string)
	ClearProviders()
}

// WithClientIdentity adds request defaults above the registry's defaults.
// Request fields win. Other operations are delegated to the same registry.
// The result implements MutableModelRegistry only when the source does.
func WithClientIdentity(models ModelRegistry, identity *ClientIdentity) ModelRegistry {
	wrapped := &identityModels{ModelRegistry: models, identity: identity}
	if mutable, ok := models.(MutableModelRegistry); ok {
		return &mutableIdentityModels{identityModels: wrapped, mutable: mutable}
	}
	return wrapped
}

type mutableIdentityModels struct {
	*identityModels
	mutable MutableModelRegistry
}

func (m *mutableIdentityModels) SetProvider(provider *Provider) { m.mutable.SetProvider(provider) }
func (m *mutableIdentityModels) DeleteProvider(id string)       { m.mutable.DeleteProvider(id) }
func (m *mutableIdentityModels) ClearProviders()                { m.mutable.ClearProviders() }

type identityModels struct {
	ModelRegistry
	identity *ClientIdentity
}

func (m *identityModels) simpleOptions(options *ModelsSimpleStreamOptions) *ModelsSimpleStreamOptions {
	var merged ModelsSimpleStreamOptions
	if options != nil {
		merged = *options
	}
	merged.Identity = MergeClientIdentity(m.identity, merged.Identity)
	return &merged
}

func (m *identityModels) options(options *ModelsStreamOptions) *ModelsStreamOptions {
	var merged ModelsStreamOptions
	if options != nil {
		merged = *options
	}
	merged.Identity = MergeClientIdentity(m.identity, merged.Identity)
	return &merged
}

func (m *identityModels) Stream(model *Model, transcript Context, options *ModelsStreamOptions) *AssistantMessageEventStream {
	return m.ModelRegistry.Stream(model, transcript, m.options(options))
}

func (m *identityModels) Complete(model *Model, transcript Context, options *ModelsStreamOptions) (*AssistantMessage, error) {
	return m.ModelRegistry.Complete(model, transcript, m.options(options))
}

func (m *identityModels) StreamSimple(model *Model, transcript Context, options *ModelsSimpleStreamOptions) *AssistantMessageEventStream {
	return m.ModelRegistry.StreamSimple(model, transcript, m.simpleOptions(options))
}

func (m *identityModels) CompleteSimple(model *Model, transcript Context, options *ModelsSimpleStreamOptions) (*AssistantMessage, error) {
	return m.ModelRegistry.CompleteSimple(model, transcript, m.simpleOptions(options))
}

func (m *identityModels) StreamDeferred(model *Model, handle *DeferredHandle, options *ModelsStreamOptions) *AssistantMessageEventStream {
	return m.ModelRegistry.StreamDeferred(model, handle, m.options(options))
}

func (m *identityModels) FetchDeferred(model *Model, handle *DeferredHandle, options *ModelsStreamOptions) (*AssistantMessage, error) {
	return m.ModelRegistry.FetchDeferred(model, handle, m.options(options))
}

func (m *identityModels) CancelDeferred(model *Model, handle *DeferredHandle, options *ModelsStreamOptions) error {
	return m.ModelRegistry.CancelDeferred(model, handle, m.options(options))
}
