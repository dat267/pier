package ai

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
	"time"
)

// Port of image-models.ts: the image-generation counterpart of Models.

//go:embed image_catalog.json
var imageCatalogJSON []byte

// ImagesApi names an image-generation API implementation.
type ImagesApi = string

// KnownImagesApiOpenRouter is the only image API upstream ships.
const KnownImagesApiOpenRouter ImagesApi = "openrouter-images"

// ImagesStopReason explains why image generation ended.
type ImagesStopReason = string

const (
	ImagesStopStop    ImagesStopReason = "stop"
	ImagesStopError   ImagesStopReason = "error"
	ImagesStopAborted ImagesStopReason = "aborted"
)

// ImagesInputContent is one input block (text or image).
type ImagesInputContent = Content

// ImagesOutputContent is one output block (text or image).
type ImagesOutputContent = Content

// ImagesContext is the generation input.
type ImagesContext struct {
	Input []ImagesInputContent
}

// ImagesModel is one image-generation model (upstream ImagesModel).
type ImagesModel struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	API      ImagesApi  `json:"api"`
	Provider ProviderId `json:"provider"`
	BaseURL  string     `json:"baseUrl"`
	// Input lists the accepted modalities ("text" | "image").
	Input []string `json:"input"`
	// Output lists the produced modalities ("text" | "image").
	Output []string  `json:"output"`
	Cost   ModelCost `json:"cost"`
	// Headers are per-model request headers.
	Headers map[string]string `json:"headers,omitempty"`
}

// ImagesOptions are the image-generation request options.
type ImagesOptions struct {
	StreamOptions
	// Metadata is optional request metadata; providers extract what they use.
	Metadata map[string]any
}

// imagesTextOutput and imagesImageOutput mirror the blocks openrouter-images.ts
// pushes into output: the image block is built as {type, mimeType, data}, not in
// the ImageContent declaration order.
type imagesTextOutput struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type imagesImageOutput struct {
	Type     string `json:"type"`
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

// MarshalJSON emits the upstream AssistantImages member order and includes the
// output blocks (tagged `json:"-"` so the default encoder skips them).
func (a AssistantImages) MarshalJSON() ([]byte, error) {
	node := &orderedJSONFields{values: map[string]json.RawMessage{}}
	set := func(name string, value any) error {
		raw, err := MarshalJSON(value)
		if err != nil {
			return err
		}
		node.set(name, raw)
		return nil
	}
	for _, name := range []string{"api", "provider"} {
		value := any(a.API)
		if name == "provider" {
			value = a.Provider
		}
		if err := set(name, value); err != nil {
			return nil, err
		}
	}
	if err := set("model", a.Model); err != nil {
		return nil, err
	}
	blocks := make([]json.RawMessage, 0, len(a.Output))
	for _, block := range a.Output {
		encoded, err := marshalImagesOutputContent(block)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, encoded)
	}
	encodedBlocks, err := MarshalJSON(blocks)
	if err != nil {
		return nil, err
	}
	node.set("output", encodedBlocks)
	if err := set("stopReason", a.StopReason); err != nil {
		return nil, err
	}
	if err := set("timestamp", a.Timestamp); err != nil {
		return nil, err
	}
	// Upstream assigns these after the literal that creates the result, so they
	// follow stopReason/timestamp in the serialized object.
	if a.ResponseID != nil {
		if err := set("responseId", *a.ResponseID); err != nil {
			return nil, err
		}
	}
	if a.Usage != nil {
		if err := set("usage", a.Usage); err != nil {
			return nil, err
		}
	}
	if a.ErrorMessage != nil {
		if err := set("errorMessage", *a.ErrorMessage); err != nil {
			return nil, err
		}
	}
	return node.marshal()
}

