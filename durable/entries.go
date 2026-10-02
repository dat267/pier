package durable

import (
	"fmt"

	"github.com/dat267/pier/chord"
)

// Port of entries.ts: the typed entry kinds the session writes.

// DefineEntry declares a typed entry kind. The Go port narrows by kind (the
// token carries no phantom data type).
func DefineEntry(kind string) (Entry, error) {
	if kind == "" {
		return Entry{}, fmt.Errorf("Entry kind must be a non-empty string")
	}
	return Entry{Kind: kind}, nil
}

// Built-in entry kinds (upstream UserEntry, AssistantEntry, ...).
var (
	// UserEntry is user input; model is [UserMessage]. Written by submissions.
	UserEntry = Entry{Kind: "pi.user"}
	// AssistantEntry is a provider result with any stop reason.
	AssistantEntry = Entry{Kind: "pi.assistant"}
	// SystemEntry is a positional prompt and tool change.
	SystemEntry = Entry{Kind: "pi.system"}
	// ToolResultEntry is a tool result whose data holds structured diagnostics.
	ToolResultEntry = Entry{Kind: "pi.tool-result"}
	// ResetEntry starts a new context (always head self).
	ResetEntry = Entry{Kind: "pi.reset"}
	// CompactionEntry is a compaction summary whose data holds its reason.
	CompactionEntry = Entry{Kind: "pi.compaction"}
)

// EntryDataOf returns an entry's decoded data payload when the entry matches
// the token.
func EntryDataOf(token Entry, record *EntryRecord) (chord.JsonValue, bool) {
	if !token.Matches(record) || len(record.Data) == 0 {
		return nil, false
	}
	var value chord.JsonValue
	if err := unmarshalBytes(record.Data, &value); err != nil {
		return nil, false
	}
	return value, true
}
