package ai

import (
	"slices"
	"strings"
	"testing"
)

// pi v1.5.2 model-catalog.ts flattenModelCatalog keeps the generated catalog's
// group order, so a provider's models arrive grouped by api and sorted by id.
// The port read them from a Go map, which returns a random order on every call;
// the model picker and any "first model of a provider" choice saw that order.
func TestBuiltinModelsKeepTheCatalogOrder(t *testing.T) {
	for _, provider := range GetBuiltinProviders() {
		first := GetBuiltinModels(provider)
		for range 4 {
			again := GetBuiltinModels(provider)
			if len(again) != len(first) {
				t.Fatalf("%s: length changed between calls", provider)
			}
			for index := range first {
				if again[index].ID != first[index].ID {
					t.Fatalf("%s: order changed between calls at %d (%s then %s)",
						provider, index, first[index].ID, again[index].ID)
				}
			}
		}
		if !slices.IsSortedFunc(first, func(a, b *Model) int {
			if a.API != b.API {
				return strings.Compare(string(a.API), string(b.API))
			}
			return strings.Compare(a.ID, b.ID)
		}) {
			t.Fatalf("%s: models are not grouped by api and sorted by id", provider)
		}
	}
}

// A model id listed under two apis resolves through the later group, as the
// reference's flattened catalog does.
func TestCatalogDuplicateModelIDUsesTheLastAPI(t *testing.T) {
	catalog := catalogShape{}
	catalog.Providers = map[string]map[string]map[string]*catalogModelEntry{
		"p": {
			"messages":  {"m": {Model: Model{ID: "m"}}},
			"responses": {"m": {Model: Model{ID: "m"}}},
		},
	}
	data, err := MarshalJSON(catalog)
	if err != nil {
		t.Fatal(err)
	}
	decoded, models, err := decodeCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Providers) != 1 {
		t.Fatalf("providers = %d", len(decoded.Providers))
	}
	if got := models["p"]["m"].API; got != "responses" {
		t.Fatalf("duplicate id resolved to api %q, want the last group", got)
	}
}
