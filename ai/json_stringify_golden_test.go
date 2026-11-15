package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// JSON.stringify is the reference's serializer for transcripts and wire
// payloads. The fixture holds Node's output for a corpus of documents, compact
// and two-space indented; this compares pier's canonical marshalers with it.
// Regenerate with node ai/testdata/json-stringify/generate.mjs.
func TestMarshalJSONMatchesJavaScriptStringify(t *testing.T) {
	data, err := os.ReadFile("testdata/json-stringify/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input    string `json:"input"`
		Compact  string `json:"compact"`
		Indented string `json:"indented"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty corpus")
	}
	for _, testCase := range cases {
		compact, err := MarshalJSON(json.RawMessage(testCase.Input))
		if err != nil {
			t.Fatalf("%s: %v", testCase.Input, err)
		}
		if string(compact) != testCase.Compact {
			t.Errorf("%s compact = %s, want %s", testCase.Input, compact, testCase.Compact)
		}
		indented, err := MarshalIndentJSON(json.RawMessage(testCase.Input))
		if err != nil {
			t.Fatalf("%s: %v", testCase.Input, err)
		}
		if string(indented) != testCase.Indented {
			t.Errorf("%s indented = %s, want %s", testCase.Input, indented, testCase.Indented)
		}
	}
}
