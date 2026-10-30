package ai

import (
	_ "embed"
	"sort"
	"sync"
)

// Embedded classifier catalogs are generated from pinned provider model data
// by scripts/gen_classifier_catalog.py.
//
//go:embed classifier_catalog.json
var classifierCatalogData []byte

//go:embed typesafe_classifier_catalog.json
var typesafeClassifierCatalogData []byte

//go:embed cloudflare_classifier_catalog.json
var cloudflareClassifierCatalogData []byte

var (
	classifierCatalogOnce   sync.Once
	classifierCatalogModels map[string]map[string]*Model
	classifierCatalogErr    error
)

func loadClassifierCatalog() {
	classifierCatalogOnce.Do(func() {
		_, classifierCatalogModels, classifierCatalogErr = decodeCatalog(classifierCatalogData)
		if classifierCatalogErr != nil {
			return
		}
		_, extraModels, err := decodeCatalog(typesafeClassifierCatalogData)
		if err != nil {
			classifierCatalogErr = err
			return
		}
		mergeClassifierCatalog(extraModels)
		_, cloudflareModels, err := decodeCatalog(cloudflareClassifierCatalogData)
		if err != nil {
			classifierCatalogErr = err
			return
		}
		mergeClassifierCatalog(cloudflareModels)
	})
}

func mergeClassifierCatalog(extraModels map[string]map[string]*Model) {
	for provider, models := range extraModels {
		if classifierCatalogModels[provider] == nil {
			classifierCatalogModels[provider] = map[string]*Model{}
		}
		for id, model := range models {
			classifierCatalogModels[provider][id] = model
		}
	}
}

// GetBuiltinClassifierModel returns a supported generated classifier model.
func GetBuiltinClassifierModel(provider, modelID string) *Model {
	loadClassifierCatalog()
	if classifierCatalogErr != nil {
		return nil
	}
	model := classifierCatalogModels[provider][modelID]
	if model == nil || model.Type != "classifier" {
		return nil
	}
	return model
}

// GetBuiltinClassifierModels returns the supported generated classifier models
// for one provider, ordered by model ID.
func GetBuiltinClassifierModels(provider string) []*Model {
	loadClassifierCatalog()
	if classifierCatalogErr != nil {
		return nil
	}
	var models []*Model
	for _, model := range classifierCatalogModels[provider] {
		if model.Type == "classifier" {
			models = append(models, model)
		}
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return models
}
