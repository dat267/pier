package highlight

import (
	"slices"
	"sort"
	"testing"
)

func TestSupportsLanguage(t *testing.T) {
	for _, name := range []string{"go", "golang", "typescript", "ts", "xml"} {
		if !SupportsLanguage(name) {
			t.Errorf("SupportsLanguage(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"", "not-a-real-language"} {
		if SupportsLanguage(name) {
			t.Errorf("SupportsLanguage(%q) = true, want false", name)
		}
	}
}

// pi v1.5.2 core.js listLanguages returns the registered canonical names; the
// alias table is separate, so an alias such as "js" or "py" is not a language.
// The port listed every registry key, aliases included: 364 entries for 191
// languages.
func TestLanguagesListsCanonicalNamesOnly(t *testing.T) {
	registryMu.RLock()
	want := make([]string, 0, len(registry))
	for name, language := range registry {
		if language.Name == name {
			want = append(want, name)
		}
	}
	registryMu.RUnlock()
	sort.Strings(want)
	if len(want) == 0 {
		t.Fatal("no canonical languages registered")
	}

	got := Languages()
	if !slices.Equal(got, want) {
		extra := make([]string, 0)
		missing := make([]string, 0)
		for _, name := range got {
			if !slices.Contains(want, name) {
				extra = append(extra, name)
			}
		}
		for _, name := range want {
			if !slices.Contains(got, name) {
				missing = append(missing, name)
			}
		}
		t.Fatalf("languages = %d entries; %d aliases listed (%v), %d canonical names missing (%v)",
			len(got), len(extra), firstN(extra, 5), len(missing), firstN(missing, 5))
	}
	for _, alias := range []string{"js", "jsx", "py", "sh", "rb", "ts"} {
		if slices.Contains(got, alias) {
			t.Fatalf("alias %q listed as a language", alias)
		}
	}
}

func firstN(values []string, count int) []string {
	if len(values) <= count {
		return values
	}
	return values[:count]
}

// D413: nginx's third variable variant never matches in highlight.js 10.7.3
// because the reference composes it from a RegExp and a string, so it adds no
// relevance to the language's score. The port's working pattern colors the
// variable, and it must not also score for it: a 444-document auto-detection
// differential over all registered languages picked nginx for perl, bash and
// processing snippets until the variant's relevance was pinned to zero.
func TestNginxVariableVariantScoresNothing(t *testing.T) {
	// "my $x = 1;" is perl; the reference's highlightAuto picks apache at
	// relevance 2 and never nginx.
	result, name := highlightAuto("my $x = 1;\nfd 3", nil)
	if name == "nginx" {
		t.Fatalf("auto-detection picked %q at relevance %d for a perl snippet", name, result.Relevance)
	}
	// The variable is still colored by an explicit nginx highlight.
	ngx, err := Highlight("set $x 1;", "nginx")
	if err != nil {
		t.Fatal(err)
	}
	want := `<span class="hljs-attribute">set</span> <span class="hljs-variable">$x</span> <span class="hljs-number">1</span>;`
	if got := RenderHTML(ngx.Nodes); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}
