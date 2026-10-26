package coding

import (
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/messages.ts convertToLlm. D248 deliberately
// reorders a custom message that lands between an unanswered tool call and its
// result; those inputs are excluded here.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/convert-to-llm/generate.mjs ./pi > coding/testdata/convert-to-llm/golden.json
func TestConvertToLlmMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/convert-to-llm/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Expected json.RawMessage   `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty convert-to-llm corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			messages := make([]ai.Message, 0, len(fixture.Messages))
			for _, encoded := range fixture.Messages {
				message, err := ai.UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				messages = append(messages, message)
			}
			converted := ConvertToLlm(messages)
			// Marshal each message so the role member the wire form carries is
			// part of the comparison.
			encoded, err := json.Marshal(messageJSONList(t, converted))
			if err != nil {
				t.Fatal(err)
			}
			got := canonicalTestJSON(t, encoded)
			want := canonicalTestJSON(t, fixture.Expected)
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("converted messages differ\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func canonicalTestJSON(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var sortKeys func(any) any
	sortKeys = func(input any) any {
		switch typed := input.(type) {
		case map[string]any:
			keys := make([]string, 0, len(typed))
			for key := range typed {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			out := make(map[string]any, len(typed))
			for _, key := range keys {
				out[key] = sortKeys(typed[key])
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

// messageJSONList encodes messages with their role member.
func messageJSONList(t *testing.T, messages []ai.Message) []json.RawMessage {
	t.Helper()
	encoded := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		raw, err := ai.MarshalMessage(message)
		if err != nil {
			t.Fatalf("marshal message: %v", err)
		}
		encoded = append(encoded, raw)
	}
	return encoded
}
