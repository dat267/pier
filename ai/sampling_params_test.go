package ai

import (
	"encoding/json"
	"testing"
)

// TestResolveSamplingParamsMergesByThinkingLevel pins the merge order: model
// defaults, then the effective thinking level, then the request parameters
// (api/simple-options.ts resolveSamplingParams).
func TestResolveSamplingParamsMergesByThinkingLevel(t *testing.T) {
	model := &Model{
		Reasoning: true,
		SamplingParams: SamplingParams{
			"temperature": json.RawMessage("1"),
			"top_p":       json.RawMessage("0.95"),
		},
		SamplingParamsByThinkingLevel: SamplingParamsByThinkingLevel{
			ThinkHigh: SamplingParams{
				"temperature": json.RawMessage("0.8"),
				"top_k":       json.RawMessage("64"),
			},
		},
	}
	got := ResolveSamplingParams(model, ThinkHigh, SamplingParams{"top_p": json.RawMessage("0.5")})
	if string(got["temperature"]) != "0.8" || string(got["top_p"]) != "0.5" || string(got["top_k"]) != "64" {
		t.Fatalf("high merge = %v", got)
	}
	// Off falls back to the model defaults only.
	off := ResolveSamplingParams(model, ThinkOff, nil)
	if string(off["temperature"]) != "1" || off["top_k"] != nil {
		t.Fatalf("off merge = %v", off)
	}
	// No parameters anywhere is nil, not an empty map.
	if empty := ResolveSamplingParams(&Model{Reasoning: true}, ThinkOff, nil); empty != nil {
		t.Fatalf("empty merge = %v", empty)
	}
}

// TestClampThinkingLevelForSampling checks an unsupported requested level maps
// to the clamped one before the sampling lookup.
func TestClampThinkingLevelForSampling(t *testing.T) {
	// minimal is explicitly unsupported (null), so it clamps up to low.
	unsupported := (*string)(nil)
	model := &Model{
		Reasoning:        true,
		ThinkingLevelMap: ThinkingLevelMap{ThinkMinimal: unsupported},
		SamplingParamsByThinkingLevel: SamplingParamsByThinkingLevel{
			ThinkLow: SamplingParams{"temperature": json.RawMessage("0.6")},
		},
	}
	got := ResolveSamplingParams(model, ThinkMinimal, nil)
	if string(got["temperature"]) != "0.6" {
		t.Fatalf("clamped merge = %v", got)
	}
}

// TestBuildOpenAICompletionsParamsThinkingLevelSampling checks the thinking
// level sits between the model defaults and the request keys.
func TestBuildOpenAICompletionsParamsThinkingLevelSampling(t *testing.T) {
	model := testOpenAIModel()
	model.SamplingParams = SamplingParams{
		"temperature": json.RawMessage("1"),
		"top_p":       json.RawMessage("0.95"),
	}
	model.SamplingParamsByThinkingLevel = SamplingParamsByThinkingLevel{
		ThinkLow: SamplingParams{"temperature": json.RawMessage("0.6"), "top_k": json.RawMessage("64")},
	}
	params, err := BuildOpenAICompletionsParams(model, NormalizeContext(Context{}), &OpenAICompletionsOptions{
		StreamOptions:   StreamOptions{SamplingParams: SamplingParams{"top_p": json.RawMessage("0.5")}},
		ReasoningEffort: ThinkLow,
	}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := marshalParamsProbe(t, params)
	if string(probe["temperature"]) != "0.6" {
		t.Fatalf("temperature = %s, want 0.6", probe["temperature"])
	}
	if string(probe["top_p"]) != "0.5" {
		t.Fatalf("top_p = %s, want 0.5", probe["top_p"])
	}
	if string(probe["top_k"]) != "64" {
		t.Fatalf("top_k = %s, want 64", probe["top_k"])
	}
}

// TestBuildOpenAIResponsesParamsSummaryOnlySampling checks a summary-only
// request resolves to the medium level's parameters.
func TestBuildOpenAIResponsesParamsSummaryOnlySampling(t *testing.T) {
	model := testResponsesModel()
	model.SamplingParamsByThinkingLevel = SamplingParamsByThinkingLevel{
		ThinkOff:    SamplingParams{"temperature": json.RawMessage("0.7")},
		ThinkMedium: SamplingParams{"temperature": json.RawMessage("0.8")},
	}
	params, err := BuildOpenAIResponsesParams(model, NormalizeContext(Context{}), &OpenAIResponsesOptions{
		ReasoningSummary: "auto",
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe := marshalParamsProbe(t, params)
	if string(probe["temperature"]) != "0.8" {
		t.Fatalf("temperature = %s, want the medium value 0.8", probe["temperature"])
	}
	var reasoning struct {
		Effort string `json:"effort"`
	}
	if err := json.Unmarshal(probe["reasoning"], &reasoning); err != nil || reasoning.Effort != "medium" {
		t.Fatalf("reasoning = %s, %v", probe["reasoning"], err)
	}
}

func marshalParamsProbe(t *testing.T, params any) map[string]json.RawMessage {
	t.Helper()
	enc, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(enc, &probe); err != nil {
		t.Fatal(err)
	}
	return probe
}
