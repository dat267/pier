package agent

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/sdk.ts cacheContextIsCurrent checks object
// identity for every warmed-prefix message, not a mutation epoch or count.
func TestMessagePrefixCurrencyMatchesUpstream(t *testing.T) {
	for _, fixture := range []struct {
		operation string
		want      bool
	}{
		{"unchanged", true}, {"append", true}, {"replace", false},
		{"restore", true}, {"truncate", false}, {"model-change", false},
		{"replace-unwarmed-tail", true},
	} {
		t.Run(fixture.operation, func(t *testing.T) {
			model := &ai.Model{ID: "model", Provider: "fixture"}
			original := []ai.Message{
				&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "system"}},
				&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}},
			}
			a, err := NewAgent(&AgentOptions{InitialState: &AgentInitialState{Model: model, Messages: original}, StreamFn: unusedStreamFn()})
			if err != nil {
				t.Fatal(err)
			}
			current := a.MessagePrefixIsCurrent(model)
			replacement := []ai.Message{original[0], &ai.UserMessage{Content: ai.StringOrBlocks{Text: "replaced"}}}
			switch fixture.operation {
			case "append":
				a.SetMessages(append(append([]ai.Message(nil), original...), &ai.AssistantMessage{}))
			case "replace":
				a.SetMessages(replacement)
			case "restore":
				a.SetMessages(replacement)
				a.SetMessages(original)
			case "truncate":
				a.SetMessages(original[:1])
			case "model-change":
				a.SetModel(&ai.Model{ID: "other", Provider: "fixture"})
			case "replace-unwarmed-tail":
				a.SetMessages(append(append([]ai.Message(nil), original...), &ai.AssistantMessage{}))
				a.SetMessages(append(append([]ai.Message(nil), original...), &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "changed tail"}}}))
			}
			if got := current(); got != fixture.want {
				t.Fatalf("currency after %s = %v, want %v", fixture.operation, got, fixture.want)
			}
		})
	}
}
