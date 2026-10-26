package ai

import "testing"

// Reference: pi v1.4.0 (4a080252), packages/ai/src/api/bedrock-converse-stream.ts
// `id.replace(/[\s_.:]+/g, "-")`: JavaScript's \s includes the Unicode space
// separators, so model-match candidates normalize them to "-" as well.
func TestBedrockCandidateSeparatorsUseJavaScriptWhitespace(t *testing.T) {
	for _, separator := range []struct {
		name  string
		value string
	}{
		{"ASCII space", " "},
		{"tab", "\t"},
		{"no-break space", "\u00a0"},
		{"ideographic space", "\u3000"},
		{"underscore", "_"},
		{"dot", "."},
		{"colon", ":"},
	} {
		if got, want := bedrockNormalizeCandidate("a"+separator.value+"b"), "a-b"; got != want {
			t.Errorf("%s: normalized %q, want %q", separator.name, got, want)
		}
	}
	// A non-separator keeps its character.
	if got := bedrockNormalizeCandidate("a-b"); got != "a-b" {
		t.Errorf("normalized %q, want %q", got, "a-b")
	}
}
