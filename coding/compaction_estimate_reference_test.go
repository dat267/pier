package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

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
