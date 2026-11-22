package highlight

import (
	"testing"
	"time"
)

// runWithTimeout fails instead of hanging the suite when a parser regresses
// into a non-terminating loop.
func runWithTimeout(t *testing.T, name string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s did not terminate", name)
	}
}

// An unclosed heredoc opener used to loop forever: the closing-name search
// advanced i to len(s) and the loop condition admitted that empty final line,
// so the same no-newline branch ran again unchanged.
func TestRubyHeredocUnclosedTerminates(t *testing.T) {
	runWithTimeout(t, "rubyHeredocBegin", func() {
		if _, ok := rubyHeredocBegin("<<EOF\nhello\n", 0); ok {
			t.Error("unclosed heredoc reported a match")
		}
	})
	runWithTimeout(t, "Highlight", func() {
		if _, err := HighlightWith("<<EOF\nhello\n", "ruby", false); err != nil {
			t.Errorf("HighlightWith: %v", err)
		}
	})
}

// The backreference still matches a closed heredoc, including one whose
// closing name ends the input without a trailing newline.
func TestRubyHeredocClosedStillMatches(t *testing.T) {
	if end, ok := rubyHeredocBegin("<<EOF\nhello\nEOF\n", 0); !ok || end != len("<<EOF\nhello\nEOF") {
		t.Fatalf("closed heredoc = (%d, %v)", end, ok)
	}
	if end, ok := rubyHeredocBegin("<<EOF\nhello\nEOF", 0); !ok || end != len("<<EOF\nhello\nEOF") {
		t.Fatalf("closed heredoc without trailing newline = (%d, %v)", end, ok)
	}
}

// TestRubyVariableFirstAlternativeTakesNoLookahead pins the precedence of the
// reference's variable rule: `(\$\W)|((\$|@@?)(\w+))(?=[^@$?])(?![A-Za-z])(?![@$?'])`.
// Concatenation binds tighter than `|`, so the three lookaheads qualify only the
// second alternative. A `$` followed by a non-word byte matches through the first
// alternative with no lookahead at all, so `$ `, `$$`, `$/` and `$?` colour even
// at the end of the input. The port applied the guard to both alternatives and
// declined all of them.
func TestRubyVariableFirstAlternativeTakesNoLookahead(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{"$ ", `<span class="hljs-variable">$ </span>`},
		{"$$", `<span class="hljs-variable">$$</span>`},
		{"$@", `<span class="hljs-variable">$@</span>`},
		{"$?", `<span class="hljs-variable">$?</span>`},
		{"/$/", `/<span class="hljs-variable">$/</span>`},
		{"a $ b", `a <span class="hljs-variable">$ </span>b`},
		// The second alternative still takes the lookaheads: a name that ends the
		// input stays uncolored (D413), and one followed by a word byte or `$`
		// does not match.
		{"$x", "$x"},
		{"$x$", "$x$"},
		{"$x ", `<span class="hljs-variable">$x</span> `},
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
