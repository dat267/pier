package coding

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

func nestedCall(id, name string) ai.ToolCall {
	return ai.ToolCall{ID: id, Name: name, Arguments: json.RawMessage(`{"a":1}`)}
}

// TestNestedCallRecorder covers start/finish, usage summing and the snapshot.
func TestNestedCallRecorder(t *testing.T) {
	recorder := NewNestedCallRecorder()
	if recorder.Snapshot() != nil {
		t.Fatal("an empty recorder must snapshot nil")
	}
	record := recorder.Start(nestedCall("c/1", "grep"))
	if record == nil || record.ID != "c/1" || record.Status != "unfinished" || len(record.Arguments) == 0 {
		t.Fatalf("record = %+v", record)
	}
	// Unfinished calls mark the record incomplete.
	if snapshot := recorder.Snapshot(); snapshot == nil || snapshot.Complete {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	recorder.Finish(record, false, "")
	if record.Status != "ok" || record.DurationMs == nil {
		t.Fatalf("record = %+v", record)
	}
	recorder.AddUsage(ai.Usage{Input: 2, Output: 3, Cost: ai.UsageCost{}})
	recorder.AddUsage(ai.Usage{Input: 1, Output: 4, Cost: ai.UsageCost{}})
	if usage := recorder.TotalUsage(); usage == nil || usage.Input != 3 || usage.Output != 7 {
		t.Fatalf("usage = %+v", usage)
	}
	snapshot := recorder.Snapshot()
	if snapshot == nil || !snapshot.Complete || len(snapshot.Calls) != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

// TestNestedCallRecorderErrorTruncated covers the truncated error text.
func TestNestedCallRecorderErrorTruncated(t *testing.T) {
	recorder := NewNestedCallRecorder()
	record := recorder.Start(nestedCall("c/1", "bash"))
	recorder.Finish(record, true, strings.Repeat("x", NestedCallLimits.MaxErrorChars+50))
	if record.Status != "error" || record.Error == nil || len(*record.Error) != NestedCallLimits.MaxErrorChars {
		t.Fatalf("record = %+v", record)
	}
}

// TestNestedCallRecorderLimits covers the count and argument-size limits.
func TestNestedCallRecorderLimits(t *testing.T) {
	// The call count caps at maxCalls and marks the record incomplete.
	countRecorder := NewNestedCallRecorder()
	dropped := 0
	for index := 0; index < NestedCallLimits.MaxCalls+5; index++ {
		if countRecorder.Start(nestedCall("c", "t")) == nil {
			dropped++
		}
	}
	if dropped != 5 {
		t.Fatalf("dropped = %d", dropped)
	}
	if snapshot := countRecorder.Snapshot(); snapshot == nil || snapshot.Complete || len(snapshot.Calls) != NestedCallLimits.MaxCalls {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	// One oversized argument is omitted and its size recorded.
	oversized := NewNestedCallRecorder()
	big := ai.ToolCall{ID: "c", Name: "t", Arguments: json.RawMessage(`"` + strings.Repeat("a", NestedCallLimits.MaxArgumentBytesPerCall) + `"`)}
	record := oversized.Start(big)
	if record == nil || record.Arguments != nil || record.ArgumentsBytes == nil || *record.ArgumentsBytes <= NestedCallLimits.MaxArgumentBytesPerCall {
		t.Fatalf("record = %+v", record)
	}
	if snapshot := oversized.Snapshot(); snapshot == nil || snapshot.Complete {
		t.Fatalf("snapshot = %+v", snapshot)
	}

	// The total argument budget marks the record incomplete.
	total := NewNestedCallRecorder()
	perCall := &ai.NestedToolCallRecord{}
	_ = perCall
	chunk := ai.ToolCall{ID: "c", Name: "t", Arguments: json.RawMessage(`"` + strings.Repeat("a", NestedCallLimits.MaxArgumentBytesPerCall-10) + `"`)}
	for index := 0; index < 5; index++ {
		total.Start(chunk)
	}
	if snapshot := total.Snapshot(); snapshot == nil || snapshot.Complete {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

// TestToolResultNestedCallsRoundTrip pins that the tool result's nestedCalls
// field survives the message codec.
func TestToolResultNestedCallsRoundTrip(t *testing.T) {
	message := &ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "read",
		Content: ai.UserContentList{ai.TextContent{Text: "out"}},
		NestedCalls: &ai.NestedToolCalls{
			Calls:    []ai.NestedToolCallRecord{{ID: "c1/1", Name: "grep", Status: "ok"}},
			Complete: true,
		},
		Timestamp: 1,
	}
	encoded, err := ai.MarshalMessage(message)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"nestedCalls"`) {
		t.Fatalf("nestedCalls missing: %s", encoded)
	}
	decoded, err := ai.UnmarshalMessage(encoded)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := decoded.(*ai.ToolResultMessage)
	if !ok || result.NestedCalls == nil || len(result.NestedCalls.Calls) != 1 || !result.NestedCalls.Complete {
		t.Fatalf("decoded = %+v", decoded)
	}
	if result.NestedCalls.Calls[0].ID != "c1/1" || result.NestedCalls.Calls[0].Name != "grep" {
		t.Fatalf("calls = %+v", result.NestedCalls.Calls)
	}
}
