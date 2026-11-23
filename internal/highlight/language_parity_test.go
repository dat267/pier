package highlight

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252) highlight.js language reference behavior for cases the
// generated language samples and earlier corpora did not reach.
func TestLanguageParityRegressions(t *testing.T) {
	for _, tc := range []struct{ name, language, code, html string }{
		// swift.js PLAIN_KEYWORDS appends "_|0".
		{"swift underscore", "swift", "_", `<span class="hljs-keyword">_</span>`},
		// crmsh.js has no facet/instance-of/name-brace modes.
		{"crmsh property", "crmsh", "property x[a{\n", `<span class="hljs-keyword">property</span><span class="hljs-title"> </span>x[a{` + "\n"},
		{"crmsh attributes", "crmsh", "property stonith-enabled=false*[a{\n",
			`<span class="hljs-keyword">property</span><span class="hljs-title"> </span><span class="hljs-attr">stonith-enabled=</span><span class="hljs-literal">false</span>*[a{` + "\n"},
		// groovy.js and haxe.js both declare `illegal: /<\//`, the close-tag
		// pattern. The port wrote `#|<\//` and `<//`, which compile to a literal
		// `</` followed by `/` and to `<//`, so neither ever matched and a string
		// inside a tag-like span was still colored. An illegal match aborts the
		// whole highlight, so the reference escapes everything.
		{"groovy close tag", "groovy", `<a href="x">y</a>`, `&lt;a href=&quot;x&quot;&gt;y&lt;/a&gt;`},
		{"haxe close tag", "haxe", `<a href="x">y</a>`, `&lt;a href=&quot;x&quot;&gt;y&lt;/a&gt;`},
		// Without the close tag the same text still highlights, so the fix did not
		// disable the string mode.
		{"groovy no close tag", "groovy", `<a b="x">`, `&lt;a b=<span class="hljs-string">&quot;x&quot;</span>&gt;`},
		{"haxe no close tag", "haxe", `<a b="x">`, `&lt;a b=<span class="hljs-string">&quot;x&quot;</span>&gt;`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Highlight(tc.code, tc.language)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.html {
				t.Fatalf("HTML = %q; want %q", got, tc.html)
			}
		})
	}
}

// The reference's keyword guard and JSDoc variable set relevance 0; number
// variants inherit it from the wrapper mode.
func TestJavaScriptRelevanceMatchesUpstream(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{"$ca", 0}, {"var", 1}, {"var x = 1;", 1}, {"1;", 0}, {"for", 0},
	} {
		result, err := Highlight(tc.code, "javascript")
		if err != nil {
			t.Fatal(err)
		}
		if result.Relevance != tc.want {
			t.Errorf("%q relevance = %d; want %d", tc.code, result.Relevance, tc.want)
		}
	}
}

// highlightAuto highlights candidates with ignoreIllegals false. R's illegal
// `->` therefore yields plain text and relevance 0, not a scored candidate.
func TestHighlightAutoDoesNotIgnoreIllegals(t *testing.T) {
	result, name := highlightAuto("1 ->", []string{"r"})
	if name != "" || result.Relevance != 0 {
		t.Fatalf("auto result name=%q relevance=%d; want plaintext", name, result.Relevance)
	}
	if html := RenderHTML(result.Nodes); html != "1 -&gt;" {
		t.Fatalf("HTML = %q; want plain text", html)
	}
}