func marshalImagesOutputContent(block Content) (json.RawMessage, error) {
	switch typed := block.(type) {
	case TextContent:
		return MarshalJSON(imagesTextOutput{Type: string(KindText), Text: typed.Text})
	case *TextContent:
		return MarshalJSON(imagesTextOutput{Type: string(KindText), Text: typed.Text})
	case ImageContent:
		return MarshalJSON(imagesImageOutput{Type: string(KindImage), MimeType: typed.MimeType, Data: typed.Data})
	case *ImageContent:
		return MarshalJSON(imagesImageOutput{Type: string(KindImage), MimeType: typed.MimeType, Data: typed.Data})
	default:
		return marshalContentBlock(block)
	}
}

// AssistantImages is the generation result.
type AssistantImages struct {
	API          ImagesApi             `json:"api"`
	Provider     ProviderId            `json:"provider"`
	Model        string                `json:"model"`
	Output       []ImagesOutputContent `json:"-"`
	ResponseID   *string               `json:"responseId,omitempty"`
	Usage        *Usage                `json:"usage,omitempty"`
	StopReason   ImagesStopReason      `json:"stopReason"`
	ErrorMessage *string               `json:"errorMessage,omitempty"`
	Timestamp    int64                 `json:"timestamp"`
}

// ProviderImages is the uniform image API implementation (upstream
// ProviderImages).
type ProviderImages interface {
	GenerateImages(model *ImagesModel, context ImagesContext, options *ImagesOptions) *AssistantImages
}

// ImagesProvider is one image-generation provider (upstream ImagesProvider).
type ImagesProvider struct {
	ID   string
	Name string
	Auth ProviderAuth

	mu        sync.Mutex
	models    []*ImagesModel
	inflight  *sync.WaitGroup
	refreshFn func() ([]*ImagesModel, error)
	generate  ProviderImages
}

// ImageProviderOptions configure CreateImagesProvider.
type ImageProviderOptions struct {
	ID   string
	Name string
	Auth ProviderAuth
	// Models is the initial model list (empty for purely dynamic providers).
	Models []*ImagesModel
	// RefreshModels fetches the current list for dynamic providers. Concurrent
	// calls share one in-flight fetch.
	RefreshModels func() ([]*ImagesModel, error)
	API           ProviderImages
}

// CreateImagesProvider builds an image provider from parts (upstream
// createImagesProvider).
func CreateImagesProvider(options ImageProviderOptions) *ImagesProvider {
	name := options.Name
	if name == "" {
		name = options.ID
	}
	return &ImagesProvider{
		ID:        options.ID,
		Name:      name,
		Auth:      options.Auth,
		models:    options.Models,
		refreshFn: options.RefreshModels,
		generate:  options.API,
	}
}

// GetModels returns the current known models.
func (p *ImagesProvider) GetModels() []*ImagesModel {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.models
}

// RefreshModels fetches and stores the current model list; concurrent calls
// share one in-flight fetch (upstream's inflightRefresh promise).
func (p *ImagesProvider) RefreshModels() error {
	p.mu.Lock()
	if p.refreshFn == nil {
		p.mu.Unlock()
		return nil
	}
	if p.inflight != nil {
		wait := p.inflight
		p.mu.Unlock()
		wait.Wait()
		return nil
	}
	inflight := &sync.WaitGroup{}
	inflight.Add(1)
	p.inflight = inflight
	refresh := p.refreshFn
	p.mu.Unlock()

	models, err := refresh()
	p.mu.Lock()
	if err == nil {
		p.models = models
	}
	p.inflight = nil
	p.mu.Unlock()
	inflight.Done()
	return err
}

// GenerateImages delegates to the API implementation.
func (p *ImagesProvider) GenerateImages(model *ImagesModel, context ImagesContext, options *ImagesOptions) *AssistantImages {
	return p.generate.GenerateImages(model, context, options)
}

// ImagesModels is the runtime collection of image providers (upstream
// ImagesModels).
type ImagesModels struct {
	mu          sync.Mutex
	providers   map[string]*ImagesProvider
	credentials CredentialStore
	authContext AuthContext
	identity    *ClientIdentity
}

