package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of core/remote-catalog-provider.ts: a persisted pi.dev catalog overlay
// on top of a static built-in provider (closing D37).

// DefaultCatalogBaseURL is the default catalog host.
const DefaultCatalogBaseURL = "https://pi.dev"

// RemoteCatalogAttemptTimeoutMS bounds each catalog request attempt.
const RemoteCatalogAttemptTimeoutMS int64 = 4_000

// RemoteCatalogRefreshIntervalMS is how long a stored catalog stays fresh.
const RemoteCatalogRefreshIntervalMS int64 = 4 * 60 * 60 * 1000

const remoteCatalogModelTypes = "chat,image,classifier"

// mergeCatalogModels merges dynamic models over a baseline by model type and id.
// The lookup is one map pass: upstream mergeModels ran a findIndex per remote
// model, quadratic in catalog size (c34f2d6ad). Insertion order is preserved,
// so a replacement lands at its first position and new ids append, matching the
// upstream Map's values() order.
func mergeCatalogModels(baseline, dynamic []*ai.Model) []*ai.Model {
	key := func(model *ai.Model) string {
		modelType := model.Type
		if modelType == "" {
			modelType = "chat"
		}
		return modelType + "\x00" + model.ID
	}
	index := make(map[string]int, len(baseline)+len(dynamic))
	merged := make([]*ai.Model, 0, len(baseline)+len(dynamic))
	for _, model := range baseline {
		modelKey := key(model)
		if at, ok := index[modelKey]; ok {
			merged[at] = model
			continue
		}
		index[modelKey] = len(merged)
		merged = append(merged, model)
	}
	for _, model := range dynamic {
		modelKey := key(model)
		if at, ok := index[modelKey]; ok {
			merged[at] = model
			continue
		}
		index[modelKey] = len(merged)
		merged = append(merged, model)
	}
	return merged
}

