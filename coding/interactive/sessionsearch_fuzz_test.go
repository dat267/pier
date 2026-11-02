package interactive

// The /resume search parser takes arbitrary user input: quotes, regex syntax
// and bare tokens must all parse into the two token kinds, never panic.

import "testing"

func FuzzParseSearchQuery(f *testing.F) {
	for _, seed := range []string{"node", `"node cve"`, "re:[", "re:", "a b", `""`, "  ", "re:n[o]de", "\"unclosed"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, query string) {
		parsed := ParseSearchQuery(query)
		for _, token := range parsed.Tokens {
			if token.Kind != "phrase" && token.Kind != "fuzzy" {
				t.Fatalf("unexpected token kind %q", token.Kind)
			}
		}
	})
}
