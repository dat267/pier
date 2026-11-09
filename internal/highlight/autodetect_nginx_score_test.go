package highlight

import "testing"

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
