package ai

import (
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) agent-loop.ts createToolResultMessage builds
// the object as { role, toolCallId, toolName, content, details, usage, isError,
// durationMs?, timestamp }, and agent-session.ts then assigns
// `message.nestedCalls = summary.calls`, which JavaScript orders after every
// existing key. Pier put nestedCalls between usage and isError, so a result
// with nested calls persisted in a different byte order.
func TestToolResultMessagePutsNestedCallsAfterTimestamp(t *testing.T) {
	message := &ToolResultMessage{
		ToolCallID: "call-1",
		ToolName:   "bash",
		Content:    UserContentList{TextContent{Text: "ok"}},
		NestedCalls: &NestedToolCalls{
			Calls:    []NestedToolCallRecord{{ID: "n1", Name: "read", Status: "ok"}},
			Complete: true,
		},
		IsError:   false,
		Timestamp: 1700000000000,
	}
	encoded, err := MarshalMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	json := string(encoded)
	nested := strings.Index(json, `"nestedCalls"`)
	timestamp := strings.Index(json, `"timestamp"`)
	if nested < 0 || timestamp < 0 {
		t.Fatalf("missing keys: %s", json)
	}
	if nested < timestamp {
		t.Fatalf("nestedCalls before timestamp: %s", json)
	}
}
