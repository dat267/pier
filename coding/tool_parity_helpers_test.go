package coding

// Shared helpers for the tool-level parity corpora, kept in one file so a
// corpus can be removed without taking another corpus's fixtures with it.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// manyLines builds "line 1\n" through "line count\n", the numbered lines
// the read and distant-edits fixtures use.
func manyLines(count int) string {
	var builder strings.Builder
	for index := 1; index <= count; index++ {
		builder.WriteString("line ")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString("\n")
	}
	return builder.String()
}

// canonicalReadJSON decodes raw (nil for empty or null) so that marshaling it
// again gives key-sorted JSON: json.Marshal writes map keys in sorted order.
func canonicalReadJSON(t *testing.T, raw []byte) any {
	t.Helper()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return value
}

// checkParityToolResult asserts one tool run against a parity fixture and
// reports whether the caller should keep comparing: a fixture that expects an
// error has nothing further to check.
func checkParityToolResult(t *testing.T, result agent.AgentToolResult, execErr error, expectedError, expectedText string, expectedDetails json.RawMessage) bool {
	t.Helper()
	if expectedError != "" {
		if execErr == nil {
			t.Fatalf("expected error %q, got success", expectedError)
		}
		if execErr.Error() != expectedError {
			t.Fatalf("error = %q, want %q", execErr.Error(), expectedError)
		}
		return false
	}
	if execErr != nil {
		t.Fatalf("unexpected error: %v", execErr)
	}
	var text strings.Builder
	for _, block := range result.Content {
		if content, ok := block.(ai.TextContent); ok {
			text.WriteString(content.Text)
		}
	}
	if text.String() != expectedText {
		t.Fatalf("text differs\n got: %q\nwant: %q", text.String(), expectedText)
	}
	gotDetails, err := json.Marshal(canonicalReadJSON(t, result.Details))
	if err != nil {
		t.Fatal(err)
	}
	wantDetails, err := json.Marshal(canonicalReadJSON(t, expectedDetails))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotDetails) != string(wantDetails) {
		t.Fatalf("details = %s, want %s", gotDetails, wantDetails)
	}
	return true
}

// D279: the reference surfaces Node's libuv wording for a missing file
// ("ENOENT: no such file or directory, access '<path>'"); the port keeps Go's
// os error text. This pins the port's message so the divergence stays visible.