// pi v1.4.0 syntax-highlight.ts loads highlight.js 10.7.3 languages/basic.js.
// Its illegal string '^\.' evaluates to '^.' in JavaScript: the compiled regex
// rejects any line start not already consumed by a begin rule, not just dots.
func TestBASICIllegalPatternMatchesUpstream(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{"PRINT 1", "PRINT 1"},
		{" 10 PRINT 1", " 10 PRINT 1"},
		{"10 PRINT 1", `<span class="hljs-symbol">10 </span><span class="hljs-keyword">PRINT</span> <span class="hljs-number">1</span>`},
		{".abc", ".abc"},
	} {
		result, err := Highlight(c.input, "basic")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != c.want {
			t.Errorf("BASIC %q = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestJuliaReplHasNoJldoctestAlias pins pi v1.5.2 (b4afdddf) highlight.js 10.7.3
// lib/languages/julia-repl.js: the `aliases: ['jldoctest']` entry sits on the
// `^julia>` mode object, where the compiler ignores it, so the registered
// language has no aliases and `jldoctest` resolves to nothing. The port lifted it
// to the language definition, so pier accepted a name the reference rejects.
func TestJuliaReplHasNoJldoctestAlias(t *testing.T) {
	if !SupportsLanguage("julia-repl") {
		t.Fatal("julia-repl must stay registered")
	}
	if !SupportsLanguage("julia") {
		t.Fatal("julia must stay registered")
	}
	if SupportsLanguage("jldoctest") {
		t.Fatal("jldoctest is not a registered language in the reference")
	}
	if _, err := Highlight("x", "jldoctest"); err == nil {
		t.Fatal("highlighting with an unregistered alias must fail")
	}
}

// pi v1.4.0 syntax-highlight.ts uses highlight.js 10.7.3 languages/ini.js.
// DOTTED_KEY's lookahead is part of the match, so an unmatched bare key is
// still subject to the root illegal rule. Expectations are from that engine.
func TestINIKeyLookaheadPreservesIllegalFallback(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{"x = 1\nbare", "x = 1\nbare"},
		{"bare", "bare"},
		{"owner.name", "owner.name"},
		{"x = 1\nbare = 2", `<span class="hljs-attr">x</span> = <span class="hljs-number">1</span>` + "\n" + `<span class="hljs-attr">bare</span> = <span class="hljs-number">2</span>`},
		{`owner.name = "Tom"`, `<span class="hljs-attr">owner.name</span> = <span class="hljs-string">&quot;Tom&quot;</span>`},
	} {
		result, err := Highlight(c.input, "ini")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != c.want {
			t.Errorf("INI %q = %q, want %q", c.input, got, c.want)
		}
	}
}

// pi v1.4.0 utils/syntax-highlight.ts uses highlight.js 10.7.3 languages/yaml.js.
// Literal HTML expectations were read from that pinned implementation. Its
// three whitespace rules use JS \s and \S, not RE2's ASCII-only classes.
func TestYAMLUsesJavaScriptWhitespace(t *testing.T) {
	for _, item := range []struct{ input, want string }{
		{"foo\u2003bar", "<span class=\"hljs-string\">foo</span>\u2003<span class=\"hljs-string\">bar</span>"},
		{"[foo\u2003bar]", "[<span class=\"hljs-string\">foo</span>\u2003<span class=\"hljs-string\">bar</span>]"},
		{"---\u2003", "<span class=\"hljs-meta\">---\u2003</span>"},
		{"foo\u0085bar", "<span class=\"hljs-string\">foo\u0085bar</span>"},
		{"---\u0085", "<span class=\"hljs-string\">---\u0085</span>"},
	} {
		result, err := Highlight(item.input, "yaml")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != item.want {
			t.Errorf("YAML %q = %q, want %q", item.input, got, item.want)
		}
	}
}

// pi v1.4.0 utils/syntax-highlight.ts delegates supportsLanguage and highlight
// to highlight.js 10.7.3. Its getLanguage lowercases names with JS semantics.
func TestLanguageLookupUsesJSCaseFolding(t *testing.T) {
	for _, item := range []struct {
		name string
		want bool
	}{
		{"Go", true}, {"GO", true}, {"Js", true}, {"TYPESCRIPT", true},
		{"AWK", true}, {"İni", false},
	} {
		if got := SupportsLanguage(item.name); got != item.want {
			t.Errorf("SupportsLanguage(%q) = %v, want %v", item.name, got, item.want)
		}
	}
	result, err := Highlight("package main", "Go")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := RenderHTML(result.Nodes), `<span class="hljs-keyword">package</span> main`; got != want {
		t.Fatalf("mixed-case Go highlight = %q, want %q", got, want)
	}
}

// pi v1.4.0 (4a080252) autohotkey.js title variants use `:(?!=)` and `::(?!=)`.
// JavaScript backtracks to an earlier colon when the last is followed by "=",
// which the manual begins reproduce.
func TestAutoHotkeyTitleBacktracking(t *testing.T) {
	for _, tc := range []struct{ name, code, html string }{
		{"double then equals", "a::=b", `<span class="hljs-title">a:</span>:=b`},
		{"double colon", "a::b", `<span class="hljs-title">a::</span>b`},
		{"assignment", "a:=b", "a:=b"},
		{"single colon", "a:b", `<span class="hljs-title">a:</span>b`},
		{"next line assignment", "x::=1\ny:=2\n",
			`<span class="hljs-title">x:</span>:=<span class="hljs-number">1</span>` + "\ny:=" + `<span class="hljs-number">2</span>` + "\n"},
		{"random differential case", "^!s::*\n-(** 0^!s::=+(b\n",
			`<span class="hljs-title">^!s::</span>*` + "\n" + `<span class="hljs-title">-(** 0^!s:</span>:=+(b` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Highlight(tc.code, "autohotkey")
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.html {
				t.Fatalf("HTML = %q; want %q", got, tc.html)
			}
		})
	}
}

// pi v1.4.0 (4a080252) r.js implements `beforeMatch` through a compiler
// extension: the preceding non-identifier character only starts the number
// mode when a number variant follows. Otherwise the root illegal `->` aborts.
func TestRNumberAndIllegalParity(t *testing.T) {
	for _, tc := range []struct {
		code, html string
		illegal    bool
	}{
		{"->", "-&gt;", true},
		{"[->", "[-&gt;", true},
		{"'x' ->", "&#x27;x&#x27; -&gt;", true},
		{"1", "1", false},
		{" 1", ` <span class="hljs-number">1</span>`, false},
		{"x * 2", `x * <span class="hljs-number">2</span>`, false},
		{"x.1", "x.1", false},
		{"x <- 1", "x &lt;- 1", false},
	} {
		result, err := Highlight(tc.code, "r")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Illegal != tc.illegal {
			t.Errorf("%q: html=%q illegal=%v; want %q %v", tc.code, got, result.Illegal, tc.html, tc.illegal)
		}
	}
}

// pi v1.4.0 syntax-highlight.ts loads highlight.js 10.7.3 languages/stylus.js.
// LOOKAHEAD_TAG_END requires a following punctuation/JS-whitespace character;
// EOF is not a match. Literal expectations were checked against that engine.
func TestStylusSelectorLookaheadMatchesUpstream(t *testing.T) {
	for _, c := range []struct{ input, want string }{
		{".card", ".card"},
		{"#main", "#main"},
		{"a", "a"},
		{"&:hover", "&amp;:hover"},
		{".card ", `<span class="hljs-selector-class">.card</span> `},
		{".card,", `<span class="hljs-selector-class">.card</span>,`},
		{".card\n", `<span class="hljs-selector-class">.card</span>` + "\n"},
		{".card\u00A0", `<span class="hljs-selector-class">.card</span>` + "\u00A0"},
		{".card\u2028", `<span class="hljs-selector-class">.card</span>` + "\u2028"},
		{".card\uFEFF", `<span class="hljs-selector-class">.card</span>` + "\uFEFF"},
		{".card\u0085", ".card\u0085"},
	} {
		result, err := Highlight(c.input, "stylus")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != c.want {
			t.Errorf("Stylus %q = %q, want %q", c.input, got, c.want)
		}
	}
}

// pi v1.4.0 utils/syntax-highlight.ts loads highlight.js 10.7.3, whose
// languages/json.js uses JavaScript /\S/ for illegal text. Expectations were
// checked against that pinned implementation, including non-whitespace controls.
func TestJSONUsesJavaScriptWhitespace(t *testing.T) {
	for _, space := range []string{" ", "\n", "\v", "\u00A0", "\u2028", "\u2029", "\uFEFF"} {
		for _, c := range []struct{ input, want string }{
			{space + `{"n":1}`, space + `{<span class="hljs-attr">&quot;n&quot;</span>:<span class="hljs-number">1</span>}`},
			{"{" + space + `"n":1}`, "{" + space + `<span class="hljs-attr">&quot;n&quot;</span>:<span class="hljs-number">1</span>}`},
			{"[" + space + "1]", "[" + space + `<span class="hljs-number">1</span>]`},
		} {
			result, err := Highlight(c.input, "json")
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != c.want {
				t.Errorf("JSON %q = %q, want %q", c.input, got, c.want)
			}
		}
	}
	for _, nonSpace := range []string{"\u0085", "\u200B"} {
		input := nonSpace + `{"n":1}`
		result, err := Highlight(input, "json")
		if err != nil {
			t.Fatal(err)
		}
		want := nonSpace + `{&quot;n&quot;:1}`
		if got := RenderHTML(result.Nodes); got != want {
			t.Errorf("JSON %q = %q, want %q", input, got, want)
		}
	}
}

// Reference: highlight.js 10.7.3 (pinned checkout), languages/reasonml.js.
// PARAMS_CONTENTS is built, referenced by PARAMS_MODE's typing mode, and the
// module-access mode is pushed into it only afterwards. A JavaScript array is a
// reference, so the typing mode sees that entry; a Go slice header copied into
// the literal does not, so the port has to install the appended slice.
// Without this the compiled typing scope differs from the reference wherever a
// module access appears inside a type annotation.
func TestReasonMLTypingContentsIncludeModuleAccess(t *testing.T) {
	compiled, err := getCompiledLanguage("reasonml")
	if err != nil {
		t.Fatal(err)
	}
	var typing *compiledMode
	var walk func(modes []*compiledMode)
	walk = func(modes []*compiledMode) {
		for _, mode := range modes {
			if mode == nil || typing != nil {
				continue
			}
			if mode.className == "typing" {
				typing = mode
				return
			}
			walk(mode.contains)
			if mode.starts != nil {
				walk([]*compiledMode{mode.starts})
			}
		}
	}
	walk(compiled.root.contains)
	if typing == nil {
		t.Fatal("reasonml typing scope not found")
	}
	classes := map[string]bool{}
	for _, mode := range typing.contains {
		classes[mode.className] = true
	}
	if !classes["module-access"] {
		t.Fatalf("typing contents = %v, want the module-access mode appended after construction", classes)
	}
	if !classes["module"] {
		t.Fatalf("typing contents = %v, want the original module mode retained", classes)
	}
}

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

// pi v1.4.0 (4a080252), highlight.js lib/core.js: a mode contributes its
// `relevance` only when it closes, `compileRelevance` defaults an undeclared
// relevance to 1, and variant expansion inherits the wrapper's relevance.
// CR-167 fixed modes whose declared 0 (or default 1) the port had inverted, so
// highlightAuto scored languages differently from the reference. The corpus is
// generated by executing the pinned engine; see testdata/relevance/generate.mjs.
func TestRelevanceMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/relevance/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name            string
		Language        string
		Code            string
		HTML            string
		Illegal         bool
		Relevance       int
		Auto            bool
		Second          *string
		SecondRelevance *int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty relevance corpus")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			if c.Auto {
				// core.js highlightAuto: relevance first, then the base language,
				// then registration order.
				result, name := highlightAuto(c.Code, nil)
				if name != c.Language || result.Relevance != c.Relevance {
					t.Fatalf("auto = %s %d; want %s %d", name, result.Relevance, c.Language, c.Relevance)
				}
				if got := RenderHTML(result.Nodes); got != c.HTML {
					t.Fatalf("auto HTML = %q; want %q", got, c.HTML)
				}
				return
			}
			result, err := Highlight(c.Code, c.Language)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != c.HTML {
				t.Fatalf("HTML = %q; want %q", got, c.HTML)
			}
			if result.Illegal != c.Illegal {
				t.Fatalf("illegal = %v; want %v", result.Illegal, c.Illegal)
			}
			if result.Relevance != c.Relevance {
				t.Fatalf("relevance = %d; want %d", result.Relevance, c.Relevance)
			}
		})
	}
}

