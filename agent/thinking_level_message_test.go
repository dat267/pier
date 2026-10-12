package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Upstream agent-loop.ts:409 wraps the stream result as
// Object.assign(result, { thinkingLevel: config.reasoning ?? "off" }), so every
// loop-produced assistant message records the requested Pi thinking level.
func TestAssistantMessageRecordsRequestedThinkingLevel(t *testing.T) {
	for _, level := range []ai.ThinkingLevel{"", ai.ThinkHigh} {
		streamFn, _ := mockStreamFn(createAssistantMessage(ai.ContentList{ai.TextContent{Text: "ok"}}, ai.StopStop))
		messages := RunAgentLoop(
			[]ai.Message{createUserMessage("hi")},
			AgentContext{},
			&AgentLoopConfig{
				Model:               createModel(),
				ConvertToLlm:        identityConverter,
				SimpleStreamOptions: ai.SimpleStreamOptions{Reasoning: level},
			},
			context.Background(),
			func(AgentEvent) error { return nil },
			streamFn,
		)
		final, ok := messages[len(messages)-1].(*ai.AssistantMessage)
		if !ok {
			t.Fatalf("reasoning %q: last message = %T, want assistant", level, messages[len(messages)-1])
		}
		want := level
		if want == "" {
			want = ai.ThinkOff
		}
		if final.ThinkingLevel == nil || *final.ThinkingLevel != want {
			t.Fatalf("reasoning %q: thinkingLevel = %v, want %q", level, final.ThinkingLevel, want)
		}
		encoded, err := ai.MarshalMessage(final)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(encoded), `"thinkingLevel":"`+want+`"`) {
			t.Fatalf("reasoning %q: persisted message lacks thinkingLevel: %s", level, encoded)
		}
	}
}
