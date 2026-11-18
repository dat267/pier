package coding

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// Reference: pi v1.5.0 (f9a26365), utils/frontmatter.ts parses the frontmatter
// with the `yaml` package, so a skill may describe itself with a block scalar.
// The expectations below come from that package (see
// coding/testdata/frontmatter-block/generate.mjs).
func TestParseFrontmatterBlockScalars(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"literal clip", "---\ndescription: |\n  line one\n  line two\n---\nBody\n", "line one\nline two\n"},
		{"literal strip", "---\ndescription: |-\n  line one\n  line two\n---\nBody\n", "line one\nline two"},
		{"literal keep", "---\ndescription: |+\n  line one\n\n---\nBody\n", "line one\n"},
		{"literal keep two blanks", "---\ndescription: |+\n  line one\n\n\n---\nBody\n", "line one\n\n"},
		{"folded clip", "---\ndescription: >\n  line one\n  line two\n---\nBody\n", "line one line two\n"},
		{"folded strip", "---\ndescription: >-\n  line one\n  line two\n---\nBody\n", "line one line two"},
		{"folded paragraphs", "---\ndescription: >\n  paragraph one\n\n  paragraph two\n---\nBody\n", "paragraph one\nparagraph two\n"},
		{"explicit indent", "---\ndescription: |2\n    four spaces\n---\nBody\n", "  four spaces\n"},
		{"folded more indented", "---\ndescription: >\n  normal\n    indented\n  back\n---\nBody\n", "normal\n  indented\nback\n"},
		{"folded blank then more indented", "---\ndescription: >\n  a\n\n   b\n---\nBody\n", "a\n\n b\n"},
		{"empty block", "---\ndescription: |\nname: n\n---\nBody\n", ""},
		{"block then key", "---\nname: block-skill\ndescription: |\n  multi\n  line\nargument-hint: \"<x>\"\n---\nBody\n", "multi\nline\n"},
		{"trailing spaces", "---\ndescription: |\n  trailing   \n  next\n---\nBody\n", "trailing   \nnext\n"},
		{"blank line inside", "---\ndescription: |\n  a\n\n  b\n---\nBody\n", "a\n\nb\n"},
		{"crlf", "---\ndescription: |\r\n  crlf one\r\n  crlf two\r\n---\r\nBody\r\n", "crlf one\ncrlf two\n"},
		{"comment after indicator", "---\ndescription: | # comment\n  a\n---\nBody\n", "a\n"},
		{"dashes inside content", "---\ndescription: |\n  ---\n  b\n---\nBody\n", "---\nb\n"},
		{"pipe inside a plain value", "---\ndescription: a | b\n---\nBody\n", "a | b"},
		{"quoted pipe", "---\ndescription: \"|\"\n---\nBody\n", "|"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			parsed := ParseFrontmatter(test.input)
			value, ok := parsed.Frontmatter["description"]
			if !ok || value != test.want {
				t.Fatalf("description = %q (present %v), want %q", value, ok, test.want)
			}
			// Every block scalar is a string regardless of its text, so a value
			// like "true" inside a block must not become a boolean.
			if parsed.NonStrings["description"] || parsed.Booleans["description"] {
				t.Errorf("block scalar marked as %+v", parsed)
			}
			if parsed.Body != "Body" {
				t.Errorf("body = %q, want %q", parsed.Body, "Body")
			}
		})
	}
}

// A block scalar whose text happens to be a boolean or number stays a string.
func TestParseFrontmatterBlockScalarKeepsStringTypes(t *testing.T) {
	for _, text := range []string{"true", "false", "42", "null", "~", "1.5"} {
		parsed := ParseFrontmatter("---\nflag: |\n  " + text + "\n---\nBody\n")
		if parsed.NonStrings["flag"] || parsed.Booleans["flag"] {
			t.Errorf("block scalar %q marked as non-string/boolean", text)
		}
		if got := parsed.Frontmatter["flag"]; got != text+"\n" {
			t.Errorf("block scalar %q = %q", text, got)
		}
	}
}

