package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), utils/overflow.ts `isContextOverflow`, with
// every expectation produced by running it. The corpus covers each provider
// error string the reference documents, the non-overflow messages that must win
// over them, case and Unicode lookalike variants of a few strings (the port
// emulates the reference's case-insensitive matching over ASCII letters), and
// the two silent-overflow signals: usage past the context window, and a length
// stop with no output filling it.
//
// Regenerate: node --experimental-strip-types testdata/overflow-classify/generate.mjs ../pi > testdata/overflow-classify/golden.json
func TestOverflowClassificationMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/overflow-classify/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Provider      string `json:"provider"`
		StopReason    string `json:"stopReason"`
		ErrorMessage  string `json:"errorMessage"`
		ContextWindow int64  `json:"contextWindow"`
		Want          bool   `json:"want"`
		Usage         struct {
			Input     int64 `json:"input"`
			Output    int64 `json:"output"`
			CacheRead int64 `json:"cacheRead"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 200 {
		t.Fatalf("fixture has %d cases, expected the full corpus", len(cases))
	}
	for _, testCase := range cases {
		stopReason := StopStop
		switch testCase.StopReason {
		case "error":
			stopReason = StopError
		case "length":
			stopReason = StopLength
		}
		message := &AssistantMessage{
			Provider: testCase.Provider, Model: "m", StopReason: stopReason,
			Usage: Usage{
				Input: testCase.Usage.Input, Output: testCase.Usage.Output, CacheRead: testCase.Usage.CacheRead,
			},
		}
		if testCase.ErrorMessage != "" {
			text := testCase.ErrorMessage
			message.ErrorMessage = &text
		}
		if got := IsContextOverflow(message, testCase.ContextWindow); got != testCase.Want {
			t.Errorf("%s stop=%s cw=%d %q = %v, want %v",
				testCase.Provider, testCase.StopReason, testCase.ContextWindow, testCase.ErrorMessage, got, testCase.Want)
		}
	}
}
