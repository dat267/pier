package coding

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/model-resolver.ts resolveModelReference.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/model-resolve/generate.mjs ./pi > coding/testdata/model-resolve/golden.json
func TestResolveModelReferenceMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/model-resolve/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Models []struct {
			Provider, ID, Name, API string
		} `json:"models"`
		Fixtures []struct {
			Name     string                `json:"name"`
			Request  ModelReferenceRequest `json:"request"`
			Usable   string                `json:"usable"`
			Expected map[string]any        `json:"expected"`
		} `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Fixtures) == 0 {
		t.Fatal("empty model-resolution corpus")
	}
	models := make([]*ai.Model, 0, len(corpus.Models))
	for _, entry := range corpus.Models {
		models = append(models, &ai.Model{Provider: entry.Provider, ID: entry.ID, Name: entry.Name, API: ai.Api(entry.API)})
	}
	for _, fixture := range corpus.Fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			usable := func(provider string) bool { return true }
			if fixture.Usable == "no-openai" {
				usable = func(provider string) bool { return provider != "openai" }
			}
			got := projectModelReference(ResolveModelReference(fixture.Request, models, usable))
			encodedGot, _ := json.Marshal(got)
			encodedWant, _ := json.Marshal(fixture.Expected)
			if string(encodedGot) != string(encodedWant) {
				t.Fatalf("request %+v differs\n got: %s\nwant: %s", fixture.Request, encodedGot, encodedWant)
			}
		})
	}
}

func projectModelReference(result ModelReferenceResult) map[string]any {
	out := map[string]any{"status": result.Status}
	if result.Model != nil {
		out["model"] = result.Model.Provider + "/" + result.Model.ID
	}
	if result.ThinkingLevel != "" {
		out["thinkingLevel"] = result.ThinkingLevel
	}
	if result.Warning != "" {
		out["warning"] = result.Warning
	}
	if result.Target != nil {
		providerModels := make([]string, 0, len(result.Target.ProviderModels))
		for _, model := range result.Target.ProviderModels {
			providerModels = append(providerModels, model.Provider+"/"+model.ID)
		}
		target := map[string]any{
			"provider": result.Target.Provider, "providerModels": providerModels,
			"modelText": result.Target.ModelText, "modelId": result.Target.ModelID,
		}
		if result.Target.ThinkingLevel != "" {
			target["thinkingLevel"] = result.Target.ThinkingLevel
		}
		out["target"] = target
	}
	return out
}
