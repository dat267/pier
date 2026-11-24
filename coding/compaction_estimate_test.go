package coding

import (
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Compaction tests keyed to upstream compaction.ts semantics.

func TestEstimateAgentMessageTokens(t *testing.T) {
	// chars/4 heuristic over the role-specific text.
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("x", 40)}}
	if got := EstimateAgentMessageTokens(user); got != 10 {
		t.Fatalf("user tokens = %d; want 10", got)
	}
	// Images estimate 4800 chars.
	withImage := &ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
		ai.ImageContent{Data: "aGk=", MimeType: "image/png"},
	}}}
	if got := EstimateAgentMessageTokens(withImage); got != 1200 {
		t.Fatalf("image tokens = %d; want 1200", got)
	}
	// Assistant: text + thinking + toolCall name + serialized args.
	assistant := &ai.AssistantMessage{Content: ai.ContentList{
		ai.ThinkingContent{Thinking: strings.Repeat("t", 20)},
		ai.TextContent{Text: strings.Repeat("a", 20)},
		ai.ToolCall{ID: "c", Name: "read", Arguments: json.RawMessage(`{"path":"file.txt"}`)},
	}}
	// 20+20 chars + name + JSON args, ceil(chars/4).
	want := (20 + 20 + len("read") + len(`{"path":"file.txt"}`) + 3) / 4
	if want*4 < 20+20+len("read")+len(`{"path":"file.txt"}`) {
		want++
	}
	if got := EstimateAgentMessageTokens(assistant); got != want {
		t.Fatalf("assistant tokens = %d; want %d", got, want)
	}
}

func TestEstimateContextTokensPrefersUsage(t *testing.T) {
	messages := []ai.Message{
		createUserMessage("hi"),
		&ai.AssistantMessage{
			Content:    ai.ContentList{ai.TextContent{Text: "reply"}},
			Usage:      ai.Usage{TotalTokens: 500},
			StopReason: ai.StopStop, Timestamp: 2,
		},
		createUserMessage("tail"),
	}
	estimate := EstimateContextTokens(messages)
	if estimate.UsageTokens != 500 || estimate.Tokens != 500+EstimateAgentMessageTokens(messages[2]) {
		t.Fatalf("estimate = %+v", estimate)
	}
	if estimate.LastUsageIndex != 1 {
		t.Fatalf("lastUsageIndex = %d", estimate.LastUsageIndex)
	}
	// Errored assistant usage is skipped.
	errored := &ai.AssistantMessage{Usage: ai.Usage{TotalTokens: 999}, StopReason: ai.StopError, Timestamp: 3}
	messages = append(messages, errored)
	estimate = EstimateContextTokens(messages)
	if estimate.UsageTokens != 500 {
		t.Fatalf("errored usage leaked: %+v", estimate)
	}
}

// Reference: pi v1.5.0 f9a26365 compaction/compaction.ts
// calculateContextTokens adds usage as JavaScript Numbers. An int64 sum would
// wrap a near-range provider value negative and suppress compaction.
func TestCalculateCompactionContextTokensHandlesHugeUsage(t *testing.T) {
	if got := CalculateCompactionContextTokens(ai.Usage{Input: math.MaxInt64, CacheRead: math.MaxInt64}); got <= 0 || got < 200000 {
		t.Fatalf("huge usage tokens = %d, want a saturated positive count", got)
	}
}

// Reference: pi v1.5.0 f9a26365 compaction/compaction.ts adds the usage and
// trailing-message estimates as JavaScript Numbers. Saturating the usage alone
// is not enough: adding trailing messages must not wrap the total negative.
func TestEstimateContextTokensSaturatesHugeUsage(t *testing.T) {
	assistant := &ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "zai", Model: "m", StopReason: ai.StopStop,
		Usage: ai.Usage{Input: math.MaxInt64, CacheRead: math.MaxInt64},
	}
	trailing := &ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("x", 40)}}
	estimate := EstimateContextTokens([]ai.Message{assistant, trailing})
	if estimate.Tokens <= 0 || estimate.UsageTokens <= 0 || estimate.TrailingTokens <= 0 {
		t.Fatalf("huge estimate = %+v, want saturated positive totals", estimate)
	}
}

