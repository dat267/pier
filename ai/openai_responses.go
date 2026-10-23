package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Port of api/openai-responses.ts and api/openai-responses-shared.ts: the
// OpenAI Responses API (GPT-5 / o-series), the Codex-compatible request
// shapes, reasoning encrypted-content replay, custom (grammar) tools, and
// service-tier pricing.

// OpenAIResponsesOptions extends StreamOptions for the Responses API.
type OpenAIResponsesOptions struct {
	StreamOptions
	ReasoningEffort  ThinkingLevel   `json:"-"`
	ReasoningSummary string          `json:"-"` // "auto" | "detailed" | "concise" | ""
	ServiceTier      string          `json:"-"` // "flex" | "priority" | ...
	ToolChoice       json.RawMessage `json:"-"`
	// ResolveServiceTier lets a dialect adjust the tier precedence between the
	// request and the response (Codex prefers an explicit flex/priority
	// request over the response's "default"). Nil keeps the response tier.
	ResolveServiceTier func(responseTier *string, requestTier string) string
}

// toolCallProviders accept the `call_id|item_id` tool-call id form.
var openAIToolCallProviders = map[string]bool{
	"openai": true, "openai-codex": true, "opencode": true,
}

// openAIResponsesMinOutputTokens: the Responses API rejects lower caps
// (upstream issue #6265).
const openAIResponsesMinOutputTokens = 16

// ResolvedOpenAIResponsesCompat is the resolved Responses compat surface.
type ResolvedOpenAIResponsesCompat struct {
	SupportsDeveloperRole           bool
	SupportsMidConvoSystemMessages  bool
	SessionAffinityFormat           SessionAffinityFormat
	SupportsLongCacheRetention      bool
	SupportsStrictMode              bool
	SupportsOpenAIGrammarTools      bool
	SupportsAdditionalTools         bool
	SupportsToolSearch              bool
	SupportsExplicitPromptCacheMode bool
	SupportsMidConvoEffort          bool
	SupportsMaxOutputTokens         bool
}

// DetectOpenAIResponsesSessionAffinity: OpenRouter endpoints use their own
// header name.
func detectOpenAIResponsesSessionAffinity(model *Model) SessionAffinityFormat {
	if model.Provider == "openrouter" || strings.Contains(model.BaseURL, "openrouter.ai") {
		return SessionAffinityOpenRouter
	}
	return SessionAffinityOpenAI
}

// GetOpenAIResponsesCompat resolves the Responses compat surface.
func GetOpenAIResponsesCompat(model *Model) ResolvedOpenAIResponsesCompat {
	compat := ResolvedOpenAIResponsesCompat{
		SupportsDeveloperRole:      true,
		SessionAffinityFormat:      detectOpenAIResponsesSessionAffinity(model),
		SupportsLongCacheRetention: true,
		SupportsMaxOutputTokens:    true,
	}
	if model.Compat != nil && model.Compat.OpenAIResponses != nil {
		c := model.Compat.OpenAIResponses
		if c.SupportsDeveloperRole != nil {
			compat.SupportsDeveloperRole = *c.SupportsDeveloperRole
		}
		if c.SupportsMidConvoSystemMessages != nil {
			compat.SupportsMidConvoSystemMessages = *c.SupportsMidConvoSystemMessages
		}
		if c.SessionAffinityFormat != nil {
			compat.SessionAffinityFormat = *c.SessionAffinityFormat
		}
		if c.SupportsLongCacheRetention != nil {
			compat.SupportsLongCacheRetention = *c.SupportsLongCacheRetention
		}
		if c.SupportsStrictMode != nil {
			compat.SupportsStrictMode = *c.SupportsStrictMode
		}
		if c.SupportsOpenAIGrammarTools != nil {
			compat.SupportsOpenAIGrammarTools = *c.SupportsOpenAIGrammarTools
		}
		if c.SupportsAdditionalTools != nil {
			compat.SupportsAdditionalTools = *c.SupportsAdditionalTools
		}
		if c.SupportsToolSearch != nil {
			compat.SupportsToolSearch = *c.SupportsToolSearch
		}
		if c.SupportsExplicitPromptCacheMode != nil {
			compat.SupportsExplicitPromptCacheMode = *c.SupportsExplicitPromptCacheMode
		}
		if c.SupportsMidConvoEffort != nil {
			compat.SupportsMidConvoEffort = *c.SupportsMidConvoEffort
		}
		if c.SupportsMaxOutputTokens != nil {
			compat.SupportsMaxOutputTokens = *c.SupportsMaxOutputTokens
		}
	}
	return compat
}

