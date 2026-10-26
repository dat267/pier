package mcp

import (
	"context"
	"github.com/dat267/pier/mcp/protocol"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), packages/mcp/src/client.ts
// requestInternal and stdio.ts/streamable-http.ts propagate JSON serialization
// failures even when requestInternal adds a progress token.
func TestClientRejectsCyclicArgumentsWithProgress(t *testing.T) {
	for _, progress := range []bool{false, true} {
		name := "plain"
		if progress {
			name = "progress"
		}
		t.Run(name, func(t *testing.T) {
			client, server := connectTestClient(t)
			defer client.Close(context.Background())
			server.setHandler("tools/call", func(*protocol.JsonRpcRequest) (any, error) { return map[string]any{"content": []any{}}, nil })
			args := map[string]any{}
			args["self"] = args
			options := RequestOptions{}
			if progress {
				options.OnProgress = func(*protocol.ProgressNotification) {}
			}
			_, err := client.CallTool(context.Background(), "echo", args, options)
			if err == nil || !strings.Contains(err.Error(), "encountered a cycle") {
				t.Fatalf("cyclic arguments error = %v, want serialization failure", err)
			}
			for _, message := range server.recordedMessages() {
				if request, ok := message.(*protocol.JsonRpcRequest); ok && request.Method == "tools/call" {
					t.Fatalf("unserializable tool call reached server: %+v", request)
				}
			}
		})
	}
}
