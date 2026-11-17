package coding

import (
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
)

// compaction.ts cutPointMessage: user, assistant, bash-execution, custom,
// branch-summary and compaction-summary messages are boundaries a summary may
// cut at; a tool result is not, so a call and its result never straddle one.
func TestIsCutPointMessage(t *testing.T) {
	custom, err := json.Marshal(map[string]any{"customType": "note", "content": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name    string
		message ai.Message
		want    bool
	}{
		{"user", &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}, true},
		{"assistant", &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "hi"}}}, true},
		{"tool result", &ai.ToolResultMessage{ToolCallID: "c", ToolName: "read"}, false},
		{"bash execution", &ai.CustomMessage{Role: RoleBashExecution, Content: json.RawMessage(`{}`)}, true},
		{"custom", &ai.CustomMessage{Role: RoleCustom, Content: custom}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isCutPointMessage(testCase.message); got != testCase.want {
				t.Fatalf("isCutPointMessage(%T) = %v, want %v", testCase.message, got, testCase.want)
			}
		})
	}
}
