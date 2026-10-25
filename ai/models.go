package ai

import (
	"bytes"
	"encoding/json"
)

// Port of the Model half of types.ts: ModelCost, Model, and the per-API
// compat interfaces.

// ModelCostRates is $/million-token pricing.
type ModelCostRates struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// ModelCostTier is a request-wide pricing tier. The highest matching input
// threshold applies to the full request.
type ModelCostTier struct {
	ModelCostRates
	// InputTokensAbove: use this tier for requests whose total input usage
	// exceeds this token count.
	InputTokensAbove int64 `json:"inputTokensAbove"`
}

// ModelCost is a model's pricing with optional tiers.
type ModelCost struct {
	ModelCostRates
	Tiers []ModelCostTier `json:"tiers,omitempty"`
}

// OpenAICompletionsCompat holds compatibility settings for OpenAI-compatible
// completions APIs. Use it to override URL-based auto-detection for custom
// providers.
type OpenAICompletionsCompat struct {
	SupportsStore                               *bool                      `json:"supportsStore,omitempty"`
	SupportsDeveloperRole                       *bool                      `json:"supportsDeveloperRole,omitempty"`
	SupportsReasoningEffort                     *bool                      `json:"supportsReasoningEffort,omitempty"`
	SupportsUsageInStreaming                    *bool                      `json:"supportsUsageInStreaming,omitempty"`
	SupportsFinishReason                        *bool                      `json:"supportsFinishReason,omitempty"`
	MaxTokensField                              *string                    `json:"maxTokensField,omitempty"` // "max_completion_tokens" | "max_tokens"
	RequiresToolResultName                      *bool                      `json:"requiresToolResultName,omitempty"`
	RequiresAssistantAfterToolResult            *bool                      `json:"requiresAssistantAfterToolResult,omitempty"`
	RequiresThinkingAsText                      *bool                      `json:"requiresThinkingAsText,omitempty"`
	RequiresReasoningContentOnAssistantMessages *bool                      `json:"requiresReasoningContentOnAssistantMessages,omitempty"`
	ThinkingFormat                              *string                    `json:"thinkingFormat,omitempty"`
	ChatTemplateKwargs                          map[string]json.RawMessage `json:"chatTemplateKwargs,omitempty"`
	ChatTemplateArgs                            map[string]json.RawMessage `json:"chatTemplateArgs,omitempty"`
	OpenRouterRouting                           *OpenRouterRouting         `json:"openRouterRouting,omitempty"`
	VercelGatewayRouting                        *VercelGatewayRouting      `json:"vercelGatewayRouting,omitempty"`
	ZaiToolStream                               *bool                      `json:"zaiToolStream,omitempty"`
	ThinkingTokenBudgetField                    *string                    `json:"thinkingTokenBudgetField,omitempty"`
	SupportsThinkingTokenBudget                 *bool                      `json:"supportsThinkingTokenBudget,omitempty"`
	SupportsOpenAIGrammarTools                  *bool                      `json:"supportsOpenAIGrammarTools,omitempty"`
	SupportsMidConvoSystemMessages              *bool                      `json:"supportsMidConvoSystemMessages,omitempty"`
	SupportsMidConvoToolAdditions               *bool                      `json:"supportsMidConvoToolAdditions,omitempty"`
	SupportsStrictMode                          *bool                      `json:"supportsStrictMode,omitempty"`
	CacheControlFormat                          *string                    `json:"cacheControlFormat,omitempty"` // "anthropic"
	SendSessionAffinityHeaders                  *bool                      `json:"sendSessionAffinityHeaders,omitempty"`
	SessionAffinityFormat                       *SessionAffinityFormat     `json:"sessionAffinityFormat,omitempty"`
	SupportsLongCacheRetention                  *bool                      `json:"supportsLongCacheRetention,omitempty"`
	VllmPriority                                *float64                   `json:"vllmPriority,omitempty"`
}

