package ai

// Port of utils/openrouter-cost.ts (pi v1.5.0): OpenRouter's billed cost
// replaces the catalog estimate, and BYOK requests add the upstream cost.

import (
	"encoding/json"
	"testing"
)

func floatPtr(value float64) *float64 { return &value }

func openRouterAccounting(cost *float64, byok *bool, details *OpenRouterCostDetails) OpenRouterUsageAccounting {
	return OpenRouterUsageAccounting{Cost: cost, IsBYOK: byok, CostDetails: details}
}

func TestApplyOpenRouterReportedCost(t *testing.T) {
	estimated := false
	base := func() Usage {
		return Usage{Cost: UsageCost{Input: 1, Output: 2, Total: 3, TotalFromProvider: &estimated}}
	}
	byok := true
	notByok := false

	// Direct billing replaces the estimate and marks it provider-reported.
	usage := base()
	ApplyOpenRouterReportedCost(&usage, openRouterAccounting(floatPtr(1.25), &notByok, nil))
	if usage.Cost.Total != 1.25 || usage.Cost.TotalFromProvider == nil || !*usage.Cost.TotalFromProvider {
		t.Fatalf("direct billing = %+v", usage.Cost)
	}

	// BYOK adds the upstream inference cost.
	usage = base()
	ApplyOpenRouterReportedCost(&usage, openRouterAccounting(floatPtr(0.5), &byok, &OpenRouterCostDetails{
		UpstreamInferenceCost: floatPtr(2),
	}))
	if usage.Cost.Total != 2.5 || !*usage.Cost.TotalFromProvider {
		t.Fatalf("byok total = %+v", usage.Cost)
	}

	// Without the upstream total the prompt and completion parts sum.
	usage = base()
	ApplyOpenRouterReportedCost(&usage, openRouterAccounting(floatPtr(0.5), &byok, &OpenRouterCostDetails{
		UpstreamInferencePromptCost:      floatPtr(1),
		UpstreamInferenceCompletionsCost: floatPtr(0.75),
	}))
	if usage.Cost.Total != 2.25 || !*usage.Cost.TotalFromProvider {
		t.Fatalf("byok parts total = %+v", usage.Cost)
	}

	// Incomplete or invalid accounting leaves the estimate untouched.
	for name, accounting := range map[string]OpenRouterUsageAccounting{
		"no cost":         openRouterAccounting(nil, &notByok, nil),
		"negative cost":   openRouterAccounting(floatPtr(-1), &notByok, nil),
		"byok no details": openRouterAccounting(floatPtr(1), &byok, nil),
		"byok no parts":   openRouterAccounting(floatPtr(1), &byok, &OpenRouterCostDetails{}),
		"nil byok":        openRouterAccounting(floatPtr(1), nil, nil),
	} {
		usage = base()
		ApplyOpenRouterReportedCost(&usage, accounting)
		if usage.Cost.Total != 3 || usage.Cost.TotalFromProvider == nil || *usage.Cost.TotalFromProvider {
			t.Fatalf("%s: cost = %+v", name, usage.Cost)
		}
	}
}

// The OpenAI-compatible usage parser applies the accounting for OpenRouter
// models only.
func TestParseChunkUsageAppliesOpenRouterAccounting(t *testing.T) {
	raw := []byte(`{"prompt_tokens":1000,"completion_tokens":500,"cost":0.0042,"is_byok":false}`)
	openRouter := &Model{Provider: "openrouter", BaseURL: "https://openrouter.ai/api/v1",
		Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 3, Output: 15}}}
	usage := ParseChunkUsage(raw, openRouter)
	if usage.Cost.Total != 0.0042 || usage.Cost.TotalFromProvider == nil || !*usage.Cost.TotalFromProvider {
		t.Fatalf("openrouter cost = %+v", usage.Cost)
	}
	// A provider not routed through OpenRouter keeps the catalog estimate even
	// when the body carries the extra fields.
	plain := *openRouter
	plain.Provider = "openai"
	plain.BaseURL = "https://api.openai.com/v1"
	usage = ParseChunkUsage(raw, &plain)
	if usage.Cost.TotalFromProvider == nil || *usage.Cost.TotalFromProvider {
		t.Fatalf("plain cost = %+v", usage.Cost)
	}
	if usage.Cost.Total == 0.0042 {
		t.Fatalf("plain cost took the reported total: %+v", usage.Cost)
	}
}

// The wire shape of the accounting block matches OpenRouter's snake_case keys.
func TestOpenRouterUsageAccountingDecodes(t *testing.T) {
	raw := []byte(`{"cost":0.5,"is_byok":true,"cost_details":{"upstream_inference_cost":null,"upstream_inference_prompt_cost":0.1,"upstream_inference_completions_cost":0.2,"server_tool_cost":null}}`)
	var accounting OpenRouterUsageAccounting
	if err := json.Unmarshal(raw, &accounting); err != nil {
		t.Fatal(err)
	}
	if accounting.Cost == nil || *accounting.Cost != 0.5 || accounting.IsBYOK == nil || !*accounting.IsBYOK {
		t.Fatalf("accounting = %+v", accounting)
	}
	if accounting.CostDetails == nil || accounting.CostDetails.UpstreamInferenceCost != nil ||
		accounting.CostDetails.UpstreamInferencePromptCost == nil || *accounting.CostDetails.UpstreamInferencePromptCost != 0.1 {
		t.Fatalf("details = %+v", accounting.CostDetails)
	}
}
