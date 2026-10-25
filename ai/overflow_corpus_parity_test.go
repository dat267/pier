package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 utils/overflow.ts isContextOverflow, executed over provider error
// texts, near-misses (case folding, non-ASCII lookalikes, status mentions in
// the body) and the silent/length-stop usage cases. Regenerate:
// node ai/testdata/overflow/generate.mjs ./pi > ai/testdata/overflow/golden.json
func TestContextOverflowMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/overflow/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name         string  `json:"name"`
		Provider     string  `json:"provider"`
		StopReason   string  `json:"stopReason"`
		ErrorMessage *string `json:"errorMessage"`
		Usage        struct {
			Input     int64 `json:"input"`
			Output    int64 `json:"output"`
			CacheRead int64 `json:"cacheRead"`
		} `json:"usage"`
		ContextWindow *int64 `json:"contextWindow"`
		Expected      bool   `json:"expected"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty overflow corpus")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			message := &AssistantMessage{
				Provider: test.Provider, StopReason: test.StopReason, ErrorMessage: test.ErrorMessage,
				Usage: Usage{Input: test.Usage.Input, Output: test.Usage.Output, CacheRead: test.Usage.CacheRead},
			}
			window := int64(0)
			if test.ContextWindow != nil {
				window = *test.ContextWindow
			}
			if got := IsContextOverflow(message, window); got != test.Expected {
				t.Errorf("IsContextOverflow(%q) = %v, want %v", stringOrEmpty(test.ErrorMessage), got, test.Expected)
			}
		})
	}
}

func stringOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