// GetPromptCacheRetention: long retention without the explicit cache mode
// uses the 24h field.
func GetPromptCacheRetention(compat ResolvedOpenAIResponsesCompat, cacheRetention CacheRetention) string {
	if cacheRetention == CacheRetentionLong && compat.SupportsLongCacheRetention && !compat.SupportsExplicitPromptCacheMode {
		return "24h"
	}
	return ""
}

// GetPromptCacheOptions: GPT-5.6+ accepts prompt_cache_options instead.
func GetPromptCacheOptions(compat ResolvedOpenAIResponsesCompat, cacheRetention CacheRetention) *OpenAIPromptCacheOptions {
	if !compat.SupportsExplicitPromptCacheMode {
		return nil
	}
	if cacheRetention == CacheRetentionNone {
		return &OpenAIPromptCacheOptions{Mode: "explicit"}
	}
	if cacheRetention == CacheRetentionLong && compat.SupportsLongCacheRetention {
		return &OpenAIPromptCacheOptions{TTL: "30m"}
	}
	return nil
}

// OpenAIPromptCacheOptions is the prompt_cache_options request field.
type OpenAIPromptCacheOptions struct {
	Mode string `json:"mode,omitempty"` // "explicit"
	TTL  string `json:"ttl,omitempty"`  // "30m"
}

// OpenAIResponsesParams is the Responses streaming request body.
type OpenAIResponsesParams struct {
	Model                string                    `json:"model"`
	Input                json.RawMessage           `json:"input"`
	Stream               bool                      `json:"stream"`
	PromptCacheKey       *string                   `json:"prompt_cache_key,omitempty"`
	PromptCacheRetention *string                   `json:"prompt_cache_retention,omitempty"`
	PromptCacheOptions   *OpenAIPromptCacheOptions `json:"prompt_cache_options,omitempty"`
	Store                *bool                     `json:"store,omitempty"`
	MaxOutputTokens      *int64                    `json:"max_output_tokens,omitempty"`
	Temperature          *float64                  `json:"temperature,omitempty"`
	ServiceTier          *string                   `json:"service_tier,omitempty"`
	Tools                []OpenAIResponsesTool     `json:"tools,omitempty"`
	ToolChoice           json.RawMessage           `json:"tool_choice,omitempty"`
	Reasoning            *OpenAIResponsesReasoning `json:"reasoning,omitempty"`
	Include              []string                  `json:"include,omitempty"`
	// Sampling params merge last (key overrides).
	SamplingExtras map[string]json.RawMessage `json:"-"`
}

// OpenAIResponsesReasoning is the reasoning request field.
type OpenAIResponsesReasoning struct {
	Effort  string `json:"effort,omitempty"`
	Summary string `json:"summary,omitempty"`
}

// OpenAIResponsesTool is a function or custom (grammar) tool definition.
type OpenAIResponsesTool struct {
	Type         string               `json:"type"` // "function" | "custom"
	Name         string               `json:"name"`
	Description  string               `json:"description"`
	Parameters   json.RawMessage      `json:"parameters,omitempty"`
	Strict       *bool                `json:"strict,omitempty"`
	Format       *OpenAIGrammarFormat `json:"format,omitempty"`
	DeferLoading *bool                `json:"defer_loading,omitempty"`
}