// CreateImagesModels builds an empty image-model registry.
func CreateImagesModels(options *CreateModelsOptions) *ImagesModels {
	credentials := CredentialStore(NewInMemoryCredentialStore())
	authContext := AuthContext(DefaultAuthContext{})
	var identity *ClientIdentity
	if options != nil {
		identity = options.Identity
		if options.Credentials != nil {
			credentials = options.Credentials
		}
		if options.AuthContext != nil {
			authContext = options.AuthContext
		}
	}
	return &ImagesModels{providers: map[string]*ImagesProvider{}, credentials: credentials, authContext: authContext, identity: identity}
}

// SetProvider upserts a provider by id.
func (m *ImagesModels) SetProvider(provider *ImagesProvider) {
	m.mu.Lock()
	m.providers[provider.ID] = provider
	m.mu.Unlock()
}

// DeleteProvider removes a provider by id.
func (m *ImagesModels) DeleteProvider(id string) {
	m.mu.Lock()
	delete(m.providers, id)
	m.mu.Unlock()
}

// ClearProviders removes every provider.
func (m *ImagesModels) ClearProviders() {
	m.mu.Lock()
	m.providers = map[string]*ImagesProvider{}
	m.mu.Unlock()
}

// GetProviders lists the registered providers.
func (m *ImagesModels) GetProviders() []*ImagesProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ImagesProvider, 0, len(m.providers))
	for _, provider := range m.providers {
		out = append(out, provider)
	}
	return out
}

// GetProvider returns one provider by id.
func (m *ImagesModels) GetProvider(id string) *ImagesProvider {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.providers[id]
}

// GetModels lists last-known models, best-effort.
func (m *ImagesModels) GetModels(provider string) []*ImagesModel {
	if provider != "" {
		entry := m.GetProvider(provider)
		if entry == nil {
			return nil
		}
		return entry.GetModels()
	}
	var models []*ImagesModel
	for _, entry := range m.GetProviders() {
		models = append(models, entry.GetModels()...)
	}
	return models
}

// GetModel looks one model up by provider and id.
func (m *ImagesModels) GetModel(provider, id string) *ImagesModel {
	for _, model := range m.GetModels(provider) {
		if model.ID == id {
			return model
		}
	}
	return nil
}

// Refresh re-fetches dynamic providers; with an id, failures surface as a
// ModelsError ("model_source"), otherwise refresh is best-effort.
func (m *ImagesModels) Refresh(provider string) error {
	if provider != "" {
		entry := m.GetProvider(provider)
		if entry == nil || entry.refreshFn == nil {
			return nil
		}
		if err := entry.RefreshModels(); err != nil {
			var modelsErr *ModelsError
			if asModelsError(err, &modelsErr) {
				return err
			}
			return NewModelsError("model_source", "Model refresh failed for "+provider, err)
		}
		return nil
	}
	providers := m.GetProviders()
	var wait sync.WaitGroup
	for _, entry := range providers {
		if entry.refreshFn == nil {
			continue
		}
		wait.Add(1)
		go func(entry *ImagesProvider) {
			defer wait.Done()
			_ = entry.RefreshModels()
		}(entry)
	}
	wait.Wait()
	return nil
}

// GetAuth resolves auth for a provider id (upstream ImagesModels.getAuth).
func (m *ImagesModels) GetAuth(providerID string, overrides *AuthResolutionOverrides) (*AuthResult, error) {
	provider := m.GetProvider(providerID)
	if provider == nil {
		return nil, nil
	}
	return ResolveProviderAuth(providerID, provider.Auth, m.credentials, m.authContext, overrides)
}

