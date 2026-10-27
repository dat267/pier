package coding

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// The listing scanner must agree with the typed decoder for every message it
// claims to understand; anything it rejects falls back to that decoder.
func TestScanListingMessageMatchesTypedDecode(t *testing.T) {
	marshal := func(message ai.Message) string {
		t.Helper()
		raw, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	assistantWithToolCall := marshal(&ai.AssistantMessage{
		Content: ai.ContentList{
			ai.TextContent{Text: "answer"},
			ai.ThinkingContent{Thinking: "long reasoning"},
			ai.ToolCall{ID: "call_1", Name: "bash", Arguments: []byte(`{"command":"ls"}`)},
		},
		Timestamp: 42,
	})
	userWithImage := marshal(&ai.UserMessage{
		Content: ai.StringOrBlocks{Blocks: ai.ContentList{
			ai.TextContent{Text: "look at this"},
			ai.ImageContent{Data: "AAAA", MimeType: "image/png"},
		}},
		Timestamp: 7,
	})
	systemMessage := marshal(&ai.SystemMessage{Content: ai.StringOrBlocks{Text: "sys"}, Timestamp: 3})

	cases := []struct {
		name string
		raw  string
	}{
		{"user string", `{"role":"user","content":"hello","timestamp":5}`},
		{"user string content first", `{"content":"hello","role":"user","timestamp":5}`},
		{"user empty string", `{"role":"user","content":"","timestamp":5}`},
		{"user null content", `{"role":"user","content":null,"timestamp":5}`},
		{"user blocks", userWithImage},
		{"user escaped content", `{"role":"user","content":"line\n\"quoted\" \u00e9","timestamp":5}`},
		{"user missing timestamp", `{"role":"user","content":"hello"}`},
		{"user null timestamp", `{"role":"user","content":"hello","timestamp":null}`},
		{"user negative timestamp", `{"role":"user","content":"hello","timestamp":-5}`},
		{"assistant blocks", assistantWithToolCall},
		{"tool result", `{"role":"toolResult","toolCallId":"c1","toolName":"bash","content":[{"type":"text","text":"output"}],"timestamp":9}`},
		{"tool result content first", `{"content":[{"type":"text","text":"output"}],"role":"toolResult","toolCallId":"c1","toolName":"bash","timestamp":9}`},
		{"system", systemMessage},
		{"custom role", `{"role":"bashExecution","content":[{"type":"text","text":"x"}],"timestamp":4}`},
		{"missing role", `{"content":"x","timestamp":4}`},
		{"empty object", `{}`},
		{"unknown top-level member", `{"role":"user","content":"hello","timestamp":5,"extra":{"a":1}}`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			scanned := scanListingMessage([]byte(testCase.raw))
			message, err := ai.UnmarshalMessage([]byte(testCase.raw))
			if err != nil {
				t.Fatalf("reference decode failed: %v", err)
			}
			role := string(ai.RoleOf(message))
			text := ""
			if role == "user" || role == "assistant" {
				text = extractTextContent(message)
			}
			if !scanned.ok {
				t.Fatalf("scanner rejected a decodable message")
			}
			if scanned.role != role || scanned.timestamp != listingMessageTimestamp(message) || scanned.text != text {
				t.Fatalf("scanner = %+v, reference role=%q timestamp=%d text=%q",
					scanned, role, listingMessageTimestamp(message), text)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal([]byte(testCase.raw), &fields); err != nil {
				t.Fatal(err)
			}
			_, wantContent := fields["content"]
			if scanned.hasContent != wantContent {
				t.Fatalf("scanner hasContent = %v, want %v", scanned.hasContent, wantContent)
			}
			var number float64
			wantTimestamp := false
			if raw, ok := fields["timestamp"]; ok && json.Unmarshal(raw, &number) == nil {
				wantTimestamp = true
			}
			if scanned.hasTimestamp != wantTimestamp {
				t.Fatalf("scanner hasTimestamp = %v, want %v", scanned.hasTimestamp, wantTimestamp)
			}
		})
	}

	// Shapes the scanner cannot take at face value fall back instead of
	// guessing: the caller then full-decodes.
	rejected := []struct {
		name string
		raw  string
	}{
		{"unknown block type", `{"role":"assistant","content":[{"type":"weird"}],"timestamp":1}`},
		{"non-string text", `{"role":"assistant","content":[{"type":"text","text":123}],"timestamp":1}`},
		{"non-object message", `"nope"`},
		{"non-string role", `{"role":5,"content":"x"}`},
		{"duplicate role", `{"role":"user","role":"assistant","content":"x"}`},
		{"trailing data", `{"role":"user","content":"x"} extra`},
	}
	for _, testCase := range rejected {
		t.Run(testCase.name, func(t *testing.T) {
			if scanned := scanListingMessage([]byte(testCase.raw)); scanned.ok {
				t.Fatalf("scanner accepted %q: %+v", testCase.raw, scanned)
			}
		})
	}
}

// Listing metadata must not allocate proportional to block payloads it does
// not read: tool-call arguments, thinking and tool-result bodies dominate real
// sessions. The typed decode allocated the payload strings; the scanner skips
// them.
func TestBuildSessionInfoSkipsUnusedMessagePayloads(t *testing.T) {
	thinking := strings.Repeat("reasoning payload ", 512) // ~9 KiB
	var buf strings.Builder
	buf.WriteString(`{"type":"session","version":3,"id":"s1","timestamp":"t0","cwd":"/tmp"}` + "\n")
	for i := range 400 {
		// Alternate key order: both exist in stored sessions.
		if i%2 == 0 {
			fmt.Fprintf(&buf, `{"type":"message","id":"m%04d","parentId":null,"timestamp":"t","message":{"role":"assistant","content":[{"type":"text","text":"hello %d"},{"type":"thinking","thinking":%q}],"timestamp":%d}}`+"\n", i, i, thinking, i+1)
		} else {
			fmt.Fprintf(&buf, `{"type":"message","id":"m%04d","parentId":null,"timestamp":"t","message":{"content":[{"type":"text","text":"hello %d"},{"type":"thinking","thinking":%q}],"role":"assistant","timestamp":%d}}`+"\n", i, i, thinking, i+1)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(buf.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	allocated := allocatedBytesPerRun(t, 1, func() {
		built := buildSessionInfo(path)
		if built == nil || built.MessageCount != 400 || strings.Count(built.AllMessagesText, "hello") != 400 {
			t.Fatalf("buildSessionInfo = %+v", built)
		}
	})
	if allocated > int64(info.Size())*2 {
		t.Fatalf("buildSessionInfo allocated %d bytes for a %d-byte file of unread payloads", allocated, info.Size())
	}
}

// listingMessageTimestamp is the numeric timestamp a decoded message carries
// (the scanner's counterpart before presence handling).
func listingMessageTimestamp(message ai.Message) int64 {
	switch m := message.(type) {
	case *ai.SystemMessage:
		return m.Timestamp
	case *ai.UserMessage:
		return m.Timestamp
	case *ai.AssistantMessage:
		return m.Timestamp
	case *ai.ToolResultMessage:
		return m.Timestamp
	case *ai.CustomMessage:
		return m.Timestamp
	}
	return 0
}
