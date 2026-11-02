package ai

import (
	"math"
	"strings"
)

// Port of utils/openrouter-cost.ts (pi v1.5.0): OpenRouter reports the billed
// cost on the usage object of a response, which is more accurate than the
// catalog estimate. The additional fields are not in its OpenAPI schema, so
// prose here documents them.

// OpenRouterUsageAccounting is OpenRouter's billing block on a usage object.
type OpenRouterUsageAccounting struct {
	// Cost is the amount charged to the user's OpenRouter account.
	Cost *float64 `json:"cost"`
	// IsBYOK reports whether OpenRouter used the user's upstream provider key.
	IsBYOK      *bool                  `json:"is_byok"`
	CostDetails *OpenRouterCostDetails `json:"cost_details"`
}

// OpenRouterCostDetails is the upstream cost breakdown of a BYOK request.
type OpenRouterCostDetails struct {
	// UpstreamInferenceCost is the total billed through the upstream provider
	// account, when available.
	UpstreamInferenceCost *float64 `json:"upstream_inference_cost"`
	// UpstreamInferencePromptCost is the upstream cache-aware prompt cost.
	UpstreamInferencePromptCost *float64 `json:"upstream_inference_prompt_cost"`
	// UpstreamInferenceCompletionsCost is the upstream generation cost.
	UpstreamInferenceCompletionsCost *float64 `json:"upstream_inference_completions_cost"`
	// ServerToolCost is the metered server-tool cost already included in Cost.
	ServerToolCost *float64 `json:"server_tool_cost"`
}

func isValidCost(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value >= 0
}

// IsOpenRouterModel reports whether model is served by OpenRouter.
func IsOpenRouterModel(model *Model) bool {
	if model == nil {
		return false
	}
	return model.Provider == "openrouter" || strings.Contains(model.BaseURL, "openrouter.ai")
}

// ApplyOpenRouterReportedCost replaces the catalog total with OpenRouter's
// billed cost when the response contains complete, valid accounting.
func ApplyOpenRouterReportedCost(usage *Usage, reported OpenRouterUsageAccounting) {
	if !isValidCost(reported.Cost) {
		return
	}
	if reported.IsBYOK != nil && !*reported.IsBYOK {
		usage.Cost.Total = *reported.Cost
		usage.Cost.TotalFromProvider = boolPtrTrue()
		return
	}
	if reported.IsBYOK == nil || !*reported.IsBYOK {
		return
	}
	details := reported.CostDetails
	if details == nil {
		return
	}
	upstreamCost := details.UpstreamInferenceCost
	if upstreamCost == nil {
		if !isValidCost(details.UpstreamInferencePromptCost) || !isValidCost(details.UpstreamInferenceCompletionsCost) {
			return
		}
		sum := *details.UpstreamInferencePromptCost + *details.UpstreamInferenceCompletionsCost
		upstreamCost = &sum
	}
	if !isValidCost(upstreamCost) {
		return
	}
	usage.Cost.Total = *reported.Cost + *upstreamCost
	usage.Cost.TotalFromProvider = boolPtrTrue()
}

func boolPtrTrue() *bool {
	value := true
	return &value
}
