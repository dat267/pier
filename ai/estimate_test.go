package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// charsPerToken fell from 4 to 3.5 in v1.1.0 (#10497), so the same text estimates a
// little higher and the request is given correspondingly less room for its reply. The
// rounding is upstream's Math.ceil(chars / CHARS_PER_TOKEN); the message, content and
// tool estimators all divide by the same ratio.
func TestTextTokenEstimateUsesThreeAndAHalfCharsPerToken(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{name: "empty", text: "", want: 0},
		{name: "one char", text: "a", want: 1},
		{name: "just under a token", text: "abc", want: 1},
		{name: "just over a token", text: "abcd", want: 2},
		{name: "exactly ten tokens", text: strings.Repeat("x", 35), want: 10},
		{name: "one char past ten", text: strings.Repeat("x", 36), want: 11},
		{name: "three runes", text: "日本語", want: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := EstimateTextTokens(testCase.text); got != testCase.want {
				t.Fatalf("EstimateTextTokens(%q) = %d, want %d", testCase.text, got, testCase.want)
			}
		})
	}
}

// Reference: pi v1.4.0 packages/ai/src/utils/estimate.ts calculates usage
// totals with JavaScript Number addition, which rounds integers above 2^53.
func TestCalculateContextTokensUsesJSNumberRounding(t *testing.T) {
	const twoTo53 = int64(1 << 53)
	for name, usage := range map[string]Usage{
		"explicit total": {TotalTokens: twoTo53 + 1},
		"summed fields":  {Input: twoTo53, Output: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if got := CalculateContextTokens(usage); got != int(twoTo53) {
				t.Fatalf("CalculateContextTokens(%+v) = %d; want JS-rounded %d", usage, got, twoTo53)
			}
		})
	}
}

func TestCalculateContextTokensSaturatesBeyondGoInt(t *testing.T) {
	maxInt64 := int64(^uint64(0) >> 1)
	maxInt := int(^uint(0) >> 1)
	if got := CalculateContextTokens(Usage{Input: maxInt64, Output: 1}); got != maxInt {
		t.Fatalf("CalculateContextTokens(max int64 + 1) = %d; want saturated %d", got, maxInt)
	}
}

func TestEstimateContextTokensUsesJSNumberAddition(t *testing.T) {
	const twoTo53 = int64(1 << 53)
	messages := []Message{
		&AssistantMessage{Usage: Usage{TotalTokens: twoTo53}, StopReason: StopStop, Timestamp: 1},
		&UserMessage{Content: StringOrBlocks{Text: "a"}, Timestamp: 2},
	}
	got := EstimateContextTokens(messages)
	if got.Tokens != int(twoTo53) || got.UsageTokens != int(twoTo53) || got.TrailingTokens != 1 || got.LastUsageIndex != 0 {
		t.Fatalf("EstimateContextTokens = %+v; want JS-rounded total %d with 1 trailing token", got, twoTo53)
	}
}

// A tool call's token estimate measures JSON.stringify(arguments).length, and
// JS string length is UTF-16 code units, not bytes. A multi-byte argument must
// not inflate the estimate (the ASCII fixture cannot distinguish the two).
func TestToolCallTokenEstimateMeasuresJSStringLength(t *testing.T) {
	raw := `{"text":"日本"}` // 13 UTF-16 code units, 17 bytes
	message := &AssistantMessage{Content: ContentList{ToolCall{Name: "echo", Arguments: json.RawMessage(raw)}}}
	// ceil((len("echo") + 13) / 3.5) = ceil(17 / 3.5) = 5; a byte count
	// (4 + 17 = 21) would give 6.
	if got := EstimateMessageTokens(message); got != 5 {
		t.Fatalf("EstimateMessageTokens = %d, want 5", got)
	}
}
