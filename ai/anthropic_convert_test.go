package ai

import (
	"encoding/json"
	"testing"
)

// Request-construction tests keyed to upstream's buildParams behavior
// (api/anthropic-messages.ts). Each expectation cites the upstream source
// path it verifies.

func testAnthropicModel() *Model {
	return &Model{
		ID: "claude-opus-4-5", Name: "Claude Opus 4.5", API: APIAnthropicMessages, Provider: "anthropic",
		BaseURL: "https://api.anthropic.com", Reasoning: true, Input: []string{"text", "image"},
		Cost:          ModelCost{ModelCostRates: ModelCostRates{Input: 5, Output: 25, CacheRead: 0.5, CacheWrite: 6.25}},
		ContextWindow: 200000, MaxTokens: 64000,
		Compat: &ModelCompat{AnthropicMessages: &AnthropicMessagesCompat{SupportsStrictTools: new(true)}},
	}
}

func build(t *testing.T, model *Model, messages []Message, options *AnthropicOptions) *AnthropicMessageCreateParams {
	t.Helper()
	params, err := BuildAnthropicParams(model, NormalizeContext(Context{Messages: messages}), false, options)
	if err != nil {
		t.Fatal(err)
	}
	return params
}

func TestConvertAnthropicMessagesThinkingReplay(t *testing.T) {
	sig := "encrypted-signature"
	emptySig := ""
	messages := []Message{
		&AssistantMessage{
			Content: ContentList{
				ThinkingContent{Thinking: "deliberation", ThinkingSignature: &sig},
				ThinkingContent{Thinking: "unsigned thinking"},
				TextContent{Text: "answer"},
			},
			API: APIAnthropicMessages, Provider: "anthropic", Model: "claude-opus-4-5",
			StopReason: StopStop, Timestamp: 1,
		},
	}
	converted, err := ConvertAnthropicMessages(messages, false, nil, false, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var blocks []AnthropicContentBlock
	if err := json.Unmarshal(converted.Messages[0].Content, &blocks); err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 {
		t.Fatalf("blocks = %+v", blocks)
	}
	if blocks[0].Type != "thinking" || deref(blocks[0].Thinking) != "deliberation" || *blocks[0].Signature != sig {
		t.Fatalf("block 0 = %+v", blocks[0])
	}
	// Unsigned thinking converts to text for replay; the text block follows.
	if blocks[1].Type != "text" || blocks[1].Text != "unsigned thinking" {
		t.Fatalf("block 1 = %+v", blocks[1])
	}
	if blocks[2].Type != "text" || blocks[2].Text != "answer" {
		t.Fatalf("block 2 = %+v", blocks[2])
	}

	// allowEmptySignature preserves the block with an empty signature.
	converted, err = ConvertAnthropicMessages([]Message{
		&AssistantMessage{
			Content: ContentList{ThinkingContent{Thinking: "unsigned", ThinkingSignature: &emptySig}},
			API:     APIAnthropicMessages, Provider: "anthropic", Model: "m",
			StopReason: StopStop, Timestamp: 1,
		},
	}, false, nil, true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var emptySigBlocks []AnthropicContentBlock
	if err := json.Unmarshal(converted.Messages[0].Content, &emptySigBlocks); err != nil {
		t.Fatal(err)
	}
	if len(emptySigBlocks) != 1 || emptySigBlocks[0].Type != "thinking" || emptySigBlocks[0].Signature == nil || *emptySigBlocks[0].Signature != "" {
		t.Fatalf("allowEmptySignature blocks = %+v", emptySigBlocks)
	}
}

func TestNormalizeAnthropicToolCallID(t *testing.T) {
	// Upstream anthropic-messages.ts uses a non-Unicode regex over UTF-16 code units.
	if got := NormalizeAnthropicToolCallID("id😀"); got != "id__" {
		t.Fatalf("astral id = %q; want id__", got)
	}
	// OpenAI Responses ids are 450+ chars with special characters.
	long := "resp|" + string(make([]byte, 0)) + "abc$def"
	for range 100 {
		long += "|xyz"
	}
	got := NormalizeAnthropicToolCallID(long)
	if len(got) > 64 {
		t.Fatalf("len = %d", len(got))
	}
	for _, c := range got {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '_' && c != '-' {
			t.Fatalf("char %q in %q", c, got)
		}
	}
}

func TestMapAnthropicStopReason(t *testing.T) {
	cases := map[string]StopReason{
		"end_turn": StopStop, "max_tokens": StopLength, "tool_use": StopToolUse,
		"pause_turn": StopStop, "stop_sequence": StopStop,
	}
	for in, want := range cases {
		got, _, err := MapAnthropicStopReason(in, "")
		if err != nil || got != want {
			t.Fatalf("%s = %s, %v; want %s", in, got, err, want)
		}
	}
	got, msg, err := MapAnthropicStopReason("refusal", "cannot comply")
	if err != nil || got != StopError || msg != "cannot comply" {
		t.Fatalf("refusal = %s/%s/%v", got, msg, err)
	}
	_, msg, _ = MapAnthropicStopReason("refusal", "")
	if msg != "The model refused to complete the request" {
		t.Fatalf("default refusal message = %q", msg)
	}
	got, msg, _ = MapAnthropicStopReason("sensitive", "")
	if got != StopError || msg != "Provider stopped with: sensitive" {
		t.Fatalf("sensitive = %s/%s", got, msg)
	}
	if _, _, err := MapAnthropicStopReason("mystery", ""); err == nil {
		t.Fatal("unknown stop reason must error")
	}
}

func TestClaudeCodeToolNaming(t *testing.T) {
	// OAuth requests map names to CC canonical casing both ways.
	if got := toClaudeCodeName("read"); got != "Read" {
		t.Fatalf("toCC(read) = %q", got)
	}
	if got := fromClaudeCodeName("Read", []Tool{{Name: "read"}}); got != "read" {
		t.Fatalf("fromCC(Read) = %q", got)
	}
	if got := toClaudeCodeName("my_tool"); got != "my_tool" {
		t.Fatalf("unmatched = %q", got)
	}
}

// Upstream api/anthropic-messages.ts convertMessages (v1.5.0) always sets
// `thinking` on a signed thinking block and `data` on a redacted one, so an
// empty thought or an empty redacted payload still sends the key.
func TestConvertAnthropicMessagesKeepsEmptyThinkingAndData(t *testing.T) {
	sig, empty := "sigA", ""
	params := build(t, testAnthropicModel(), []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
		&AssistantMessage{
			API: APIAnthropicMessages, Provider: "anthropic", Model: "claude-opus-4-5", StopReason: StopStop,
			Content: ContentList{
				ThinkingContent{Thinking: "", ThinkingSignature: &sig},
				ThinkingContent{Thinking: "", ThinkingSignature: &empty, Redacted: true},
			},
		},
	}, nil)
	body, err := MarshalJSON(params.Messages[1])
	if err != nil {
		t.Fatal(err)
	}
	want := `{"role":"assistant","content":[{"type":"thinking","thinking":"","signature":"sigA"},{"type":"redacted_thinking","data":""}]}`
	if string(body) != want {
		t.Fatalf("assistant = %s\nwant %s", body, want)
	}
}

// A short id is returned unchanged: the 64-character cut is the only
// transformation, so an id below it must not be sliced (upstream
// anthropic-messages.ts normalizes ids without truncating short ones).
func TestNormalizeAnthropicToolCallIDKeepsShortIDs(t *testing.T) {
	for _, id := range []string{"abc", "call_1", "a-b_c"} {
		if got := NormalizeAnthropicToolCallID(id); got != id {
			t.Fatalf("NormalizeAnthropicToolCallID(%q) = %q, want it unchanged", id, got)
		}
	}
}
