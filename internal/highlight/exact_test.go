package highlight

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

// D523: an end rule keeps its capture groups, and so is left to the regexp,
// only in a language with an OnEnd callback. highlight.js 10.7.3's
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
