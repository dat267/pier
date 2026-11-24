package ai

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

// Tests for api/bedrock-converse-stream.ts (message conversion, request
// construction, and the streaming event fold).

func TestBedrockTextBlockUsesJavaScriptTrim(t *testing.T) {
	if block := bedrockNonBlankTextBlock("\uFEFF"); block != nil {
		t.Fatalf("BOM-only text block = %#v; want omitted", block)
	}
}

func TestBedrockModelPredicates(t *testing.T) {
	claude := &Model{ID: "anthropic.claude-sonnet-4-5-v1:0", Name: "Claude Sonnet 4.5"}
	if !IsAnthropicClaudeModel(claude) {
		t.Fatal("Claude models must be detected")
	}
	if IsAnthropicClaudeModel(&Model{ID: "amazon.nova-pro-v1:0", Name: "Nova Pro"}) {
		t.Fatal("Nova is not Claude")
	}
	// Adaptive thinking models.
	for _, id := range []string{"anthropic.claude-opus-4-6-v1", "us.anthropic.claude-sonnet-5", "anthropic.claude-fable-5"} {
		if !SupportsAdaptiveThinking(id, "") {
			t.Errorf("%s must support adaptive thinking", id)
		}
	}
	if SupportsAdaptiveThinking("anthropic.claude-sonnet-4-5-v1:0", "") {
		t.Fatal("Sonnet 4.5 is not adaptive")
	}
	// The name is checked too (inference-profile ARNs).
	if !SupportsAdaptiveThinking("arn:aws:bedrock:us-east-1:1:application-inference-profile/abc", "Claude Opus 4.8") {
		t.Fatal("the model name must be considered")
	}
	if !SupportsNativeXhighEffort(&Model{ID: "anthropic.claude-opus-4-8-v1"}) {
		t.Fatal("Opus 4.8 supports native xhigh")
	}
	if SupportsNativeXhighEffort(&Model{ID: "anthropic.claude-sonnet-4-5-v1:0"}) {
		t.Fatal("Sonnet 4.5 has no native xhigh")
	}

	// Prompt caching support matrix.
	caching := map[string]bool{
		"anthropic.claude-sonnet-4-5-v1:0":     true,
		"anthropic.claude-3-7-sonnet-20250219": true,
		"anthropic.claude-3-5-haiku-20241022":  true,
		"anthropic.claude-fable-5":             true,
		"amazon.nova-pro-v1:0":                 false,
	}
	for id, want := range caching {
		if got := SupportsBedrockPromptCaching(&Model{ID: id}, nil); got != want {
			t.Errorf("SupportsBedrockPromptCaching(%s) = %v, want %v", id, got, want)
		}
	}
	// The environment override enables cache points for inference profiles.
	profile := &Model{ID: "arn:aws:bedrock:us-east-1:1:application-inference-profile/abc"}
	if SupportsBedrockPromptCaching(profile, ProviderEnv{"AWS_BEDROCK_FORCE_CACHE": "1"}) != true {
		t.Fatal("AWS_BEDROCK_FORCE_CACHE must enable cache points")
	}
}

