package coding

import "testing"

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
