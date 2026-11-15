package highlight

import (
	"slices"
	"sort"
	"testing"
)

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