// ConvertResponsesToolsOptions tune tool conversion.
type ConvertResponsesToolsOptions struct {
	Strict                     *bool
	SupportsStrictMode         bool
	SupportsOpenAIGrammarTools bool
	ToolSearchResult           bool
}

// ConvertResponsesTools converts tool declarations to Responses tools
// (port of convertResponsesTools).
func ConvertResponsesTools(tools []Tool, options *ConvertResponsesToolsOptions) ([]OpenAIResponsesTool, error) {
	if options == nil {
		options = &ConvertResponsesToolsOptions{SupportsStrictMode: true}
	}
	defaultStrict := false
	if options.Strict != nil {
		defaultStrict = *options.Strict
	}
	var out []OpenAIResponsesTool
	for _, tool := range tools {
		grammar, err := ResolveGrammarConstrainedSampling(tool, options.SupportsOpenAIGrammarTools)
		if err != nil {
			return nil, err
		}
		if grammar != nil {
			entry := OpenAIResponsesTool{
				Type: "custom", Name: tool.Name, Description: tool.Description,
				Format: &OpenAIGrammarFormat{
					Type:    "grammar",
					Grammar: OpenAIGrammarDef{Syntax: grammar.Format, Definition: grammar.Definition},
				},
			}
			if options.ToolSearchResult {
				entry.DeferLoading = boolPtr(true)
			}
			out = append(out, entry)
			continue
		}

		constrainedStrict, unset, err := ResolveJSONSchemaStrictSampling(tool, options.SupportsStrictMode, nil)
		if err != nil {
			return nil, err
		}
		strict := defaultStrict
		if !unset {
			strict = constrainedStrict
		}
		entry := OpenAIResponsesTool{
			Type: "function", Name: tool.Name, Description: tool.Description,
			Parameters: GetJSONSchemaToolParameters(tool, strict),
		}
		if options.ToolSearchResult {
			entry.DeferLoading = boolPtr(true)
		}
		if options.SupportsStrictMode {
			entry.Strict = boolPtr(strict)
		}
		out = append(out, entry)
	}
	return out, nil
}

// EncodeTextSignatureV1 serializes the versioned text signature.
func EncodeTextSignatureV1(id string, phase string) string {
	signature := TextSignatureV1{V: 1, ID: id, Phase: phase}
	enc, _ := MarshalJSON(signature)
	return string(enc)
}

// ParseTextSignature decodes a text signature (v1 JSON or legacy string id).
func ParseTextSignature(signature string) *TextSignatureV1 {
	if signature == "" {
		return nil
	}
	if strings.HasPrefix(signature, "{") {
		var parsed TextSignatureV1
		if jsonUnmarshalStrict(json.RawMessage(signature), &parsed) == nil && parsed.V == 1 && parsed.ID != "" {
			if parsed.Phase == "commentary" || parsed.Phase == "final_answer" {
				return &TextSignatureV1{V: 1, ID: parsed.ID, Phase: parsed.Phase}
			}
			return &TextSignatureV1{V: 1, ID: parsed.ID}
		}
	}
	return &TextSignatureV1{V: 1, ID: signature}
}

// responseInputItem is one Responses input item; raw JSON is used because
// replay items (reasoning, function_call) must survive byte-exact.
type responseInputItem = json.RawMessage

// ConvertResponsesMessagesOptions tune message conversion.
type ConvertResponsesMessagesOptions struct {
	IncludeSystemPrompt *bool
	// ToolCallProviders overrides the dialects that accept the
	// `call_id|item_id` tool-call id form.
	ToolCallProviders              map[string]bool
	GrammarToolInputProperties     map[string]string
	SupportsMidConvoSystemMessages bool
	SupportsAdditionalTools        bool
	SupportsToolSearch             bool
	ToolOptions                    *ConvertResponsesToolsOptions
	// ConfigurationUpdateEffort, when set, sends the thinking level as
	// configuration_update items instead of a changing reasoning.effort: one
	// before every earlier reply with the level recorded on it
	// (providerThinkingLevel), and one at the end with this effort. Earlier
	// input therefore never changes, keeping the prompt cache.
	ConfigurationUpdateEffort string
}

