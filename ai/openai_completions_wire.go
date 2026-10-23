package ai

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Port of api/openai-completions.ts: compat auto-detection, params building,
// and message/tool conversion. The streaming loop lives in
// openai_completions_stream.go.

// OpenAICompletionsOptions extends StreamOptions for openai-completions.
type OpenAICompletionsOptions struct {
	StreamOptions
	// ToolChoice is the ChatCompletionToolChoiceOption wire shape.
	ToolChoice json.RawMessage
	// ReasoningEffort: "minimal"|"low"|"medium"|"high"|"xhigh"|"max".
	ReasoningEffort ThinkingLevel
	// ThinkingBudgets per level, used with thinkingTokenBudgetField.
	ThinkingBudgets *ThinkingBudgets
}

// ResolvedOpenAICompletionsCompat is the fully-resolved compat surface
// (defaults + URL detection + model.compat overrides).
type ResolvedOpenAICompletionsCompat struct {
	SupportsStore                               bool
	SupportsDeveloperRole                       bool
	SupportsReasoningEffort                     bool
	SupportsUsageInStreaming                    bool
	SupportsFinishReason                        bool
	MaxTokensField                              string // "max_completion_tokens" | "max_tokens"
	RequiresToolResultName                      bool
	RequiresAssistantAfterToolResult            bool
	RequiresThinkingAsText                      bool
	RequiresReasoningContentOnAssistantMessages bool
	ThinkingFormat                              string
	ChatTemplateKwargs                          map[string]json.RawMessage
	ChatTemplateArgs                            map[string]json.RawMessage
	ZaiToolStream                               bool
	SupportsThinkingTokenBudget                 bool
	ThinkingTokenBudgetField                    ThinkingTokenBudgetField
	SupportsStrictMode                          bool
	SupportsOpenAIGrammarTools                  bool
	SupportsMidConvoSystemMessages              bool
	SupportsMidConvoToolAdditions               bool
	CacheControlFormat                          string // "anthropic" or ""
	SendSessionAffinityHeaders                  bool
	SessionAffinityFormat                       SessionAffinityFormat
	SupportsLongCacheRetention                  bool
	VllmPriority                                *float64
}