// ParseRemoteCatalog parses a catalog payload: a bare array, an object with a
// models array, or an object whose values are models. Pass json.RawMessage to
// retain source property order; Go maps have no insertion order.
func ParseRemoteCatalog(providerID string, value any) ([]*ai.Model, error) {
	// Canonicalization retains string-key insertion order, resolves duplicate
	// keys and sorts array-index keys like JSON.parse/Object.values upstream.
	raw, err := ai.MarshalJSON(value)
	if err != nil {
		return nil, err
	}
	var entries []json.RawMessage
	switch raw[0] {
	case '[':
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
	case '{':
		keys, values, err := readJSONObjectOrdered(raw)
		if err != nil {
			return nil, err
		}
		if models := values["models"]; len(models) > 0 && models[0] == '[' {
			if err := json.Unmarshal(models, &entries); err != nil {
				return nil, err
			}
		} else {
			for _, key := range keys {
				entries = append(entries, values[key])
			}
		}
	default:
		return nil, fmt.Errorf("Invalid model catalog for provider %q", providerID)
	}
	var models []*ai.Model
	for _, entry := range entries {
		if !isJSONObject(entry) {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(entry, &object); err != nil {
			continue
		}
		if _, hasID := object["id"]; !hasID {
			continue
		}
		if rawType, present := object["type"]; present {
			var modelType string
			if json.Unmarshal(rawType, &modelType) != nil || (modelType != "chat" && modelType != "image" && modelType != "classifier") {
				continue
			}
		}
		var model ai.Model
		if err := json.Unmarshal(entry, &model); err != nil {
			continue
		}
		model.Provider = ai.ProviderId(providerID)
		models = append(models, &model)
	}
	return models, nil
}

func chatCatalogModels(models []*ai.Model) []*ai.Model {
	chat := make([]*ai.Model, 0, len(models))
	for _, model := range models {
		if ai.IsModelType(model, "chat") {
			chat = append(chat, model)
		}
	}
	return chat
}

// remoteCatalogModels decides which stored models are usable: a stored overlay
// only applies when it is newer than the local generated catalog.
func remoteCatalogModels(entry *ai.ModelsStoreEntry, localGeneratedAt int64, hasLocalGeneratedAt bool) []*ai.Model {
	if entry == nil {
		return nil
	}
	if hasLocalGeneratedAt && (entry.LastModified == nil || *entry.LastModified <= localGeneratedAt) {
		return nil
	}
	return entry.Models
}

// RemoteCatalogFetcher performs the catalog HTTP request (the shared
// management-HTTP helper; swappable in tests).
type RemoteCatalogFetcher func(ctx context.Context, request *http.Request) (*http.Response, error)

// remoteCatalogURL mirrors requestCatalog's absolute path and encodeURIComponent
// provider ID, discarding any base pathname, query and fragment.
func remoteCatalogURL(catalogBaseURL, providerID string) (string, error) {
	base, err := url.Parse(catalogBaseURL)
	if err != nil {
		return "", err
	}
	encodedID := strings.NewReplacer(
		"+", "%20", "%21", "!", "%27", "'", "%28", "(", "%29", ")", "%2A", "*",
	).Replace(url.QueryEscape(providerID))
	endpoint, err := base.Parse("/api/models/providers/" + encodedID)
	if err != nil {
		return "", err
	}
	return endpoint.String(), nil
}

// WithRemoteCatalog adds a persisted pi.dev catalog overlay to a static
// built-in provider. The overlay only applies when the stored catalog is newer
// than the locally generated one; refreshes revalidate with the stored etag and
// keep the cached body on transient failures.
func WithRemoteCatalog(provider *ai.Provider, catalogBaseURL string, localGeneratedAt int64, hasLocalGeneratedAt bool, fetch RemoteCatalogFetcher) *ai.Provider {
	if catalogBaseURL == "" {
		catalogBaseURL = DefaultCatalogBaseURL
	}
	if fetch == nil {
		attemptTimeout := RemoteCatalogAttemptTimeoutMS
		fetch = func(ctx context.Context, request *http.Request) (*http.Response, error) {
			return FetchWithRetry(ctx, request, http.DefaultClient, FetchRetryOptions{
				AttemptTimeoutMS: &attemptTimeout,
			})
		}
	}
	dynamicModels := []*ai.Model{}

	streams := funcStreams{
		stream: func(model *ai.Model, context ai.TranscriptContext, options *ai.StreamOptions) *ai.AssistantMessageEventStream {
			return provider.Stream(model, context, options)
		},
		streamSimple: func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return provider.StreamSimple(model, context, options)
		},
	}
	var implementation ai.ProviderStreams = streams
	if provider.HasDeferred() {
		implementation = funcStreamsWithDeferred{funcStreams: streams, base: provider}
	}

	wrapped := ai.CreateProvider(ai.CreateProviderOptions{
		ID:      provider.ID,
		Name:    provider.Name,
		BaseURL: provider.BaseURL,
		Headers: provider.Headers,
		Auth:    provider.Auth,
		// The baseline is the built-in catalog; the persisted pi.dev overlay
		// merges over it by model id.
		ModelsGetter: func() []*ai.Model {
			return mergeCatalogModels(provider.GetModels(), chatCatalogModels(dynamicModels))
		},
		Single: implementation,
	})
	wrapped.FilterModels = provider.FilterModels

	wrapped.RefreshModels = func(refresh *ai.RefreshModelsContext) error {
		stored := refresh.Stored
		restored := remoteCatalogModels(stored, localGeneratedAt, hasLocalGeneratedAt)
		var kept []*ai.Model
		for _, model := range restored {
			if string(model.Provider) == provider.ID {
				kept = append(kept, model)
			}
		}
		published := refresh.Publish(ai.ModelsPublication{
			Update: func() { dynamicModels = kept },
		})
		if !published {
			return nil
		}
		if !refresh.AllowNetwork || refresh.Ctx.Err() != nil {
			return nil
		}
		force := refresh.Force != nil && *refresh.Force
		if !force && stored != nil && stored.CheckedAt != nil && stored.LastModified != nil &&
			time.Now().UnixMilli()-*stored.CheckedAt < RemoteCatalogRefreshIntervalMS {
			return nil
		}

		ctx := refresh.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		// Only revalidate when a cached body backs the validator, so a 304 can
		// never leave the overlay empty.
		validator := ""
		if stored != nil && len(stored.Models) > 0 && stored.Etag != nil {
			validator = *stored.Etag
		}
		endpoint, err := remoteCatalogURL(catalogBaseURL, provider.ID)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("accept", "application/json")
		request.Header.Set("User-Agent", PiUserAgent(Version))
		query := request.URL.Query()
		query.Set("types", remoteCatalogModelTypes)
		request.URL.RawQuery = query.Encode()
		if validator != "" {
			request.Header.Set("if-none-match", validator)
		}
		response, err := fetch(ctx, request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if ctx.Err() != nil {
			return nil
		}
		checkedAt := time.Now().UnixMilli()

		// Unchanged: the stored overlay is already live, so only the freshness
		// window moves.
		if response.StatusCode == http.StatusNotModified && stored != nil {
			refresh.Publish(ai.ModelsPublication{Persist: storedWithCheckedAt(stored, checkedAt)})
			return nil
		}
		if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusNotImplemented {
			refresh.Publish(ai.ModelsPublication{Persist: storedWithoutOverlay(stored, checkedAt)})
			return nil
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			// Transient failure: the cached body and its validator stay valid, so
			// keep the etag and let the next refresh revalidate instead of
			// downloading the catalog.
			refresh.Publish(ai.ModelsPublication{Persist: storedWithCheckedAt(stored, checkedAt)})
			return fmt.Errorf("Model catalog request failed for %s: %d", provider.ID, response.StatusCode)
		}
		raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		if err != nil {
			return err
		}
		refreshed, err := ParseRemoteCatalog(provider.ID, json.RawMessage(raw))
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		lastModified := int64(0)
		if parsed, err := http.ParseTime(response.Header.Get("last-modified")); err == nil {
			lastModified = parsed.UnixMilli()
		}
		entry := &ai.ModelsStoreEntry{Models: refreshed, CheckedAt: &checkedAt, LastModified: &lastModified}
		if etag := response.Header.Get("etag"); etag != "" {
			entry.Etag = &etag
		}
		publishedModels := remoteCatalogModels(entry, localGeneratedAt, hasLocalGeneratedAt)
		refresh.Publish(ai.ModelsPublication{
			Persist: entry,
			Update:  func() { dynamicModels = publishedModels },
		})
		return nil
	}
	return wrapped
}

// CatalogPersonalizer applies credential-specific availability and metadata to a catalog.
type CatalogPersonalizer func(catalog []*ai.Model, refresh *ai.RefreshModelsContext) ([]*ai.Model, error)

// WithPersonalizedCatalog replaces a provider catalog with a credential-specific list. The stored
// list is the personalized catalog, so it remains available offline. The optional pi.dev overlay
// is merged before personalization; unlike WithRemoteCatalog, this wrapper does not reuse its ETag
// or refresh interval because the user model list may change with the key.
func WithPersonalizedCatalog(
	provider *ai.Provider,
	personalize CatalogPersonalizer,
	catalogBaseURL string,
	localGeneratedAt int64,
	hasLocalGeneratedAt bool,
	fetch RemoteCatalogFetcher,
) *ai.Provider {
	if catalogBaseURL == "" {
		catalogBaseURL = DefaultCatalogBaseURL
	}
	if fetch == nil {
		attemptTimeout := RemoteCatalogAttemptTimeoutMS
		fetch = func(ctx context.Context, request *http.Request) (*http.Response, error) {
			return FetchWithRetry(ctx, request, http.DefaultClient, FetchRetryOptions{AttemptTimeoutMS: &attemptTimeout})
		}
	}
	personalModels := []*ai.Model{}
	getModels := func() []*ai.Model {
		if len(personalModels) == 0 {
			return chatCatalogModels(provider.GetModels())
		}
		return chatCatalogModels(personalModels)
	}
	streams := funcStreams{
		stream: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.StreamOptions) *ai.AssistantMessageEventStream {
			return provider.Stream(model, transcript, options)
		},
		streamSimple: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return provider.StreamSimple(model, transcript, options)
		},
	}
	var implementation ai.ProviderStreams = streams
	if provider.HasDeferred() {
		implementation = funcStreamsWithDeferred{funcStreams: streams, base: provider}
	}
	wrapped := ai.CreateProvider(ai.CreateProviderOptions{
		ID: provider.ID, Name: provider.Name, BaseURL: provider.BaseURL, Headers: provider.Headers,
		Auth: provider.Auth, ModelsGetter: getModels, Single: implementation,
	})
	wrapped.FilterModels = provider.FilterModels
	wrapped.RefreshModels = func(refresh *ai.RefreshModelsContext) error {
		var stored []*ai.Model
		if refresh.Stored != nil {
			for _, model := range refresh.Stored.Models {
				if model.Provider == provider.ID {
					stored = append(stored, model)
				}
			}
		}
		if !refresh.Publish(ai.ModelsPublication{Update: func() { personalModels = stored }}) {
			return nil
		}
		ctx := refresh.Ctx
		if ctx == nil {
			ctx = context.Background()
		}
		if !refresh.AllowNetwork || ctx.Err() != nil {
			return nil
		}
		endpoint, err := remoteCatalogURL(catalogBaseURL, provider.ID)
		if err != nil {
			return err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return err
		}
		request.Header.Set("accept", "application/json")
		request.Header.Set("User-Agent", PiUserAgent(Version))
		query := request.URL.Query()
		query.Set("types", remoteCatalogModelTypes)
		request.URL.RawQuery = query.Encode()
		response, err := fetch(ctx, request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		var remote []*ai.Model
		if response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusNotImplemented {
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return fmt.Errorf("Model catalog request failed for %s: %d", provider.ID, response.StatusCode)
			}
			raw, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
			if err != nil {
				return err
			}
			parsed, err := ParseRemoteCatalog(provider.ID, json.RawMessage(raw))
			if err != nil {
				return err
			}
			lastModified := int64(0)
			if parsedTime, parseErr := http.ParseTime(response.Header.Get("last-modified")); parseErr == nil {
				lastModified = parsedTime.UnixMilli()
			}
			if !hasLocalGeneratedAt || lastModified > localGeneratedAt {
				remote = parsed
			}
		}
		catalog := mergeCatalogModels(provider.GetModels(), remote)
		personalized, err := personalize(catalog, refresh)
		if err != nil {
			return err
		}
		if refresh.Ctx.Err() != nil {
			return nil
		}
		entry := &ai.ModelsStoreEntry{Models: personalized}
		checkedAt := time.Now().UnixMilli()
		entry.CheckedAt = &checkedAt
		refresh.Publish(ai.ModelsPublication{
			Persist: entry,
			Update:  func() { personalModels = personalized },
		})
		return nil
	}
	return wrapped
}

// storedWithCheckedAt copies a stored entry with a new freshness timestamp.
func storedWithCheckedAt(stored *ai.ModelsStoreEntry, checkedAt int64) *ai.ModelsStoreEntry {
	copied := ai.ModelsStoreEntry{}
	if stored != nil {
		copied = *stored
	}
	copied.CheckedAt = &checkedAt
	return &copied
}

// storedWithoutOverlay invalidates freshness and the validator but retains the
// cached body, matching remote-catalog-provider.ts's spread of stored.
func storedWithoutOverlay(stored *ai.ModelsStoreEntry, checkedAt int64) *ai.ModelsStoreEntry {
	copied := *storedWithCheckedAt(stored, checkedAt)
	zero := int64(0)
	copied.LastModified = &zero
	copied.Etag = nil
	return &copied
}
