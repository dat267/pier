package interactive

import (
	"regexp"
	"testing"

	"github.com/dat267/pier/coding"
)

// scopeOrderANSI strips the theme colouring so the ordering is assertable on
// every host, not only linux (ansiPattern lives in a linux-gated file).
var scopeOrderANSI = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// interactive-mode.ts:1778-1787 sorts a group's paths, its package sources and
// each package's paths with `localeCompare` (pi v1.5.0 f9a26365), not byte
// order. Pier already uses locale-neutral root collation for this question in
// modelselector.go, footer.go and listmodels.go (D257); this call site was
// missed, so mixed-case and punctuated resource names listed in an order the
// reference never produces (D434).
func TestFormatScopeGroupsUsesLocaleOrdering(t *testing.T) {
	InitTheme("dark", false)
	lines := func(paths []string) string {
		items := make([]scopeGroupItem, 0, len(paths))
		for _, p := range paths {
			items = append(items, scopeGroupItem{
				path:       p,
				sourceInfo: coding.SourceInfo{Source: "local", Scope: coding.SourceScopeProject},
			})
		}
		return formatScopeGroups(buildScopeGroups(items),
			func(item scopeGroupItem) string { return item.path },
			func(item scopeGroupItem, _ string) string { return item.path })
	}

	// localeCompare orders case-insensitively and ranks letters before
	// punctuation; byte order puts every uppercase before every lowercase.
	strip := func(s string) string { return scopeOrderANSI.ReplaceAllString(s, "") }

	got := strip(lines([]string{"a/c.md", "a/B.md", "a/b.md"}))
	want := "  project\n    a/b.md\n    a/B.md\n    a/c.md"
	if got != want {
		t.Errorf("scope group order =\n%q\nwant\n%q", got, want)
	}

	got = strip(lines([]string{"Zebra.md", "apple.md", "Banana.md", "cherry.md"}))
	want = "  project\n    apple.md\n    Banana.md\n    cherry.md\n    Zebra.md"
	if got != want {
		t.Errorf("case-folded order =\n%q\nwant\n%q", got, want)
	}
}

// interactive-mode.ts:1888-1893 sorts the collapsed label list with
// localeCompare as well, unless the caller passes {sort:false} (D434).
func TestFormatCompactListUsesLocaleOrdering(t *testing.T) {
	InitTheme("dark", false)
	strip := func(s string) string { return scopeOrderANSI.ReplaceAllString(s, "") }

	got := strip(formatCompactList([]string{"Zebra", "apple", "Banana", "cherry"}))
	if want := "  apple, Banana, cherry, Zebra"; got != want {
		t.Errorf("compact list = %q, want %q", got, want)
	}

	// sort:false keeps the caller's discovery order (context files).
	got = strip(formatCompactListSorted([]string{"z.md", "A.md"}, false))
	if want := "  z.md, A.md"; got != want {
		t.Errorf("unsorted compact list = %q, want %q", got, want)
	}
}