// DetectOpenAICompletionsCompat auto-detects compatibility settings from
// provider name and baseUrl (port of detectCompat).
func DetectOpenAICompletionsCompat(model *Model) ResolvedOpenAICompletionsCompat {
	provider := model.Provider
	baseURL := model.BaseURL
	contains := func(s, sub string) bool { return strings.Contains(s, sub) }
	containsFold := func(s, sub string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(sub)) }

	isZai := provider == "zai" || provider == "zai-coding-cn" || contains(baseURL, "api.z.ai") || contains(baseURL, "open.bigmodel.cn")
	isTogether := provider == "together" || contains(baseURL, "api.together.ai") || contains(baseURL, "api.together.xyz")
	isMoonshot := provider == "moonshotai" || provider == "moonshotai-cn" || contains(baseURL, "api.moonshot.")
	isOpenRouter := provider == "openrouter" || contains(baseURL, "openrouter.ai")
	isCloudflareWorkersAI := provider == "cloudflare-workers-ai" || contains(baseURL, "api.cloudflare.com")
	isCloudflareAiGateway := provider == "cloudflare-ai-gateway" || contains(baseURL, "gateway.ai.cloudflare.com")
	isNvidia := provider == "nvidia" || contains(baseURL, "integrate.api.nvidia.com")
	isAntLing := provider == "ant-ling" || contains(baseURL, "api.ant-ling.com")
	isDeepSeek := provider == "deepseek" || containsFold(baseURL, "deepseek.com")

	isNonStandard := isNvidia || provider == "cerebras" || contains(baseURL, "cerebras.ai") ||
		provider == "xai" || contains(baseURL, "api.x.ai") ||
		isTogether || contains(baseURL, "chutes.ai") || isDeepSeek || isZai || isMoonshot ||
		provider == "opencode" || contains(baseURL, "opencode.ai") ||
		isCloudflareWorkersAI || isCloudflareAiGateway || isAntLing

	useMaxTokens := contains(baseURL, "chutes.ai") || isDeepSeek || isMoonshot || isCloudflareAiGateway ||
		isTogether || isNvidia || isAntLing || isZai

	isGrok := provider == "xai" || contains(baseURL, "api.x.ai")
	isOpenRouterDeveloperRoleModel := isOpenRouter && (strings.HasPrefix(model.ID, "anthropic/") || strings.HasPrefix(model.ID, "openai/"))
	cacheControlFormat := ""
	if provider == "openrouter" && strings.HasPrefix(model.ID, "anthropic/") {
		cacheControlFormat = "anthropic"
	}

	maxTokensField := "max_completion_tokens"
	if useMaxTokens {
		maxTokensField = "max_tokens"
	}
	thinkingFormat := "openai"
	switch {
	case isDeepSeek:
		thinkingFormat = "deepseek"
	case isZai:
		thinkingFormat = "zai"
	case isTogether:
		thinkingFormat = "together"
	case isAntLing:
		thinkingFormat = "ant-ling"
	case isOpenRouter:
		thinkingFormat = "openrouter"
	}

	return ResolvedOpenAICompletionsCompat{
		SupportsStore:                               !isNonStandard,
		SupportsDeveloperRole:                       isOpenRouterDeveloperRoleModel || (!isNonStandard && !isOpenRouter),
		SupportsReasoningEffort:                     !isGrok && !isZai && !isMoonshot && !isTogether && !isCloudflareAiGateway && !isNvidia && !isAntLing,
		SupportsUsageInStreaming:                    true,
		SupportsFinishReason:                        true,
		MaxTokensField:                              maxTokensField,
		RequiresToolResultName:                      false,
		RequiresAssistantAfterToolResult:            false,
		RequiresThinkingAsText:                      false,
		RequiresReasoningContentOnAssistantMessages: isDeepSeek,
		ThinkingFormat:                              thinkingFormat,
		ChatTemplateKwargs:                          map[string]json.RawMessage{},
		ChatTemplateArgs:                            map[string]json.RawMessage{},
		ZaiToolStream:                               false,
		SupportsThinkingTokenBudget:                 false,
		// OpenAI compatibility alone does not imply strict JSON-schema tool
		// support; only an explicit model.compat.supportsStrictMode opts in
		// (upstream detectCompat).
		SupportsStrictMode:             false,
		SupportsOpenAIGrammarTools:     false,
		SupportsMidConvoSystemMessages: false,
		SupportsMidConvoToolAdditions:  false,
		CacheControlFormat:             cacheControlFormat,
		SendSessionAffinityHeaders:     isOpenRouter,
		SessionAffinityFormat: func() SessionAffinityFormat {
			if isOpenRouter {
				return SessionAffinityOpenRouter
			}
			return SessionAffinityOpenAI
		}(),
		SupportsLongCacheRetention: !(isTogether || isCloudflareWorkersAI || isCloudflareAiGateway || isNvidia || isAntLing),
	}
}

