package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// The golden file is generated from the pinned reference's Markdown component
// with the tagging theme below:
//
//	node --experimental-strip-types tui/testdata/mdrender/generate.mjs ./pi
//
// It rewrites tui/testdata/md_render_golden.txt in place, and `just goldens`
// runs it, so a case that stops matching the pin fails the gate.

func loadMDRenderGolden(t *testing.T) map[string][]string {
	t.Helper()
	file, err := os.Open("testdata/md_render_golden.txt")
	if err != nil {
		t.Fatalf("open golden: %v", err)
	}
	defer file.Close()
	values := map[string][]string{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " => ", 2)
		if len(parts) != 2 {
			t.Fatalf("bad golden line %q", line)
		}
		label := strings.TrimPrefix(parts[0], "md ")
		var lines []string
		if err := json.Unmarshal([]byte(parts[1]), &lines); err != nil {
			t.Fatalf("bad golden render %q: %v", line, err)
		}
		values[label] = lines
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	return values
}

func mdTag(name string) func(string) string {
	return func(text string) string { return "<" + name + ">" + text + "</" + name + ">" }
}

func mdTestTheme() MarkdownTheme {
	return MarkdownTheme{
		Heading:         mdTag("h"),
		Link:            mdTag("link"),
		LinkURL:         mdTag("url"),
		Code:            mdTag("code"),
		CodeBlock:       mdTag("cb"),
		CodeBlockBorder: mdTag("cbb"),
		Quote:           mdTag("q"),
		QuoteBorder:     mdTag("qb"),
		Hr:              mdTag("hr"),
		ListBullet:      mdTag("lb"),
		Bold:            mdTag("b"),
		Italic:          mdTag("i"),
		Strikethrough:   mdTag("s"),
		Underline:       mdTag("u"),
	}
}

type mdRenderCase struct {
	label        string
	text         string
	widths       []int
	paddingX     int
	paddingY     int
	defaultStyle *DefaultTextStyle
	options      MarkdownOptions
}