func TestBedrockThinkingFields(t *testing.T) {
	model := &Model{ID: "anthropic.claude-sonnet-4-5-v1:0", Name: "Claude Sonnet 4.5", Reasoning: true}
	options := &BedrockOptions{Reasoning: ThinkHigh}
	fields := BuildBedrockAdditionalModelRequestFields(model, options)
	thinking, _ := fields["thinking"].(map[string]any)
	if thinking["type"] != "enabled" || thinking["budget_tokens"] != 16384 || thinking["display"] != "summarized" {
		t.Fatalf("thinking = %#v", thinking)
	}
	// Custom budgets apply to token-based levels; xhigh/max clamp to high.
	options.ThinkingBudgets = &ThinkingBudgets{High: new(4096)}
	fields = BuildBedrockAdditionalModelRequestFields(model, options)
	thinking, _ = fields["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 4096 {
		t.Fatalf("thinking = %#v", thinking)
	}
	options.Reasoning = ThinkMax
	fields = BuildBedrockAdditionalModelRequestFields(model, options)
	thinking, _ = fields["thinking"].(map[string]any)
	if thinking["budget_tokens"] != 4096 {
		t.Fatalf("xhigh/max must clamp to the high budget: %#v", thinking)
	}
	// Interleaved thinking adds the Anthropic beta.
	beta, _ := fields["anthropic_beta"].([]any)
	if len(beta) != 1 || beta[0] != "interleaved-thinking-2025-05-14" {
		t.Fatalf("beta = %#v", fields["anthropic_beta"])
	}
	options.InterleavedThinking = new(false)
	fields = BuildBedrockAdditionalModelRequestFields(model, options)
	if fields["anthropic_beta"] != nil {
		t.Fatalf("interleaved thinking was disabled: %#v", fields["anthropic_beta"])
	}

	// Adaptive models use effort instead of a budget.
	adaptive := &Model{ID: "anthropic.claude-opus-4-6-v1", Name: "Claude Opus 4.6", Reasoning: true}
	fields = BuildBedrockAdditionalModelRequestFields(adaptive, &BedrockOptions{Reasoning: ThinkMedium})
	thinking, _ = fields["thinking"].(map[string]any)
	if thinking["type"] != "adaptive" || thinking["budget_tokens"] != nil {
		t.Fatalf("thinking = %#v", thinking)
	}
	config, _ := fields["output_config"].(map[string]any)
	if config["effort"] != "medium" {
		t.Fatalf("output_config = %#v", config)
	}
	// GovCloud omits the display field.
	fields = BuildBedrockAdditionalModelRequestFields(adaptive, &BedrockOptions{
		Env: ProviderEnv{"AWS_REGION": "us-gov-west-1"}, Reasoning: ThinkHigh})
	thinking, _ = fields["thinking"].(map[string]any)
	if _, hasDisplay := thinking["display"]; hasDisplay {
		t.Fatalf("GovCloud must omit display: %#v", thinking)
	}
	// Opus 4.7 binds replayed thinking blocks so stale ones are dropped, and
	// sends the binding-controls beta; Opus 4.6 rejects both and GovCloud skips
	// them.
	bindingModel := &Model{ID: "anthropic.claude-opus-4-7-v1:0", Name: "Claude Opus 4.7", Reasoning: true}
	fields = BuildBedrockAdditionalModelRequestFields(bindingModel, &BedrockOptions{Reasoning: ThinkHigh})
	thinking, _ = fields["thinking"].(map[string]any)
	binding, _ := thinking["block_binding"].(map[string]any)
	if binding["prefix_mismatch_behavior"] != "drop_block" {
		t.Fatalf("block_binding = %#v", thinking["block_binding"])
	}
	beta, _ = fields["anthropic_beta"].([]any)
	if len(beta) != 1 || beta[0] != ThinkingBindingControlsBeta {
		t.Fatalf("beta = %#v", fields["anthropic_beta"])
	}
	if fields := BuildBedrockAdditionalModelRequestFields(adaptive, &BedrockOptions{Reasoning: ThinkHigh}); fields["anthropic_beta"] != nil {
		t.Fatalf("opus-4-6 must not send block binding: %#v", fields)
	}
	fields = BuildBedrockAdditionalModelRequestFields(bindingModel, &BedrockOptions{
		Env: ProviderEnv{"AWS_REGION": "us-gov-west-1"}, Reasoning: ThinkHigh})
	if fields["anthropic_beta"] != nil {
		t.Fatalf("GovCloud must skip block binding: %#v", fields)
	}

	// Unsupported non-Claude models and missing reasoning produce no fields.
	if fields := BuildBedrockAdditionalModelRequestFields(&Model{ID: "amazon.nova-pro-v1:0", Reasoning: true}, &BedrockOptions{Reasoning: ThinkHigh}); fields != nil {
		t.Fatalf("fields = %#v", fields)
	}
	if fields := BuildBedrockAdditionalModelRequestFields(model, &BedrockOptions{}); fields != nil {
		t.Fatalf("fields = %#v", fields)
	}
}

