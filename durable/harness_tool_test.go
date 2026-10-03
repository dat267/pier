package durable

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/tool.ts result helpers.

func TestHarnessError(t *testing.T) {
	result := HarnessError("tool_unavailable", "Tool x is not available")
	if result.IsError == nil || !*result.IsError || len(result.Content) != 0 {
		t.Fatalf("result = %+v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Severity != DiagnosticError ||
		result.Diagnostics[0].Code == nil || *result.Diagnostics[0].Code != "tool_unavailable" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestRenderDiagnosticsAndTruncated(t *testing.T) {
	rendered := RenderDiagnostics([]ToolDiagnostic{
		{Severity: DiagnosticError, Message: "one"},
		{Severity: DiagnosticWarn, Message: "two"},
	})
	if rendered != "<harness>\n[error] one\n[warn] two\n</harness>" {
		t.Fatalf("rendered = %q", rendered)
	}
	if diagnostic := Truncated(120, 3, nil); diagnostic.Code == nil || *diagnostic.Code != "truncated" ||
		diagnostic.Severity != DiagnosticWarn || diagnostic.Message != "Output truncated: 3 lines, 120 bytes dropped" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	head := RetainHead
	if diagnostic := Truncated(1, 2, &head); diagnostic.Message != "Output truncated to its beginning: 2 lines, 1 bytes dropped" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
	tail := RetainTail
	if diagnostic := Truncated(1, 2, &tail); diagnostic.Message != "Output truncated to its end: 2 lines, 1 bytes dropped" {
		t.Fatalf("diagnostic = %+v", diagnostic)
	}
}

func TestBoundContent(t *testing.T) {
	content := []ai.UserContent{ai.TextContent{Text: "aaaa"}, ai.TextContent{Text: "bbbb"}}
	limits := OutputLimits{MaxBytes: 4, MaxLines: 2000, Retain: RetainHead}
	bounded, droppedBytes, droppedLines := BoundContent(content, limits)
	if droppedBytes != 4 || droppedLines != 0 || len(bounded) != 1 || bounded[0].(ai.TextContent).Text != "aaaa" {
		t.Fatalf("bounded = %+v, %d, %d", bounded, droppedBytes, droppedLines)
	}
	limits.Retain = RetainTail
	bounded, droppedBytes, _ = BoundContent(content, limits)
	if droppedBytes != 4 || len(bounded) != 1 || bounded[0].(ai.TextContent).Text != "bbbb" {
		t.Fatalf("bounded = %+v, %d", bounded, droppedBytes)
	}
	// Within the limits the content is unchanged.
	big := OutputLimits{MaxBytes: 1024, MaxLines: 2000, Retain: RetainHead}
	bounded, droppedBytes, _ = BoundContent(content, big)
	if droppedBytes != 0 || len(bounded) != 2 {
		t.Fatalf("bounded = %+v, %d", bounded, droppedBytes)
	}
}

func TestAppendToolResult(t *testing.T) {
	session, _ := newRootSession(t)
	ctx := context.Background()
	call := ai.ToolCall{ID: "c1", Name: "bash"}
	usage := ai.Usage{Input: 5, Output: 7, TotalTokens: 12}
	result := ToolExecutionResult{
		Content:     []ai.UserContent{ai.TextContent{Text: "out"}},
		Details:     map[string]any{"a": 1},
		Diagnostics: []ToolDiagnostic{{Severity: DiagnosticWarn, Message: "m"}},
		Usage:       &usage,
	}
	var entry *EntryRecord
	if err := session.Commit(ctx, func(tx *Transaction) error {
		var err error
		entry, err = AppendToolResult(tx, RootConversationID, call, result, 42)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if entry == nil || entry.Kind != ToolResultEntry.Kind {
		t.Fatalf("entry = %+v", entry)
	}
	message, ok := entry.Model[0].(*ai.ToolResultMessage)
	if !ok || message.ToolCallID != "c1" || message.ToolName != "bash" || message.Timestamp != 42 || message.IsError {
		t.Fatalf("message = %+v", message)
	}
	if len(message.Content) != 2 || message.Content[0].(ai.TextContent).Text != "out" ||
		message.Content[1].(ai.TextContent).Text != "<harness>\n[warn] m\n</harness>" {
		t.Fatalf("content = %+v", message.Content)
	}
	var details map[string]any
	if err := json.Unmarshal(message.Details, &details); err != nil || details["a"] != 1.0 {
		t.Fatalf("details = %s", message.Details)
	}
	var data struct {
		Diagnostics []ToolDiagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(entry.Data, &data); err != nil || len(data.Diagnostics) != 1 {
		t.Fatalf("data = %s", entry.Data)
	}
	value, ok, err := session.Snapshot(ctx, UsageDoc.Definition, RootConversationID)
	if err != nil || !ok {
		t.Fatalf("usage snapshot = %v, %v", ok, err)
	}
	state, _ := decodeJSONInto[UsageState](value)
	if state.Tools["bash"].TotalTokens != 12 || state.Tools["bash"].Input != 5 {
		t.Fatalf("usage = %+v", state.Tools)
	}
}

func TestPrepareAndValidateArguments(t *testing.T) {
	repairing := ToolRegistration{Tool: ai.Tool{Name: "x"}}
	repairing.PrepareArguments = func(args chord.JsonValue) (chord.JsonValue, error) {
		object := args.(map[string]any)
		object["a"] = "repaired"
		return object, nil
	}
	args, errText := PrepareArguments(repairing, JsonObject{"a": "given"})
	if errText != nil || args["a"] != "repaired" {
		t.Fatalf("args = %+v, %v", args, errText)
	}
	// A repair that panics is invalid.
	panicking := ToolRegistration{Tool: ai.Tool{Name: "x"}}
	panicking.PrepareArguments = func(chord.JsonValue) (chord.JsonValue, error) { panic("boom") }
	if _, errText := PrepareArguments(panicking, JsonObject{}); errText == nil || *errText != "boom" {
		t.Fatalf("errText = %v", errText)
	}
	// Validation checks and coerces against the schema.
	schema := ToolRegistration{Tool: ai.Tool{
		Name:       "x",
		Parameters: json.RawMessage(`{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`),
	}}
	call := ai.ToolCall{ID: "c1", Name: "x"}
	if _, errText := ValidateArguments(schema, call, JsonObject{"a": "ok"}); errText != nil {
		t.Fatalf("errText = %v", *errText)
	}
	if _, errText := ValidateArguments(schema, call, JsonObject{}); errText == nil {
		t.Fatal("a missing required argument must be invalid")
	}
}
