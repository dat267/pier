package markdown

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Regenerate with the command in testdata/angle-html/generate.mjs. Its parser
// setup is extracted read-only from pi v1.4.0, not re-created in this test.
func TestAngleHTMLReferenceFixtures(t *testing.T) {
	data, err := os.ReadFile("testdata/angle-html/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string `json:"name"`
		Source string `json:"source"`
		Tokens []any  `json:"tokens"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, item := range cases {
		t.Run(item.Name, func(t *testing.T) {
			got := stableValue(projectInlines(ParseInline(item.Source)))
			if want := stableValue(item.Tokens); got != want {
				t.Fatalf("ParseInline(%q)\n got: %s\nwant: %s", item.Source, got, want)
			}
		})
	}
}

// The same reference's autolink rule requires a 2–32-character scheme or a
// dotted email domain; rejecting it must also survive the HTML fallback.
func TestAngleAutolinksRejectInvalidBodies(t *testing.T) {
	for _, src := range []string{"<a:b>", "<a@b>", "<x:y z>"} {
		want := []Inline{{Kind: InlineText, Text: src}}
		if got := ParseInline(src); !reflect.DeepEqual(got, want) {
			t.Errorf("ParseInline(%q) = %+v, want %+v", src, got, want)
		}
	}
}

// pi v1.4.0 packages/tui/src/components/markdown.ts uses Marked 18.0.11's tag
// rule. A slash/colon in an unquoted attribute name does not make an HTML tag;
// normal inline lexing continues and can recognize a bare URL instead.
func TestInlineHTMLRejectsMalformedAttributes(t *testing.T) {
	want := []Inline{
		{Kind: InlineText, Text: "<see "},
		{Kind: InlineLink, Text: "HTTP://Example.COM>", Dest: "HTTP://Example.COM>", Children: []Inline{{Kind: InlineText, Text: "HTTP://Example.COM>"}}},
	}
	if got := ParseInline("<see HTTP://Example.COM>"); !reflect.DeepEqual(got, want) {
		t.Fatalf("inlines = %+v, want %+v", got, want)
	}
}