func TestBedrockOpenAIReasoningFields(t *testing.T) {
	openAILevels := []struct {
		level ThinkingLevel
		want  string
	}{
		{ThinkMinimal, "low"},
		{ThinkLow, "low"},
		{ThinkMedium, "medium"},
		{ThinkHigh, "high"},
		{ThinkXHigh, "xhigh"},
		{ThinkMax, "max"},
	}
	for _, id := range []string{"global.openai.gpt-6-sol", "global.openai.gpt-5.6-sol"} {
		model := &Model{ID: id, Reasoning: true}
		for _, testCase := range openAILevels {
			fields := BuildBedrockAdditionalModelRequestFields(model, &BedrockOptions{Reasoning: testCase.level})
			reasoning, _ := fields["reasoning"].(map[string]any)
			if len(fields) != 1 || reasoning["effort"] != testCase.want {
				t.Errorf("%s at %s: fields = %#v; want reasoning.effort %q", id, testCase.level, fields, testCase.want)
			}
		}
	}

	profile := &Model{
		ID:   "arn:aws:bedrock:us-east-1:123456789012:application-inference-profile/my-profile",
		Name: "GPT-6 Sol", Reasoning: true,
	}
	fields := BuildBedrockAdditionalModelRequestFields(profile, &BedrockOptions{Reasoning: ThinkMedium})
	reasoning, _ := fields["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("name-matched fields = %#v; want reasoning.effort=medium", fields)
	}

	oss := &Model{ID: "openai.gpt-oss-120b-1:0", Reasoning: true}
	ossLevels := []struct {
		level ThinkingLevel
		want  string
	}{
		{ThinkMinimal, "low"}, {ThinkLow, "low"}, {ThinkMedium, "medium"},
		{ThinkHigh, "high"}, {ThinkXHigh, "high"}, {ThinkMax, "high"},
	}
	for _, testCase := range ossLevels {
		fields := BuildBedrockAdditionalModelRequestFields(oss, &BedrockOptions{Reasoning: testCase.level})
		if len(fields) != 1 || fields["reasoning_effort"] != testCase.want {
			t.Errorf("gpt-oss at %s: fields = %#v; want reasoning_effort %q", testCase.level, fields, testCase.want)
		}
	}

	customEffort := "custom"
	mapped := &Model{
		ID: "openai.gpt-5.6-sol", Reasoning: true,
		ThinkingLevelMap: ThinkingLevelMap{ThinkHigh: &customEffort},
	}
	fields = BuildBedrockAdditionalModelRequestFields(mapped, &BedrockOptions{Reasoning: ThinkHigh})
	reasoning, _ = fields["reasoning"].(map[string]any)
	if reasoning["effort"] != customEffort {
		t.Fatalf("model-mapped fields = %#v; want custom reasoning effort", fields)
	}
	if fields := BuildBedrockAdditionalModelRequestFields(&Model{ID: "openai.gpt-6-sol", Reasoning: true}, &BedrockOptions{}); fields != nil {
		t.Fatalf("reasoning-off fields = %#v; want nil", fields)
	}
}

