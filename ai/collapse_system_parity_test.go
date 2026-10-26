package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), ai/src/utils/transcript.ts collapseSystemMessages and
// resolveTranscript. Regenerate:
//
//	node --experimental-strip-types ai/testdata/collapse-system/generate.mjs ./pi > ai/testdata/collapse-system/golden.json
func TestCollapseSystemMessagesMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/collapse-system/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Function string            `json:"function"`
		Supports bool              `json:"supports"`
		Expected []json.RawMessage `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty collapse-system corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			messages := make([]Message, 0, len(fixture.Messages))
			for _, encoded := range fixture.Messages {
				message, err := UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				messages = append(messages, message)
			}
			context := TranscriptContext{Messages: messages}
			var resolved TranscriptContext
			if fixture.Function == "collapse" {
				resolved = CollapseSystemMessages(context)
			} else {
				resolved = ResolveTranscript(context, fixture.Supports)
			}
			// Pass-through members keep the input object's key order, which is a
			// harness concern; compare the structural value. Message byte order is
			// pinned by the session round-trip tests.
			gotBytes, err := json.Marshal(messageJSONListAi(t, resolved.Messages))
			if err != nil {
				t.Fatal(err)
			}
			wantBytes, err := json.Marshal(fixture.Expected)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(canonicalCollapseJSON(t, gotBytes))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := json.Marshal(canonicalCollapseJSON(t, wantBytes))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != string(expected) {
				t.Fatalf("messages = %s, want %s", encoded, expected)
			}
		})
	}
}

func messageJSONListAi(t *testing.T, messages []Message) []json.RawMessage {
	t.Helper()
	encoded := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		raw, err := MarshalMessage(message)
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		encoded = append(encoded, raw)
	}
	return encoded
}

// canonicalCollapseJSON decodes and sorts object keys so the comparison covers
// structure and values rather than the harness's own key order.
func canonicalCollapseJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sortKeys func(any) any
	sortKeys = func(input any) any {
		switch typed := input.(type) {
		case map[string]any:
			out := make(map[string]any, len(typed))
			for key, item := range typed {
				out[key] = sortKeys(item)
			}
			return out
		case []any:
			for index := range typed {
				typed[index] = sortKeys(typed[index])
			}
			return typed
		default:
			return input
		}
	}
	return sortKeys(value)
}