// GetResponsesToolSupport says how a Responses transport expresses tool changes
// (upstream getResponsesToolSupport).
//
// `additional_tools` items accumulate: a later item adds tools and replaces
// same-name definitions from earlier items, but it cannot replace a top-level
// tool and cannot remove any tool. Like Codex, every tool is therefore declared
// in `additional_tools` items, starting with one at the head of the input, and
// removals are announced as text. Synthetic tool search can only add tools.
// Without mid-conversation system messages there is nothing to anchor.
func GetResponsesToolSupport(supportsMidConvoSystemMessages bool, supportsAdditionalTools bool, supportsToolSearch bool) TranscriptToolSupport {
	if supportsMidConvoSystemMessages && supportsAdditionalTools {
		return TranscriptToolSupportChanges
	}
	if supportsToolSearch {
		return TranscriptToolSupportAdditions
	}
	return TranscriptToolSupportNone
}

// incrementalToolsNotice precedes every additional_tools item after the first.
// Without it, models read the latest item as the complete tool set once a
// removal was announced; Codex precedes its incremental tool batches with the
// same guidance. Restating the tools that are still unavailable keeps smaller
// models from calling them again after later loads.
func incrementalToolsNotice(unavailable []string) string {
	notice := "This is an incremental tools update. Previously declared tools remain available unless explicitly marked unavailable. " +
		"A tool declared again here is available again, and its latest definition replaces the earlier one."
	if len(unavailable) > 0 {
		return notice + "\nStill unavailable: " + strings.Join(unavailable, ", ") + "."
	}
	return notice
}

// toolRemovalNotice announces the tools a later system message removed, since
// no wire item can remove a declared tool.
func toolRemovalNotice(names []string) string {
	var builder strings.Builder
	builder.WriteString("The following tools are no longer available. Do not call them:")
	for _, name := range names {
		builder.WriteString("\n- ")
		builder.WriteString(name)
	}
	return builder.String()
}

// configurationUpdateRequestEffort is the fixed reasoning.effort while
// configuration_update items carry the thinking level.
const configurationUpdateRequestEffort = "medium"

// isChatGPTSignInResponses: Sign in with ChatGPT rejects configuration_update
// items (upstream isChatGPTSignIn).
func isChatGPTSignInResponses(model *Model, apiKey string) bool {
	return model.Provider == "openai" && model.BaseURL == OpenAIChatGPTResource &&
		apiKey != "" && !strings.HasPrefix(apiKey, "sk-")
}

// getConfigurationUpdateEffort returns the effort to send in
// configuration_update items, or "" to send it as reasoning.effort.
func getConfigurationUpdateEffort(model *Model, options *OpenAIResponsesOptions, compat ResolvedOpenAIResponsesCompat) string {
	if !compat.SupportsMidConvoEffort || options == nil || options.ReasoningEffort == "" {
		return ""
	}
	if isChatGPTSignInResponses(model, options.APIKey) {
		return ""
	}
	if mapped, ok := model.ThinkingLevelMap[options.ReasoningEffort]; ok && mapped != nil {
		return *mapped
	}
	return options.ReasoningEffort
}

// normalizeResponsesIDPart sanitizes/truncates an id part (≤64 chars).
func normalizeResponsesIDPart(part string) string {
	sanitized := sanitizeID(part)
	if JSLength(sanitized) > 64 {
		sanitized = JSSlice(sanitized, 0, 64)
	}
	return strings.TrimRight(sanitized, "_")
}

