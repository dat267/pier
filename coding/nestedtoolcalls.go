package coding

// Port of core/nested-tool-calls.ts: the record of the calls a tool makes while
// it runs (`ctx.executeTool()`). The port has no nested-execution host (its
// only triggers, codemode and extension tools, are out of scope), so this is
// the data model and recorder used by the session record.

import (
	"encoding/json"
	"time"
	"unicode/utf16"

	"github.com/dat267/pier/ai"
)

// NestedCallLimits bounds the nested-call record on a tool result: arguments
// over the per-call or total size are omitted, calls beyond the count are
// dropped, and the record is marked incomplete when any of that happens.
var NestedCallLimits = struct {
	MaxCalls                int
	MaxArgumentBytesPerCall int
	MaxArgumentBytesTotal   int
	MaxErrorChars           int
}{MaxCalls: 256, MaxArgumentBytesPerCall: 8 * 1024, MaxArgumentBytesTotal: 32 * 1024, MaxErrorChars: 500}

// NestedCallSummary is what the nested calls of one model-issued tool call
// leave on its tool result message.
type NestedCallSummary struct {
	// Calls becomes nestedCalls; nil when no nested call was made.
	Calls *ai.NestedToolCalls
	// Usage is the summed usage of the nested results, added to the message's.
	Usage *ai.Usage
}

// NestedCallRecorder collects the nested calls of one model-issued tool call,
// including calls made by nested tools.
type NestedCallRecorder struct {
	calls         []*ai.NestedToolCallRecord
	startedAt     map[*ai.NestedToolCallRecord]time.Time
	complete      bool
	argumentBytes int
	usage         *ai.Usage
}

// NewNestedCallRecorder creates a recorder.
func NewNestedCallRecorder() *NestedCallRecorder {
	return &NestedCallRecorder{startedAt: map[*ai.NestedToolCallRecord]time.Time{}, complete: true}
}

// Start records a call as it starts, returning nil when the call is dropped.
func (r *NestedCallRecorder) Start(toolCall ai.ToolCall) *ai.NestedToolCallRecord {
	if len(r.calls) >= NestedCallLimits.MaxCalls {
		r.complete = false
		return nil
	}
	record := &ai.NestedToolCallRecord{ID: toolCall.ID, Name: toolCall.Name, Status: "unfinished"}
	arguments := toolCall.Arguments
	if len(arguments) == 0 {
		arguments = json.RawMessage("{}")
	}
	size := len(arguments)
	if size > NestedCallLimits.MaxArgumentBytesPerCall || r.argumentBytes+size > NestedCallLimits.MaxArgumentBytesTotal {
		record.ArgumentsBytes = &size
		r.complete = false
	} else {
		record.Arguments = append(json.RawMessage(nil), arguments...)
		r.argumentBytes += size
	}
	r.calls = append(r.calls, record)
	r.startedAt[record] = time.Now()
	return record
}

// Finish records the outcome of a started call.
func (r *NestedCallRecorder) Finish(record *ai.NestedToolCallRecord, isError bool, errorText string) {
	if record == nil {
		return
	}
	if isError {
		record.Status = "error"
	} else {
		record.Status = "ok"
	}
	started, ok := r.startedAt[record]
	if !ok {
		started = time.Now()
	}
	duration := time.Since(started).Milliseconds()
	record.DurationMs = &duration
	delete(r.startedAt, record)
	if isError && errorText != "" {
		trimmed := truncateUTF16(errorText, NestedCallLimits.MaxErrorChars)
		record.Error = &trimmed
	}
}

// AddUsage sums one nested result's usage.
func (r *NestedCallRecorder) AddUsage(usage ai.Usage) {
	if r.usage == nil {
		copied := usage
		r.usage = &copied
		return
	}
	combined := CombineUsage(*r.usage, usage)
	r.usage = &combined
}

// TotalUsage is the summed usage of every nested result, including dropped
// calls.
func (r *NestedCallRecorder) TotalUsage() *ai.Usage { return r.usage }

// Snapshot is a copy of the record so far, or nil when no nested call was made.
func (r *NestedCallRecorder) Snapshot() *ai.NestedToolCalls {
	if len(r.calls) == 0 && r.complete {
		return nil
	}
	calls := make([]ai.NestedToolCallRecord, 0, len(r.calls))
	allFinished := true
	for _, call := range r.calls {
		copied := *call
		calls = append(calls, copied)
		if copied.Status == "unfinished" {
			allFinished = false
		}
	}
	return &ai.NestedToolCalls{Calls: calls, Complete: r.complete && allFinished}
}

// Summary builds the message additions for the recorder.
func (r *NestedCallRecorder) Summary() NestedCallSummary {
	return NestedCallSummary{Calls: r.Snapshot(), Usage: r.TotalUsage()}
}

// truncateUTF16 keeps the first max UTF-16 code units (upstream `.slice`).
func truncateUTF16(value string, max int) string {
	if max <= 0 {
		return ""
	}
	encoded := utf16.Encode([]rune(value))
	if len(encoded) <= max {
		return value
	}
	return string(utf16.Decode(encoded[:max]))
}
