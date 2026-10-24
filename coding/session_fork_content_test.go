package coding

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 core/agent-session.ts getUserMessagesForForking uses
// contentText(content, ""). Prompt builds block content even without images.
func TestForkSelectorIncludesSubmittedPrompt(t *testing.T) {
	session := newPromptSession(t, &ai.Model{ID: "m", Provider: "anthropic", API: ai.APIAnthropicMessages, ContextWindow: 100000}, mockSessionStreamFn(createAssistantMessageT("reply")))
	if err := session.Prompt(context.Background(), "fork this prompt", nil); err != nil {
		t.Fatal(err)
	}
	forks := session.GetUserMessagesForForking()
	if len(forks) != 1 || forks[0].Text != "fork this prompt" || forks[0].EntryID == "" {
		t.Fatalf("fork messages = %+v; want the submitted prompt", forks)
	}
}