// ConvertResponsesMessages builds the Responses `input` array
// (port of convertResponsesMessages). Set ToolCallProviders to override the
// dialects that accept the `call_id|item_id` tool-call id form.
func ConvertResponsesMessages(model *Model, context TranscriptContext, options *ConvertResponsesMessagesOptions) (json.RawMessage, error) {
	if options == nil {
		options = &ConvertResponsesMessagesOptions{}
	}
	normalizedContext := ResolveTranscript(context, options.SupportsMidConvoSystemMessages)
	var messages []responseInputItem

	providers := options.ToolCallProviders
	if providers == nil {
		providers = openAIToolCallProviders
	}
	normalizeToolCallID := func(id string, _ *Model, source *AssistantMessage) string {
		if !providers[model.Provider] {
			return normalizeResponsesIDPart(id)
		}
		if !strings.Contains(id, "|") {
			return normalizeResponsesIDPart(id)
		}
		parts := strings.SplitN(id, "|", 2)
		normalizedCallID := normalizeResponsesIDPart(parts[0])
		isForeignToolCall := source.Provider != model.Provider || source.API != model.API
		var normalizedItemID string
		if isForeignToolCall {
			normalizedItemID = normalizeResponsesIDPart("fc_" + ShortHash(parts[1]))
		} else {
			normalizedItemID = normalizeResponsesIDPart(parts[1])
		}
		if !strings.HasPrefix(normalizedItemID, "fc_") {
			normalizedItemID = normalizeResponsesIDPart("fc_" + normalizedItemID)
		}
		return normalizedCallID + "|" + normalizedItemID
	}

	transformedMessages := TransformMessages(normalizedContext.Messages, model, normalizeToolCallID)
	compat := GetOpenAIResponsesCompat(model)
	instructionRole := "system"
	if model.Reasoning && compat.SupportsDeveloperRole {
		instructionRole = "developer"
	}
	toolSupport := GetResponsesToolSupport(
		options.SupportsMidConvoSystemMessages, options.SupportsAdditionalTools, options.SupportsToolSearch)
	transcriptTools := ResolveTranscriptTools(normalizedContext.Messages, toolSupport)
	// declaredAdditionalTools reports whether an additional_tools item was sent,
	// so every later one is preceded by the incremental notice.
	declaredAdditionalTools := false
	// unavailableTools are the tools announced as removed and not declared again
	// since, in announcement order (upstream keeps a Set's insertion order).
	var unavailableTools []string
	removeUnavailable := func(name string) {
		for i, existing := range unavailableTools {
			if existing == name {
				unavailableTools = append(unavailableTools[:i], unavailableTools[i+1:]...)
				return
			}
		}
	}

	appendSystemToolAdditions := func(message *aiSystemMessageAlias, seed string) error {
		tools := message.ToolsAdded
		for _, tool := range tools {
			removeUnavailable(tool.Name)
		}
		if len(tools) == 0 {
			return nil
		}
		if toolSupport == TranscriptToolSupportChanges {
			if declaredAdditionalTools {
				messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
					"role": instructionRole, "content": incrementalToolsNotice(unavailableTools),
				}, "role", "content")))
			}
			declaredAdditionalTools = true
			converted, err := ConvertResponsesTools(tools, options.ToolOptions)
			if err != nil {
				return err
			}
			item := map[string]any{"type": "additional_tools", "role": "developer", "tools": converted}
			messages = append(messages, mustMarshalJSON(orderKeys(item, "type", "role", "tools")))
			return nil
		}
		if toolSupport != TranscriptToolSupportAdditions {
			return nil
		}
		var names []string
		for _, tool := range tools {
			names = append(names, tool.Name)
		}
		callID := "pi_tool_load_" + ShortHash(seed+":"+strings.Join(names, ","))
		messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
			"type": "tool_search_call", "call_id": callID,
			"execution": "client", "status": "completed",
			"arguments": map[string]any{"query": strings.Join(names, " "), "limit": len(names)},
		}, "type", "call_id", "execution", "status", "arguments")))
		converted, err := ConvertResponsesTools(tools, &ConvertResponsesToolsOptions{
			Strict:                     toolOptionsStrict(options.ToolOptions),
			SupportsStrictMode:         toolOptionsStrictMode(options.ToolOptions),
			SupportsOpenAIGrammarTools: toolOptionsGrammar(options.ToolOptions),
			ToolSearchResult:           true,
		})
		if err != nil {
			return err
		}
		messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
			"type": "tool_search_output", "call_id": callID,
			"execution": "client", "status": "completed", "tools": converted,
		}, "type", "call_id", "execution", "status", "tools")))
		return nil
	}

	includeInitialSystemMessage := options.IncludeSystemPrompt == nil || *options.IncludeSystemPrompt

	pushConfigurationUpdate := func(effort string) {
		messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
			"type":      "configuration_update",
			"reasoning": map[string]any{"effort": effort},
		}, "type", "reasoning")))
	}

	msgIndex := 0
	sourceIndex := 0
	systemIndex := 0
	for _, msg := range transformedMessages {
		isLeadingSystemMessage := sourceIndex == 0 && RoleOf(msg) == RoleSystem
		sourceIndex++

		switch m := msg.(type) {
		case *SystemMessage:
			// transformMessages keeps system messages in order, so their position
			// matches the transcript.
			anchorsToolChanges := systemIndex >= transcriptTools.FoldedSystemMessages
			systemIndex++
			if anchorsToolChanges {
				if err := appendSystemToolAdditions((*aiSystemMessageAlias)(m), "system:"+fmt.Sprint(msgIndex)); err != nil {
					return nil, err
				}
			}
			if !isLeadingSystemMessage || includeInitialSystemMessage {
				parts := []string{RenderSystemMessageUpdate(m)}
				if isLeadingSystemMessage {
					parts[0] = GetSystemMessageText(m)
				}
				if anchorsToolChanges {
					// A redefinition is a removal plus an addition of the same name;
					// only true removals are announced.
					added := map[string]bool{}
					for _, tool := range m.ToolsAdded {
						added[tool.Name] = true
					}
					var removed []string
					for _, tool := range m.ToolsRemoved {
						if !added[tool.Name] {
							removed = append(removed, tool.Name)
						}
					}
					if len(removed) > 0 {
						parts = append(parts, toolRemovalNotice(removed))
					}
					// Recorded after this message's additions, whose notice
					// precedes this removal notice.
					unavailableTools = append(unavailableTools, removed...)
				}
				var nonEmpty []string
				for _, part := range parts {
					if len(part) > 0 {
						nonEmpty = append(nonEmpty, part)
					}
				}
				text := strings.Join(nonEmpty, "\n\n")
				if len(text) > 0 {
					messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
						"role": instructionRole, "content": SanitizeSurrogates(text),
					}, "role", "content")))
				}
			}
		case *UserMessage:
			if m.Content.Blocks == nil {
				messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
					"role":    "user",
					"content": []any{orderKeys(map[string]any{"type": "input_text", "text": SanitizeSurrogates(m.Content.Text)}, "type", "text")},
				}, "role", "content")))
			} else {
				var content []any
				for _, item := range m.Content.Blocks {
					switch b := item.(type) {
					case TextContent:
						content = append(content, orderKeys(map[string]any{
							"type": "input_text", "text": SanitizeSurrogates(b.Text),
						}, "type", "text"))
					case ImageContent:
						content = append(content, orderKeys(map[string]any{
							"type": "input_image", "detail": "auto",
							"image_url": "data:" + b.MimeType + ";base64," + b.Data,
						}, "type", "detail", "image_url"))
					}
				}
				if len(content) == 0 {
					continue
				}
				messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
					"role": "user", "content": content,
				}, "role", "content")))
			}
		case *AssistantMessage:
			items, err := convertResponsesAssistantMessage(model, m, msgIndex, options)
			if err != nil {
				return nil, err
			}
			if len(items) == 0 {
				continue
			}
			if options.ConfigurationUpdateEffort != "" && m.Provider == model.Provider && m.API == model.API &&
				m.Model == model.ID && m.ProviderThinkingLevel != nil {
				pushConfigurationUpdate(*m.ProviderThinkingLevel)
			}
			messages = append(messages, items...)
		case *ToolResultMessage:
			callID := strings.SplitN(m.ToolCallID, "|", 2)[0]
			output := convertResponsesToolResultOutput(model, m.Content)
			itemType := "function_call_output"
			if len(options.GrammarToolInputProperties) > 0 && options.GrammarToolInputProperties[m.ToolName] != "" {
				itemType = "custom_tool_call_output"
			}
			messages = append(messages, mustMarshalJSON(orderKeys(map[string]any{
				"type": itemType, "call_id": callID, "output": output,
			}, "type", "call_id", "output")))
		}
		if !isLeadingSystemMessage {
			msgIndex++
		}
	}
	if options.ConfigurationUpdateEffort != "" {
		pushConfigurationUpdate(options.ConfigurationUpdateEffort)
	}

	enc, err := MarshalJSON(messages)
	if err != nil {
		return nil, err
	}
	return enc, nil
}