// Reference: pi v1.5.0 f9a26365 usage-totals.ts adds token counters as
// JavaScript Numbers. Near-range provider usage must stay a large positive
// total instead of wrapping negative in Go's int64 addition.
func TestCombineUsageSaturatesHugeTokens(t *testing.T) {
	combined := CombineUsage(
		ai.Usage{Input: math.MaxInt64, TotalTokens: math.MaxInt64},
		ai.Usage{Input: math.MaxInt64, TotalTokens: math.MaxInt64},
	)
	if combined.Input <= 0 || combined.TotalTokens <= 0 {
		t.Fatalf("combined huge usage = %+v, want saturated positive totals", combined)
	}
}

func TestCombineUsage(t *testing.T) {
	r1 := int64(5)
	r2 := int64(7)
	first := ai.Usage{Input: 10, Output: 20, CacheRead: 1, CacheWrite: 2, Reasoning: &r1,
		TotalTokens: 33, Cost: ai.UsageCost{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.2, Total: 3.3}}
	second := ai.Usage{Input: 100, Output: 200, CacheRead: 10, CacheWrite: 20, Reasoning: &r2,
		TotalTokens: 330, Cost: ai.UsageCost{Input: 10, Output: 20, CacheRead: 1, CacheWrite: 2, Total: 33}}
	combined := CombineUsage(first, second)
	if combined.Input != 110 || combined.TotalTokens != 363 || combined.Cost.Total != 36.3 {
		t.Fatalf("combined = %+v", combined)
	}
	if combined.Reasoning == nil || *combined.Reasoning != 12 {
		t.Fatalf("reasoning = %v", combined.Reasoning)
	}
	// A sum is provider-reported only when every part is (pi v1.5.0
	// usage-totals.ts).
	yes, no := true, false
	first.Cost.TotalFromProvider, second.Cost.TotalFromProvider = &yes, &yes
	if got := CombineUsage(first, second).Cost.TotalFromProvider; got == nil || !*got {
		t.Fatalf("both reported: %v", got)
	}
	second.Cost.TotalFromProvider = &no
	if got := CombineUsage(first, second).Cost.TotalFromProvider; got == nil || *got {
		t.Fatalf("one estimated: %v", got)
	}
	second.Cost.TotalFromProvider = nil
	if got := CombineUsage(first, second).Cost.TotalFromProvider; got == nil || *got {
		t.Fatalf("unknown part: %v", got)
	}
}

// Upstream adds JSON.stringify(arguments).length, a UTF-16 code-unit count, so
// multi-byte tool arguments must not inflate the compaction estimate the way a
// byte count would.
func TestEstimateAgentMessageTokensMeasuresJSStringLength(t *testing.T) {
	raw := `{"text":"日本"}` // 13 UTF-16 code units, 17 bytes
	assistant := &ai.AssistantMessage{Content: ai.ContentList{
		ai.ToolCall{ID: "c", Name: "echo", Arguments: json.RawMessage(raw)},
	}}
	// ceil((4 + 13) / 4) = 5; a byte count (4 + 17 = 21) would give 6.
	if got := EstimateAgentMessageTokens(assistant); got != 5 {
		t.Fatalf("assistant tokens = %d; want 5", got)
	}
}