// Reference: highlight.js 11 `languages/latex.js` VERBATIM_DELIMITED_EQUAL:
//
//	hljs.END_SAME_AS_BEGIN({ className: innerName, begin: /(.|\r?\n)/,
//	  end: /(.|\r?\n)/, excludeBegin: true, excludeEnd: true, endsParent: true })
//
// The delimited text is the string; the delimiter characters are plain. pier
// expanded the same mode by hand and emitted an empty string node at the
// delimiter, then swallowed the rest of the input into it, so `\verb|a b c|`
// rendered `|\x3cspan class="hljs-string">\x3c/span>a b c|` instead of
// `|<span class="hljs-string">a b c</span>|`.
func TestLatexVerbatimDelimitsTheString(t *testing.T) {
	for _, tc := range []struct{ name, code, html string }{
		{"verb", "\\verb|a b c|\n",
			`<span class="hljs-keyword">\verb</span>|<span class="hljs-string">a b c</span>|` + "\n"},
		{"verb one char", "\\verb|a|\n",
			`<span class="hljs-keyword">\verb</span>|<span class="hljs-string">a</span>|` + "\n"},
		{"lstinline", "\\lstinline|a b|\n",
			`<span class="hljs-keyword">\lstinline</span>|<span class="hljs-string">a b</span>|` + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Highlight(tc.code, "latex")
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.html {
				t.Fatalf("HTML = %q; want %q", got, tc.html)
			}
		})
	}
}

