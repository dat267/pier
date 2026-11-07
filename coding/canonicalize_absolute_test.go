package coding

import (
	"os"
	"path/filepath"
	"testing"
)

// Reference: pi v1.5.0 utils/paths.ts canonicalizePath is fs.realpathSync,
// which always returns an absolute path (it resolves against process.cwd()).
// filepath.EvalSymlinks keeps a relative input relative, so the same directory
// yields two different comparison keys ("sub" vs "/work/sub"), letting one
// location be recorded under two spellings.
func TestCanonicalizePathMatchesRealpathOnRelativeInput(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(previous)

	for _, input := range []string{"", ".", "..", "sub", "./sub", "sub/"} {
		canonical := CanonicalizePath(input)
		if !filepath.IsAbs(canonical) {
			t.Errorf("CanonicalizePath(%q) = %q, want an absolute path", input, canonical)
		}
	}
	// The two spellings of one directory must compare equal.
	if !PathsEqual(CanonicalizePath("sub"), CanonicalizePath("./sub")) {
		t.Errorf("relative spellings did not agree: %q vs %q",
			CanonicalizePath("sub"), CanonicalizePath("./sub"))
	}
	// Control: an already absolute path is unchanged.
	absolute := CanonicalizePath(filepath.Join(dir, "sub"))
	if absolute != filepath.Join(dir, "sub") {
		t.Errorf("absolute canonical path = %q", absolute)
	}
}