// OpenAIResponsesCompat holds compatibility settings for OpenAI Responses APIs.
type OpenAIResponsesCompat struct {
	SupportsDeveloperRole           *bool                  `json:"supportsDeveloperRole,omitempty"`
	SupportsMidConvoSystemMessages  *bool                  `json:"supportsMidConvoSystemMessages,omitempty"`
	SessionAffinityFormat           *SessionAffinityFormat `json:"sessionAffinityFormat,omitempty"`
	SupportsLongCacheRetention      *bool                  `json:"supportsLongCacheRetention,omitempty"`
	SupportsStrictMode              *bool                  `json:"supportsStrictMode,omitempty"`
	SupportsOpenAIGrammarTools      *bool                  `json:"supportsOpenAIGrammarTools,omitempty"`
	SupportsAdditionalTools         *bool                  `json:"supportsAdditionalTools,omitempty"`
	SupportsToolSearch              *bool                  `json:"supportsToolSearch,omitempty"`
	SupportsExplicitPromptCacheMode *bool                  `json:"supportsExplicitPromptCacheMode,omitempty"`
	SupportsMidConvoEffort          *bool                  `json:"supportsMidConvoEffort,omitempty"`
	SupportsMaxOutputTokens         *bool                  `json:"supportsMaxOutputTokens,omitempty"`
}

// AnthropicAllowedFallbackModel names a model Anthropic accepts in
// `fallbacks` for server-side refusal fallback, with local pricing metadata.
type AnthropicAllowedFallbackModel struct {
	Provider ProviderId `json:"provider"`
	Model    string     `json:"model"`
	Cost     ModelCost  `json:"cost"`
}

// AnthropicMessagesCompat holds compatibility settings for Anthropic
// Messages-compatible APIs.
type AnthropicMessagesCompat struct {
	SupportsEagerToolInputStreaming *bool                           `json:"supportsEagerToolInputStreaming,omitempty"`
	SupportsLongCacheRetention      *bool                           `json:"supportsLongCacheRetention,omitempty"`
	SendSessionAffinityHeaders      *bool                           `json:"sendSessionAffinityHeaders,omitempty"`
	SessionAffinityFormat           *string                         `json:"sessionAffinityFormat,omitempty"` // "openrouter"
	SupportsCacheControlOnTools     *bool                           `json:"supportsCacheControlOnTools,omitempty"`
	SupportsTemperature             *bool                           `json:"supportsTemperature,omitempty"`
	ForceAdaptiveThinking           *bool                           `json:"forceAdaptiveThinking,omitempty"`
	AllowEmptySignature             *bool                           `json:"allowEmptySignature,omitempty"`
	SupportsStrictTools             *bool                           `json:"supportsStrictTools,omitempty"`
	SupportsMidConvoEffort          *bool                           `json:"supportsMidConvoEffort,omitempty"`
	SupportsMidConvoSystemMessages  *bool                           `json:"supportsMidConvoSystemMessages,omitempty"`
	SupportsMidConvoToolChanges     *bool                           `json:"supportsMidConvoToolChanges,omitempty"`
	AllowedFallbackModels           []AnthropicAllowedFallbackModel `json:"allowedFallbackModels,omitempty"`
}

// BedrockCompat holds compatibility settings for Amazon Bedrock models.
type BedrockCompat struct {
	SupportsStrictMode *bool `json:"supportsStrictMode,omitempty"`
}

// MistralConversationsCompat holds compatibility settings for the Mistral
// chat API.
type MistralConversationsCompat struct {
	SupportsMidConvoSystemMessages *bool `json:"supportsMidConvoSystemMessages,omitempty"`
}

// OpenRouterRouting is OpenRouter provider routing preferences, sent as the
// `provider` field in the request body.
type OpenRouterRouting struct {
	AllowFallbacks         *bool                      `json:"allow_fallbacks,omitempty"`
	RequireParameters      *bool                      `json:"require_parameters,omitempty"`
	DataCollection         *string                    `json:"data_collection,omitempty"` // "deny" | "allow"
	Zdr                    *bool                      `json:"zdr,omitempty"`
	EnforceDistillableText *bool                      `json:"enforce_distillable_text,omitempty"`
	Order                  []string                   `json:"order,omitempty"`
	Only                   []string                   `json:"only,omitempty"`
	Ignore                 []string                   `json:"ignore,omitempty"`
	Quantizations          []string                   `json:"quantizations,omitempty"`
	Sort                   json.RawMessage            `json:"sort,omitempty"` // string | { by, partition }
	MaxPrice               map[string]json.RawMessage `json:"max_price,omitempty"`
	PreferredMinThroughput json.RawMessage            `json:"preferred_min_throughput,omitempty"`
	PreferredMaxLatency    json.RawMessage            `json:"preferred_max_latency,omitempty"`
}