// highlight.js 10.7.3 (pi v1.6.0, faac5858) declares `illegal: /#(?!!)/` in
// languages/arcade.js and `/#(?![$_A-z])/` in javascript.js and typescript.js.
// The lookahead does not consume the next character, so with ignoreIllegals
// (the interactive theme's setting) `#"s"` still highlights its string, and
// `A-z` also spans `[\]^_` and the backtick. Without ignoreIllegals the stray
// `#` makes the whole input plain text. The expected HTML is the reference's.
func TestHashIllegalLookaheadMatchesReference(t *testing.T) {
	for _, tc := range []struct {
		code, language  string
		ignore, illegal bool
		html            string
	}{
		{"// c\n/* c */\n# h\n", "arcade", false, true, "// c\n/* c */\n# h\n"},
		{"// c\n#!x\n", "arcade", false, false, "<span class=\"hljs-comment\">// c</span>\n#!x\n"},
		{"#\"s\"", "arcade", true, false, "#<span class=\"hljs-string\">&quot;s&quot;</span>"},
		{"#\"s\"", "javascript", true, false, "#<span class=\"hljs-string\">&quot;s&quot;</span>"},
		{"#`t`", "typescript", true, false, "#<span class=\"hljs-string\">`t`</span>"},
		{"this.#x = 1", "javascript", false, false, "<span class=\"hljs-built_in\">this</span>.#x = <span class=\"hljs-number\">1</span>"},
		{"a[#]", "javascript", true, false, "a[#]"},
		{"#", "javascript", true, false, "#"},
		{"#\"s\"", "javascript", false, true, "#&quot;s&quot;"},
	} {
		result, err := HighlightWith(tc.code, tc.language, tc.ignore)
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); result.Illegal != tc.illegal || got != tc.html {
			t.Errorf("%s %q ignore=%v: illegal=%v HTML %q; want illegal=%v %q", tc.language, tc.code, tc.ignore, result.Illegal, got, tc.illegal, tc.html)
		}
	}
}