// TestMarkdownAgainstUpstreamGolden replays the upstream render corpus.
func TestMarkdownAgainstUpstreamGolden(t *testing.T) {
	golden := loadMDRenderGolden(t)
	theme := mdTestTheme()

	cases := []mdRenderCase{
		{label: "para", text: "Hello world", widths: []int{20, 5}},
		{label: "para-spacing", text: "one\n\ntwo", widths: []int{20}},
		{label: "headings", text: "# H1\n\n## H2\n\n### H3\n\n#### H4", widths: []int{24}},
		{label: "headings-inline", text: "# A **b** and `c`", widths: []int{30}},
		{label: "emphasis", text: "a *em* and **strong** and ~~del~~ and `code`", widths: []int{40}},
		{label: "fence", text: "```js\nconst x = 1;\n```", widths: []int{30}},
		{label: "fence-long", text: "```python\ndef f(x):\n    return x + 1\n```", widths: []int{20}},
		{label: "indented-code", text: "    indented code", widths: []int{20}},
		{label: "ul", text: "- a\n- b\n- c", widths: []int{20}},
		{label: "ul-nested", text: "- a\n  - b\n    - c\n- d", widths: []int{24}},
		{label: "ol", text: "1. one\n2. two", widths: []int{20}},
		{label: "ol-start", text: "5. five\n6. six", widths: []int{20}},
		{label: "task", text: "- [ ] todo\n- [x] done", widths: []int{24}},
		{label: "loose", text: "- a\n\n- b", widths: []int{20}},
		{label: "quote", text: "> quoted text here", widths: []int{20}},
		{label: "quote-multi", text: "> para\n>\n> - a\n> - b", widths: []int{24}},
		{label: "quote-nested", text: "> > deep", widths: []int{20}},
		{label: "quote-spacing", text: "before\n\n> quote\n\nafter", widths: []int{24}},
		{label: "hr", text: "a\n\n---\n\nb", widths: []int{10}},
		{label: "table", text: "| a | b |\n| - | - |\n| 1 | 2 |\n| 3 | 4 |", widths: []int{30, 20}},
		{label: "table-align", text: "| left | center | right |\n| :- | :-: | -: |\n| a | b | c |", widths: []int{40}},
		{label: "table-wrap", text: "| col |\n| - |\n| a very long cell value here |", widths: []int{20}},
		{label: "table-narrow", text: "| a | b | c |\n| - | - | - |\n| 1 | 2 | 3 |", widths: []int{8}},
		{label: "table-inline", text: "| **b** | `c` |\n| - | - |\n| [l](u) | x |", widths: []int{30}},
		{label: "links", text: "[text](http://x) and [http://y](http://y) and <http://z>", widths: []int{40}},
		{label: "image", text: "![alt](img.png)", widths: []int{20}},
		{label: "html", text: "<div>\nraw\n</div>", widths: []int{20}},
		{label: "inline-html", text: "text <b>bold</b> text", widths: []int{30}},
		{label: "escapes", text: "a \\* b \\_ c", widths: []int{20}},
		{label: "br", text: "line one  \nline two", widths: []int{20}},
		{label: "mixed", text: "# Title\n\npara **b**\n\n- item one\n- item two\n\n> quote\n\n```py\nx=1\n```\n\n| a |\n| - |\n| 1 |", widths: []int{30}},
		{label: "padding", text: "text", widths: []int{20}, paddingX: 2, paddingY: 1},
		// JSON drops functions, so the colour/background style is exercised by
		// this case and the transform by the next one.
		{label: "default-style", text: "plain **bold** text", widths: []int{24},
			defaultStyle: &DefaultTextStyle{Bold: true}},
		{label: "default-style-full", text: "plain **bold** text", widths: []int{24},
			defaultStyle: &DefaultTextStyle{
				Color:   func(text string) string { return "<col>" + text + "</col>" },
				BgColor: func(text string) string { return "<bg>" + text + "</bg>" },
				Bold:    true,
				Italic:  true,
			}},
		{label: "transform", text: "abc", widths: []int{20}},
		{label: "transform-upper", text: "abc", widths: []int{20}, options: MarkdownOptions{
			Transform: func(markdown string, availableWidth int) string { return strings.ToUpper(markdown) },
		}},
		{label: "preserve-markers", text: "- a\n1. b", widths: []int{20}, options: MarkdownOptions{PreserveOrderedListMarkers: true}},
		{label: "preserve-escapes", text: "a \\* b", widths: []int{20}, options: MarkdownOptions{PreserveBackslashEscapes: true}},
		{label: "no-latex", text: "math $x$ here", widths: []int{20}, options: MarkdownOptions{RenderLatex: new(false)}},
		// Degenerate sources: the reference returns no lines for empty or
		// whitespace-only text and one line for a definition-only source.
		{label: "empty", text: "", widths: []int{20}},
		{label: "whitespace", text: "  \n ", widths: []int{20}},
		{label: "definition-only", text: "[a]: /u\n", widths: []int{20}},
		{label: "definition-then-blank", text: "[a]: /u\n\n", widths: []int{20}},
		{label: "definition-only-padding", text: "[a]: /u\n", widths: []int{20}, paddingX: 1, paddingY: 1},
		{label: "comment-only", text: "<!-- x -->\n", widths: []int{20}},
	}

	for _, tc := range cases {
		markdown := NewMarkdown(tc.text, tc.paddingX, tc.paddingY, theme, tc.defaultStyle, tc.options)
		for _, width := range tc.widths {
			label := tc.label + "@" + strconv.Itoa(width)
			want, ok := golden[label]
			if !ok {
				t.Fatalf("missing golden %q", label)
			}
			got := markdown.Render(width)
			if len(got) != len(want) {
				t.Fatalf("%s:\n got %q\nwant %q", label, got, want)
			}
			for index := range got {
				if got[index] != want[index] {
					t.Fatalf("%s line %d:\n got %q\nwant %q", label, index, got[index], want[index])
				}
			}
		}
	}
}

