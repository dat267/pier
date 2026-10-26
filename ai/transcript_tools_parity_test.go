package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), ai/src/utils/transcript.ts resolveTranscriptTools.
// Regenerate:
//
//	node --experimental-strip-types ai/testdata/transcript-tools/generate.mjs ./pi > ai/testdata/transcript-tools/golden.json
func TestResolveTranscriptToolsMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/transcript-tools/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string                `json:"name"`
		Messages []json.RawMessage     `json:"messages"`
		Support  TranscriptToolSupport `json:"support"`
		Expected struct {
			RequestTools         []json.RawMessage `json:"requestTools"`
			FoldedSystemMessages int               `json:"foldedSystemMessages"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty transcript-tools corpus")
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
			tools := ResolveTranscriptTools(messages, fixture.Support)
			if tools.FoldedSystemMessages != fixture.Expected.FoldedSystemMessages {
				t.Errorf("folded = %d, want %d", tools.FoldedSystemMessages, fixture.Expected.FoldedSystemMessages)
			}
			encodedTools, err := MarshalJSON(tools.RequestTools)
			if err != nil {
				t.Fatal(err)
			}
			expected := fixture.Expected.RequestTools
			if expected == nil {
				if len(tools.RequestTools) != 0 {
					t.Errorf("request tools = %s, want none", encodedTools)
				}
				return
			}
			encodedExpected, err := MarshalJSON(expected)
			if err != nil {
				t.Fatal(err)
			}
			if string(encodedTools) != string(encodedExpected) {
				t.Errorf("request tools = %s, want %s", encodedTools, encodedExpected)
			}
		})
	}
}
