package coding

import "testing"

// Skill files come from project directories, so their frontmatter is untrusted
// input to the flat YAML subset parser.
func FuzzParseFrontmatter(f *testing.F) {
	for _, seed := range []string{
		"", "---\nname: x\n---\nbody", "---\n", "---\r\nname: x\r\n---\r\n",
		"---\nkey: \"\\u0041\"\n---", "---\nkey: 'a''b' # comment\n---\nbody",
		"\uFEFF---\nkey: value\n---\nbody", "---\nkey: \"unterminated\n---",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, content string) {
		parsed := ParseFrontmatter(content)
		for key := range parsed.Frontmatter {
			if key == "" {
				t.Fatalf("empty key from %q", content)
			}
		}
	})
}