// TestMarkdownLatexAgainstUpstreamGolden verifies the LaTeX path inside the
// markdown renderer (upstream renders "math $x^2$ here" as "math x² here").
func TestMarkdownLatexAgainstUpstreamGolden(t *testing.T) {
	golden := loadMDRenderGolden(t)
	want, ok := golden["latex-fallback@20"]
	if !ok {
		t.Fatal("missing latex golden")
	}
	markdown := NewMarkdown("math $x^2$ here", 0, 0, mdTestTheme(), nil, MarkdownOptions{})
	got := markdown.Render(20)
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestMarkdownCacheAndLatexFallback covers the cache and the LaTeX renderer.
func TestMarkdownCacheAndLatexFallback(t *testing.T) {
	markdown := NewMarkdown("cache me", 0, 0, mdTestTheme(), nil, MarkdownOptions{})
	first := markdown.Render(20)
	second := markdown.Render(20)
	if len(first) != len(second) || first[0] != second[0] {
		t.Fatal("cache mismatch")
	}
	markdown.SetText("other")
	if got := markdown.Render(20); len(got) != 1 || !strings.HasPrefix(got[0], "other") {
		t.Fatalf("render after setText = %q", got)
	}
	markdown.Invalidate()
	if got := markdown.Render(0); len(got) == 0 {
		t.Fatal("empty width render failed")
	}
	empty := NewMarkdown("", 0, 0, mdTestTheme(), nil, MarkdownOptions{})
	if got := empty.Render(10); len(got) != 0 {
		t.Fatalf("empty render = %q", got)
	}
	blank := NewMarkdown("   \n  ", 0, 0, mdTestTheme(), nil, MarkdownOptions{})
	if got := blank.Render(10); len(got) != 0 {
		t.Fatalf("blank render = %q", got)
	}
	// The latex renderer is ported: x^2 renders as x².
	if got, ok := RenderLatex("x^2", RenderLatexOptions{Display: false}); !ok || got != "x²" {
		t.Fatalf("RenderLatex = %q,%v", got, ok)
	}
	// Unsupported syntax still reports unsupported so the renderer falls back
	// to the raw source.
	if _, ok := RenderLatex("\\foo{x}", RenderLatexOptions{Display: false}); ok {
		t.Fatal("unknown command must be unsupported")
	}
}

// The framing must survive any URL a caller passes: an OSC 8 body ends at ST or
// BEL, so a URL carrying one would close the link early and leave the rest of
// the string as terminal input (D460, D465). Callers sanitize the label; the
// target is the renderer's own responsibility.
func TestHyperlinkKeepsControlCharactersOutOfTheFraming(t *testing.T) {
	hostile := "https://evil.example/\x1b]0;owned\x07done"
	link := Hyperlink("label", hostile)
	if strings.Contains(link, "\x1b]0;") || strings.Contains(link, "\x07") {
		t.Fatalf("framing carried the payload: %q", link)
	}
	if !strings.HasPrefix(link, "\x1b]8;;") || !strings.HasSuffix(link, "\x1b]8;;\x1b\\") {
		t.Fatalf("framing lost its shape: %q", link)
	}
	if got, want := Hyperlink("label", "https://ok.example/a?b=c"), "\x1b]8;;https://ok.example/a?b=c\x1b\\label\x1b]8;;\x1b\\"; got != want {
		t.Fatalf("ordinary URL changed: %q", got)
	}
}

// A fenced code block's info string is unescaped, not shown verbatim: pi
// v1.5.0 (f9a26365) packages/tui/src/components/markdown.ts renders
// “ ```${token.lang}“, and marked's fences tokenizer produces
// `lang: text.trim().replace(anyPunctuation, "$1")`. A backslash-escaped
// punctuation byte therefore reaches the fence header and the highlighter
// without its backslash (A441).
func TestMarkdownFenceInfoUnescapesPunctuation(t *testing.T) {
	theme := mdTestTheme()
	got := mdTrimmedLines(NewMarkdown("```js\\/ts\nx\n```", 0, 0, theme, nil, MarkdownOptions{}), 20)
	want := []string{"<cbb>```js/ts</cbb>", "  <cb>x</cb>", "<cbb>```</cbb>"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

// A GFM table followed by a whitespace-only final line must not gain a blank
// line. pi v1.5.0 (f9a26365) `packages/tui/src/components/markdown.ts` lexes
// with marked, whose `table` rule takes a trailing whitespace-only line that
// has no newline after it, as long as it does not begin an indented code line
// (`(?: {4}| {0,3}\t)[^\n]`); `tableRowBlankLine` then drops it from the rows.
// The line renders as nothing, and pier rendered it as an empty line (A439).
func TestMarkdownTableSwallowsTrailingWhitespaceLine(t *testing.T) {
	theme := mdTestTheme()
	table := "| a |\n| - |\n| 1 |"
	base := NewMarkdown(table, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
	for _, suffix := range []string{"\n", "\n ", "\n  ", "\n   ", "\n    "} {
		got := NewMarkdown(table+suffix, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		if len(got) != len(base) {
			t.Errorf("suffix %q: got %d lines %q, want %d", suffix, len(got), got, len(base))
		}
	}
	// A line that does begin an indented code line still ends the table, so it
	// keeps its own blank line.
	for _, suffix := range []string{"\n     ", "\n \t ", "\n  \n", "\n\n"} {
		got := NewMarkdown(table+suffix, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		if len(got) != len(base)+1 {
			t.Errorf("suffix %q: got %d lines %q, want %d", suffix, len(got), got, len(base)+1)
		}
	}
}

// The OSC 8 branch is reachable only when the installed capabilities say the terminal
// forwards hyperlinks, so this covers both sides of it. Before the setter existed
// nothing wrote the state, which left the branch unreachable and always rendered the
// legacy `text (url)` form (upstream markdown.ts link handling).
func TestMarkdownLinksFollowInstalledCapabilities(t *testing.T) {
	previous := GetTerminalCapabilities()
	t.Cleanup(func() { SetTerminalCapabilities(previous) })

	render := func(capabilities TerminalCapabilities) string {
		SetTerminalCapabilities(capabilities)
		markdown := NewMarkdown("[text](http://x)", 0, 0, mdTestTheme(), nil, MarkdownOptions{})
		return strings.Join(markdown.Render(40), "\n")
	}

	plain := render(TerminalCapabilities{})
	if strings.Contains(plain, "\x1b]8;;") {
		t.Fatalf("a terminal without hyperlink support got an OSC 8 link: %q", plain)
	}
	if !strings.Contains(plain, "http://x") {
		t.Fatalf("the legacy form dropped the URL: %q", plain)
	}

	linked := render(TerminalCapabilities{Hyperlinks: true})
	if !strings.Contains(linked, "\x1b]8;;http://x\x1b\\") {
		t.Fatalf("a hyperlink terminal got no OSC 8 link: %q", linked)
	}
	if !strings.Contains(linked, "\x1b]8;;\x1b\\") {
		t.Fatalf("the OSC 8 link was not closed: %q", linked)
	}
	if strings.Contains(linked, "(http://x)") {
		t.Fatalf("the OSC 8 form still printed the URL twice: %q", linked)
	}
}

// Carriage returns are line terminators, not text: pi v1.5.0 (f9a26365)
// `packages/tui/src/components/markdown.ts` lexes the source with marked, whose
// `Lexer.lex` begins with `src.replace(/\r\n|\r/g, "\n")` (marked 18.0.11
// `carriageReturn`), before any block or inline rule runs. Pier rendered the
// raw CR instead, which put the character inside heading and list text and
// stopped a CRLF table from being recognized.
func TestMarkdownNormalizesCarriageReturns(t *testing.T) {
	theme := mdTestTheme()
	cases := []string{
		"# h\r\nx",
		"# h\rx",
		"- a\r\n- b",
		"1. a\r\n2. b",
		"> q\r\n> r",
		"| a |\r\n| - |\r\n| 1 |",
		"para\r\n\r\nafter",
		"```js\r\nx\r\n```",
		"a  \r\nb",
		// A reference definition must reach the lexer LF-only, or the href
		// keeps a carriage return and the definition is not registered.
		"[a]: /u\r\n\r\n[a]",
	}
	for _, src := range cases {
		want := NewMarkdown(strings.ReplaceAll(src, "\r\n", "\n"), 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		lfOnly := NewMarkdown(strings.ReplaceAll(src, "\r", "\n"), 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		_ = lfOnly
		got := NewMarkdown(src, 0, 0, theme, nil, MarkdownOptions{}).Render(24)
		if len(got) != len(want) {
			t.Errorf("%q: got %d lines %q, want %d lines %q", src, len(got), got, len(want), want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%q: line %d got %q, want %q", src, i, got[i], want[i])
				break
			}
		}
	}
}

// A blockquote border is two columns, so a narrow view cannot hold one per
// nesting level. pi v1.5.2 (b4afdddf) components/markdown.ts renders the nested
// content without a border when `width < 3`, because each level that re-wraps
// its children at width 1 grows the output exponentially with nesting depth.
func TestNestedBlockquotesStopAtTheBorderThatFits(t *testing.T) {
	identity := func(text string) string { return text }
	theme := MarkdownTheme{
		Heading: identity, Link: identity, LinkURL: identity, Code: identity,
		CodeBlock: identity, CodeBlockBorder: identity, Quote: identity, QuoteBorder: identity,
		Hr: identity, ListBullet: identity, Bold: identity, Italic: identity,
		Strikethrough: identity, Underline: identity,
	}
	cases := []struct {
		depth, width int
		want         []string
	}{
		{1, 1, []string{"x"}},
		{4, 1, []string{"x"}},
		{1, 2, []string{"x "}},
		{4, 2, []string{"x "}},
		{1, 3, []string{"│ x"}},
		{2, 3, []string{"│ x"}},
		{10, 8, []string{"│ │ │ x "}},
		{12, 20, []string{"│ │ │ │ │ │ │ │ │ x "}},
		{12, 40, []string{"│ │ │ │ │ │ │ │ │ │ │ │ x               "}},
	}
	for _, tc := range cases {
		source := strings.Repeat("> ", tc.depth) + "x"
		got := NewMarkdown(source, 0, 0, theme, nil, MarkdownOptions{}).Render(tc.width)
		if len(got) != len(tc.want) {
			t.Fatalf("depth %d width %d: %d lines, want %d\ngot %q", tc.depth, tc.width, len(got), len(tc.want), got)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("depth %d width %d line %d: %q, want %q", tc.depth, tc.width, i, got[i], tc.want[i])
			}
		}
	}
}

// The renderer keeps lexed blocks and rendered lines across updates, keyed by
// the source, the width and the link definitions. A reuse that does not hold
// shows up as a frame that differs from a cold render of the same text, which
// is what streaming deltas, width changes and appended definitions exercise.
func TestIncrementalRenderMatchesAColdRender(t *testing.T) {
	documents := []string{
		"# Title\n\nA [link][ref] and `code`.\n\n[ref]: https://example.com/1\n",
		"para one\n\n```go\ncode\n```\n\n- a\n- b\n",
		"| a | b |\n| - | - |\n| 1 | 2 |\n",
		"[ref]: https://example.com/2\n\nLate definition then [ref].\n",
		"## 漢字 heading\n\n漢字テキスト with 👨‍👩‍👧.\n\n> quote\n",
		"text\n\n```\nunclosed fence\n",
	}
	for _, document := range documents {
		label, _, _ := strings.Cut(document, "\n")
		for _, width := range []int{20, 40, 80} {
			streamed := NewMarkdown("", 0, 0, MarkdownTheme{}, nil, MarkdownOptions{})
			for cut := 1; cut <= len(document); cut += 7 {
				streamed.SetText(document[:cut])
				streamed.Render(width)
			}
			streamed.SetText(document)
			if got, want := streamed.Render(width), NewMarkdown(document, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{}).Render(width); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q at width %d: streamed render differs from a cold render", label, width)
			}
			// A width change and a later append must not reuse the previous frame.
			grown := document + "\nappended after a width change\n"
			streamed.Render(120)
			streamed.SetText(grown)
			if got, want := streamed.Render(width), NewMarkdown(grown, 0, 0, MarkdownTheme{}, nil, MarkdownOptions{}).Render(width); !reflect.DeepEqual(got, want) {
				t.Fatalf("%q at width %d after a width change: render differs from a cold render", label, width)
			}
		}
	}
}

// Streaming renders must reuse the cached block-key and line backing arrays;
// reallocating them per delta made long messages quadratic (M143 streaming
// follow-up).
func TestMarkdownStreamRenderReusesCacheSlices(t *testing.T) {
	var text strings.Builder
	for range 10 {
		text.WriteString("Paragraph with **bold** and `code`.\n\n")
	}
	m := NewMarkdown(text.String(), 1, 0, mdTestTheme(), nil, MarkdownOptions{})
	_ = m.Render(80)
	// One append crosses the initial exact-size capacity and doubles it.
	text.WriteString("Growth paragraph.\n\n")
	m.SetText(text.String())
	_ = m.Render(80)
	if len(m.cacheTokenKeys) == 0 || len(m.cacheTokenLines) == 0 {
		t.Fatal("cache not populated")
	}
	keyBacking := &m.cacheTokenKeys[0]
	lineBacking := &m.cacheTokenLines[0]
	for range 5 {
		text.WriteString("Another paragraph.\n\n")
		m.SetText(text.String())
		_ = m.Render(80)
		if &m.cacheTokenKeys[0] != keyBacking {
			t.Fatal("block-key slice backing was reallocated within capacity")
		}
		if &m.cacheTokenLines[0] != lineBacking {
			t.Fatal("block-line slice backing was reallocated within capacity")
		}
	}
}

// Reference: pi v1.5.2 (b4afdddf) tui/src/components/markdown.ts. The corpus in
// testdata/mdrender-fuzz is produced by rendering seeded documents that mix
// markdown constructs with the reference's own Markdown component, so the port's
// renderer is compared against the reference on shapes the curated corpus in
// md_render_golden.txt does not spell out. The document text travels in the
// fixture, so the random generator is not duplicated here.
func TestMarkdownFuzzCorpusMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/mdrender-fuzz/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Label string   `json:"label"`
		Text  string   `json:"text"`
		Width int      `json:"width"`
		Lines []string `json:"lines"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the markdown fuzz corpus is empty")
	}
	theme := mdTestTheme()
	for _, fixture := range fixtures {
		markdown := NewMarkdown(fixture.Text, 0, 0, theme, nil, MarkdownOptions{})
		got := markdown.Render(fixture.Width)
		if len(got) != len(fixture.Lines) {
			t.Fatalf("%s (width %d) rendered %d lines, want %d\ninput %q\n got %q\nwant %q",
				fixture.Label, fixture.Width, len(got), len(fixture.Lines), fixture.Text, got, fixture.Lines)
		}
		for index := range got {
			if got[index] != fixture.Lines[index] {
				t.Fatalf("%s (width %d) line %d\ninput %q\n got %q\nwant %q",
					fixture.Label, fixture.Width, index, fixture.Text, got[index], fixture.Lines[index])
			}
		}
	}
}
