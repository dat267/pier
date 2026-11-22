package tui

import (
	"encoding/json"
	"os"
	"testing"
)

// latexTable is one extracted upstream table: a map of names to glyphs, or a
// set of names.
type latexTable struct {
	Kind    string          `json:"kind"`
	Entries json.RawMessage `json:"entries"`
}

// TestLatexTablesMatchTheirSources compares every LaTeX data table with the
// pinned upstream's, extracted by testdata/gen_latex_tables.mjs into
// testdata/latex_tables.json.
//
// TestLatexAgainstUpstreamGolden renders 178 corpus entries, and only 40 of the
// 224 symbol names appear in that corpus, so a hand-edited glyph outside those 40
// would reach neither the golden nor a test. This pins all of them.
func TestLatexTablesMatchTheirSources(t *testing.T) {
	raw, err := os.ReadFile("testdata/latex_tables.json")
	if err != nil {
		t.Fatal(err)
	}
	var tables map[string]latexTable
	if err := json.Unmarshal(raw, &tables); err != nil {
		t.Fatal(err)
	}
	if len(tables) == 0 {
		t.Fatal("empty fixture")
	}

	for _, testCase := range []struct {
		table string
		name  string
		got   map[string]string
	}{
		{"SYMBOLS", "latexSymbols", latexSymbols},
		{"NEGATED_SYMBOLS", "latexNegatedSymbols", latexNegatedSymbols},
		{"BLACKBOARD", "latexBlackboard", latexBlackboard},
		{"SUPERSCRIPTS", "latexSuperscripts", latexSuperscripts},
		{"SUBSCRIPTS", "latexSubscripts", latexSubscripts},
		{"ACCENTS", "latexAccents", latexAccents},
	} {
		var want map[string]string
		if err := json.Unmarshal(tables[testCase.table].Entries, &want); err != nil {
			t.Fatalf("%s: %v", testCase.table, err)
		}
		if len(want) != len(testCase.got) {
			t.Errorf("%s has %d entries, want %d", testCase.name, len(testCase.got), len(want))
		}
		for name, glyph := range want {
			got, ok := testCase.got[name]
			if !ok {
				t.Errorf("%s is missing %q", testCase.name, name)
				continue
			}
			if got != glyph {
				t.Errorf("%s[%q] = %q, want %q", testCase.name, name, got, glyph)
			}
		}
		for name := range testCase.got {
			if _, ok := want[name]; !ok {
				t.Errorf("%s has %q, absent from the source", testCase.name, name)
			}
		}
	}

	for _, testCase := range []struct {
		table string
		name  string
		got   map[string]bool
	}{
		{"NAMED_OPERATORS", "latexNamedOperators", latexNamedOperators},
		{"LIMIT_OPERATORS", "latexLimitOperators", latexLimitOperators},
		{"DISPLAY_LIMIT_SYMBOLS", "latexDisplayLimitSymbols", latexDisplayLimitSymbols},
		{"RELATION_COMMANDS", "latexRelationCommands", latexRelationCommands},
		{"SPACING_COMMANDS", "latexSpacingCommands", latexSpacingCommands},
		{"NEGATIVE_SPACING_COMMANDS", "latexNegativeSpacingCommands", latexNegativeSpacingCommands},
		{"FONT_SWITCH_COMMANDS", "latexFontSwitchCommands", latexFontSwitchCommands},
		{"IGNORED_COMMANDS", "latexIgnoredCommands", latexIgnoredCommands},
		{"SIZE_COMMANDS", "latexSizeCommands", latexSizeCommands},
		{"PLAIN_WRAPPERS", "latexPlainWrappers", latexPlainWrappers},
	} {
		var want []string
		if err := json.Unmarshal(tables[testCase.table].Entries, &want); err != nil {
			t.Fatalf("%s: %v", testCase.table, err)
		}
		if len(want) != len(testCase.got) {
			t.Errorf("%s has %d entries, want %d", testCase.name, len(testCase.got), len(want))
		}
		for _, name := range want {
			if !testCase.got[name] {
				t.Errorf("%s is missing %q", testCase.name, name)
			}
		}
	}
}