// VercelGatewayRouting is Vercel AI Gateway routing preferences.
type VercelGatewayRouting struct {
	Only  []string `json:"only,omitempty"`
	Order []string `json:"order,omitempty"`
}

// ModelCompat is the union of per-API compat settings; exactly one arm is
// populated, keyed by the model's API.
type ModelCompat struct {
	OpenAICompletions    *OpenAICompletionsCompat
	OpenAIResponses      *OpenAIResponsesCompat
	AnthropicMessages    *AnthropicMessagesCompat
	Bedrock              *BedrockCompat
	MistralConversations *MistralConversationsCompat
}

func (c ModelCompat) MarshalJSON() ([]byte, error) {
	switch {
	case c.OpenAICompletions != nil:
		return json.Marshal(c.OpenAICompletions)
	case c.OpenAIResponses != nil:
		return json.Marshal(c.OpenAIResponses)
	case c.AnthropicMessages != nil:
		return json.Marshal(c.AnthropicMessages)
	case c.Bedrock != nil:
		return json.Marshal(c.Bedrock)
	case c.MistralConversations != nil:
		return json.Marshal(c.MistralConversations)
	default:
		return []byte("null"), nil
	}
}

// DecodeModelCompat decodes a compat object for a known API. The catalog
// loader and generated data always know the model's api, so decoding is
// explicit rather than probed.
func DecodeModelCompat(api Api, data []byte) (*ModelCompat, error) {
	if len(data) == 0 || string(data) == "null" {
		return nil, nil
	}
	c := &ModelCompat{}
	var err error
	switch api {
	case APIAnthropicMessages:
		c.AnthropicMessages = new(AnthropicMessagesCompat)
		err = jsonUnmarshalStrict(data, c.AnthropicMessages)
	case APIOpenAICompletions:
		c.OpenAICompletions = new(OpenAICompletionsCompat)
		err = jsonUnmarshalStrict(data, c.OpenAICompletions)
	case APIOpenAIResponses, APIAzureOpenAIResponses, APIOpenAICodexResponses:
		c.OpenAIResponses = new(OpenAIResponsesCompat)
		err = jsonUnmarshalStrict(data, c.OpenAIResponses)
	case APIMistralConversations:
		c.MistralConversations = new(MistralConversationsCompat)
		err = jsonUnmarshalStrict(data, c.MistralConversations)
	case APIBedrockConverse:
		c.Bedrock = new(BedrockCompat)
		err = jsonUnmarshalStrict(data, c.Bedrock)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// UnmarshalJSON decodes a compat object when the API is unambiguous (used by
// generic JSON round-trips). Catalog loading uses DecodeModelCompat, which
// knows the model's api.
func (c *ModelCompat) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return err
	}
	has := func(k string) bool { _, ok := keys[k]; return ok }
	switch {
	case has("supportsEagerToolInputStreaming") || has("forceAdaptiveThinking") || has("allowedFallbackModels"):
		c.AnthropicMessages = new(AnthropicMessagesCompat)
		return json.Unmarshal(data, c.AnthropicMessages)
	case has("supportsStore") || has("maxTokensField") || has("thinkingFormat") || has("chatTemplateKwargs"):
		c.OpenAICompletions = new(OpenAICompletionsCompat)
		return json.Unmarshal(data, c.OpenAICompletions)
	case has("supportsAdditionalTools") || has("supportsToolSearch") || has("supportsExplicitPromptCacheMode") || has("supportsMaxOutputTokens"):
		c.OpenAIResponses = new(OpenAIResponsesCompat)
		return json.Unmarshal(data, c.OpenAIResponses)
	case len(keys) == 1 && has("supportsMidConvoSystemMessages"):
		c.MistralConversations = new(MistralConversationsCompat)
		return json.Unmarshal(data, c.MistralConversations)
	default:
		c.OpenAIResponses = new(OpenAIResponsesCompat)
		return json.Unmarshal(data, c.OpenAIResponses)
	}
}