// GenerateImages resolves auth and generates, never failing: errors come back
// as a result with StopReason "error" (upstream ImagesModels.generateImages).
func (m *ImagesModels) GenerateImages(model *ImagesModel, context ImagesContext, options *ImagesOptions) *AssistantImages {
	var requestOptions ImagesOptions
	if options != nil {
		requestOptions = *options
	}
	requestOptions.Identity = MergeClientIdentity(m.identity, requestOptions.Identity)
	options = &requestOptions
	provider := m.GetProvider(model.Provider)
	if provider == nil {
		return imagesError(model, fmt.Sprintf("Unknown provider: %s", model.Provider), options)
	}
	resolution, err := m.GetAuth(model.Provider, &AuthResolutionOverrides{
		APIKey: options.APIKey, Env: options.Env, Ctx: options.Ctx,
	})
	if err != nil {
		return imagesError(model, err.Error(), options)
	}
	if resolution == nil {
		return provider.GenerateImages(model, context, options)
	}
	auth := resolution.Auth
	requestModel := model
	if auth.BaseURL != "" {
		cloned := *model
		cloned.BaseURL = auth.BaseURL
		requestModel = &cloned
	}
	// Explicit request options win per field; headers/env merge per key.
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = auth.APIKey
	}
	headers := aiMergeProviderHeaders(auth.Headers, options.Headers)
	env := aiMergeProviderEnv(resolution.Env, options.Env)
	merged := *options
	merged.APIKey = apiKey
	merged.Headers = headers
	merged.Env = env
	return provider.GenerateImages(requestModel, context, &merged)
}

func imagesError(model *ImagesModel, message string, options *ImagesOptions) *AssistantImages {
	stopReason := ImagesStopError
	if options != nil && options.Ctx != nil && options.Ctx.Err() != nil {
		stopReason = ImagesStopAborted
	}
	return &AssistantImages{
		API: model.API, Provider: model.Provider, Model: model.ID,
		Output: []ImagesOutputContent{}, StopReason: stopReason,
		ErrorMessage: &message, Timestamp: time.Now().UnixMilli(),
	}
}

func aiMergeProviderHeaders(base, overrides ProviderHeaders) ProviderHeaders {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := ProviderHeaders{}
	maps.Copy(merged, base)
	maps.Copy(merged, overrides)
	return merged
}

func aiMergeProviderEnv(base, overrides ProviderEnv) ProviderEnv {
	if len(base) == 0 && len(overrides) == 0 {
		return nil
	}
	merged := ProviderEnv{}
	maps.Copy(merged, base)
	maps.Copy(merged, overrides)
	return merged
}

// imageCatalog is the generated image model catalogue.
var imageCatalog = struct {
	once   sync.Once
	models map[string]map[string]*ImagesModel
	err    error
}{}

func loadImageCatalog() {
	imageCatalog.once.Do(func() {
		var payload struct {
			Providers map[string]map[string]json.RawMessage `json:"providers"`
		}
		if err := json.Unmarshal(imageCatalogJSON, &payload); err != nil {
			imageCatalog.err = err
			return
		}
		models := map[string]map[string]*ImagesModel{}
		for provider, entries := range payload.Providers {
			models[provider] = map[string]*ImagesModel{}
			for id, raw := range entries {
				var model ImagesModel
				if err := json.Unmarshal(raw, &model); err != nil {
					imageCatalog.err = err
					return
				}
				models[provider][id] = &model
			}
		}
		imageCatalog.models = models
	})
}

// GetBuiltinImageModels returns the generated image models for a provider.
func GetBuiltinImageModels(provider string) []*ImagesModel {
	loadImageCatalog()
	entries := imageCatalog.models[provider]
	if len(entries) == 0 {
		return nil
	}
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sortStrings(ids)
	out := make([]*ImagesModel, 0, len(ids))
	for _, id := range ids {
		out = append(out, entries[id])
	}
	return out
}

// GetBuiltinImageModel looks one generated image model up.
func GetBuiltinImageModel(provider, id string) *ImagesModel {
	loadBuiltinImage := GetBuiltinImageModels(provider)
	for _, model := range loadBuiltinImage {
		if model.ID == id {
			return model
		}
	}
	return nil
}

// GetBuiltinImageProviders lists the providers present in the image catalog.
func GetBuiltinImageProviders() []string {
	loadImageCatalog()
	providers := make([]string, 0, len(imageCatalog.models))
	for provider := range imageCatalog.models {
		providers = append(providers, provider)
	}
	sortStrings(providers)
	return providers
}

// ImageCatalogError reports a catalogue decode failure.
func ImageCatalogError() error {
	loadImageCatalog()
	return imageCatalog.err
}
