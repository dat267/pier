package highlight

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Clone a compiled language privately to exercise the original scan
// loop as an oracle without mutating the shared, concurrency-safe registry.
func languageWithoutIllegalBatch(t testing.TB, name string) *compiledLanguage {
	t.Helper()
	lang, err := getCompiledLanguage(name)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[*compiledMode]*compiledMode{}
	var clone func(*compiledMode) *compiledMode
	clone = func(mode *compiledMode) *compiledMode {
		if mode == nil {
			return nil
		}
		if copy, ok := seen[mode]; ok {
			return copy
		}
		copy := *mode
		seen[mode] = &copy
		copy.illegalBatch = nil
		copy.contains = make([]*compiledMode, len(mode.contains))
		for i, child := range mode.contains {
			copy.contains[i] = clone(child)
		}
		copy.starts = clone(mode.starts)
		if mode.matcher != nil {
			copy.matcher = &resumableMultiRegex{
				count: mode.matcher.count, caseIgnore: mode.matcher.caseIgnore,
				rules: append([]subRule(nil), mode.matcher.rules...),
			}
			for i := range copy.matcher.rules {
				copy.matcher.rules[i].data.rule = clone(copy.matcher.rules[i].data.rule)
			}
		}
		return &copy
	}
	copy := *lang
	copy.root = clone(lang.root)
	return &copy
}

func registerIgnoredIllegalTestLanguages(t testing.TB) {
	t.Helper()
	definitions := map[string]*Language{}
	compiled := map[string]*compiledLanguage{}
	for _, name := range []string{"batch-boundaries", "batch-multi", "batch-zero", "batch-callback", "batch-fold", "batch-manual", "batch-embedded"} {
		registryMu.RLock()
		definitions[name] = registry[name]
		registryMu.RUnlock()
		compiledMu.RLock()
		compiled[name] = compiledLanguages[name]
		compiledMu.RUnlock()
	}
	t.Cleanup(func() {
		registryMu.Lock()
		for name, previous := range definitions {
			if previous == nil {
				delete(registry, name)
			} else {
				registry[name] = previous
			}
		}
		registryMu.Unlock()
		compiledMu.Lock()
		for name, previous := range compiled {
			if previous == nil {
				delete(compiledLanguages, name)
			} else {
				compiledLanguages[name] = previous
			}
		}
		compiledMu.Unlock()
	})
	RegisterLanguage("batch-boundaries", &Language{
		Illegal: `\S`, Keywords: Keywords{"keyword": "true false"},
		Contains: []*Mode{
			{ClassName: "first", Begin: `ab|b`},
			{ClassName: "number", Begin: `\b\d+`},
			{ClassName: "line", Begin: `^@`},
			{ClassName: "nested", Begin: `\[`, End: `\]`, Illegal: `\S`, Contains: []*Mode{
				{ClassName: "string", Begin: `"`, End: `"`},
				{ClassName: "number", Begin: `\b\d+`},
			}},
		},
	})
	RegisterLanguage("batch-multi", &Language{Illegal: `[xy]+`, Contains: []*Mode{{ClassName: "hit", Begin: `yZ`}}})
	RegisterLanguage("batch-zero", &Language{Contains: []*Mode{{Begin: `<`, End: `>`, Illegal: `(?:)`, Contains: []*Mode{{ClassName: "hit", Begin: `ok`}}}}})
	RegisterLanguage("batch-callback", &Language{Illegal: `\S`, Contains: []*Mode{{
		ClassName: "hit", Begin: `(T\d+)`, End: `>`,
		OnBegin: func(m *match, r *Response) {
			if m.group(1) == "T0" {
				r.IgnoreMatch()
			}
		},
		OnEnd: func(m *match, r *Response) {
			if m.index > 0 && m.input[m.index-1] == '!' {
				r.IgnoreMatch()
			}
		},
	}}})
	RegisterLanguage("batch-fold", &Language{CaseInsensitive: true, Illegal: `[a-z]`, Contains: []*Mode{{ClassName: "number", Begin: `\d+`}}})
	RegisterLanguage("batch-manual", &Language{Illegal: `\S`, Contains: []*Mode{{
		ClassName: "hit", End: `;`, ManualFirst: "@",
		ManualBegin: func(s string, pos int) (int, bool) {
			return pos + 1, pos < len(s) && s[pos] == '@'
		},
	}}})
	RegisterLanguage("batch-embedded", &Language{Contains: []*Mode{{Begin: `<`, End: `>`, SubLanguage: []string{"json"}, Illegal: `\S`}}})
}

