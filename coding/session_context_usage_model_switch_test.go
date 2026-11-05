package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.5.0 (f9a26365), core/agent-session.ts getContextUsage:
// percent is estimateProjectedContextTokens / model.contextWindow, so the
// denominator follows the active model. Switching models must not change the
// token estimate, only the reported percentage (the footer shows both).
func TestContextUsagePercentFollowsTheActiveModelWindow(t *testing.T) {
	session := newControlSession(t, &ai.Model{ID: "small", Provider: "test", ContextWindow: 1000}, nil)
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIOpenAICompletions, Provider: "test", Model: "small",
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{Input: 400, Output: 100, TotalTokens: 500}, Timestamp: 2,
	})
	before := session.GetContextUsage()
	if before == nil || before.Tokens == nil || before.Percent == nil {
		t.Fatalf("control usage = %+v, want tokens and percent", before)
	}
	if *before.Tokens != 500 || *before.Percent != 50 || before.ContextWindow != 1000 {
		t.Fatalf("control usage = %d tokens, %v%% of %d; want 500, 50%%, 1000", *before.Tokens, *before.Percent, before.ContextWindow)
	}

	if err := session.SetModel(t.Context(), &ai.Model{ID: "large", Provider: "test", ContextWindow: 4000}, ModelMutationOptions{}); err != nil {
		t.Fatal(err)
	}
	after := session.GetContextUsage()
	if after == nil || after.Tokens == nil || after.Percent == nil {
		t.Fatalf("usage after switch = %+v, want tokens and percent", after)
	}
	if *after.Tokens != 500 {
		t.Fatalf("token estimate changed across a model switch: %d, want 500", *after.Tokens)
	}
	if after.ContextWindow != 4000 || *after.Percent != 12.5 {
		t.Fatalf("usage after switch = %v%% of %d; want 12.5%% of 4000", *after.Percent, after.ContextWindow)
	}
}
