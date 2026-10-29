package coding

import "testing"

// Reference: pi v1.4.0 utils/frontmatter.ts parses quoted YAML scalars.
func TestFrontmatterQuotedScalarEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`'Don''t repeat'`, "Don't repeat"},
		{`"Line\nBreak"`, "Line\nBreak"},
		{`"Use \"quoted\" text"`, `Use "quoted" text`},
		{`"\0\a\b\t\v\f\r\e\ \/\\\N\_\L\P\x41\u4f60\U0001F600"`, "\x00\a\b\t\v\f\r\x1b /\\\u0085\u00a0\u2028\u2029A你😀"},
		{`"\uD83D\uDE00"`, "😀"},
	} {
		got := ParseFrontmatter("---\ndescription: " + tc.raw + "\n---\nbody")
		if got.Frontmatter["description"] != tc.want {
			t.Errorf("%s: %q; want %q", tc.raw, got.Frontmatter["description"], tc.want)
		}
	}
}

// pi v1.4.0 utils/frontmatter.ts uses the YAML core schema to distinguish
// numeric scalars from strings; Go's strconv grammar is not identical.
func TestFrontmatterPlainScalarTypesMatchYamlCore(t *testing.T) {
	for _, tc := range []struct {
		value     string
		nonString bool
	}{
		{value: "1_000", nonString: false},
		{value: "+0xFF", nonString: false},
		{value: "0xFF", nonString: true},
		{value: "0xFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF", nonString: true},
		{value: "0o777777777777777777777777777777777777777777777777", nonString: true},
		{value: "0b11", nonString: false},
		{value: "0x1p3", nonString: false},
		{value: "1e400", nonString: true},
		{value: "1e-4000", nonString: true},
	} {
		parsed := ParseFrontmatter("---\ndescription: " + tc.value + "\n---\nbody")
		if got := parsed.NonStrings["description"]; got != tc.nonString {
			t.Errorf("%q NonStrings = %t; want %t", tc.value, got, tc.nonString)
		}
	}
}
