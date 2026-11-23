package highlight

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// D523: the exact walk must agree with the regexp it replaces, which is the
// rule anchored at the position with the preceding rune as context (the
// dispatch's prefixedRe), whenever the walk decides. Every rule of every
// language is run at every position of that language's golden samples and a
// few mixed lines; an undecided walk is not compared, since the dispatch then runs the
// regexp.
func TestExactWalkMatchesTheRegexp(t *testing.T) {
	data, err := os.ReadFile("testdata/golden_manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest []struct {
		File     string `json:"file"`
		Language string `json:"language"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	mixed := []string{
		"function f(a, b) { return a / b; } // the code is so pretty\n",
		"class A < B; def x?(y) @v = $1 end\r\nSELECT * FROM t WHERE a BETWEEN 1 AND 2;",
		"<div class=\"x\">{{#if a}}é 日本   #include <x.h>\nnamespace n { get x() {} }",
		"method(a, (b), (c(d))) {\n  ---\n  x: 1\n  ...\n}",
		"# a\r\n\t@b . c\r* d\u2028  e: f\u2029g // h\n",
		"SELECT \u212Aey, \u017Fum(x) FROM \u212A WHERE k = \u017F;\n",
	}
	// Each language's rules run over its own golden samples, where its rule
	// shapes occur, and the mixed lines above.
	samples := map[string][]string{}
	for _, entry := range manifest {
		text, err := os.ReadFile(filepath.Join("testdata", entry.File))
		if err != nil {
			t.Fatal(err)
		}
		if len(text) > 1500 {
			text = text[:1500]
		}
		samples[entry.Language] = append(samples[entry.Language], encodeCodeUnitsString(string(text)))
	}
	var ws jsWorkspace
	decided, matched := 0, 0
	seen := map[string]bool{}
	for _, name := range Languages() {
		lang, err := getCompiledLanguage(name)
		if err != nil {
			t.Fatal(err)
		}
		modes := map[*compiledMode]bool{}
		var walk func(mode *compiledMode)
		walk = func(mode *compiledMode) {
			if mode == nil || modes[mode] {
				return
			}
			modes[mode] = true
			matcher := mode.matcher.getMatcher(0)
			for i, source := range matcher.regexes {
				program := matcher.exacts[i]
				key := name + "\x00" + source
				if program == nil || seen[key] {
					continue
				}
				seen[key] = true
				plain, err := compileEncodedPattern(`\A(?:`+source+`)`, lang.flags)
				if err != nil {
					t.Fatal(err)
				}
				prefixed, err := compileEncodedPattern(`\A(?s:.)(?:`+source+`)`, lang.flags)
				if err != nil {
					t.Fatal(err)
				}
				for _, input := range append(samples[name], mixed...) {
					jsLines := nonLFLineEnds(input)
					for pos := 0; pos <= len(input); pos++ {
						if pos < len(input) && !utf8.RuneStart(input[pos]) {
							continue
						}
						end, result := matchExactAt(program, input, pos, jsLines, &ws)
						if result == exactUnknown {
							continue
						}
						decided++
						wantEnd := -1
						if pos == 0 {
							if loc := plain.findIndexWS(input, jsLines, &ws); loc != nil {
								wantEnd = loc[1]
							}
						} else {
							_, width := utf8.DecodeLastRuneInString(input[:pos])
							base := pos - width
							if loc := prefixed.findIndexWS(input[base:], jsLines, &ws); loc != nil {
								wantEnd = base + loc[1]
							}
						}
						gotEnd := -1
						if result == exactMatched {
							gotEnd = end
							matched++
						}
						if gotEnd != wantEnd {
							t.Fatalf("%s rule %q at %d of %q: walk ends at %d, regexp at %d",
								name, source, pos, input, gotEnd, wantEnd)
						}
					}
				}
			}
			for _, child := range mode.contains {
				walk(child)
			}
			walk(mode.starts)
		}
		walk(lang.root)
	}
	t.Logf("%d positions decided, %d matched", decided, matched)
	if decided < 1000000 || matched < 50000 {
		t.Fatalf("only %d positions decided, %d matched", decided, matched)
	}
}

// D523: an end rule keeps its capture groups, and so a match of it is left to
// the regexp, only in a language with an OnEnd callback. highlight.js 10.7.3's
// END_SAME_AS_BEGIN compares the end match's first group with the begin's,
// so a bash heredoc must still close only on its own tag; the expected HTML
// was read off the pinned highlight.js.
func TestEndRulesKeepGroupsOnlyWhereOnEndReadsThem(t *testing.T) {
	for name, want := range map[string]bool{"typescript": false, "json": false, "bash": true, "ruby": true} {
		registryMu.RLock()
		language := registry[name]
		registryMu.RUnlock()
		if got := modeTreeHasOnEnd(&language.Mode, map[*Mode]bool{}); got != want {
			t.Errorf("%s: end rules keep groups = %v, want %v", name, got, want)
		}
	}
	// Within such a language only a mode with OnEnd, or one that ends with
	// its parent (whose OnEnd endOfMode may then run), keeps them.
	lang, err := getCompiledLanguage("cpp")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[*compiledMode]bool{}
	dropped := 0
	var walk func(mode *compiledMode)
	walk = func(mode *compiledMode) {
		if mode == nil || seen[mode] {
			return
		}
		seen[mode] = true
		for _, rule := range mode.matcher.rules {
			if rule.data.typ != ruleEnd {
				continue
			}
			if want := mode.onEnd != nil || mode.endsWithParent; rule.keep != want {
				t.Errorf("cpp mode %q end %q keeps groups = %v, want %v", mode.beginSource, rule.src, rule.keep, want)
			}
			if !rule.keep {
				dropped++
			}
		}
		for _, child := range mode.contains {
			walk(child)
		}
		walk(mode.starts)
	}
	walk(lang.root)
	if dropped == 0 {
		t.Error("no cpp end rule drops its groups")
	}
	for code, want := range map[string]string{
		"cat <<EOF\nx\nEND\nEOF\ny": "cat &lt;&lt;<span class=\"hljs-string\">EOF\nx\nEND\nEOF</span>\ny",
		"cat <<-'TAG'\n  a\nTAG\nz": "cat &lt;&lt;-<span class=\"hljs-string\">&#x27;TAG&#x27;</span>\n  a\nTAG\nz",
	} {
		result, err := HighlightWith(code, "bash", true)
		if err != nil {
			t.Fatal(err)
		}
		if got := RenderHTML(result.Nodes); got != want {
			t.Errorf("bash %q = %q, want %q", code, got, want)
		}
	}
}

// TestExactWalkLiteralASCIIPath guards the byte-compare fast path for an ASCII
// literal. The walk skips utf8.DecodeRuneInString when the rule's rune and the
// input byte are both ASCII, so a byte compare must agree with the decoder on
// every boundary case: end of input, a non-ASCII byte where the literal
// expects ASCII, and a short literal prefix.
func TestExactWalkLiteralASCIIPath(t *testing.T) {
	patterns := []string{`abc`, `\breturn\b`, `x`, `aé`, `é`}
	inputs := []string{
		"", "a", "ab", "abc", "abcd", "x", "return", "returned",
		"\xc3\xa9", "a\xc3\xa9", "\xff\xfe", "abc\xc3\xa9", "  return  ",
	}
	ws := &jsWorkspace{}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		ex := exactProgram(parsePattern(pattern))
		if ex == nil {
			continue // not walked; the regexp is used directly
		}
		for _, input := range inputs {
			for pos := 0; pos <= len(input); pos++ {
				want := re.FindStringIndex(input[pos:])
				end, result := matchExactAt(ex, input, pos, false, ws)
				switch {
				case want == nil:
					if result == exactMatched {
						t.Errorf("%q at %d of %q: walk matched to %d, regexp did not", pattern, pos, input, end)
					}
				case result == exactMatched:
					if end-pos != want[1] {
						t.Errorf("%q at %d of %q: walk length %d, regexp length %d", pattern, pos, input, end-pos, want[1])
					}
				}
			}
		}
	}
}

// TestExactRunFastPathMatchesTheRegexp guards the byte-scan used for a
// repetition whose body matches only ASCII. The bitmap decides each byte, so
// the path is only sound for an ASCII-only body over input with no JavaScript
// line terminator; a body that can match above ASCII must take the decoding
// path, and both must agree with the regexp.
func TestExactRunFastPathMatchesTheRegexp(t *testing.T) {
	patterns := []string{
		`[a-z]+x`,
		`\w*z`,
		`[A-Za-z0-9_]*;`,
		`[\x{00C0}-\x{02B8}a-z]*;`,
		`[\x{00C0}-\x{02B8}]+`,
		`[\x{00C0}-\x{02B8}]x`,
		`(?s).*Z`,
		`[^\n]*\n`,
	}
	inputs := []string{
		"abcx", "aaa", "zz", "aZ", "", "abc", "\u00e9x", "ab\u00e9cx",
		"\xff\xfeZ", "abc\nZ", "日本語Z", "a\u2028Z",
		// A non-ASCII run the class admits: the fast path must not stop at the
		// first byte above ASCII, so these decide the asciiOnly guard.
		"ab\u00e9\u00e9;", "\u00e9;", "a\u00e9b\u00e9;", "ab\u00e9;Z",
		// A \r the dot must not consume when jsLines is set: the bitmap leaves
		// out only \n, so this decides the jsLines guard.
		"a\rb", "a\rb\rc", "\r", "a\rb\nZ",
	}
	ws := &jsWorkspace{}
	for _, pattern := range patterns {
		// Anchored at the position, as the walk is. No (?s): the patterns carry
		// their own flags, and adding it would make `.` match \n for `.*`.
		re := regexp.MustCompile(`\A(?:` + pattern + `)`)
		ex := exactProgram(parsePattern(pattern))
		if ex == nil {
			continue
		}
		// The dot under jsLines stops at \r, U+2028 and U+2029 where Go's
		// regexp does not, so a dot pattern is checked against the JS semantics
		// separately below rather than against re.
		jsLinesOf := func() []bool {
			if strings.Contains(pattern, ".") {
				return []bool{false}
			}
			return []bool{false, true}
		}()
		for _, input := range inputs {
			for _, jsLines := range jsLinesOf {
				for pos := 0; pos <= len(input); pos++ {
					if pos < len(input) && !utf8.RuneStart(input[pos]) {
						continue
					}
					want := re.FindStringIndex(input[pos:])
					end, result := matchExactAt(ex, input, pos, jsLines, ws)
					switch {
					case result == exactUnknown:
						continue
					case want == nil:
						if result == exactMatched {
							t.Errorf("%q at %d of %q (jsLines=%v): walk matched to %d, regexp did not",
								pattern, pos, input, jsLines, end)
						}
					case result == exactMatched:
						if end-pos != want[1] {
							t.Errorf("%q at %d of %q (jsLines=%v): walk length %d, regexp length %d",
								pattern, pos, input, jsLines, end-pos, want[1])
						}
					}
				}
			}
		}
	}
}

// TestExactRunFastPathRespectsJSLines guards that jsLines does not change a
// class: only the dot takes JavaScript's extra line terminators, so a class
// that admits \r keeps matching it. It also pins the dot's run, which is not
// ASCII-only and so always takes the decoding path.
func TestExactRunFastPathRespectsJSLines(t *testing.T) {
	// A class rather than the dot: the dot's bitmap is not ASCII-only (it
	// matches every non-ASCII rune), so it never takes the byte-scan path. A
	// class that admits \r is ASCII-only and does, and jsLines is what stops it.
	ex := exactProgram(parsePattern(`[\x00-\x7f]*`))
	if ex == nil {
		t.Fatal("the class run must be walkable")
	}
	ws := &jsWorkspace{}
	for _, test := range []struct {
		input   string
		jsLines bool
		want    int
	}{
		{"a\rb", false, 3},
		{"a\rb", true, 3}, // a class matches \r whether or not jsLines is set
		{"ab\nc", true, 4},
		{"abc", true, 3},
		{"", true, 0},
	} {
		end, result := matchExactAt(ex, test.input, 0, test.jsLines, ws)
		if result != exactMatched || end != test.want {
			t.Errorf("%q (jsLines=%v): matched=%v end=%d, want %d",
				test.input, test.jsLines, result == exactMatched, end, test.want)
		}
	}
	// The dot is not ASCII-only, so its run must still stop at \r under jsLines.
	dot := exactProgram(parsePattern(`.*`))
	if dot == nil {
		t.Fatal("the dot run must be walkable")
	}
	if end, result := matchExactAt(dot, "a\rb", 0, true, ws); result != exactMatched || end != 1 {
		t.Errorf("dot under jsLines: matched=%v end=%d, want 1", result == exactMatched, end)
	}
}

// TestExactLiteralASCIIFold guards the case-bit flip in exactLiteral for an
// ASCII pair. unicode.SimpleFold is the general case, so a byte flip must be
// exactly equivalent for every ASCII pair and must not be used when either side
// is not ASCII, where SimpleFold relates characters the flip does not (the long
// s and the Kelvin sign, for example).
func TestExactLiteralASCIIFold(t *testing.T) {
	for r := rune(0); r < utf8.RuneSelf; r++ {
		fold := &exactNode{fold: true}
		for c := rune(0); c < utf8.RuneSelf; c++ {
			want := c == r
			if !want {
				for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
					if f == c {
						want = true
						break
					}
				}
			}
			if got := exactLiteral(fold, r, c); got != want {
				t.Fatalf("fold %q vs %q: got %v, SimpleFold says %v", r, c, got, want)
			}
		}
	}
	// Above ASCII the flip must not decide: KELVIN SIGN folds with k, and the
	// long s with s, so exactLiteral still has to reach SimpleFold for them.
	for _, pair := range []struct {
		r, c rune
	}{{'k', '\u212A'}, {'s', '\u017F'}, {'\u212A', 'k'}, {'\u017F', 's'}} {
		if !exactLiteral(&exactNode{fold: true}, pair.r, pair.c) {
			t.Errorf("fold %q vs %q: SimpleFold pair must match", pair.r, pair.c)
		}
	}
}
