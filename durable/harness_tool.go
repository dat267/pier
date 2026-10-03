package durable

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of harness/tool.ts: the built-in tool task's input, checkpoint and
// result types, and the result helpers the task and its runtime share. The
// task's phases land with the task runtime.

// ToolTaskInput is the tool task input.
type ToolTaskInput struct {
	Assistant Id     `json:"assistant"`
	CallID    string `json:"callId"`
}

// Tool task checkpoint phases.
const (
	ToolPhaseCall    = "call"
	ToolPhaseExecute = "execute"
)

// ToolTaskCheckpoint is the tool task's durable checkpoint; the execute phase
// records the final arguments and replay policy as durable intent.
type ToolTaskCheckpoint struct {
	Phase     string     `json:"phase"`
	Arguments JsonObject `json:"arguments,omitempty"`
	Replay    string     `json:"replay,omitempty"`
}

// ToolTaskResult is the tool task result.
type ToolTaskResult struct {
	EntryID Id           `json:"entryId"`
	Control *ToolControl `json:"control,omitempty"`
}

// ToolTask is the built-in tool task definition.
var ToolTask = Task{Definition: TaskDefinition{
	Name: ToolTaskKind, Version: 1,
	Initial: func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"phase":"call"}`), nil
	},
}}

// InvalidArguments is the result of arguments that fail validation.
func InvalidArguments(message string) ToolExecutionResult {
	return HarnessError("invalid_arguments", message)
}

// FromSlot rebuilds an error result from a slot's durable partial output,
// details and diagnostics.
func FromSlot(slot map[string]any, code, message string) ToolExecutionResult {
	diagnostics := []ToolDiagnostic{}
	if slot != nil {
		if list, ok := slot["diagnostics"].([]any); ok {
			for _, item := range list {
				if diagnostic, ok := decodeJSONInto[ToolDiagnostic](item); ok {
					diagnostics = append(diagnostics, diagnostic)
				}
			}
		}
	}
	droppedBytes := 0
	droppedLines := 0
	if slot != nil {
		if value, ok := jsonID(slot["droppedBytes"]); ok {
			droppedBytes = int(value)
		}
		if value, ok := jsonID(slot["droppedLines"]); ok {
			droppedLines = int(value)
		}
	}
	if droppedBytes > 0 {
		diagnostics = append(diagnostics, Truncated(droppedBytes, droppedLines, nil))
	}
	diagnostics = append(diagnostics, ToolDiagnosticOf(code, message))
	content := []ai.UserContent{}
	if slot != nil {
		if output, ok := slot["output"].(string); ok && output != "" {
			content = append(content, ai.TextContent{Text: output})
		}
	}
	isError := true
	result := ToolExecutionResult{Content: content, IsError: &isError, Diagnostics: diagnostics}
	if slot != nil {
		if details, present := slot["details"]; present && details != nil {
			result.Details = details
		}
	}
	return result
}

// HarnessError is an error result the Harness writes itself: no content and
// one `error` diagnostic with code.
func HarnessError(code, message string) ToolExecutionResult {
	isError := true
	return ToolExecutionResult{
		Content: []ai.UserContent{}, IsError: &isError,
		Diagnostics: []ToolDiagnostic{ToolDiagnosticOf(code, message)},
	}
}

// ToolDiagnosticOf builds one error diagnostic.
func ToolDiagnosticOf(code, message string) ToolDiagnostic {
	return ToolDiagnostic{Severity: DiagnosticError, Code: &code, Message: message}
}

// Truncated is the Harness's truncation diagnostic; retain is nil when rebuilt
// from a slot after recovery.
func Truncated(droppedBytes, droppedLines int, retain *string) ToolDiagnostic {
	kept := ""
	if retain != nil {
		if *retain == RetainHead {
			kept = " to its beginning"
		} else {
			kept = " to its end"
		}
	}
	code := "truncated"
	return ToolDiagnostic{
		Severity: DiagnosticWarn, Code: &code,
		Message: fmt.Sprintf("Output truncated%s: %d lines, %d bytes dropped", kept, droppedLines, droppedBytes),
	}
}

// RenderDiagnostics renders diagnostics for the model.
func RenderDiagnostics(diagnostics []ToolDiagnostic) string {
	rendered := "<harness>\n"
	for index, diagnostic := range diagnostics {
		if index > 0 {
			rendered += "\n"
		}
		rendered += "[" + diagnostic.Severity + "] " + diagnostic.Message
	}
	return rendered + "\n</harness>"
}

// BoundContent bounds the text of result content. When the joined text exceeds
// the limits, the text items are replaced by one bounded item at the position
// of the first (head) or last (tail) text item; other content is kept.
func BoundContent(content []ai.UserContent, limits OutputLimits) (boundedContent []ai.UserContent, droppedBytes, droppedLines int) {
	texts := []ai.TextContent{}
	for _, item := range content {
		if text, ok := item.(ai.TextContent); ok {
			texts = append(texts, text)
		}
	}
	joined := ""
	for _, text := range texts {
		joined += text.Text
	}
	bounded := BoundOutput(joined, limits)
	if bounded.DroppedBytes == 0 {
		return content, 0, 0
	}
	var keep *ai.TextContent
	if limits.Retain == RetainTail {
		if len(texts) > 0 {
			keep = &texts[len(texts)-1]
		}
	} else if len(texts) > 0 {
		keep = &texts[0]
	}
	result := make([]ai.UserContent, 0, len(content))
	for _, item := range content {
		text, ok := item.(ai.TextContent)
		if !ok {
			result = append(result, item)
			continue
		}
		if keep != nil && text == *keep {
			result = append(result, ai.TextContent{Text: bounded.Text, TextSignature: text.TextSignature})
		}
	}
	return result, bounded.DroppedBytes, bounded.DroppedLines
}

// AppendToolResult appends a `pi.tool-result` entry. The content ends with the
// rendered diagnostics, so the stored message is exactly what the model sees;
// data keeps the structured list. A result's usage is added to `pi.usage` in
// the same commit.
func AppendToolResult(tx *Transaction, conversationID Id, call ai.ToolCall, result ToolExecutionResult, timestamp int64) (*EntryRecord, error) {
	diagnostics := append([]ToolDiagnostic{}, result.Diagnostics...)
	content := append([]ai.UserContent{}, result.Content...)
	if len(diagnostics) > 0 {
		content = append(content, ai.TextContent{Text: RenderDiagnostics(diagnostics)})
	}
	message := &ai.ToolResultMessage{
		ToolCallID: call.ID, ToolName: call.Name, Content: ai.UserContentList(content),
		Usage: result.Usage, IsError: result.IsError != nil && *result.IsError, Timestamp: timestamp,
	}
	if result.Details != nil {
		encoded, err := marshalJSONValue(result.Details)
		if err != nil {
			return nil, err
		}
		message.Details = json.RawMessage(encoded)
	}
	if result.Usage != nil {
		if err := RecordUsage(context.Background(), tx, conversationID, UsageBucketTools, call.Name, *result.Usage); err != nil {
			return nil, err
		}
	}
	data, err := marshalJSONValue(map[string]any{"diagnostics": diagnostics})
	if err != nil {
		return nil, err
	}
	return tx.AppendEntry(conversationID, EntryDraft{
		Kind: ToolResultEntry.Kind, Model: []ai.Message{message}, Data: json.RawMessage(data),
	})
}

// ErrorText is an error's message.
func ErrorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// PrepareArguments is the call's arguments as repaired by the tool; a failing
// repair makes them invalid.
func PrepareArguments(tool ToolRegistration, args JsonObject) (JsonObject, *string) {
	if tool.PrepareArguments == nil {
		return args, nil
	}
	repaired, err := callPrepareArguments(tool.PrepareArguments, args)
	if err != nil {
		message := err.Error()
		return nil, &message
	}
	object, ok := repaired.(map[string]any)
	if !ok {
		message := "Prepared arguments must be an object"
		return nil, &message
	}
	return object, nil
}

// ValidateArguments is the arguments validated and coerced against the tool's
// schema.
func ValidateArguments(tool ToolRegistration, call ai.ToolCall, args JsonObject) (JsonObject, *string) {
	encoded, err := marshalJSONValue(args)
	if err != nil {
		message := err.Error()
		return nil, &message
	}
	call.Arguments = json.RawMessage(encoded)
	validated, err := ai.ValidateToolArguments(tool.Tool, call)
	if err != nil {
		message := err.Error()
		return nil, &message
	}
	var object JsonObject
	if err := json.Unmarshal(validated, &object); err != nil {
		message := err.Error()
		return nil, &message
	}
	return object, nil
}

// callPrepareArguments calls a repair, turning a panic into an error (a tool
// that throws).
func callPrepareArguments(prepare func(chord.JsonValue) (chord.JsonValue, error), args JsonObject) (value chord.JsonValue, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("%v", recovered)
		}
	}()
	return prepare(args)
}