// ModelPromptCache holds prompt cache lifetimes per retention tier (seconds).
type ModelPromptCache map[CacheRetention]int

// Model is the unified model descriptor.
// ModelInputLimits bound what a model accepts in one request (upstream
// ModelInputLimitsSchema). The embedded catalogue carries them for most builtin
// models; callers still fall back to their own defaults when a model leaves
// them unset.
type ModelInputLimits struct {
	MaxRequestBytes int64             `json:"maxRequestBytes,omitempty"`
	Images          *ModelImageLimits `json:"images,omitempty"`
}

// ModelImageLimits bound image input for a model.
type ModelImageLimits struct {
	Resize        *ModelImageResize `json:"resize,omitempty"`
	MaxPerMessage int64             `json:"maxPerMessage,omitempty"`
	MaxPerRequest int64             `json:"maxPerRequest,omitempty"`
}

// ModelImageResize is the per-model inline-image resize contract (upstream
// ImageResizeSchema). Unset fields fall back to the caller's defaults, the way
// upstream spreads the object over DEFAULT_OPTIONS.
type ModelImageResize struct {
	MaxWidth    int64 `json:"maxWidth,omitempty"`
	MaxHeight   int64 `json:"maxHeight,omitempty"`
	MaxBytes    int64 `json:"maxBytes,omitempty"`
	JPEGQuality int64 `json:"jpegQuality,omitempty"`
}

type Model struct {
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	API      Api        `json:"api"`
	Provider ProviderId `json:"provider"`
	BaseURL  string     `json:"baseUrl"`
	// Reasoning reports whether the model supports thinking/reasoning.
	Reasoning        bool              `json:"reasoning"`
	ThinkingLevelMap ThinkingLevelMap  `json:"thinkingLevelMap,omitempty"`
	Input            []string          `json:"input"` // "text" | "image"
	InputLimits      *ModelInputLimits `json:"inputLimits,omitempty"`
	Cost             ModelCost         `json:"cost"`
	ContextWindow    int64             `json:"contextWindow"`
	MaxTokens        int64             `json:"maxTokens"`
	// SamplingParams are default sampling parameters for this model. See
	// StreamOptions.SamplingParams; per-request keys override these.
	SamplingParams SamplingParams `json:"samplingParams,omitempty"`
	// SamplingParamsOrder retains JavaScript insertion order for samplingParams
	// loaded from JSON. Go map literals should set this explicitly when wire
	// member order matters.
	SamplingParamsOrder []string `json:"-"`
	// SamplingParamsByThinkingLevelOrder retains per-level JSON key order.
	SamplingParamsByThinkingLevelOrder map[ModelThinkingLevel][]string `json:"-"`
	// SamplingParamsByThinkingLevel overrides SamplingParams per effective
	// pi thinking level.
	SamplingParamsByThinkingLevel SamplingParamsByThinkingLevel `json:"samplingParamsByThinkingLevel,omitempty"`
	Headers                       map[string]string             `json:"headers,omitempty"`
	// Compat holds compatibility overrides. If not set, auto-detected from
	// BaseURL.
	Compat *ModelCompat `json:"compat,omitempty"`
	// PromptCache holds the prompt-cache lifetimes per retention tier (seconds).
	// Unset when the provider's cache behavior is unknown; a missing tier means
	// the lifetime is unknown and pi does not warm such caches.
	PromptCache ModelPromptCache `json:"promptCache,omitempty"`
	// Type is the model type; absent means chat (upstream Model.type).
	Type string `json:"type,omitempty"`
	// Loaded records may be image models or carry fields this chat descriptor
	// does not expose. Keep immutable JSON provenance for lossless store clones
	// while still applying changes to the typed fields.
	jsonOriginal json.RawMessage
	jsonBaseline *orderedJSONFields
}

