package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252) api/openai-responses-shared.ts convertResponsesTools
// uses a flat grammar format, unlike Chat Completions' nested grammar object.
// Regenerate: node --experimental-strip-types testdata/responses-grammar/generate.mjs ../pi > testdata/responses-grammar/golden.json
func TestResponsesGrammarToolsMatchUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/responses-grammar/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Tools    []Tool
		Options  ConvertResponsesToolsOptions
		Expected string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			tools, err := ConvertResponsesTools(fixture.Tools, &fixture.Options)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := MarshalJSON(tools)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fixture.Expected {
				t.Fatalf("tools = %s\nwant    %s", encoded, fixture.Expected)
			}
		})
	}
}