func TestBedrockMessageConversion(t *testing.T) {
	model := bedrockTestModel("")

	// User text, images, and empty-content handling.
	image := ImageContent{MimeType: "image/png", Data: base64.StdEncoding.EncodeToString([]byte("png"))}
	messages, err := ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
		&UserMessage{Content: StringOrBlocks{Text: "   "}, Timestamp: 2},
		&UserMessage{Content: StringOrBlocks{Blocks: ContentList{image}}, Timestamp: 3},
	}}, model, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %#v", messages)
	}
	first, _ := messages[0].(map[string]any)
	content, _ := first["content"].([]any)
	if first["role"] != "user" || len(content) != 1 {
		t.Fatalf("first = %#v", first)
	}
	// Blank text becomes the placeholder.
	second, _ := messages[1].(map[string]any)
	secondContent, _ := second["content"].([]any)
	if block, _ := secondContent[0].(map[string]any); block["text"] != "<empty>" {
		t.Fatalf("second = %#v", second)
	}
	// Images convert to the source/format shape.
	third, _ := messages[2].(map[string]any)
	imageContent, _ := third["content"].([]any)
	imageBlock, _ := imageContent[0].(map[string]any)
	source, _ := imageBlock["image"].(map[string]any)
	if source["format"] != "png" {
		t.Fatalf("image = %#v", imageBlock)
	}
	bytesValue, _ := source["source"].(map[string]any)["bytes"].([]byte)
	if string(bytesValue) != "png" {
		t.Fatalf("image bytes = %#v", bytesValue)
	}

	// Assistant text/tool-call/thinking, including redacted replay. The
	// assistant message must belong to the target model for signatures to
	// survive the shared transcript transform.
	signature := base64.StdEncoding.EncodeToString([]byte("sig-bytes"))
	assistantMessage := &AssistantMessage{
		Provider: model.Provider, API: model.API, Model: model.ID,
		Content: ContentList{
			TextContent{Text: "answer"},
			ThinkingContent{Thinking: "reasoning", ThinkingSignature: &signature},
			ThinkingContent{Thinking: "[Reasoning redacted]", Redacted: true, ThinkingSignature: &signature},
			ThinkingContent{Thinking: "   "},
			ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"a"}`)},
		},
	}
	messages, err = ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		assistantMessage,
		// A matching tool result avoids the synthetic orphan result.
		&ToolResultMessage{ToolCallID: "call-1", ToolName: "read", Content: UserContentList{TextContent{Text: "done"}}},
		// An empty assistant message is skipped.
		&AssistantMessage{Provider: model.Provider, API: model.API, Model: model.ID},
	}}, model, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	assistant, _ := messages[0].(map[string]any)
	blocks, _ := assistant["content"].([]any)
	if assistant["role"] != "assistant" || len(blocks) != 4 {
		t.Fatalf("assistant = %#v", assistant)
	}
	// The blank thinking block is dropped: text, signed thinking, redacted
	// reasoning, tool call.
	thinkingBlock, _ := blocks[1].(map[string]any)
	reasoningContent, _ := thinkingBlock["reasoningContent"].(map[string]any)
	reasoningText, _ := reasoningContent["reasoningText"].(map[string]any)
	if reasoningText["text"] != "reasoning" || reasoningText["signature"] != signature {
		t.Fatalf("thinking = %#v", thinkingBlock)
	}
	redactedBlock, _ := blocks[2].(map[string]any)
	redactedContent, _ := redactedBlock["reasoningContent"].(map[string]any)
	if string(redactedContent["redactedContent"].([]byte)) != "sig-bytes" {
		t.Fatalf("redacted = %#v", redactedBlock)
	}
	toolUseBlock, _ := blocks[3].(map[string]any)
	toolUse, _ := toolUseBlock["toolUse"].(map[string]any)
	if toolUse["toolUseId"] != "call-1" || toolUse["name"] != "read" {
		t.Fatalf("toolUse = %#v", toolUse)
	}

	// An assistant message from a different model has its thinking (signed and
	// redacted) dropped (the shared transcript transform, v1.4.0), and an
	// orphaned tool call gains a synthetic error result.
	foreign := &AssistantMessage{
		Provider: "anthropic", API: APIAnthropicMessages, Model: "claude-other",
		Content: ContentList{
			ThinkingContent{Thinking: "foreign reasoning", ThinkingSignature: &signature},
			ThinkingContent{Thinking: "[Reasoning redacted]", Redacted: true, ThinkingSignature: &signature},
			ToolCall{ID: "call-foreign", Name: "read", Arguments: json.RawMessage(`{}`)},
		},
	}
	messages, err = ConvertBedrockMessages(TranscriptContext{Messages: []Message{foreign}}, model, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages = %#v", messages)
	}
	assistant, _ = messages[0].(map[string]any)
	blocks, _ = assistant["content"].([]any)
	if len(blocks) != 1 {
		t.Fatalf("blocks = %#v", blocks)
	}
	if block, _ := blocks[0].(map[string]any); block["toolUse"] == nil {
		t.Fatalf("cross-model thinking was not dropped: %#v", blocks)
	}
	synthetic, _ := messages[1].(map[string]any)
	toolResults, _ := synthetic["content"].([]any)
	syntheticResult, _ := toolResults[0].(map[string]any)["toolResult"].(map[string]any)
	if synthetic["role"] != "user" || syntheticResult["status"] != "error" {
		t.Fatalf("synthetic = %#v", synthetic)
	}

	// A non-Claude model omits the signature.
	nova := &Model{ID: "amazon.nova-pro-v1:0", Name: "Nova Pro"}
	messages, err = ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Provider: nova.Provider, API: nova.API, Model: nova.ID,
			Content: ContentList{ThinkingContent{Thinking: "reasoning", ThinkingSignature: &signature}},
		},
	}}, nova, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	assistant, _ = messages[0].(map[string]any)
	blocks, _ = assistant["content"].([]any)
	thinkingBlock, _ = blocks[0].(map[string]any)
	reasoningContent, _ = thinkingBlock["reasoningContent"].(map[string]any)
	reasoningText, _ = reasoningContent["reasoningText"].(map[string]any)
	if _, hasSignature := reasoningText["signature"]; hasSignature {
		t.Fatalf("non-Claude models must omit the signature: %#v", reasoningText)
	}

	// A missing signature falls back to a plain text block for Claude (same model).
	messages, err = ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Provider: model.Provider, API: model.API, Model: model.ID,
			Content: ContentList{ThinkingContent{Thinking: "reasoning"}},
		},
	}}, model, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	assistant, _ = messages[0].(map[string]any)
	blocks, _ = assistant["content"].([]any)
	if block, _ := blocks[0].(map[string]any); block["text"] != "reasoning" {
		t.Fatalf("blocks = %#v", blocks)
	}

	// Consecutive tool results collapse into one user message.
	messages, err = ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		&ToolResultMessage{ToolCallID: "c1", ToolName: "read", Content: UserContentList{TextContent{Text: "one"}}},
		&ToolResultMessage{ToolCallID: "c2", ToolName: "read", Content: UserContentList{TextContent{Text: "two"}}, IsError: true},
		&ToolResultMessage{ToolCallID: "c3", ToolName: "read", Content: UserContentList{}},
	}}, model, CacheRetentionNone, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	combined, _ := messages[0].(map[string]any)
	combinedResults, _ := combined["content"].([]any)
	if combined["role"] != "user" || len(combinedResults) != 3 {
		t.Fatalf("combined = %#v", combined)
	}
	firstResult, _ := combinedResults[0].(map[string]any)["toolResult"].(map[string]any)
	if firstResult["toolUseId"] != "c1" || firstResult["status"] != "success" {
		t.Fatalf("first = %#v", firstResult)
	}
	secondResult, _ := combinedResults[1].(map[string]any)["toolResult"].(map[string]any)
	if secondResult["status"] != "error" {
		t.Fatalf("second = %#v", secondResult)
	}
	// Empty tool content becomes the placeholder.
	thirdRaw, _ := combinedResults[2].(map[string]any)
	thirdResult, _ := thirdRaw["toolResult"].(map[string]any)
	emptyToolContent, _ := thirdResult["content"].([]any)
	if block, _ := emptyToolContent[0].(map[string]any); block["text"] != "<empty>" {
		t.Fatalf("third = %#v", thirdResult)
	}
}

func TestBedrockCachePoints(t *testing.T) {
	model := bedrockTestModel("")
	// A cache point is appended to the system prompt and the last user message.
	system := BuildBedrockSystemPrompt("be helpful", model, CacheRetentionShort, nil)
	if len(system) != 2 {
		t.Fatalf("system = %#v", system)
	}
	if point, _ := system[1].(map[string]any)["cachePoint"].(map[string]any); point["type"] != "default" {
		t.Fatalf("system = %#v", system)
	}
	// Long retention adds the 1h TTL.
	system = BuildBedrockSystemPrompt("be helpful", model, CacheRetentionLong, nil)
	point, _ := system[1].(map[string]any)["cachePoint"].(map[string]any)
	if point["ttl"] != "1h" {
		t.Fatalf("system = %#v", system)
	}
	// Retention "none" adds nothing, and no prompt means no blocks.
	if system := BuildBedrockSystemPrompt("be helpful", model, CacheRetentionNone, nil); len(system) != 1 {
		t.Fatalf("system = %#v", system)
	}
	if system := BuildBedrockSystemPrompt("", model, CacheRetentionShort, nil); system != nil {
		t.Fatalf("system = %#v", system)
	}

	messages, err := ConvertBedrockMessages(TranscriptContext{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}}, model, CacheRetentionShort, nil)
	if err != nil {
		t.Fatal(err)
	}
	last, _ := messages[len(messages)-1].(map[string]any)
	content, _ := last["content"].([]any)
	if _, ok := content[len(content)-1].(map[string]any)["cachePoint"]; !ok {
		t.Fatalf("content = %#v", content)
	}
}

func TestBedrockToolConfigConversion(t *testing.T) {
	tool := Tool{Name: "read", Description: "Read a file", Parameters: json.RawMessage(`{"type":"object"}`)}
	convert := func(tools []Tool, choice, name string) map[string]any {
		t.Helper()
		config, _, err := convertBedrockToolConfigWithPreferred(tools, choice, name, false, false)
		if err != nil {
			t.Fatal(err)
		}
		return config
	}
	// toolChoice "none" disables the config entirely, as does an empty tool list.
	if config := convert([]Tool{tool}, "none", ""); config != nil {
		t.Fatalf("config = %#v", config)
	}
	if config := convert(nil, "auto", ""); config != nil {
		t.Fatalf("config = %#v", config)
	}
	config := convert([]Tool{tool}, "auto", "")
	tools, _ := config["tools"].([]any)
	spec, _ := tools[0].(map[string]any)["toolSpec"].(map[string]any)
	if spec["name"] != "read" || spec["inputSchema"] == nil {
		t.Fatalf("spec = %#v", spec)
	}
	choice, _ := config["toolChoice"].(map[string]any)
	if _, ok := choice["auto"]; !ok {
		t.Fatalf("choice = %#v", choice)
	}
	// A forced tool choice names the tool.
	config = convert([]Tool{tool}, "tool", "read")
	choice, _ = config["toolChoice"].(map[string]any)
	forced, _ := choice["tool"].(map[string]any)
	if forced["name"] != "read" {
		t.Fatalf("choice = %#v", config["toolChoice"])
	}
	// "any" and no choice at all.
	config = convert([]Tool{tool}, "any", "")
	choice, _ = config["toolChoice"].(map[string]any)
	if _, ok := choice["any"]; !ok {
		t.Fatalf("choice = %#v", choice)
	}
	config = convert([]Tool{tool}, "", "")
	if config["toolChoice"] != nil {
		t.Fatalf("config = %#v", config)
	}
}

// Upstream bedrock-converse-stream.ts replaces invalid ID characters per UTF-16 code unit.
func TestNormalizeBedrockToolCallIDAstral(t *testing.T) {
	if got := NormalizeBedrockToolCallID("id😀"); got != "id__" {
		t.Fatalf("astral id = %q; want id__", got)
	}
}

// Reference: pi v1.6.0 api/bedrock-converse-stream.ts isAnthropicClaudeModel
// checks the id only for "anthropic.claude" and "anthropic/claude", and checks
// "claude" in the *name* alone:
//
//	return id.includes("anthropic.claude") || id.includes("anthropic/claude") ||
//		name.includes("anthropic.claude") || name.includes("anthropic/claude") || name.includes("claude");
//
// pier folded all five tests over one value list, so it also accepted a bare
// "claude" inside the id. That widened SupportsBedrockThinkingSignature and
// SupportsBedrockPromptCaching for any Bedrock model id containing "claude"
// without the anthropic prefix, which the reference rejects.
func TestBedrockClaudeDetectionMatchesTheReferencePredicate(t *testing.T) {
	for _, tc := range []struct {
		id, name string
		want     bool
	}{
		{"anthropic.claude-sonnet-4-5", "Claude Sonnet 4.5", true},
		{"anthropic/claude-sonnet-4-5", "", true},
		{"arn:aws:bedrock:us-east-1:1:application-inference-profile/abc", "my claude proxy", true},
		// A bare "claude" in the id is not the reference's test.
		{"claude-sonnet-4-5", "Sonnet", false},
		{"claude-3-5-haiku", "", false},
		{"amazon.nova-pro-v1:0", "Nova Pro", false},
	} {
		got := IsAnthropicClaudeModel(&Model{ID: tc.id, Name: tc.name})
		if got != tc.want {
			t.Errorf("IsAnthropicClaudeModel(%q, %q) = %v, want %v", tc.id, tc.name, got, tc.want)
		}
	}
}

// TestBedrockLoginNotifiesBeforeAWSMethods pins upstream bedrockAuth.login
// (pi v1.6.0, faac5858, packages/ai/src/providers/amazon-bedrock.ts:15): the
// bearer token skips the credential notice, and every other method, including
// an unknown one before it fails, sees it once.
func TestBedrockLoginNotifiesBeforeAWSMethods(t *testing.T) {
	for _, tc := range []struct {
		method      string
		wantNotices int
		wantErr     string
		want        ApiKeyCredential
	}{
		{method: "bearer-token", want: ApiKeyCredential{Key: "answer"}},
		{method: "aws-profile", wantNotices: 1, want: ApiKeyCredential{Env: ProviderEnv{"AWS_PROFILE": "answer"}}},
		{method: "credential-chain", wantNotices: 1},
		{method: "sso", wantNotices: 1, wantErr: "Unknown Amazon Bedrock auth method: sso"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			notices := 0
			prompts := 0
			credential, err := BedrockAPIKeyAuth().Login(&AuthInteraction{
				Prompt: func(AuthPrompt) (string, error) {
					prompts++
					if prompts == 1 {
						return tc.method, nil
					}
					return "answer", nil
				},
				Notify: func(AuthEvent) { notices++ },
			})
			if notices != tc.wantNotices {
				t.Fatalf("notices = %d; want %d", notices, tc.wantNotices)
			}
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr {
					t.Fatalf("err = %v; want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if credential.Key != tc.want.Key || credential.Env["AWS_PROFILE"] != tc.want.Env["AWS_PROFILE"] {
				t.Fatalf("credential = %#v; want %#v", credential, tc.want)
			}
		})
	}
}
