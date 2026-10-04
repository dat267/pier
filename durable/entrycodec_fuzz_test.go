package durable

import (
	"encoding/json"
	"testing"
)

// FuzzEntryRecordCodec checks the entry codec never panics and is a fixed point:
// a decoded entry marshaled and parsed back must reproduce the same bytes
// (entries are immutable session records).
func FuzzEntryRecordCodec(f *testing.F) {
	seeds := []string{
		`{"id":1,"conversationId":2,"kind":"note"}`,
		`{"id":1,"conversationId":2,"kind":"message","model":[{"role":"user","content":"hi","timestamp":1}]}`,
		`{"id":1,"conversationId":2,"kind":"note","head":0,"data":{"x":1},"byTaskId":3}`,
		`{"id":1,"conversationId":2,"kind":"note","edits":[{"target":1,"action":"omit"}]}`,
		`{"id":1,"conversationId":2,"kind":"note","edits":[{"target":1,"action":"replace","messages":[{"role":"user","content":"hi","timestamp":1}]}]}`,
		`{"id":1,"conversationId":2,"kind":"message","model":[{"role":"assistant","content":[{"type":"text","text":"x"}],"api":"a","provider":"p","model":"m","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop","timestamp":2}]}`,
		`{"id":1}`,
		`null`,
		`[]`,
		`{`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		var record EntryRecord
		if err := json.Unmarshal([]byte(input), &record); err != nil {
			return
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal failed for %q: %v", input, err)
		}
		var again EntryRecord
		if err := json.Unmarshal(encoded, &again); err != nil {
			t.Fatalf("re-parse of %q failed: %v", encoded, err)
		}
		reencoded, err := json.Marshal(again)
		if err != nil || string(reencoded) != string(encoded) {
			t.Fatalf("entry codec is not a fixed point for %q:\n first = %q\n again = %q (%v)", input, encoded, reencoded, err)
		}
	})
}