// GetOpenAICompletionsCompat resolves compat: auto-detect then explicit
// model.compat overrides (port of getCompat).
func GetOpenAICompletionsCompat(model *Model) ResolvedOpenAICompletionsCompat {
	detected := DetectOpenAICompletionsCompat(model)
	if model.Compat == nil || model.Compat.OpenAICompletions == nil {
		return detected
	}
	c := model.Compat.OpenAICompletions
	boolOr := func(v *bool, d bool) bool {
		if v != nil {
			return *v
		}
		return d
	}
	out := detected
	out.SupportsStore = boolOr(c.SupportsStore, detected.SupportsStore)
	out.SupportsDeveloperRole = boolOr(c.SupportsDeveloperRole, detected.SupportsDeveloperRole)
	out.SupportsReasoningEffort = boolOr(c.SupportsReasoningEffort, detected.SupportsReasoningEffort)
	out.SupportsUsageInStreaming = boolOr(c.SupportsUsageInStreaming, detected.SupportsUsageInStreaming)
	out.SupportsFinishReason = boolOr(c.SupportsFinishReason, detected.SupportsFinishReason)
	if c.MaxTokensField != nil {
		out.MaxTokensField = *c.MaxTokensField
	}
	out.RequiresToolResultName = boolOr(c.RequiresToolResultName, detected.RequiresToolResultName)
	out.RequiresAssistantAfterToolResult = boolOr(c.RequiresAssistantAfterToolResult, detected.RequiresAssistantAfterToolResult)
	out.RequiresThinkingAsText = boolOr(c.RequiresThinkingAsText, detected.RequiresThinkingAsText)
	out.RequiresReasoningContentOnAssistantMessages = boolOr(c.RequiresReasoningContentOnAssistantMessages, detected.RequiresReasoningContentOnAssistantMessages)
	if c.ThinkingFormat != nil {
		out.ThinkingFormat = *c.ThinkingFormat
	}
	if c.ChatTemplateKwargs != nil {
		out.ChatTemplateKwargs = c.ChatTemplateKwargs
	}
	if c.ChatTemplateArgs != nil {
		out.ChatTemplateArgs = c.ChatTemplateArgs
	}
	out.ZaiToolStream = boolOr(c.ZaiToolStream, detected.ZaiToolStream)
	out.SupportsThinkingTokenBudget = boolOr(c.SupportsThinkingTokenBudget, detected.SupportsThinkingTokenBudget)
	if c.ThinkingTokenBudgetField != nil {
		out.ThinkingTokenBudgetField = *c.ThinkingTokenBudgetField
	}
	out.SupportsStrictMode = boolOr(c.SupportsStrictMode, detected.SupportsStrictMode)
	out.SupportsOpenAIGrammarTools = boolOr(c.SupportsOpenAIGrammarTools, detected.SupportsOpenAIGrammarTools)
	out.SupportsMidConvoSystemMessages = boolOr(c.SupportsMidConvoSystemMessages, detected.SupportsMidConvoSystemMessages)
	out.SupportsMidConvoToolAdditions = boolOr(c.SupportsMidConvoToolAdditions, detected.SupportsMidConvoToolAdditions)
	if c.CacheControlFormat != nil {
		out.CacheControlFormat = *c.CacheControlFormat
	}
	out.SendSessionAffinityHeaders = boolOr(c.SendSessionAffinityHeaders, detected.SendSessionAffinityHeaders)
	if c.SessionAffinityFormat != nil {
		out.SessionAffinityFormat = *c.SessionAffinityFormat
	}
	out.SupportsLongCacheRetention = boolOr(c.SupportsLongCacheRetention, detected.SupportsLongCacheRetention)
	if c.VllmPriority != nil {
		out.VllmPriority = c.VllmPriority
	}
	return out
}

// OpenAI wire types (Chat Completions).

// OpenAIMessage is one ChatCompletionMessageParam.
type OpenAIMessage struct {
	Role string `json:"role"`
	// Content is the raw message content: a JSON string, a content-parts
	// array, or openaiNull for the assistant default (upstream `content:
	// null`). nil omits the key (Kimi tool system message).
	Content    json.RawMessage  `json:"content,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	Name       string           `json:"name,omitempty"`
	// Kimi tool system message.
	Tools []OpenAITool `json:"tools,omitempty"`
	// Reasoning extension fields (reasoning_content / reasoning /
	// reasoning_text) and reasoning_details, merged in the wire order below.
	ReasoningContent *string           `json:"reasoning_content,omitempty"`
	Reasoning        *string           `json:"reasoning,omitempty"`
	ReasoningText    *string           `json:"reasoning_text,omitempty"`
	ReasoningDetails []json.RawMessage `json:"reasoning_details,omitempty"`
}

// OpenAIContentPart is a text or image content part.
type OpenAIContentPart struct {
	Type     string          `json:"type"` // "text" | "image_url"
	Text     string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL `json:"image_url,omitempty"`
	// Anthropic-style cache control on cacheControlFormat providers.
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// OpenAIImageURL is a data-URL image reference.
type OpenAIImageURL struct {
	URL string `json:"url"`
}

// OpenAIToolCall is one assistant tool call (function or custom/grammar).
type OpenAIToolCall struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"` // "function" | "custom"
	Function *OpenAIFunction `json:"function,omitempty"`
	Custom   *OpenAICustom   `json:"custom,omitempty"`
}

// OpenAIFunction is a function tool call body.
type OpenAIFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// OpenAICustom is a custom (grammar) tool call body.
type OpenAICustom struct {
	Name  string `json:"name"`
	Input string `json:"input"`
}

// OpenAITool is one tool definition.
type OpenAITool struct {
	Type         string                 `json:"type"` // "function" | "custom"
	Function     *OpenAIFunctionDef     `json:"function,omitempty"`
	Custom       *OpenAICustomDef       `json:"custom,omitempty"`
	CacheControl *AnthropicCacheControl `json:"cache_control,omitempty"`
}

