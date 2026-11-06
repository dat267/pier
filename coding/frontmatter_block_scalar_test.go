package coding

import (
	"os"
	"path/filepath"
	"testing"
)

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
