package ai

import (
	"reflect"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), src/model-catalog.ts keeps imported
// descriptors intact; src/api/simple-options.ts merges sampling parameters in
// their JavaScript insertion order. The embedded decoder must retain that order
// when future generated catalogs include sampling parameters.
func TestCatalogDecodeRetainsSamplingParameterOrder(t *testing.T) {
	data := []byte(`{"providers":{"test":{"openai-completions":{"model":{"id":"model","provider":"test","samplingParams":{"z":1,"a":2},"samplingParamsByThinkingLevel":{"high":{"last":3,"first":4},"low":{}}}}}}}`)
	_, models, err := decodeCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	model := models["test"]["model"]
	if !reflect.DeepEqual(model.SamplingParamsOrder, []string{"z", "a"}) {
		t.Fatalf("sampling parameter order = %v", model.SamplingParamsOrder)
	}
	if !reflect.DeepEqual(model.SamplingParamsByThinkingLevelOrder["high"], []string{"last", "first"}) {
		t.Fatalf("thinking-level parameter order = %v", model.SamplingParamsByThinkingLevelOrder)
	}
	encoded, err := MarshalJSON(model)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"samplingParams":{"z":1,"a":2}`,
		`"samplingParamsByThinkingLevel":{"high":{"last":3,"first":4},"low":{}}`,
	} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("model JSON lacks %s: %s", want, encoded)
		}
	}
}

// Reference: pi v1.4.0 (4a080252), src/providers/openai.models.ts and
// src/api/openai-completions.ts getCompat. The catalog's enclosing API selects
// the compatibility type; decoding through Model.UnmarshalJSON too early would
// select the wrong arm when the descriptor's api is absent or stale.
func TestCatalogDecodeSelectsCompatByEnclosingAPI(t *testing.T) {
	for _, field := range []string{"", `"api":"anthropic-messages",`} {
		t.Run(field, func(t *testing.T) {
			data := []byte(`{"providers":{"test":{"openai-completions":{"model":{` + field + `"id":"model","provider":"test","compat":{"supportsStore":false}}}}}}`)
			_, models, err := decodeCatalog(data)
			if err != nil {
				t.Fatal(err)
			}
			model := models["test"]["model"]
			if model.API != APIOpenAICompletions || model.Compat == nil || model.Compat.OpenAICompletions == nil {
				t.Fatalf("wrong API/compat: %s/%+v", model.API, model.Compat)
			}
			if model.Compat.AnthropicMessages != nil {
				t.Fatal("compat selected the descriptor's stale API")
			}
			store := model.Compat.OpenAICompletions.SupportsStore
			if store == nil || *store {
				t.Fatalf("supportsStore = %v, want false", store)
			}
		})
	}
}
