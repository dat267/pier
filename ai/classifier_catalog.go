package ai

import (
	_ "embed"
	"sort"
	"sync"
)

// classifierCatalogData is generated from the pinned OpenAI model data by
// scripts/gen_classifier_catalog.py. It contains only the classifier API that
// currently has a Pier implementation.
//
//go:embed classifier_catalog.json
var classifierCatalogData []byte

var (
	classifierCatalogOnce   sync.Once
	classifierCatalogModels map[string]map[string]*Model
	classifierCatalogErr    error
)

func loadClassifierCatalog() {
	classifierCatalogOnce.Do(func() {
		_, classifierCatalogModels, classifierCatalogErr = decodeCatalog(classifierCatalogData)
	})
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
