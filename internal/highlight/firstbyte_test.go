package highlight

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// The first-byte set is a safe over-approximation: a byte that can begin a
// non-empty match must be in it, or the engine's per-byte dispatch skips a real
// match. The JavaScript corpus provides the patterns and inputs, and the port's
// own compiled matcher supplies the matches, so this checks the set against
// everything the corpus can match without trusting the recorded JS indices.
func TestRuleFirstByteSetNeverExcludesAMatch(t *testing.T) {
	data, err := os.ReadFile("testdata/js-regex/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Source string `json:"source"`
		Flags  string `json:"flags"`
		Inputs []struct {
			Input string `json:"input"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		set, usable := ruleFirstByteSet(preparePattern(tc.Source), strings.Contains(tc.Flags, "i"))
		if !usable {
			continue
		}
		re, err := compilePattern(tc.Source, tc.Flags)
		if err != nil {
			continue
		}
		for _, in := range tc.Inputs {
			encoded := encodeCodeUnitsString(in.Input)
			for _, match := range re.FindAllStringIndex(encoded, -1) {
				if match[0] == match[1] {
					continue
				}
				checked++
				if strings.IndexByte(set, encoded[match[0]]) < 0 {
					t.Fatalf("%q on %q: match starts with byte %#x, which the set %q omits",
						tc.Source, in.Input, encoded[match[0]], set)
				}
			}
		}
	}
	if checked < 1000 {
		t.Fatalf("only %d matches checked", checked)
	}
}

func TestMinMatchLenAndLeadingContext(t *testing.T) {
	lengths := map[string]int{
		"a": 1, "ab": 2, "a*": 0, "a+": 1, "a?": 0, "(?:ab|a)": 1, "(?:ab|abc)": 2,
		"a{2,3}": 2, "a{0,3}": 0, "": 0, "\\b": 0, "\\bword": 4, "(a)(b)": 2,
	}
	for source, want := range lengths {
		parsed := parsePattern(source)
		if parsed == nil {
			t.Fatalf("%q did not parse", source)
		}
		if got := minMatchLen(parsed); got != want {
			t.Errorf("minMatchLen(%q) = %d, want %d", source, got, want)
		}
	}
	context := map[string]bool{
		"\\bword": true, "\\Bword": true, "^word": true, "\\Aword": true,
		"(?:^|x)word": true, "\\s*\\bword": true, "word\\b": false, "a\\b": false,
		"word": false, "(?:a|b)": false,
	}
	for source, want := range context {
		parsed := parsePattern(source)
		if parsed == nil {
			t.Fatalf("%q did not parse", source)
		}
		if got := leadingContextSensitive(parsed); got != want {
			t.Errorf("leadingContextSensitive(%q) = %v, want %v", source, got, want)
		}
	}
}

// mayMatchAt is a necessary condition: wherever a rule matches, it must answer
// true, or the dispatch skips a real match (D523). The JavaScript corpus and
// every compiled rule of every registered grammar supply the patterns; the
// port's own regexes supply the matches.
func TestMayMatchAtNeverRejectsAMatch(t *testing.T) {
	var ws jsWorkspace
	checked := 0
	check := func(source string, caseInsensitive bool, re *patternRegexp, input string) {
		prefilter := rulePrefilter(preparePattern(source), caseInsensitive)
		if prefilter == nil {
			return
		}
		for pos := 0; pos < len(input); pos++ {
			if !utf8.RuneStart(input[pos]) {
				continue
			}
			loc := re.FindStringIndex(input[pos:])
			if loc == nil || loc[0] != 0 {
				continue
			}
			checked++
			if !mayMatchAt(prefilter, input, pos, &ws) {
				t.Fatalf("%q at %d of %q: matches %q, but the prefilter rejects it",
					source, pos, input, input[pos:pos+loc[1]])
			}
		}
	}
	data, err := os.ReadFile("testdata/js-regex/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Source string `json:"source"`
		Flags  string `json:"flags"`
		Inputs []struct {
			Input string `json:"input"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		re, err := compilePattern(tc.Source, tc.Flags)
		if err != nil {
			continue
		}
		for _, in := range tc.Inputs {
			check(tc.Source, strings.Contains(tc.Flags, "i"), re, encodeCodeUnitsString(in.Input))
		}
	}
	inputs := []string{
		"function f(a, b) { return a / b; } // the code is so pretty\n",
		"class A < B; def x?(y) @v = $1 end\r\nSELECT * FROM t WHERE a BETWEEN 1 AND 2;",
		"<div class=\"x\">{{#if a}}é 日本   #include <x.h>\nnamespace n { get x() {} }",
	}
	compiled := map[string]bool{}
	for _, name := range Languages() {
		lang, err := getCompiledLanguage(name)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[*compiledMode]bool{}
		var walk func(mode *compiledMode)
		walk = func(mode *compiledMode) {
			if mode == nil || seen[mode] {
				return
			}
			seen[mode] = true
			for _, source := range mode.matcher.getMatcher(0).regexes {
				key := lang.flags + "\x00" + source
				if compiled[key] {
					continue
				}
				compiled[key] = true
				re, err := compileEncodedPattern(source, lang.flags)
				if err != nil {
					continue
				}
				for _, input := range inputs {
					check(source, lang.caseInsensitive, re, input)
				}
			}
			for _, child := range mode.contains {
				walk(child)
			}
			walk(mode.starts)
		}
		walk(lang.root)
	}
	if checked < 5000 {
		t.Fatalf("only %d matches checked", checked)
	}
}
