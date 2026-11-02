package highlight

import (
	"strings"
	"testing"
)

// pi v1.5.0 highlight.js 10.7.3 leaves rejected `0x` tokens plain and still
// highlights the following literal; a run rejected past the dispatch budget is
// left plain as a responsiveness guard, which is D351's intentional difference.
func TestRejectedCandidateRunStopsAtBudget(t *testing.T) {
	short, err := HighlightWith(`0x "s"`, "javascript", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderHTML(short.Nodes); !strings.Contains(got, `<span class="hljs-string">&quot;s&quot;</span>`) {
		t.Fatalf("short run output = %q; want the string highlighted", got)
	}

	long := strings.Repeat("0x", maxRejectedCandidates) + ` "s"`
	result, err := HighlightWith(long, "javascript", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := RenderHTML(result.Nodes); strings.Contains(got, "<span") {
		t.Fatalf("long run output = %q; want plain text after the budget", got)
	}
}
