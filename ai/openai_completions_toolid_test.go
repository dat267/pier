package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// openai-completions.ts normalizeToolCallId: an id from the Responses API is
// "{call_id}|{item_id}" where the item id can be hundreds of characters of
// base64. Replaying into Chat Completions sanitizes both halves, joins them
// when the item id is non-empty, and caps the result at 40 characters with a
// short hash. The tool result must follow the normalized call id, or the
// provider rejects the pair.
func TestConvertOpenAICompletionsMessagesNormalizesPipeToolCallIDs(t *testing.T) {
	model := testOpenAIModel() // provider openai, model gpt-5
	compat := DetectOpenAICompletionsCompat(model)
	base64Item := strings.Repeat("a+/=", 100)

	for _, testCase := range []struct {
		name    string
		id      string
		want    string
		exact   bool
		wantLen int
	}{
		{name: "joined halves", id: "call_1|item_1", want: "call_1_item_1", exact: true},
		{name: "empty item id", id: "call_2|", want: "call_2", exact: true},
		{name: "sanitized characters", id: "call|3+item", want: "call_3_item", exact: true},
		// Both halves are long, so the 40-character cap truncates the call id
		// before the hash; a short call id keeps its whole self plus the hash.
		{name: "capped at forty", id: strings.Repeat("c", 50) + "|" + base64Item, wantLen: 40, exact: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			messages := []Message{
				// The assistant that produced the call ran on a different model,
				// which is when the reference normalizes ids.
				&AssistantMessage{
					Content: ContentList{ToolCall{ID: testCase.id, Name: "bash", Arguments: json.RawMessage(`{}`)}},
					API:     APIOpenAICompletions, Provider: "openai-codex", Model: "codex-1",
					StopReason: StopToolUse, Timestamp: 1,
				},
				&ToolResultMessage{
					ToolCallID: testCase.id, ToolName: "bash",
					Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 2,
				},
			}
			params := ConvertOpenAICompletionsMessages(model, TranscriptContext{Messages: messages}, compat, nil)
			if len(params) != 2 || len(params[0].ToolCalls) != 1 {
				t.Fatalf("params = %+v", params)
			}
			got := params[0].ToolCalls[0].ID
			if testCase.exact {
				if got != testCase.want {
					t.Fatalf("tool call id = %q, want %q", got, testCase.want)
				}
			} else {
				if JSLength(got) != testCase.wantLen {
					t.Fatalf("tool call id = %q (len %d), want %d characters", got, JSLength(got), testCase.wantLen)
				}
				prefix, hash, ok := strings.Cut(got, "_")
				if !ok || len(hash) != 8 || len(prefix) != 31 {
					t.Fatalf("capped id = %q, want 31 sanitized characters and an 8-character hash", got)
				}
				if strings.ContainsAny(hash, "+/=") {
					t.Fatalf("hash %q is not base36", hash)
				}
			}
			if params[1].ToolCallID != got {
				t.Fatalf("tool result id = %q, want the call's %q", params[1].ToolCallID, got)
			}
		})
	}

	// A plain id replayed into an openai provider is cut to 40 characters
	// without the pipe branch (openai-completions.ts `id.length > 40 ?
	// id.slice(0, 40) : id`). A 41-character id is the boundary case.
	longID := strings.Repeat("i", 41)
	params := ConvertOpenAICompletionsMessages(model, TranscriptContext{Messages: []Message{
		&AssistantMessage{
			Content: ContentList{ToolCall{ID: longID, Name: "bash", Arguments: json.RawMessage(`{}`)}},
			API:     APIOpenAICompletions, Provider: "openai", Model: "gpt-4o", StopReason: StopToolUse, Timestamp: 1,
		},
		&ToolResultMessage{ToolCallID: longID, ToolName: "bash", Content: UserContentList{TextContent{Text: "out"}}, Timestamp: 2},
	}}, compat, nil)
	if len(params[0].ToolCalls) != 1 {
		t.Fatalf("params = %+v", params)
	}
	if got := params[0].ToolCalls[0].ID; JSLength(got) != 40 {
		t.Fatalf("openai tool call id = %q (%d chars), want 40", got, JSLength(got))
	}
	if params[1].ToolCallID != params[0].ToolCalls[0].ID {
		t.Fatalf("tool result id = %q, want the call's %q", params[1].ToolCallID, params[0].ToolCalls[0].ID)
	}
}
