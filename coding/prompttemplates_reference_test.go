package coding

// Replays the upstream-generated prompt-template goldens. The generator
// (coding/testdata/prompt-args/generate.mjs) runs the pin's substituteArgs and
// parseCommandArgs over a hand-written grammar corpus plus a seeded random one;
// regenerate with that file's header command.

import (
	"encoding/json"
	"os"
	"testing"
)

type promptArgsGolden struct {
	Substitutions []struct {
		Template string   `json:"template"`
		Args     []string `json:"args"`
		Result   string   `json:"result"`
	} `json:"substitutions"`
	ArgumentParsing []struct {
		Input  string   `json:"input"`
		Result []string `json:"result"`
	} `json:"argumentParsing"`
}

func TestPromptTemplateArgsMatchTheReference(t *testing.T) {
	data, err := os.ReadFile("testdata/prompt-args/golden.json")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden promptArgsGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatalf("decode golden: %v", err)
	}
	if len(golden.Substitutions) == 0 || len(golden.ArgumentParsing) == 0 {
		t.Fatalf("golden is empty: %d substitutions, %d parses", len(golden.Substitutions), len(golden.ArgumentParsing))
	}
	for _, testCase := range golden.Substitutions {
		if got := SubstituteArgs(testCase.Template, testCase.Args); got != testCase.Result {
			t.Errorf("SubstituteArgs(%q, %q) = %q, reference gives %q", testCase.Template, testCase.Args, got, testCase.Result)
		}
	}
	for _, testCase := range golden.ArgumentParsing {
		// The reference returns [] where pier returns nil for no arguments; the
		// slice's contents are what callers use.
		got := ParseCommandArgs(testCase.Input)
		if len(got) != len(testCase.Result) {
			t.Errorf("ParseCommandArgs(%q) = %q, reference gives %q", testCase.Input, got, testCase.Result)
			continue
		}
		for index := range got {
			if got[index] != testCase.Result[index] {
				t.Errorf("ParseCommandArgs(%q) = %q, reference gives %q", testCase.Input, got, testCase.Result)
				break
			}
		}
	}
}
