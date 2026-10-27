package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), src/models-store.ts structuredClone.
// These fields exercise every mutable descriptor shape, including compatibility
// maps and pointers. Loaded fields outside the chat descriptor remain lossless.
func TestModelsStoreClonePreservesBytesAndIsolation(t *testing.T) {
	const input = `{"models":[{"id":"m","api":"openai-completions","provider":"test","type":"image","output":["image"],"future":{"z":1,"a":2},"input":["text","image"],"thinkingLevelMap":{"high":"reason"},"inputLimits":{"images":{"resize":{"maxWidth":100}}},"cost":{"input":1,"output":2,"cacheRead":0,"cacheWrite":0,"tiers":[{"input":3,"inputTokensAbove":100}]},"samplingParams":{"z":1,"a":2},"samplingParamsByThinkingLevel":{"high":{"z":3,"a":4}},"headers":{"x-test":"old"},"promptCache":{"short":300},"compat":{"supportsStore":false,"chatTemplateKwargs":{"z":{"a":1}},"openRouterRouting":{"allow_fallbacks":true,"order":["a","b"],"max_price":{"input":1}},"vercelGatewayRouting":{"only":["a"]}}},null],"lastModified":1,"checkedAt":2,"etag":"old"}`
	var original ModelsStoreEntry
	if err := json.Unmarshal([]byte(input), &original); err != nil {
		t.Fatal(err)
	}
	before, err := MarshalJSON(&original)
	if err != nil {
		t.Fatal(err)
	}
	cloned := original.Clone()
	encoded, err := MarshalJSON(cloned)
	if err != nil || string(encoded) != string(before) {
		t.Fatalf("clone changed descriptor bytes: %s (%v), want %s", encoded, err, before)
	}
	model := cloned.Models[0]
	model.ID = "changed"
	model.Input[0] = "changed"
	*model.ThinkingLevelMap["high"] = "changed"
	model.InputLimits.Images.Resize.MaxWidth = 1
	model.Cost.Tiers[0].Input = 9
	model.Headers["x-test"] = "changed"
	model.PromptCache["short"] = 9
	model.SamplingParams["z"][0] = '9'
	model.SamplingParamsOrder[0] = "changed"
	model.SamplingParamsByThinkingLevel["high"]["z"][0] = '9'
	model.SamplingParamsByThinkingLevelOrder["high"][0] = "changed"
	compat := model.Compat.OpenAICompletions
	*compat.SupportsStore = true
	compat.ChatTemplateKwargs["z"][5] = '9'
	*compat.OpenRouterRouting.AllowFallbacks = false
	compat.OpenRouterRouting.Order[0] = "changed"
	compat.OpenRouterRouting.MaxPrice["input"][0] = '9'
	compat.VercelGatewayRouting.Only[0] = "changed"
	*cloned.LastModified, *cloned.CheckedAt, *cloned.Etag = 9, 9, "changed"
	cloned.Models[1] = &Model{ID: "added"}
	after, err := MarshalJSON(&original)
	if err != nil || string(after) != string(before) {
		t.Fatalf("clone mutation changed its source: %s (%v), want %s", after, err, before)
	}
	// Editing typed fields must still preserve unknown fields and order using
	// the shared immutable JSON provenance.
	encoded, err = MarshalJSON(cloned)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"output":["image"]`, `"future":{"z":1,"a":2}`} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("typed edits dropped retained field %s: %s", want, encoded)
		}
	}
	var roundTrip ModelsStoreEntry
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if roundTrip.Models[0].ID != "changed" || roundTrip.Models[0].InputLimits.Images.Resize.MaxWidth != 1 {
		t.Fatalf("typed edits disappeared: %+v", roundTrip.Models[0])
	}
}
