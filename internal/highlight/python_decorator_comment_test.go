package highlight

import "testing"

// pi v1.5.0 (f9a26365) renders highlight.js 10.7.3, whose python grammar ends
// the decorator mode at `end: /(?=#)|$/` (languages/python.js). The first
// alternative is a zero-width end before `#`, so the meta span stops there and
// the comment rule that follows colors the trailing comment:
//
//	<div class="hljs-meta">@decorator  </div><div class="hljs-comment"># comment</div>
//
// RE2 has no lookahead, so the port ended the mode only at the line end and the
// trailing comment stayed inside the meta span.
func TestPythonDecoratorEndsBeforeTrailingComment(t *testing.T) {
	for _, test := range []struct{ code, want string }{
		{
			code: "@decorator  # comment",
			want: `<span class="hljs-meta">@decorator  </span><span class="hljs-comment"># comment</span>`,
		},
		{
			code: "@property  # setter",
			want: `<span class="hljs-meta">@property  </span><span class="hljs-comment"># setter</span>`,
		},
		{
			code: "  @staticmethod # x",
			want: `<span class="hljs-meta">  @staticmethod </span><span class="hljs-comment"># x</span>`,
		},
		{
			code: `@app.route("/")  # noqa`,
			want: `<span class="hljs-meta">@app.route(<span class="hljs-params"><span class="hljs-string">&quot;/&quot;</span></span>)  </span><span class="hljs-comment"># noqa</span>`,
		},
		// Controls: no comment, and the line after the decorator.
		{code: "@decorator", want: `<span class="hljs-meta">@decorator</span>`},
		{
			code: "@decorator\n# comment",
			want: `<span class="hljs-meta">@decorator</span>` + "\n" + `<span class="hljs-comment"># comment</span>`,
		},
	} {
		result, err := Highlight(test.code, "python")
		if err != nil {
			t.Fatalf("%q: %v", test.code, err)
		}
		if got := RenderHTML(result.Nodes); got != test.want {
			t.Errorf("%q:\n got %s\nwant %s", test.code, got, test.want)
		}
	}
}
