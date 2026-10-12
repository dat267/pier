package durable

import (
	"strings"
	"testing"
)

// Durable records are persisted with JSON.stringify, which does not HTML-escape.
func TestEntryRecordMarshalDoesNotHTMLEscape(t *testing.T) {
	encoded, err := EntryRecord{ID: 1, ConversationID: 2, Kind: "a < b & c > d"}.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	assertNoHTMLEscape(t, string(encoded), "a < b & c > d")
}

func TestJsonlCommitDoesNotHTMLEscape(t *testing.T) {
	prepared := &PreparedCommit{Seq: 1, Writes: []StorageWrite{
		{Type: "entry", Entry: &EntryRecord{ID: 2, ConversationID: 1, Kind: "a < b & c > d"}},
	}}
	marker, sidecars, err := encodeJsonlCommit(prepared)
	if err != nil {
		t.Fatal(err)
	}
	all := marker
	for _, sidecar := range sidecars {
		all += sidecar
	}
	assertNoHTMLEscape(t, all, "a < b & c > d")
}

func assertNoHTMLEscape(t *testing.T, got, want string) {
	t.Helper()
	for _, escaped := range []string{`\u003c`, `\u0026`, `\u003e`} {
		if strings.Contains(got, escaped) {
			t.Fatalf("HTML-escaped: %s", got)
		}
	}
	if !strings.Contains(got, want) {
		t.Fatalf("value missing: %s", got)
	}
}