// core.js REGEXP_MODE (highlight.js 10.7.3, pi v1.6.0, faac5858) wraps the
// `regexp` rule in a zero-width outer rule with no end. After the inner rule
// closes, a `/` at the same index beats that outer end, so a second regexp
// opens without the outer lookahead: `*/9//` ends in an unterminated regexp.
// The expected HTML and relevance are the reference's.
func TestRegexpModeReopensAfterAClosingSlash(t *testing.T) {
	for _, tc := range []struct {
		code      string
		relevance int
		html      string
	}{
		{"*/9//", 1, "*<span class=\"hljs-regexp\">/9/</span><span class=\"hljs-regexp\">/</span>"},
		{"</t//", 1, "&lt;<span class=\"hljs-regexp\">/t/</span><span class=\"hljs-regexp\">/</span>"},
		{"x = /a/ / 2", 2, "x = <span class=\"hljs-regexp\">/a/</span> / <span class=\"hljs-number\">2</span>"},
		{"/a//b/", 1, "/a<span class=\"hljs-comment\">//b/</span>"},
	} {
		result, err := HighlightWith(tc.code, "javascript", true)
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != tc.relevance {
			t.Errorf("%q: relevance %d HTML %q; want %d %q", tc.code, result.Relevance, got, tc.relevance, tc.html)
		}
	}
}

