package agent

import (
	"context"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 agent.ts stores runtimeOptions.identity and snapshots
// the current public identity into the next loop configuration.
func TestAgentIdentityOverrides(t *testing.T) {
	initial := &ai.ClientIdentity{UserAgent: "short", Version: "9"}
	var captured *ai.ClientIdentity
	mock, _ := mockStreamFn()
	a, err := NewAgent(&AgentOptions{
		Identity: initial,
		StreamFn: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			captured = options.Identity
			return mock(model, transcript, options)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PromptText(context.Background(), "hello"); err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.UserAgent != "short" || captured.Version != "9" || captured.Attribution != ai.PIAttribution {
		t.Fatalf("initial identity = %+v", captured)
	}
	a.Identity = &ai.ClientIdentity{UserAgent: "detailed", AttributionSet: true}
	if err := a.PromptText(context.Background(), "again"); err != nil {
		t.Fatal(err)
	}
	if captured == nil || captured.UserAgent != "detailed" || captured.Attribution != nil || !captured.AttributionSet {
		t.Fatalf("updated identity = %+v", captured)
	}
	if initial.Attribution != nil || initial.UserAgent != "short" {
		t.Fatal("agent mutated the caller's identity")
	}
}

// Reference: pi v1.4.0 agent-loop.ts streamAssistantResponse and
// DEFAULT_AGENT_IDENTITY (adc5fa9f8/04a93bad8). An agent attributes requests
// to pi by default, while an explicit null override turns attribution off.
func TestAgentLoopIdentity(t *testing.T) {
	for _, tc := range []struct {
		name            string
		identity        *ai.ClientIdentity
		wantAttribution *ai.ClientAttribution
	}{
		{"default", nil, ai.PIAttribution},
		{"version override", &ai.ClientIdentity{Version: "9.9.9"}, ai.PIAttribution},
		{"disabled", &ai.ClientIdentity{UserAgent: "short", AttributionSet: true}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured *ai.ClientIdentity
			mock, _ := mockStreamFn()
			streamFn := func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				captured = options.Identity
				return mock(model, transcript, options)
			}
			config := &AgentLoopConfig{Model: createModel(), SimpleStreamOptions: ai.SimpleStreamOptions{
				StreamOptions: ai.StreamOptions{Identity: tc.identity},
			}}
			drainAgentStream(t, AgentLoop([]ai.Message{createUserMessage("hello")}, AgentContext{}, config, nil, streamFn))
			if captured == nil || captured.Attribution != tc.wantAttribution {
				t.Fatalf("identity = %+v", captured)
			}
			if tc.identity != nil && (captured.Version != tc.identity.Version || captured.UserAgent != tc.identity.UserAgent) {
				t.Fatalf("identity override was dropped: %+v", captured)
			}
			if config.Identity != tc.identity {
				t.Fatal("loop changed the caller's identity")
			}
		})
	}
}
