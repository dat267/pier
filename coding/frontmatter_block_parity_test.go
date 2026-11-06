package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// Reference: pi v1.5.0 (f9a26365), utils/frontmatter.ts parses the frontmatter
// with the `yaml` package, so literal (`|`) and folded (`>`) block scalars with
// either chomping indicator and an indentation digit are valid skill
// frontmatter. Regenerate the golden with
// `node --experimental-strip-types coding/testdata/frontmatter-block/generate.mjs ./pi`.
// Cases the package rejects are absent: pier's subset parser reports invalid
// YAML differently on purpose (D380).
func TestParseFrontmatterBlockScalarsMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/frontmatter-block/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string            `json:"name"`
		Content string            `json:"content"`
		Strings map[string]string `json:"strings"`
		Body    string            `json:"body"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("block-scalar corpus is empty")
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			parsed := ParseFrontmatter(testCase.Content)
			for key, want := range testCase.Strings {
				if got, ok := parsed.Frontmatter[key]; !ok || got != want {
					t.Errorf("%s = %q, want %q", key, got, want)
				}
				if parsed.NonStrings[key] || parsed.Booleans[key] {
					t.Errorf("%s marked as non-string or boolean", key)
				}
			}
			for key, value := range parsed.Frontmatter {
				if _, ok := testCase.Strings[key]; !ok && !parsed.NonStrings[key] {
					t.Errorf("unexpected string key %q = %q", key, value)
				}
			}
			if parsed.Body != testCase.Body {
				t.Errorf("body = %q, want %q", parsed.Body, testCase.Body)
			}
		})
	}
}