// pi v1.5.0 (f9a26365) syntax-highlight.ts uses highlight.js 10.7.3.
// Batching must have no effect on the public token tree or usage of the
// reference's callbacks, including modes that fall back to the ordinary scan.
func TestIgnoredIllegalBatchPreservesBuiltinScanner(t *testing.T) {
	inputs := []string{
		`xx xx "value" 0x0x0x true false null`,
		`xx xx {"key": [1,2,3], "nested": {"number": -1.2e3}}`,
		"xx xx [xx xx \"value\" xx xx] {xx xx}",
		"xx xx // comment\r\n123\u2028/* comment */\u2029\"value\"",
		"xx xx 中文 😀\u00a0!\u0301 123",
		"xx xx T0 ?? T1!>later>\r@xx\n@yy",
		"<a>xx xx</a>\r\n<script>xx xx 0x0x</script>",
	}
	for _, name := range []string{"json", "javascript", "typescript", "python", "ruby", "php", "go", "xml", "yaml", "sql"} {
		t.Run(name, func(t *testing.T) {
			plain := languageWithoutIllegalBatch(t, name)
			for _, input := range inputs {
				for _, ignore := range []bool{false, true} {
					encoded, astral := encodeCodeUnits(input)
					want := highlightWith(plain, encoded, ignore)
					if astral {
						want.Nodes = decodeCodeUnitNodes(want.Nodes)
					}
					got, err := HighlightWith(input, name, ignore)
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(got.Nodes, want.Nodes) || got.Illegal != want.Illegal || got.Relevance != want.Relevance {
						t.Fatalf("input %q, ignore=%v\ngot %q\nwant %q", input, ignore, RenderHTML(got.Nodes), RenderHTML(want.Nodes))
					}
				}
			}
		})
	}
}

// The port's existing scan budget stops after iteration 1,000,001. Counting
// batched illegals must stop at the identical character, not consume a token
// that would otherwise stay plain text after the hang guard fires.
func TestIgnoredIllegalBatchPreservesIterationBudget(t *testing.T) {
	lang, err := getCompiledLanguage("json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ code, want string }{
		{"! \r\n!  123", "! \r\n!"},
		// The budget ends after the emoji's high surrogate, not the whole rune.
		{"!😀 123", "!\U000fd83d"},
	} {
		encoded, _ := encodeCodeUnits(tc.code)
		h := &highlighter{top: &modeInstance{mode: lang.root}, iterations: 1000000, jsLines: true}
		m := &match{input: encoded, index: 0, lexeme: "!", typ: ruleIllegal}
		lang.root.illegalBatch.extend(h, m)
		if m.lexeme != tc.want || h.iterations != 1000001 {
			t.Fatalf("input %q: consumed %q at iteration %d", tc.code, m.lexeme, h.iterations)
		}
	}
}

func FuzzIgnoredIllegalBatch(f *testing.F) {
	registerIgnoredIllegalTestLanguages(f)
	// Zero-width illegals are covered by closed modes in the reference fixtures.
	// Arbitrary unterminated ones would repeatedly exercise the hang guard.
	languages := []string{"json", "batch-boundaries", "batch-multi", "batch-callback", "batch-fold"}
	originals := make([]*compiledLanguage, len(languages))
	for i, name := range languages {
		originals[i] = languageWithoutIllegalBatch(f, name)
		for _, code := range []string{"0x0x0x", "xx xx [\"value\",true]", "xx xx\r\n@xx\u2028@😀", "xxxyZ", "xxok", "!! !!T0??T1!>later>"} {
			f.Add(code, uint8(i), true)
			f.Add(code, uint8(i), false)
		}
	}
	f.Fuzz(func(t *testing.T, code string, selector uint8, ignore bool) {
		if len(code) > 2048 {
			return // Bound the deliberately unoptimized comparison scanner.
		}
		index := int(selector) % len(languages)
		encoded, astral := encodeCodeUnits(code)
		want := highlightWith(originals[index], encoded, ignore)
		if astral {
			want.Nodes = decodeCodeUnitNodes(want.Nodes)
		}
		got, err := HighlightWith(code, languages[index], ignore)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Nodes, want.Nodes) || got.Illegal != want.Illegal || got.Relevance != want.Relevance {
			t.Fatalf("input %q, language=%s ignore=%v\ngot %q\nwant %q", code, languages[index], ignore, RenderHTML(got.Nodes), RenderHTML(want.Nodes))
		}
	})
}

// Generated fixtures pin ordinary, ignored, zero-width and multi-rune illegals,
// true preceding-rune context, callback groups, manual rules, JS whitespace and
// astral input against highlight.js 10.7.3, as pinned by pi v1.5.0 (f9a26365).
func TestIgnoredIllegalScannerMatchesReference(t *testing.T) {
	registerIgnoredIllegalTestLanguages(t)
	data, err := os.ReadFile("testdata/ignored-illegals/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Language, Code, HTML string
		IgnoreIllegals, Illegal    bool
		Relevance                  int
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			result, err := HighlightWith(tc.Code, tc.Language, tc.IgnoreIllegals)
			if err != nil {
				t.Fatal(err)
			}
			if got := RenderHTML(result.Nodes); got != tc.HTML || result.Relevance != tc.Relevance || result.Illegal != tc.Illegal {
				t.Fatalf("input %q\nHTML = %q, relevance=%d illegal=%v\nwant = %q, relevance=%d illegal=%v", tc.Code, got, result.Relevance, result.Illegal, tc.HTML, tc.Relevance, tc.Illegal)
			}
		})
	}
}
