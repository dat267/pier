package highlight

import (
	"encoding/json"
	"os"
	"strings"
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

// languages/cpp.js (highlight.js 10.7.3, pi v1.6.0, faac5858) begins
// FUNCTION_DISPATCH with `\b(?!decltype)(?!if)(?!for)(?!while)IDENT(?=\s*\()`.
// Each lookahead rejects an identifier that only starts with the word, so
// `format(x)` and `if0(x)` stay plain, and `\s` admits NBSP and \v before `(`.
// The expected HTML is the reference's.
func TestCPPFunctionDispatchLookaheads(t *testing.T) {
	for _, tc := range []struct{ code, html string }{
		{"foo(1)", "<span class=\"hljs-built_in\">foo</span>(<span class=\"hljs-number\">1</span>)"},
		{"format(x)", "format(x)"},
		{"if0(x)", "if0(x)"},
		{"x = forward(y);", "x = forward(y);"},
		{"foo (x)", "<span class=\"hljs-built_in\">foo</span> (x)"},
		{"foo\v(x)", "<span class=\"hljs-built_in\">foo</span>\v(x)"},
	} {
		result, err := Highlight(tc.code, "cpp")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != 0 {
			t.Errorf("%q: relevance %d HTML %q; want 0 %q", tc.code, result.Relevance, got, tc.html)
		}
	}
}

// languages/javascript.js (highlight.js 10.7.3, pi v1.6.0, faac5858) marks a
// JSDoc `@param` name only where `(?=\s*(-)|$)` holds: `\s` is JavaScript's
// whitespace (NBSP included) and the multiline `$` matches before any line
// terminator inside the run, a lone `\r` and U+2028 too. The expected HTML is
// the reference's.
func TestJSDocVariableLookahead(t *testing.T) {
	const open = "<span class=\"hljs-comment\">/** <span class=\"hljs-doctag\">@param "
	const marked = open + "<span class=\"hljs-variable\">x</span></span>"
	for _, tc := range []struct{ code, html string }{
		{"/** @param x\ry */", marked + "\ry */</span>"},
		{"/** @param x - d */", marked + " - d */</span>"},
		{"/** @param x y */", marked + " y */</span>"},
		{"/** @param x y */", open + "</span>x y */</span>"},
		{"/** @param x\r\n * y */", marked + "\r\n * y */</span>"},
	} {
		result, err := Highlight(tc.code, "javascript")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html {
			t.Errorf("%q: HTML %q; want %q", tc.code, got, tc.html)
		}
	}
}

// languages/markdown.js (highlight.js 10.7.3, pi v1.6.0, faac5858) opens the
// setext heading's text line with a zero-width `^`, so the inline rules see
// the whole line: `Title *x*` emphasises `*x*`, and an unclosed `_` or tag
// runs on into the underline. The port consumed the line as the begin
// lexeme and left it plain. The expected HTML and relevance are the reference's.
func TestMarkdownSetextHeadingParsesItsInlines(t *testing.T) {
	for _, tc := range []struct {
		code      string
		relevance int
		html      string
	}{
		{"_\n--", 0, "<span class=\"hljs-section\"><span class=\"hljs-emphasis\">_\n--</span></span>"},
		{"<L\n--", 0, "<span class=\"hljs-section\"><span class=\"xml\"><span class=\"hljs-tag\">&lt;<span class=\"hljs-name\">L</span>\n<span class=\"hljs-attr\">--</span></span></span></span>"},
		{"Title *x*\n===", 4, "<span class=\"hljs-section\">Title <span class=\"hljs-emphasis\">*x*</span>\n===</span>"},
		{"a **b** c\n---\n\ntext", 4, "<span class=\"hljs-section\">a <span class=\"hljs-strong\">**b**</span> c\n---</span>\n\ntext"},
	} {
		result, err := Highlight(tc.code, "markdown")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != tc.relevance {
			t.Errorf("%q: relevance %d HTML %q; want %d %q", tc.code, result.Relevance, got, tc.relevance, tc.html)
		}
	}
}

// TestIniDottedKeyScan pins the hand-written dotted-key scan against the
// reference's lookahead, `([A-Za-z0-9_-]+|"(\\"|[^"])*"|'[^']*')
// (\s*\.\s*...)*` followed by `(?=\s*=\s*[^#\s])` (languages/ini.js in pi
// v1.6.0, faac5858). A key needs the `=` lookahead to become an attribute, and
// JavaScript's `\s` is not ASCII-only: U+00A0 is a member, so a byte-only scan
// loses the attribute.
func TestIniDottedKeyScan(t *testing.T) {
	for _, tc := range []struct {
		code string
		html string
	}{
		{"key = 1", `<span class="hljs-attr">key</span> = <span class="hljs-number">1</span>`},
		// U+00A0 is JavaScript whitespace in the lookahead's `\s*`.
		{"key\u00a0= true", `<span class="hljs-attr">key</span>` + "\u00a0" + `= <span class="hljs-literal">true</span>`},
		// No `=` after the key: the lookahead fails, so the key stays plain.
		{"# not a key", `<span class="hljs-comment"># not a key</span>`},
		// The lookahead needs a non-# character after `=`, so no attribute.
		{"key = #comment", `key = #comment`},
		// A dotted key is one attribute.
		{"a.b = 1", `<span class="hljs-attr">a.b</span> = <span class="hljs-number">1</span>`},
		// A quoted key, and an unterminated one that must not match.
		{`"quoted key" = 1`, `<span class="hljs-attr">&quot;quoted key&quot;</span> = <span class="hljs-number">1</span>`},
		{`"unterminated = 1`, `&quot;unterminated = 1`},
	} {
		result, err := Highlight(tc.code, "ini")
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html {
			t.Errorf("%q: HTML %q; want %q", tc.code, got, tc.html)
		}
	}
}

// TestHTTPGuards pins the hand-written version and request guards to the
// reference's lookaheads, `\AHTTP/(2|1\.[01]) \d{3}` and
// `\A[A-Z]+ .* HTTP/(2|1\.[01])$` (languages/http.js in pi v1.6.0, faac5858).
// Both were `\A`-anchored regexps, which cost a full pass over the remaining
// input per call (119 us on a 24 KiB tail) and run at every candidate.
func TestHTTPGuards(t *testing.T) {
	for _, tc := range []struct {
		code string
		html string
	}{
		{"HTTP/1.1 200 OK\n\nbody", `<span class="hljs-meta">HTTP/1.1</span> <span class="hljs-number">200</span> OK`},
		{"HTTP/2 500 X\n\nb", `<span class="hljs-meta">HTTP/2</span> <span class="hljs-number">500</span> X`},
		{"HTTP/1.0 404 X\n\nb", `<span class="hljs-meta">HTTP/1.0</span> <span class="hljs-number">404</span> X`},
		// Rejected: lower case, a version the guard does not admit, two digits,
		// and a request whose line does not end at the version.
		{"http/1.1 200 OK\n\nb", "http/1.1 200 OK"},
		{"HTTP/1.2 200 OK\n\nb", "HTTP/1.2 200 OK"},
		{"HTTP/1.1 20 OK\n\nb", "HTTP/1.1 20 OK"},
		{"GET /x HTTP/1.1 extra\n\nb", "GET /x HTTP/1.1 extra"},
		// A request whose method is lower case is not a request.
		{"get /x HTTP/1.1\n\nb", "get /x HTTP/1.1"},
	} {
		result, err := Highlight(tc.code, "http")
		if err != nil {
			t.Fatal(err)
		}
		got := RenderHTML(result.Nodes)
		if !strings.HasPrefix(got, tc.html) {
			t.Errorf("%q: HTML %q; want it to start with %q", tc.code, got, tc.html)
		}
	}
}

// TestAnchoredSpaceGuards pins the hand-written guards for mipsasm's `(?!\s*$)`
// and protobuf's `(?=\s*=[^\n]+;$)`, both of which were `\A`-anchored regexps
// with an unbounded `\s*` and cost 120 us per call on a 24 KiB tail
// (languages/mipsasm.js and languages/protobuf.js in pi v1.6.0, faac5858).
// JavaScript's `\s` is not ASCII-only, so a byte-only scan loses U+00A0,
// U+3000 and U+2028 as whitespace.
func TestAnchoredSpaceGuards(t *testing.T) {
	for _, tc := range []struct {
		language string
		code     string
		want     string
	}{
		// `\s*$` matches: a comment of the marker and whitespace only.
		{"mipsasm", "#\n", "#\n"},
		{"mipsasm", "# x\n", `<span class="hljs-comment"># x</span>`},
		{"mipsasm", "#\u00a0\n", "#\u00a0\n"},
		{"mipsasm", "#\u3000\n", "#\u3000\n"},
		{"mipsasm", "#\u2028\n", "#\u2028\n"},
		// `\s*=[^\n]+;$` decides where the enum group ends, so a non-ASCII
		// space before `=` must still be seen as whitespace.
		{"protobuf", "enum E { A = 1; }", `<span class="hljs-class">`},
		{"protobuf", "enum E { A\u00a0= 1; }", `<span class="hljs-class">`},
		{"protobuf", "enum E { A　= 1; }", `<span class="hljs-class">`},
		{"protobuf", "enum E { A = 1\n}", `<span class="hljs-class">`},
	} {
		result, err := Highlight(tc.code, tc.language)
		if err != nil {
			t.Fatalf("%s %q: %v", tc.language, tc.code, err)
		}
		got := RenderHTML(result.Nodes)
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s %q: HTML %q; want it to contain %q", tc.language, tc.code, got, tc.want)
		}
	}
}