// Reference: pi v1.5.0 (f9a26365), core/compaction/compaction.ts:298
// `estimateTokens`. Every want below is that function's own output, obtained by
// running its body (types stripped) against the same message JSON under node
// --experimental-strip-types. Only the user, assistant, toolResult and system
// branches are pinned here; the custom-message roles are covered by
// custom_message_test.go.
//
// The shapes are the ones the estimator treats differently: a string versus
// block content, image blocks (a fixed chars estimate each), empty content and
// an absent one, assistant text/thinking/toolCall blocks (a tool call counts its
// name plus its serialized arguments), and a system message whose sections and
// toolsAdded count toward the prompt.
func TestTokenEstimateMatchesReference(t *testing.T) {
	cases := []struct {
		name    string
		message string
		want    int
	}{
		{"user-ascii", `{"role":"user","content":"plain ascii"}`, 3},
		{"user-wide-chars", `{"role":"user","content":"日本語のテキストです 😀🚀 astral 𝒜"}`, 7},
		{"user-blocks-with-image", `{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","data":"QQ==","mimeType":"image/png"},{"type":"text","text":"again"}]}`, 1203},
		{"user-image-only", `{"role":"user","content":[{"type":"image","data":"QQ==","mimeType":"image/png"}]}`, 1200},
		{"user-empty-blocks", `{"role":"user","content":[]}`, 0},
		{"user-empty-string", `{"role":"user","content":""}`, 0},
		{"assistant-text-and-thinking", `{"role":"assistant","content":[{"type":"text","text":"answer"},{"type":"thinking","thinking":"pondering"}],"usage":{"promptTokens":1,"completionTokens":1}}`, 4},
		{"assistant-tool-call", `{"role":"assistant","content":[{"type":"toolCall","id":"c|fc","name":"lookup","arguments":{"z":"<&>","a":1,"é":"ü"}}]}`, 8},
		{"assistant-empty-arguments", `{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"empty","arguments":{}}]}`, 2},
		{"assistant-empty", `{"role":"assistant","content":[]}`, 0},
		{"tool-result-with-image", `{"role":"toolResult","toolCallId":"c","toolName":"t","content":[{"type":"text","text":"out"},{"type":"image","data":"QQ==","mimeType":"image/png"}],"isError":false}`, 1201},
		{"tool-result-empty-text", `{"role":"toolResult","toolCallId":"c","toolName":"t","content":[{"type":"text","text":""}],"isError":true}`, 0},
		{"system-plain", `{"role":"system","content":"system prompt"}`, 4},
		{"system-sections-and-tools", `{"role":"system","content":"","sections":{"a":"xx","b":null},"toolsAdded":[{"name":"t","description":"d","parameters":{"type":"object"}}]}`, 17},
		{"assistant-arguments-with-wide-escape", `{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"lookup","arguments":{"q":"é"}}]}`, 4},
		{"assistant-arguments-with-control-escape", `{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"lookup","arguments":{"q":"a\u0001b"}}]}`, 6},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			message, err := ai.UnmarshalMessage(json.RawMessage(testCase.message))
			if err != nil {
				t.Fatal(err)
			}
			if got := EstimateAgentMessageTokens(message); got != testCase.want {
				t.Fatalf("estimate = %d, want %d (upstream estimateTokens)", got, testCase.want)
			}
		})
	}
}

// pi v1.4.0 (4a080252), core/compaction/compaction.ts estimateContextTokens.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/estimate/generate.mjs ./pi > coding/testdata/estimate/golden.json
func TestEstimateContextTokensMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/estimate/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Expected struct {
			Tokens         int  `json:"tokens"`
			UsageTokens    int  `json:"usageTokens"`
			TrailingTokens int  `json:"trailingTokens"`
			LastUsageIndex *int `json:"lastUsageIndex"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty estimate corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			messages := make([]ai.Message, 0, len(fixture.Messages))
			for _, encoded := range fixture.Messages {
				message, err := ai.UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				messages = append(messages, message)
			}
			got := EstimateContextTokens(messages)
			lastUsage := got.LastUsageIndex
			if fixture.Expected.LastUsageIndex == nil {
				if lastUsage != -1 {
					t.Errorf("last usage index = %d, want none", lastUsage)
				}
			} else if lastUsage != *fixture.Expected.LastUsageIndex {
				t.Errorf("last usage index = %d, want %d", lastUsage, *fixture.Expected.LastUsageIndex)
			}
			if got.Tokens != fixture.Expected.Tokens || got.UsageTokens != fixture.Expected.UsageTokens ||
				got.TrailingTokens != fixture.Expected.TrailingTokens {
				t.Errorf("estimate = %+v, want tokens %d usage %d trailing %d", got,
					fixture.Expected.Tokens, fixture.Expected.UsageTokens, fixture.Expected.TrailingTokens)
			}
		})
	}
}