// The upstream fixture that exposed the gap: a multiline description.
func TestLoadSkillWithBlockScalarDescription(t *testing.T) {
	dir := t.TempDir()
	content := "---\nname: multiline-description\ndescription: |\n  This is a multiline description.\n  It spans multiple lines.\n---\n\n# Body\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	result := LoadSkillsFromDir(dir)
	if len(result.Skills) != 1 {
		t.Fatalf("skills = %d, diagnostics %+v", len(result.Skills), result.Diagnostics)
	}
	want := "This is a multiline description.\nIt spans multiple lines.\n"
	if result.Skills[0].Description != want {
		t.Errorf("description = %q, want %q", result.Skills[0].Description, want)
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("diagnostics = %+v", result.Diagnostics)
	}
}

// Reference: pi v1.4.0 utils/frontmatter.ts parses quoted YAML scalars.
func TestFrontmatterQuotedScalarEscapes(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`'Don''t repeat'`, "Don't repeat"},
		{`"Line\nBreak"`, "Line\nBreak"},
		{`"Use \"quoted\" text"`, `Use "quoted" text`},
		{`"\0\a\b\t\v\f\r\e\ \/\\\N\_\L\P\x41\u4f60\U0001F600"`, "\x00\a\b\t\v\f\r\x1b /\\\u0085\u00a0\u2028\u2029A你😀"},
		{`"\uD83D\uDE00"`, "😀"},
		// js-yaml keeps lone surrogate code units; every UTF-8 write replaces
		// them, so the port's decoded value is the replacement character.
		{`"\ud800"`, "\uFFFD"},
		{`"\udc00"`, "\uFFFD"},
		{`"\U0000D800"`, "\uFFFD"},
		{`"\ud800\u0041"`, "\uFFFDA"},
		{`"\ud800x"`, "\uFFFDx"},
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
		{value: "0XFF", nonString: false},
		{value: "0O10", nonString: false},
		{value: "0XFFFFFFFFFFFFFFFFFFFFFFFF", nonString: false},
		{value: "0O777777777777777777777777777777777777777777777777", nonString: false},
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

// YAML folds a plain scalar that continues on more-indented lines with single
// spaces, and utils/frontmatter.ts reads the block with yaml's parse. A
// description wrapped by hand across lines otherwise loses everything after its
// first line, which is what the skills list shows the model.
func TestFrontmatterFoldsAPlainScalarContinuation(t *testing.T) {
	parsed := ParseFrontmatter("---\nname: x\ndescription: multi\n  line plain\n---\nbody")
	if got := parsed.Frontmatter["description"]; got != "multi line plain" {
		t.Fatalf("description = %q, want the folded scalar", got)
	}
	if parsed.NonStrings["description"] {
		t.Error("a folded plain scalar of words is a string")
	}
	if parsed.Body != "body" {
		t.Fatalf("body = %q, want the text after the fence", parsed.Body)
	}

	// The scalar stops at the next key, which is less indented.
	next := ParseFrontmatter("---\ndescription: multi\n  line\nname: x\n---\nbody")
	if got := next.Frontmatter["description"]; got != "multi line" {
		t.Fatalf("description = %q, want the continuation only", got)
	}
	if got := next.Frontmatter["name"]; got != "x" {
		t.Fatalf("name = %q, want the key after the scalar", got)
	}

	// A nested mapping or sequence is not a continuation: the value line is
	// empty, so the more-indented lines belong to the key's own block.
	for _, content := range []string{
		"---\ndescription:\n  nested: value\n---\nbody",
		"---\ndescription:\n  - one\n  - two\n---\nbody",
	} {
		if got := ParseFrontmatter(content).Frontmatter["description"]; got != "" {
			t.Errorf("%q: description = %q, want no scalar", content, got)
		}
	}

	// A URL is not a mapping start, so it still continues a plain scalar.
	link := ParseFrontmatter("---\ndescription: see\n  https://example.com/x\n---\nbody")
	if got := link.Frontmatter["description"]; got != "see https://example.com/x" {
		t.Fatalf("description = %q, want the URL kept in the scalar", got)
	}
}