// The m flag's `$` is a line end, so mipsasm's `(?!\s*$)` fails when the
// whitespace after the marker holds a line terminator, and protobuf's
// `(?=\s*=[^\n]+;$)` holds at a `;` that ends a line, not only at the end of
// the input. HTML and relevance were read off the pinned highlight.js
// (pi v1.6.0, 10.7.3 languages/mipsasm.js and languages/protobuf.js).
func TestAnchoredSpaceGuardsUseLineEnds(t *testing.T) {
	for _, tc := range []struct {
		language, code, html string
		relevance            int
	}{
		{"mipsasm", "#\r\nx", "#\r\nx", 0},
		{"mipsasm", "#  \nnext", "#  \nnext", 0},
		{"mipsasm", "#\u2028x", "#\u2028x", 0},
		{"mipsasm", "li $t0, 1 ;\n  add", "li $<span class=\"hljs-built_in\">t0</span>, <span class=\"hljs-number\">1</span> ;\n  <span class=\"hljs-keyword\">add</span>", 1},
		{"mipsasm", "# x\n", "<span class=\"hljs-comment\"># x</span>\n", 1},
		{"mipsasm", "#\n", "#\n", 0},
		{"mipsasm", "#  ", "#  ", 0},
		{"protobuf", "enum E {\n  A = 1;\n  B = 2;\n}", "<span class=\"hljs-class\"><span class=\"hljs-keyword\">enum</span> <span class=\"hljs-title\">E</span> </span>{\n  A = <span class=\"hljs-number\">1</span>;\n  B = <span class=\"hljs-number\">2</span>;\n}", 4},
		{"protobuf", "enum E {\n  A = 1; \n}", "<span class=\"hljs-class\"><span class=\"hljs-keyword\">enum</span> <span class=\"hljs-title\">E</span> </span>{\n  A = <span class=\"hljs-number\">1</span>; \n}", 2},
		{"protobuf", "enum E {\n  A =;\n}", "<span class=\"hljs-class\"><span class=\"hljs-keyword\">enum</span> <span class=\"hljs-title\">E</span> </span>{\n  A =;\n}", 2},
		{"protobuf", "enum E {\n  A = 1;\r\n}", "<span class=\"hljs-class\"><span class=\"hljs-keyword\">enum</span> <span class=\"hljs-title\">E</span> </span>{\n  A = <span class=\"hljs-number\">1</span>;\r\n}", 3},
		{"protobuf", "enum E {\n  A = 1;", "<span class=\"hljs-class\"><span class=\"hljs-keyword\">enum</span> <span class=\"hljs-title\">E</span> </span>{\n  A = <span class=\"hljs-number\">1</span>;", 3},
	} {
		result, err := HighlightWith(tc.code, tc.language, true)
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != tc.html || result.Relevance != tc.relevance {
			t.Errorf("%s %q = %q relevance %d; want %q relevance %d", tc.language, tc.code, got, result.Relevance, tc.html, tc.relevance)
		}
	}
}
