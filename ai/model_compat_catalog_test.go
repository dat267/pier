package ai

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365) parses the generated catalog into plain
// objects and `JSON.stringify`s a model back to RPC clients (modes/rpc), so a
// model's `compat` object survives a round trip member for member and in
// catalog order. Pier decodes `compat` into per-API structs, which can drop a
// member it does not model; this test walks the generated catalog so a missing
// field fails loudly instead of vanishing from `get_state`/`set_model` output.
// The comparison is by member and value, not bytes: the catalog stores its
// object keys sorted, while pier emits the compat struct's field order.
func TestEveryCatalogModelCompatRoundTrips(t *testing.T) {
	raw, err := os.ReadFile("models_catalog.json")
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Providers map[string]map[string]map[string]json.RawMessage `json:"providers"`
	}
	if err := json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for provider, apis := range catalog.Providers {
		for api, models := range apis {
			for id, modelRaw := range models {
				var probe struct {
					Compat json.RawMessage `json:"compat"`
				}
				if err := json.Unmarshal(modelRaw, &probe); err != nil {
					t.Fatalf("%s/%s/%s: %v", provider, api, id, err)
				}
				if len(probe.Compat) == 0 || string(probe.Compat) == "null" {
					continue
				}
				var model Model
				if err := json.Unmarshal(modelRaw, &model); err != nil {
					t.Fatalf("%s/%s/%s: %v", provider, api, id, err)
				}
				encoded, err := MarshalJSON(model.Compat)
				if err != nil {
					t.Fatalf("%s/%s/%s: %v", provider, api, id, err)
				}
				var catalogCompat, encodedCompat map[string]any
				if err := json.Unmarshal(probe.Compat, &catalogCompat); err != nil {
					t.Fatalf("%s/%s/%s: %v", provider, api, id, err)
				}
				if err := json.Unmarshal(encoded, &encodedCompat); err != nil {
					t.Fatalf("%s/%s/%s: %v", provider, api, id, err)
				}
				if !reflect.DeepEqual(catalogCompat, encodedCompat) {
					t.Errorf("%s/%s/%s compat = %s; catalog has %s", provider, api, id, encoded, probe.Compat)
				}
				checked++
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("checked %d compat objects; the catalog should hold more", checked)
	}
}
