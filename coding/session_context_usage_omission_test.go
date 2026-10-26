package coding

import (
	"encoding/json"
	"github.com/dat267/pier/ai"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), core/agent-session.ts getContextUsage
// counts only projected assistants as post-compaction usage evidence.
func TestContextUsageUnknownAfterOmittingPostCompactionResponse(t *testing.T) {
	session := newControlSession(t, &ai.Model{ID: "m", Provider: "test", ContextWindow: 1000}, nil)
	session.Sessions.AppendCompaction("summary", "", 10, nil, false, nil)
	assistant := &ai.AssistantMessage{API: ai.APIOpenAICompletions, Provider: "test", Model: "m", Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop, Usage: ai.Usage{Input: 100, Output: 5, TotalTokens: 105}, Timestamp: 2}
	id := session.Sessions.AppendMessage(assistant)
	if usage := session.GetContextUsage(); usage == nil || usage.Tokens == nil || *usage.Tokens != 105 {
		t.Fatalf("control usage = %+v, want 105 tokens", usage)
	}
	if _, err := session.Sessions.AppendContextEdit(id, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
	usage := session.GetContextUsage()
	if usage == nil || usage.ContextWindow != 1000 || usage.Tokens != nil || usage.Percent != nil {
		t.Fatalf("omitted response establishes usage: %+v, want unknown tokens and percent", usage)
	}
	session.Sessions.AppendMessage(assistant)
	if usage := session.GetContextUsage(); usage == nil || usage.Tokens == nil || usage.Percent == nil {
		t.Fatalf("new projected response did not restore usage: %+v", usage)
	}
}
