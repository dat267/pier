package ai

import "testing"

// TestMapThinkingLevelToEffort covers the default mapping and the model's
// override table (anthropic_stream.ts mapThinkingLevelToEffort).
func TestMapThinkingLevelToEffort(t *testing.T) {
	model := &Model{}
	cases := []struct {
		level ThinkingLevel
		want  AnthropicEffort
	}{
		{ThinkMinimal, AnthropicEffortLow},
		{ThinkLow, AnthropicEffortLow},
		{ThinkMedium, AnthropicEffortMedium},
		{ThinkHigh, AnthropicEffortHigh},
		{ThinkXHigh, AnthropicEffortHigh},
		{ThinkMax, AnthropicEffortHigh},
		{"", AnthropicEffortHigh},
	}
	for _, test := range cases {
		if got := mapThinkingLevelToEffort(model, test.level); got != test.want {
			t.Fatalf("level %q = %q, want %q", test.level, got, test.want)
		}
	}
	// A non-empty entry in the model's map wins over the default.
	max := "max"
	model.ThinkingLevelMap = ThinkingLevelMap{ThinkHigh: &max}
	if got := mapThinkingLevelToEffort(model, ThinkHigh); got != AnthropicEffortMax {
		t.Fatalf("mapped level = %q, want max", got)
	}
	// An empty mapped value falls back to the default.
	empty := ""
	model.ThinkingLevelMap = ThinkingLevelMap{ThinkHigh: &empty}
	if got := mapThinkingLevelToEffort(model, ThinkHigh); got != AnthropicEffortHigh {
		t.Fatalf("empty mapped level = %q, want high", got)
	}
}

// TestIsAnthropicEffort covers the effort vocabulary (anthropic-wire.ts
// isAnthropicEffort).
func TestIsAnthropicEffort(t *testing.T) {
	for _, value := range []string{"low", "medium", "high", "xhigh", "max"} {
		if !isAnthropicEffort(value) {
			t.Fatalf("%q must be a valid effort", value)
		}
	}
	for _, value := range []string{"", "minimal", "off", "LOW"} {
		if isAnthropicEffort(value) {
			t.Fatalf("%q must not be a valid effort", value)
		}
	}
}
