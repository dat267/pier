package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"
)

const openRouterDefaultBaseURL = "https://openrouter.ai/api/v1"
const openRouterUserModelsTimeout = 15 * time.Second

// PersonalizeOpenRouterCatalog narrows an OpenRouter catalog to the chat models
// available to the refreshing credential, using that key's model limits and prices.
// Non-chat models pass through unchanged. Unauthorized, forbidden, and unsupported
// endpoints fall back to the supplied catalog for proxies and older gateways.
func PersonalizeOpenRouterCatalog(catalog []*Model, refresh *RefreshModelsContext) ([]*Model, error) {
	if refresh == nil || refresh.Credential == nil {
		return catalog, nil
	}
	key := ""
	switch refresh.Credential.Type {
	case CredentialAPIKey:
		if refresh.Credential.APIKey != nil {
			key = refresh.Credential.APIKey.Key
		}
	case CredentialOAuth:
		if refresh.Credential.OAuth != nil {
			key = refresh.Credential.OAuth.Access
		}
	}
	if key == "" {
		return catalog, nil
	}

	baseURL := strings.TrimRight(refresh.BaseURL, "/")
	if baseURL == "" {
		baseURL = openRouterDefaultBaseURL
	}
	ctx := refresh.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, openRouterUserModelsTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models/user", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
		return catalog, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("OpenRouter model list request failed: %d", response.StatusCode)
	}
	var body struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || len(body.Data) == 0 || body.Data[0] != '[' {
		return nil, fmt.Errorf("Invalid OpenRouter model list response")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(body.Data, &rows); err != nil {
		return nil, fmt.Errorf("Invalid OpenRouter model list response")
	}
	allowed := make(map[string]openRouterUserModel, len(rows))
	for _, row := range rows {
		var item openRouterUserModel
		if json.Unmarshal(row, &item) == nil && item.ID != "" {
			allowed[item.ID] = item
		}
	}

	personalized := make([]*Model, 0, len(catalog))
	for _, model := range catalog {
		if model == nil {
			continue
		}
		if model.Type != "" && model.Type != "chat" {
			personalized = append(personalized, model)
			continue
		}
		item, ok := allowed[model.ID]
		if !ok {
			continue
		}
		copyModel := *model
		contextWindow := item.TopProvider.ContextLength
		if contextWindow <= 0 {
			contextWindow = item.ContextLength
		}
		if contextWindow > 0 {
			copyModel.ContextWindow = contextWindow
		}
		if item.TopProvider.MaxCompletionTokens > 0 {
			copyModel.MaxTokens = item.TopProvider.MaxCompletionTokens
		}
		if len(item.Pricing.Prompt) > 0 || len(item.Pricing.Completion) > 0 ||
			len(item.Pricing.InputCacheRead) > 0 || len(item.Pricing.InputCacheWrite) > 0 {
			copyModel.Cost = openRouterModelCost(item.Pricing)
		}
		personalized = append(personalized, &copyModel)
	}
	return personalized, nil
}

type openRouterUserModel struct {
	ID            string `json:"id"`
	ContextLength int64  `json:"context_length"`
	TopProvider   struct {
		ContextLength       int64 `json:"context_length"`
		MaxCompletionTokens int64 `json:"max_completion_tokens"`
	} `json:"top_provider"`
	Pricing openRouterPricing `json:"pricing"`
}

type openRouterPricing struct {
	Prompt          json.RawMessage             `json:"prompt"`
	Completion      json.RawMessage             `json:"completion"`
	InputCacheRead  json.RawMessage             `json:"input_cache_read"`
	InputCacheWrite json.RawMessage             `json:"input_cache_write"`
	Overrides       []openRouterPricingOverride `json:"overrides"`
}

type openRouterPricingOverride struct {
	MinPromptTokens *int64          `json:"min_prompt_tokens"`
	UTCStart        json.RawMessage `json:"utc_start"`
	UTCEnd          json.RawMessage `json:"utc_end"`
	UTCDays         json.RawMessage `json:"utc_days"`
	Prompt          json.RawMessage `json:"prompt"`
	Completion      json.RawMessage `json:"completion"`
	InputCacheRead  json.RawMessage `json:"input_cache_read"`
	InputCacheWrite json.RawMessage `json:"input_cache_write"`
}

func openRouterModelCost(pricing openRouterPricing) ModelCost {
	base := ModelCostRates{
		Input:      openRouterPerMillion(pricing.Prompt, 0),
		Output:     openRouterPerMillion(pricing.Completion, 0),
		CacheRead:  openRouterPerMillion(pricing.InputCacheRead, 0),
		CacheWrite: openRouterPerMillion(pricing.InputCacheWrite, 0),
	}
	cost := ModelCost{ModelCostRates: base}
	for _, override := range pricing.Overrides {
		if override.MinPromptTokens == nil || len(override.UTCStart) > 0 || len(override.UTCEnd) > 0 || len(override.UTCDays) > 0 {
			continue
		}
		cost.Tiers = append(cost.Tiers, ModelCostTier{
			InputTokensAbove: *override.MinPromptTokens,
			ModelCostRates: ModelCostRates{
				Input:      openRouterPerMillion(override.Prompt, base.Input),
				Output:     openRouterPerMillion(override.Completion, base.Output),
				CacheRead:  openRouterPerMillion(override.InputCacheRead, base.CacheRead),
				CacheWrite: openRouterPerMillion(override.InputCacheWrite, base.CacheWrite),
			},
		})
	}
	return cost
}

func openRouterPerMillion(raw json.RawMessage, fallback float64) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return fallback
	}
	var value float64
	if raw[0] == '"' {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return fallback
		}
		if _, err := fmt.Sscan(text, &value); err != nil {
			return fallback
		}
	} else if err := json.Unmarshal(raw, &value); err != nil {
		return fallback
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return fallback
	}
	return math.Round(value*1_000_000*1_000_000) / 1_000_000
}
