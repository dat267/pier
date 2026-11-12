package coding

import (
	"encoding/json"
	"os"
	"testing"
)

// pathParityFixture is generated from the pinned pi's utils/paths.ts; run
// testdata/paths-parity/generate.mjs (or `just goldens`) to refresh it.
type pathParityFixture struct {
	Source      string               `json:"source"`
	Home        string               `json:"home"`
	Base        string               `json:"base"`
	Cases       []pathParityCase     `json:"cases"`
	Comparisons []pathComparisonCase `json:"comparisons"`
	Shortened   []pathShortenedCase  `json:"shortened"`
}

type pathParityCase struct {
	Input       string  `json:"input"`
	Normalize   string  `json:"normalize"`
	Trimmed     string  `json:"trimmed"`
	Spaces      string  `json:"spaces"`
	Resolve     string  `json:"resolve"`
	NoTilde     string  `json:"noTilde"`
	CwdRelative *string `json:"cwdRelative"`
}

type pathComparisonCase struct {
	Parent string `json:"parent"`
	Child  string `json:"child"`
	Within bool   `json:"within"`
	Equal  bool   `json:"equal"`
}

type pathShortenedCase struct {
	Path   string `json:"path"`
	Home   string `json:"home"`
	Result string `json:"result"`
}

// pi v1.5.2 (b4afdddf) utils/paths.ts: normalizePath, resolvePath,
// getCwdRelativePath, isPathWithin, pathsEqual and shortenHomePath. The rows
// are the pin's own outputs for a fixed home directory and base, so a change in
// pier's path handling shows up as a diff here instead of a wrong file path.
func TestPathsMatchThePin(t *testing.T) {
	raw, err := os.ReadFile("testdata/paths-parity/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture pathParityFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Cases) == 0 || len(fixture.Comparisons) == 0 || len(fixture.Shortened) == 0 {
		t.Fatalf("empty fixture: %d cases, %d comparisons, %d shortened", len(fixture.Cases), len(fixture.Comparisons), len(fixture.Shortened))
	}
	noExpand := false
	for _, testCase := range fixture.Cases {
		home := PathInputOptions{HomeDir: fixture.Home}
		if got := NormalizePath(testCase.Input, home); got != testCase.Normalize {
			t.Errorf("NormalizePath(%q) = %q, want %q", testCase.Input, got, testCase.Normalize)
		}
		if got := NormalizePath(testCase.Input, PathInputOptions{Trim: true, HomeDir: fixture.Home}); got != testCase.Trimmed {
			t.Errorf("NormalizePath(%q, trim) = %q, want %q", testCase.Input, got, testCase.Trimmed)
		}
		if got := NormalizePath(testCase.Input, PathInputOptions{NormalizeUnicodeSpaces: true, StripAtPrefix: true, HomeDir: fixture.Home}); got != testCase.Spaces {
			t.Errorf("NormalizePath(%q, spaces) = %q, want %q", testCase.Input, got, testCase.Spaces)
		}
		if got := NormalizePath(testCase.Input, PathInputOptions{ExpandTilde: &noExpand, HomeDir: fixture.Home}); got != testCase.NoTilde {
			t.Errorf("NormalizePath(%q, no tilde) = %q, want %q", testCase.Input, got, testCase.NoTilde)
		}
		if got := ResolvePath(testCase.Input, fixture.Base, home); got != testCase.Resolve {
			t.Errorf("ResolvePath(%q) = %q, want %q", testCase.Input, got, testCase.Resolve)
		}
		if testCase.CwdRelative != nil {
			got, ok := GetCwdRelativePath(testCase.Input, fixture.Base)
			if !ok || got != *testCase.CwdRelative {
				t.Errorf("GetCwdRelativePath(%q) = %q, %v, want %q", testCase.Input, got, ok, *testCase.CwdRelative)
			}
		}
	}
	for _, comparison := range fixture.Comparisons {
		if got := IsPathWithin(comparison.Parent, comparison.Child); got != comparison.Within {
			t.Errorf("IsPathWithin(%q, %q) = %v, want %v", comparison.Parent, comparison.Child, got, comparison.Within)
		}
		if got := PathsEqual(comparison.Parent, comparison.Child); got != comparison.Equal {
			t.Errorf("PathsEqual(%q, %q) = %v, want %v", comparison.Parent, comparison.Child, got, comparison.Equal)
		}
	}
	for _, shortened := range fixture.Shortened {
		if got := ShortenHomePath(shortened.Path, shortened.Home); got != shortened.Result {
			t.Errorf("ShortenHomePath(%q) = %q, want %q", shortened.Path, got, shortened.Result)
		}
	}
}