// OpenAIFunctionDef is a function tool definition.
type OpenAIFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// OpenAICustomDef is a custom (grammar) tool definition.
type OpenAICustomDef struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Format      *OpenAIGrammarFormat `json:"format,omitempty"`
}

// OpenAIGrammarFormat is a grammar format descriptor.
type OpenAIGrammarFormat struct {
	Type    string           `json:"type"` // "grammar"
	Grammar OpenAIGrammarDef `json:"grammar"`
}

// OpenAIGrammarDef is the syntax + definition pair.
type OpenAIGrammarDef struct {
	Syntax     string `json:"syntax"` // "lark" | "regex"
	Definition string `json:"definition"`
}

// OpenAICompletionsParams is the streaming request body.
type OpenAICompletionsParams struct {
	Model    string          `json:"model"`
	Messages []OpenAIMessage `json:"messages"`
	Stream   bool            `json:"stream"`

	// Optional/request-shaped fields, emitted in upstream key order via
	// MarshalJSON override below.
	PromptCacheKey           *string                    `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention     *string                    `json:"prompt_cache_retention,omitempty"`
	StreamOptions            *OpenAIStreamOptions       `json:"stream_options,omitempty"`
	Store                    *bool                      `json:"store,omitempty"`
	MaxCompletionTokens      *int64                     `json:"max_completion_tokens,omitempty"`
	MaxTokens                *int64                     `json:"max_tokens,omitempty"`
	Temperature              *float64                   `json:"temperature,omitempty"`
	Tools                    []OpenAITool               `json:"tools,omitempty"`
	ToolChoice               json.RawMessage            `json:"tool_choice,omitempty"`
	ToolStream               *bool                      `json:"tool_stream,omitempty"`
	Priority                 *float64                   `json:"priority,omitempty"`
	Thinking                 *OpenAIThinkingParam       `json:"thinking,omitempty"`
	EnableThinking           *bool                      `json:"enable_thinking,omitempty"`
	ChatTemplateKwargs       map[string]any             `json:"chat_template_kwargs,omitempty"`
	ChatTemplateArgs         map[string]any             `json:"chat_template_args,omitempty"`
	ReasoningEffort          *string                    `json:"reasoning_effort,omitempty"`
	ReasoningObj             *OpenAIReasoningParam      `json:"reasoning,omitempty"`
	ThinkingTokenBudget      *int64                     `json:"-"`
	ThinkingTokenBudgetField string                     `json:"-"`
	SamplingExtras           map[string]json.RawMessage `json:"-"`
	ProviderRouting          json.RawMessage            `json:"-"`
	ProviderOptions          json.RawMessage            `json:"-"`
}

// openaiNull is the explicit JSON null for assistant content (upstream
// `content: null`), distinct from nil (key omitted, e.g. Kimi tool messages).
var openaiNull = json.RawMessage("null")

// OpenAIStreamOptions is the stream_options request field.
type OpenAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

// OpenAIThinkingParam is zai/deepseek thinking config.
type OpenAIThinkingParam struct {
	Type          string `json:"type"` // "enabled" | "disabled"
	ClearThinking *bool  `json:"clear_thinking,omitempty"`
}

// OpenAIReasoningParam is OpenRouter's nested reasoning object.
type OpenAIReasoningParam struct {
	Effort  *string `json:"effort,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

// orderedJSONFields emits object members in first-assignment order, the way a
// JavaScript object literal plus later property assignments does: replacing an
// existing member keeps its position, a new member appends. Upstream builds
// every request params object this way, and the payloads are byte-compared.
type orderedJSONFields struct {
	keys   []string
	values map[string]json.RawMessage
}

func (o *orderedJSONFields) set(key string, value json.RawMessage) {
	if o.values == nil {
		o.values = map[string]json.RawMessage{}
	}
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *orderedJSONFields) setAny(key string, value any) error {
	enc, err := MarshalJSON(value)
	if err != nil {
		return err
	}
	o.set(key, enc)
	return nil
}

func (o *orderedJSONFields) marshal() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for index, key := range o.keys {
		if index > 0 {
			buf.WriteByte(',')
		}
		keyEnc, err := MarshalJSON(key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyEnc)
		buf.WriteByte(':')
		buf.Write(o.values[key])
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// MarshalJSON emits upstream's key order, including the dynamic
// thinking-token-budget field and sampling extras merged last.
func (p *OpenAICompletionsParams) MarshalJSON() ([]byte, error) {
	var fields orderedJSONFields
	if err := fields.setAny("model", p.Model); err != nil {
		return nil, err
	}
	if err := fields.setAny("messages", p.Messages); err != nil {
		return nil, err
	}
	if err := fields.setAny("stream", p.Stream); err != nil {
		return nil, err
	}
	if p.PromptCacheKey != nil {
		if err := fields.setAny("prompt_cache_key", *p.PromptCacheKey); err != nil {
			return nil, err
		}
	}
	if p.PromptCacheRetention != nil {
		if err := fields.setAny("prompt_cache_retention", *p.PromptCacheRetention); err != nil {
			return nil, err
		}
	}
	if p.StreamOptions != nil {
		if err := fields.setAny("stream_options", p.StreamOptions); err != nil {
			return nil, err
		}
	}
	if p.Store != nil {
		if err := fields.setAny("store", *p.Store); err != nil {
			return nil, err
		}
	}
	if p.MaxCompletionTokens != nil {
		if err := fields.setAny("max_completion_tokens", *p.MaxCompletionTokens); err != nil {
			return nil, err
		}
	}
	if p.MaxTokens != nil {
		if err := fields.setAny("max_tokens", *p.MaxTokens); err != nil {
			return nil, err
		}
	}
	if p.Temperature != nil {
		if err := fields.setAny("temperature", *p.Temperature); err != nil {
			return nil, err
		}
	}
	if p.Tools != nil {
		if err := fields.setAny("tools", p.Tools); err != nil {
			return nil, err
		}
		// tool_stream is assigned inside upstream's tools branch, before the
		// later tool_choice assignment.
		if p.ToolStream != nil {
			if err := fields.setAny("tool_stream", *p.ToolStream); err != nil {
				return nil, err
			}
		}
	}
	if p.ToolChoice != nil {
		fields.set("tool_choice", p.ToolChoice)
	}
	if p.Priority != nil {
		if err := fields.setAny("priority", *p.Priority); err != nil {
			return nil, err
		}
	}
	if p.Thinking != nil {
		if err := fields.setAny("thinking", p.Thinking); err != nil {
			return nil, err
		}
	}
	if p.EnableThinking != nil {
		if err := fields.setAny("enable_thinking", *p.EnableThinking); err != nil {
			return nil, err
		}
	}
	if p.ChatTemplateKwargs != nil {
		if err := fields.setAny("chat_template_kwargs", p.ChatTemplateKwargs); err != nil {
			return nil, err
		}
	}
	if p.ChatTemplateArgs != nil {
		if err := fields.setAny("chat_template_args", p.ChatTemplateArgs); err != nil {
			return nil, err
		}
	}
	// The together format assigns reasoning before reasoning_effort; the other
	// thinking formats set only one of the two.
	if p.ReasoningObj != nil {
		if err := fields.setAny("reasoning", p.ReasoningObj); err != nil {
			return nil, err
		}
	}
	if p.ReasoningEffort != nil {
		if err := fields.setAny("reasoning_effort", *p.ReasoningEffort); err != nil {
			return nil, err
		}
	}
	// Thinking token budget under its provider-specific field name.
	if p.ThinkingTokenBudget != nil && p.ThinkingTokenBudgetField != "" {
		if err := fields.setAny(p.ThinkingTokenBudgetField, *p.ThinkingTokenBudget); err != nil {
			return nil, err
		}
	}
	if p.ProviderRouting != nil {
		fields.set("provider", p.ProviderRouting)
	}
	if p.ProviderOptions != nil {
		fields.set("providerOptions", p.ProviderOptions)
	}
	// Sampling params are merged last so they override the named fields. The
	// model's own member order is not retained in the port's map form (D267),
	// so the extras are applied in sorted key order.
	extras := make([]string, 0, len(p.SamplingExtras))
	for key := range p.SamplingExtras {
		extras = append(extras, key)
	}
	sortStrings(extras)
	for _, key := range extras {
		fields.set(key, p.SamplingExtras[key])
	}
	return fields.marshal()
}
