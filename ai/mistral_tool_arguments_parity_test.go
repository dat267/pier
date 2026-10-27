package ai

import (
	"context"
	"testing"
)

// pi v1.4.0 (4a080252), api/mistral-conversations.ts consumeChatStream:
// string arguments are used verbatim; absent/null non-string arguments use
// JSON.stringify(arguments || {}), producing an object delta.
func TestMistralDefaultsAbsentToolArgumentsToObjectDelta(t *testing.T) {
	for _, fixture := range []struct {
		name, arguments, want string
	}{
		{"absent", "", "{}"},
		{"null", `,"arguments":null`, "{}"},
		{"empty-string", `,"arguments":""`, ""},
		{"object", `,"arguments":{"path":"a"}`, `{"path":"a"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"id\":\"call0\",\"index\":0,\"function\":{\"name\":\"lookup\"" + fixture.arguments + "}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n"
			server := sseServer(t, []string{body}, nil)
			defer server.Close()
			stream := StreamMistralConversations(mistralModel(server.URL), mistralUserContext(), &MistralOptions{
				APIKey: "test-key",
			})
			var deltas []string
			for {
				event, ok := stream.Next(context.Background())
				if !ok {
					break
				}
				if event.Type == EventError {
					t.Fatalf("stream failed: %+v", event.Error)
				}
				if event.Type == EventToolcallDelta {
					deltas = append(deltas, event.Delta)
				}
			}
			if len(deltas) != 1 || deltas[0] != fixture.want {
				t.Fatalf("deltas = %#v, want [%q]", deltas, fixture.want)
			}
		})
	}
}