// aiSystemMessageAlias lets the tool-addition helper read ToolsAdded without
// importing the shared struct name.
type aiSystemMessageAlias = SystemMessage

func toolOptionsStrict(options *ConvertResponsesToolsOptions) *bool {
	if options == nil {
		return nil
	}
	return options.Strict
}

func toolOptionsStrictMode(options *ConvertResponsesToolsOptions) bool {
	if options == nil {
		return true
	}
	return options.SupportsStrictMode
}

func toolOptionsGrammar(options *ConvertResponsesToolsOptions) bool {
	if options == nil {
		return false
	}
	return options.SupportsOpenAIGrammarTools
}

// convertResponsesAssistantMessage projects assistant content into Responses
// output items (reasoning replay, message items with phases, tool calls).
func convertResponsesAssistantMessage(model *Model, m *AssistantMessage, msgIndex int, options *ConvertResponsesMessagesOptions) ([]responseInputItem, error) {
	var output []responseInputItem
	isSameProviderAndAPI := m.Provider == model.Provider && m.API == model.API
	isSameModel := isSameProviderAndAPI && m.Model == model.ID
	isDifferentModel := isSameProviderAndAPI && m.Model != model.ID
	textBlockIndex := 0

	for _, block := range m.Content {
		switch b := block.(type) {
		case ThinkingContent:
			if b.ThinkingSignature != nil && *b.ThinkingSignature != "" {
				// Reasoning items replay verbatim.
				output = append(output, json.RawMessage(*b.ThinkingSignature))
			}
		case TextContent:
			parsedSignature := ParseTextSignature(deref(b.TextSignature))
			fallbackMessageID := fmt.Sprintf("msg_pi_%d", msgIndex)
			if textBlockIndex > 0 {
				fallbackMessageID = fmt.Sprintf("msg_pi_%d_%d", msgIndex, textBlockIndex)
			}
			textBlockIndex++
			msgID := fallbackMessageID
			if parsedSignature != nil && parsedSignature.ID != "" {
				msgID = parsedSignature.ID
				if JSLength(msgID) > 64 {
					msgID = "msg_" + ShortHash(msgID)
				}
			}
			phase := ""
			if parsedSignature != nil {
				phase = parsedSignature.Phase
			}
			item := map[string]any{
				"type": "message", "role": "assistant",
				"content": []any{orderKeys(map[string]any{"type": "output_text", "text": SanitizeSurrogates(b.Text), "annotations": []any{}}, "type", "text", "annotations")},
				"status":  "completed", "id": msgID,
			}
			keys := []string{"type", "role", "content", "status", "id"}
			if phase != "" {
				item["phase"] = phase
				keys = append(keys, "phase")
			}
			output = append(output, mustMarshalJSON(orderKeys(item, keys...)))
		case ToolCall:
			parts := strings.SplitN(b.ID, "|", 2)
			callID := parts[0]
			var itemID string
			if len(parts) > 1 {
				itemID = parts[1]
			}
			customInputProperty := ""
			if options.GrammarToolInputProperties != nil {
				customInputProperty = options.GrammarToolInputProperties[b.Name]
			}

			// Different-model messages omit the item id entirely to avoid pairing
			// validation. So do ids that do not match the replayed item type:
			// function_call ids must be fc_* and custom_tool_call ids must be
			// ctc_*. Foreign tool call ids are normalized to fc_*, and a call can
			// switch between the two types when grammar tool support differs
			// (bc2d8dc1c).
			itemIdPrefix := "fc_"
			if customInputProperty != "" {
				itemIdPrefix = "ctc_"
			}
			if isDifferentModel || !strings.HasPrefix(itemID, itemIdPrefix) {
				itemID = ""
			}

			var item map[string]any
			var keys []string
			if customInputProperty != "" {
				input, err := GetGrammarToolInput(b.Name, b.Arguments, customInputProperty)
				if err != nil {
					return nil, err
				}
				item = map[string]any{
					"type": "custom_tool_call", "call_id": callID, "name": b.Name,
					"input": SanitizeSurrogates(input),
				}
				keys = []string{"type", "call_id", "name", "input"}
			} else {
				item = map[string]any{
					"type": "function_call", "call_id": callID, "name": b.Name,
					"arguments": string(mustMarshalJSON(json.RawMessage(b.Arguments))),
				}
				keys = []string{"type", "call_id", "name", "arguments"}
			}
			if itemID != "" {
				item["id"] = itemID
				keys = append(keys, "id")
			}
			if isSameModel && b.Namespace != nil {
				item["namespace"] = *b.Namespace
				keys = append(keys, "namespace")
			}
			output = append(output, mustMarshalJSON(orderKeys(item, keys...)))
		}
	}
	return output, nil
}

