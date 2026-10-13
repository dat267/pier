package ai

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// The embedded builtin model catalog. Ground truth: upstream pi's own
// generator (packages/ai/scripts/generate-models.ts), converted by
// scripts/gen_catalog.py from the upstream clone at the pinned commit.
// Regenerate with:
//
//	cd <upstream>/packages/ai && node scripts/generate-models.ts --strict --data-only
//	python3 scripts/gen_catalog.py <upstream> > ai/models_catalog.json
//
//go:embed models_catalog.json
var catalogData []byte

// catalogShape mirrors the embedded file's structure:
// {provider: {api: {modelId: model}}}. Model fields decode directly, while the
// compat payload stays raw until the enclosing API key selects its exact type.
type catalogShape struct {
	Meta struct {
		SchemaVersion          int    `json:"schemaVersion"`
		GeneratedAt            string `json:"generatedAt"`
		UpstreamManifestSHA256 string `json:"upstreamManifestSHA256"`
	} `json:"_meta"`
	Providers map[string]map[string]map[string]*catalogModelEntry `json:"providers"`
}

type catalogModelEntry struct {
	Model
	Compat json.RawMessage `json:"compat"`
}

var (
	catalogOnce   sync.Once
	catalog       catalogShape
	catalogModels map[string]map[string]*Model
	catalogErr    error
)

func decodeCatalog(data []byte) (catalogShape, map[string]map[string]*Model, error) {
	var decoded catalogShape
	if err := jsonUnmarshalStrict(data, &decoded); err != nil {
		return catalogShape{}, nil, fmt.Errorf("ai: decoding builtin catalog: %w", err)
	}
	modelsByProvider := make(map[string]map[string]*Model, len(decoded.Providers))
	for providerID, apis := range decoded.Providers {
		models := make(map[string]*Model)
		for api, byID := range apis {
			for modelID, entry := range byID {
				compat, err := DecodeModelCompat(Api(api), entry.Compat)
				if err != nil {
					return catalogShape{}, nil, fmt.Errorf("ai: decoding catalog model %s/%s: %w", providerID, modelID, err)
				}
				entry.Model.API = Api(api)
				entry.Model.Compat = compat
				entry.Compat = nil
				models[modelID] = &entry.Model
			}
		}
		modelsByProvider[providerID] = models
	}
	return decoded, modelsByProvider, nil
}

func loadCatalog() {
	catalogOnce.Do(func() {
		catalog, catalogModels, catalogErr = decodeCatalog(catalogData)
	})
}

// GetBuiltinModel returns one generated builtin model.
func GetBuiltinModel(provider, modelID string) *Model {
	loadCatalog()
	if catalogErr != nil {
		return nil
	}
	return catalogModels[provider][modelID]
}

// GetBuiltinProviders returns the providers present in the generated catalog.
// (KnownProvider additionally includes purely dynamic providers, e.g.
// "radius", that have no static catalog entry.)
func GetBuiltinProviders() []string {
	loadCatalog()
	if catalogErr != nil {
		return nil
	}
	out := make([]string, 0, len(catalog.Providers))
	for provider := range catalog.Providers {
		out = append(out, provider)
	}
	sortStrings(out)
	return out
}

// GetBuiltinModels returns the generated models of one builtin provider.
func GetBuiltinModels(provider string) []*Model {
	loadCatalog()
	if catalogErr != nil {
		return nil
	}
	var out []*Model
	for _, model := range catalogModels[provider] {
		out = append(out, model)
	}
	return out
}

// BuiltinModelCount returns the total number of models in the embedded
// catalog (test/monitoring convenience).
func BuiltinModelCount() int {
	loadCatalog()
	if catalogErr != nil {
		return 0
	}
	n := 0
	for _, models := range catalogModels {
		n += len(models)
	}
	return n
}

// GetBuiltinModelDataGeneratedAt returns the generation timestamp shared by
// all builtin provider catalogs (port of getBuiltinModelDataGeneratedAt).
func GetBuiltinModelDataGeneratedAt() *time.Time {
	loadCatalog()
	if catalogErr != nil || catalog.Meta.GeneratedAt == "" {
		return nil
	}
	ts, err := time.Parse(time.RFC3339Nano, catalog.Meta.GeneratedAt)
	if err != nil {
		return nil
	}
	return &ts
}

// CatalogLoadError exposes decode failures of the embedded catalog.
func CatalogLoadError() error {
	loadCatalog()
	return catalogErr
}