// languages/ruby.js (highlight.js 10.7.3, pi v1.6.0, faac5858) opens the
// superclass with the JS string '<\\s*', the regex `<\s*`; its identifier rule
// has no class, so `Bar` in `class Foo < Bar` stays plain. The port had the
// regex `<\\s*` (a literal backslash), never opened the mode, and titled the
// superclass. The expected HTML and relevance are the reference's.
func TestRubySuperclassIsNotATitle(t *testing.T) {
	for _, tc := range []struct {
		code      string
		relevance int
		html      string
	}{
		{"class Foo < Bar\nend", 3, "<span class=\"hljs-class\"><span class=\"hljs-keyword\">class</span> <span class=\"hljs-title\">Foo</span> &lt; Bar</span>\n<span class=\"hljs-keyword\">end</span>"},
		{"class A < B::C; end", 3, "<span class=\"hljs-class\"><span class=\"hljs-keyword\">class</span> <span class=\"hljs-title\">A</span> &lt; B::C;</span> <span class=\"hljs-keyword\">end</span>"},
		{"class<t", 2, "<span class=\"hljs-class\"><span class=\"hljs-keyword\">class</span>&lt;t</span>"},
	} {
		result, err := Highlight(tc.code, "ruby")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != tc.relevance {
			t.Errorf("%q: relevance %d HTML %q; want %d %q", tc.code, result.Relevance, got, tc.relevance, tc.html)
		}
	}
}

// languages/handlebars.js (highlight.js 10.7.3, pi v1.6.0, faac5858) uses
// hljs.NUMBER_MODE (`\b\d+(\.\d+)?`) for helper and hash parameters, not the
// C number mode, so `0x1F` highlights only its `0` and `2.5e3` only `2.5`.
func TestHandlebarsParametersUseThePlainNumberMode(t *testing.T) {
	for _, tc := range []struct{ code, html string }{
		{"{{foo 0x1F 2.5e3}}", "<span class=\"hljs-template-variable\">{{<span class=\"hljs-name\">foo</span> <span class=\"hljs-number\">0</span>x1F <span class=\"hljs-number\">2.5</span>e3}}</span>"},
		{"{{x a=1.5}}", "<span class=\"hljs-template-variable\">{{<span class=\"hljs-name\">x</span> <span class=\"hljs-attr\">a</span>=<span class=\"hljs-number\">1.5</span>}}</span>"},
	} {
		result, err := Highlight(tc.code, "handlebars")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != 5 {
			t.Errorf("%q: relevance %d HTML %q; want 5 %q", tc.code, result.Relevance, got, tc.html)
		}
	}
}

// languages/ruby.js (highlight.js 10.7.3, pi v1.6.0, faac5858) builds the
// class and function modes with `.concat(COMMENT_MODES)` before
// `COMMENT_MODES.unshift(IRB_OBJECT)`, so those modes hold no IRB object rule
// and `#<x>` after `class` is a comment. The expected HTML is the reference's.
func TestRubyClassHeaderHasNoIRBObject(t *testing.T) {
	for _, tc := range []struct {
		code      string
		relevance int
		html      string
	}{
		{"class#<x>", 2, "<span class=\"hljs-class\"><span class=\"hljs-keyword\">class</span><span class=\"hljs-comment\">#&lt;x&gt;</span></span>"},
		{"def f(a) #<x>\nend", 4, "<span class=\"hljs-function\"><span class=\"hljs-keyword\">def</span> <span class=\"hljs-title\">f</span><span class=\"hljs-params\">(a)</span></span> #&lt;x&gt;\n<span class=\"hljs-keyword\">end</span>"},
	} {
		result, err := Highlight(tc.code, "ruby")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != tc.relevance {
			t.Errorf("%q: relevance %d HTML %q; want %d %q", tc.code, result.Relevance, got, tc.relevance, tc.html)
		}
	}
}
