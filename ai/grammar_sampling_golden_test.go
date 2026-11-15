package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Replays the grammar-resolution goldens generated from the pinned reference's
// resolveGrammarConstrainedSampling (v1.5.2 api/constrained-sampling.ts): the
// resolved variant, its input property, and every rejection message.
func TestGrammarConstrainedSamplingMatchesThePin(t *testing.T) {
	data, err := os.ReadFile("testdata/grammar-sampling/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Label string `json:"label"`
		Tool  struct {
			Parameters json.RawMessage   `json:"parameters"`
			Variants   map[string]string `json:"variants"`
			ConfigType string            `json:"configType"`
			NoConfig   bool              `json:"noConfig"`
			Supports   bool              `json:"supports"`
		} `json:"tool"`
		Grammar *struct {
			Format        string `json:"format"`
			Definition    string `json:"definition"`
			InputProperty string `json:"inputProperty"`
		} `json:"grammar"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty grammar-sampling golden")
	}
	for _, testCase := range cases {
		tool := Tool{Name: "echo", Parameters: testCase.Tool.Parameters}
		if !testCase.Tool.NoConfig {
			tool.ConstrainedSampling = ConstrainedSamplingValue{
				Set:    true,
				Config: &ConstrainedSamplingConfig{Type: testCase.Tool.ConfigType, Variants: testCase.Tool.Variants},
			}
		}
		grammar, err := ResolveGrammarConstrainedSampling(tool, testCase.Tool.Supports)
		if testCase.Error != "" {
			if err == nil {
				t.Fatalf("case %q: want error %q, got %+v", testCase.Label, testCase.Error, grammar)
			}
			if err.Error() != testCase.Error {
				t.Fatalf("case %q: error = %q, want %q", testCase.Label, err.Error(), testCase.Error)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %q: %v", testCase.Label, err)
		}
		if testCase.Grammar == nil {
			if grammar != nil {
				t.Fatalf("case %q: want no grammar, got %+v", testCase.Label, grammar)
			}
			continue
		}
		if grammar == nil || grammar.Format != testCase.Grammar.Format ||
			grammar.Definition != testCase.Grammar.Definition ||
			grammar.InputProperty != testCase.Grammar.InputProperty {
			t.Fatalf("case %q: grammar = %+v, want %+v", testCase.Label, grammar, testCase.Grammar)
		}
	}
}