// MarshalJSON preserves loaded fields and member order while applying typed
// edits. Sampling order survives JSON-based model-store clones (upstream
// models-store.ts uses structuredClone).
func (m Model) MarshalJSON() ([]byte, error) {
	fields, err := m.marshalJSONFields()
	if err != nil {
		return nil, err
	}
	if m.jsonOriginal == nil {
		return fields.marshal()
	}
	original, err := decodeOrderedJSONFields(m.jsonOriginal)
	if err != nil {
		return nil, err
	}
	for _, key := range fields.keys {
		before, present := m.jsonBaseline.raw(key)
		after := fields.values[key]
		if !present || !bytes.Equal(before, after) {
			original.set(key, after)
		}
	}
	for _, key := range m.jsonBaseline.keys {
		if _, present := fields.raw(key); !present {
			original.remove(key)
		}
	}
	return original.marshal()
}

func (m Model) marshalJSONFields() (*orderedJSONFields, error) {
	type modelAlias Model
	encoded, err := MarshalJSON(modelAlias(m))
	if err != nil {
		return nil, err
	}
	fields, err := decodeOrderedJSONFields(encoded)
	if err != nil {
		return nil, err
	}
	if len(m.SamplingParams) > 0 {
		params, err := marshalModelSamplingParams(m.SamplingParams, m.SamplingParamsOrder)
		if err != nil {
			return nil, err
		}
		fields.set("samplingParams", params)
	}
	if len(m.SamplingParamsByThinkingLevel) > 0 {
		levels := make(map[ModelThinkingLevel]json.RawMessage, len(m.SamplingParamsByThinkingLevel))
		for level, params := range m.SamplingParamsByThinkingLevel {
			levels[level], err = marshalModelSamplingParams(params, m.SamplingParamsByThinkingLevelOrder[level])
			if err != nil {
				return nil, err
			}
		}
		if err := fields.setAny("samplingParamsByThinkingLevel", levels); err != nil {
			return nil, err
		}
	}
	return fields, nil
}

func marshalModelSamplingParams(params SamplingParams, order []string) ([]byte, error) {
	if params == nil {
		return []byte("null"), nil
	}
	var fields orderedJSONFields
	for _, key := range orderedSamplingKeys(params, order) {
		if err := fields.setAny(key, params[key]); err != nil {
			return nil, err
		}
	}
	return fields.marshal()
}

func (m *Model) UnmarshalJSON(data []byte) error {
	type modelAlias Model
	var decoded modelAlias
	wire := struct {
		*modelAlias
		Compat json.RawMessage `json:"compat"`
	}{modelAlias: &decoded}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	compat, err := DecodeModelCompat(decoded.API, wire.Compat)
	if err != nil {
		return err
	}
	decoded.Compat = compat
	*m = Model(decoded)
	m.captureSamplingParamOrder(data)
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '{' {
		m.jsonOriginal = append(json.RawMessage(nil), data...)
		m.jsonBaseline, err = m.marshalJSONFields()
		if err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) captureSamplingParamOrder(data []byte) {
	m.SamplingParamsOrder = jsonObjectPropertyKeys(data, "samplingParams")
	if raw := jsonObjectProperty(data, "samplingParamsByThinkingLevel"); len(raw) > 0 {
		var levels map[string]json.RawMessage
		if err := json.Unmarshal(raw, &levels); err == nil {
			m.SamplingParamsByThinkingLevelOrder = make(map[ModelThinkingLevel][]string, len(levels))
			for level, params := range levels {
				m.SamplingParamsByThinkingLevelOrder[ModelThinkingLevel(level)] = directJSONObjectKeys(params)
			}
		}
	}
}

func jsonObjectProperty(data []byte, property string) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(data))
	if _, err := dec.Token(); err != nil {
		return nil
	}
	var found json.RawMessage
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil
		}
		if key == property {
			found = value
		}
	}
	return found
}

func jsonObjectPropertyKeys(data []byte, property string) []string {
	return directJSONObjectKeys(jsonObjectProperty(data, property))
}

func directJSONObjectKeys(data []byte) []string {
	dec := json.NewDecoder(bytes.NewReader(data))
	if token, err := dec.Token(); err != nil || token != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		token, err := dec.Token()
		if err != nil {
			return nil
		}
		key, ok := token.(string)
		if !ok {
			return nil
		}
		keys = append(keys, key)
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil
		}
	}
	return keys
}