// convertResponsesToolResultOutput renders tool-result content.
func convertResponsesToolResultOutput(model *Model, content UserContentList) any {
	var textParts []string
	var images []ImageContent
	for _, block := range content {
		switch b := block.(type) {
		case TextContent:
			textParts = append(textParts, b.Text)
		case ImageContent:
			images = append(images, b)
		}
	}
	textResult := strings.Join(textParts, "\n")
	// Whitespace-only text counts as empty: some providers reject it with a 400,
	// which then repeats on every request (upstream 4dd2b149d, #8720).
	hasText := !JSTrimIsEmpty(textResult)
	hasImageInput := false
	for _, input := range model.Input {
		if input == "image" {
			hasImageInput = true
		}
	}

	if len(images) == 0 || !hasImageInput {
		switch {
		case hasText:
			return SanitizeSurrogates(textResult)
		case len(images) > 0:
			return SanitizeSurrogates("(see attached image)")
		default:
			return SanitizeSurrogates("(no tool output)")
		}
	}

	var output []any
	if hasText {
		output = append(output, orderKeys(map[string]any{
			"type": "input_text", "text": SanitizeSurrogates(textResult),
		}, "type", "text"))
	}
	for _, image := range images {
		output = append(output, orderKeys(map[string]any{
			"type": "input_image", "detail": "auto",
			"image_url": "data:" + image.MimeType + ";base64," + image.Data,
		}, "type", "detail", "image_url"))
	}
	return output
}

// orderKeys builds a map whose MarshalJSON emits keys in the given order
// (Go maps sort, which would deviate from upstream key order).
func orderKeys(values map[string]any, keys ...string) *orderedMap {
	out := &orderedMap{keys: keys, values: map[string]any{}}
	for _, key := range keys {
		if value, ok := values[key]; ok {
			out.values[key] = value
		}
	}
	return out
}

// orderedMap marshals its fields in declaration order.
type orderedMap struct {
	keys   []string
	values map[string]any
}

func (o *orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for _, key := range o.keys {
		value, ok := o.values[key]
		if !ok {
			continue
		}
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		keyEnc, err := MarshalJSON(key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyEnc)
		buf.WriteByte(':')
		enc, err := MarshalJSON(value)
		if err != nil {
			return nil, err
		}
		buf.Write(enc)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
