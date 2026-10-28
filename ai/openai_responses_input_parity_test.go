package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.4.0 (4a080252), api/openai-responses-shared.ts
// convertResponsesMessages. Compare serialized bytes, including optional ID
// positions, rather than decoded maps that discard JavaScript key order.
// Regenerate: node --experimental-strip-types testdata/responses-input/generate.mjs ../pi > testdata/responses-input/golden.json
func TestResponsesInputMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/responses-input/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Model    Model
		Messages []json.RawMessage
		Input    string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			input, err := ConvertResponsesMessages(&fixture.Model, TranscriptContext{Messages: messages}, nil)
			if err != nil {
				t.Fatal(err)
			}
			if string(input) != fixture.Input {
				t.Fatalf("input = %s\nwant    %s", input, fixture.Input)
			}
		})
	}
}
