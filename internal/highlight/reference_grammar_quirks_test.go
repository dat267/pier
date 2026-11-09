package highlight

import "testing"

// D413: two reference grammars stop matching where the port keeps going, and
// the port keeps its own output. pi v1.5.0 (f9a26365) renders highlight.js
// 10.7.3; see DECISIONS.md for both citations.

// languages/nginx.js builds its third variable variant as
// `begin: /[$@]/ + hljs.UNDERSCORE_IDENT_RE`. A RegExp plus a string is string
// concatenation in JavaScript, so the reference compiles the never-matching
// `/[$@]/[a-zA-Z_]\w*` and colors no nginx variable at all. The port uses the
// pattern the grammar meant to write.
func TestNginxVariablesAreHighlighted(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{"echo $HOME", `<span class="hljs-attribute">echo</span> <span class="hljs-variable">$HOME</span>`},
		{"set $x 1;", `<span class="hljs-attribute">set</span> <span class="hljs-variable">$x</span> <span class="hljs-number">1</span>;`},
	} {
		result, err := Highlight(test.code, "nginx")
		if err != nil {
			t.Fatalf("%q: %v", test.code, err)
		}
		if got := RenderHTML(result.Nodes); got != test.want {
			t.Errorf("%q:\n got %s\nwant %s", test.code, got, test.want)
		}
	}
}

// languages/ruby.js ends its variable rule with `(?=[^@$?])(?![A-Za-z])(?![@$?'])`.
// The regex engine backtracks `(\w+)` to satisfy them, so a variable that ends
// the input loses its last word character to the reference's lookahead:
// `@a42` renders `@a4` plus a plain `2`. RE2 has no lookahead, so the port
// checks the following character in a before-begin guard and declines the
// match, leaving the text uncolored. Both differ from the intended rule; the
// port does not color a truncated name. A variable followed by anything else
// matches in both.
func TestRubyVariableAtEndOfInputIsNotPartial(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{"x = @decorator42", "x = @decorator42"},
		{"@a42", "@a42"},
		{"a = @a42;", `a = <span class="hljs-variable">@a42</span>;`},
		{"def f; @a42; end", `<span class="hljs-function"><span class="hljs-keyword">def</span> <span class="hljs-title">f</span>;</span> <span class="hljs-variable">@a42</span>; <span class="hljs-keyword">end</span>`},
	} {
		result, err := Highlight(test.code, "ruby")
		if err != nil {
			t.Fatalf("%q: %v", test.code, err)
		}
		if got := RenderHTML(result.Nodes); got != test.want {
			t.Errorf("%q:\n got %s\nwant %s", test.code, got, test.want)
		}
	}
}
