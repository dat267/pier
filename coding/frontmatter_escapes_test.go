package coding

import "testing"

// Reference: pi v1.4.0 utils/frontmatter.ts parses quoted YAML scalars.
func TestFrontmatterQuotedScalarEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`'Don''t repeat'`, "Don't repeat"},
		{`"Line\nBreak"`, "Line\nBreak"},
		{`"Use \"quoted\" text"`, `Use "quoted" text`},
		{`"\0\a\b\t\v\f\r\e\ \/\\\N\_\L\P\x41\u4f60\U0001F600"`, "\x00\a\b\t\v\f\r\x1b /\\\u0085\u00a0\u2028\u2029A你😀"},
	} {
		got := ParseFrontmatter("---\ndescription: " + tc.raw + "\n---\nbody")
		if got.Frontmatter["description"] != tc.want {
			t.Errorf("%s: %q; want %q", tc.raw, got.Frontmatter["description"], tc.want)
		}
	}
}
