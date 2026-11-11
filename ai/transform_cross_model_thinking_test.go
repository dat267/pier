package ai

import "testing"

// Reference: pi v1.5.0 (f9a26365), api/transform-messages.ts thinking branch, and
// the upstream assertions in test/transform-messages-cross-model-thinking.test.ts
// (issue 10995), ported verbatim.
func crossModelThinkingModel() *Model {
	return &Model{
		ID: "moonshotai/Kimi-K3", Name: "Kimi K3", API: APIOpenAICompletions,
		Provider: "sference", BaseURL: "https://api.sference.com/v1",
		Reasoning: true, Input: []string{"text", "image"},
		ContextWindow: 1048576, MaxTokens: 32768,
	}
}

func crossModelAssistant(model string, content ContentList) *AssistantMessage {
	return &AssistantMessage{
		Content: content,
		API:     APIOpenAICompletions, Provider: "sference", Model: model,
		StopReason: StopToolUse, Timestamp: 1,
	}
}

func assistantContentJSON(t *testing.T, messages []Message) []string {
	t.Helper()
	var out []string
	for _, message := range messages {
		assistant, ok := message.(*AssistantMessage)
		if !ok {
			continue
		}
		encoded, err := MarshalJSON(assistant.Content)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(encoded))
	}
	return out
}

func assertAssistants(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("assistant messages = %d, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("assistant %d = %s, want %s", i, got[i], want[i])
		}
	}
}

// Another model's readable thinking must not reach the target model: it is
// dropped, not downgraded to visible text.
func TestTransformDropsAnotherModelsThinking(t *testing.T) {
	signature := "reasoning_content"
	messages := []Message{
		&UserMessage{Content: StringOrBlocks{Text: "Why does curl work but node fails?"}, Timestamp: 1},
		crossModelAssistant("zai-org/GLM-5.3", ContentList{
			ThinkingContent{Thinking: "The user asks about ECONNRESET. Candidates: proxy, IPv6, TLS.", ThinkingSignature: &signature},
			TextContent{Text: "Let me check the TLS versions."},
			ToolCall{ID: "call_1", Name: "bash", Arguments: []byte(`{"command":"curl -v"}`)},
		}),
		&ToolResultMessage{ToolCallID: "call_1", ToolName: "bash", Content: UserContentList{TextContent{Text: "TLSv1.2"}}, Timestamp: 3},
	}
	got := assistantContentJSON(t, TransformMessages(messages, crossModelThinkingModel(), nil))
	assertAssistants(t, got,
		`[{"type":"text","text":"Let me check the TLS versions."},{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"curl -v"}}]`)
}

// When two models alternate, each assistant turn keeps its own thinking and
// loses the other model's.
func TestTransformKeepsThinkingOnlyFromTheTargetModel(t *testing.T) {
	signature := "reasoning_content"
	thinking := func(text string) ThinkingContent {
		return ThinkingContent{Thinking: text, ThinkingSignature: &signature}
	}
	text := func(value string) TextContent { return TextContent{Text: value} }
	messages := []Message{
		&UserMessage{Content: StringOrBlocks{Text: "1"}, Timestamp: 1},
		crossModelAssistant("moonshotai/Kimi-K3", ContentList{thinking("kimi 1"), text("a")}),
		&UserMessage{Content: StringOrBlocks{Text: "2"}, Timestamp: 3},
		crossModelAssistant("zai-org/GLM-5.3", ContentList{thinking("glm 1"), text("b")}),
		&UserMessage{Content: StringOrBlocks{Text: "3"}, Timestamp: 5},
		crossModelAssistant("moonshotai/Kimi-K3", ContentList{thinking("kimi 2"), text("c")}),
		&UserMessage{Content: StringOrBlocks{Text: "4"}, Timestamp: 7},
		crossModelAssistant("zai-org/GLM-5.3", ContentList{thinking("glm 2"), text("d")}),
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 9},
	}
	got := assistantContentJSON(t, TransformMessages(messages, crossModelThinkingModel(), nil))
	assertAssistants(t, got,
		`[{"type":"thinking","thinking":"kimi 1","thinkingSignature":"reasoning_content"},{"type":"text","text":"a"}]`,
		`[{"type":"text","text":"b"}]`,
		`[{"type":"thinking","thinking":"kimi 2","thinkingSignature":"reasoning_content"},{"type":"text","text":"c"}]`,
		`[{"type":"text","text":"d"}]`)
}
