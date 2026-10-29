package coding

import "testing"

// Upstream generateUnifiedPatch delegates to jsdiff createTwoFilesPatch with
// FILE_HEADERS_ONLY: no index or underline header, newline-carrying line tokens
// (so a final line without a newline differs from one with it), and
// "\ No newline at end of file" markers. Expectations generated with the pinned
// jsdiff 8.0.4 through pi v1.4.0 core/tools/edit-diff.ts.
func TestGenerateUnifiedPatchMatchesJsdiff(t *testing.T) {
	cases := []struct {
		name, oldContent, newContent, want string
	}{
		{"identical", "same\n", "same\n", "--- f.txt\n+++ f.txt\n"},
		{
			"newline-only difference", "same", "same\n",
			"--- f.txt\n+++ f.txt\n@@ -1,1 +1,1 @@\n-same\n\\ No newline at end of file\n+same\n",
		},
		{
			"no trailing newlines", "a\nb\nc", "x",
			"--- f.txt\n+++ f.txt\n@@ -1,3 +1,1 @@\n-a\n-b\n-c\n\\ No newline at end of file\n+x\n\\ No newline at end of file\n",
		},
		{
			"trailing newlines", "a\nb\nc\n", "x\n",
			"--- f.txt\n+++ f.txt\n@@ -1,3 +1,1 @@\n-a\n-b\n-c\n+x\n",
		},
		{
			"two hunks", "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n",
			"one\ntwo\nTHREE\nfour\nfive\nsix\nseven\neight\nNINE\nten\n",
			"--- f.txt\n+++ f.txt\n@@ -1,10 +1,10 @@\n one\n two\n-three\n+THREE\n four\n five\n six\n seven\n eight\n-nine\n+NINE\n ten\n",
		},
		{"from empty", "", "new\n", "--- f.txt\n+++ f.txt\n@@ -0,0 +1,1 @@\n+new\n"},
		{"to empty", "old\n", "", "--- f.txt\n+++ f.txt\n@@ -1,1 +0,0 @@\n-old\n"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := GenerateUnifiedPatch("f.txt", test.oldContent, test.newContent, 3); got != test.want {
				t.Fatalf("patch = %q; want %q", got, test.want)
			}
		})
	}
}

// Upstream generateUnifiedPatch passes contextLines straight to jsdiff, where an
// explicit zero means "no context" and one hunk per change, not the display
// default. Expectations generated with jsdiff 8.0.4 createTwoFilesPatch
// (headerOptions: FILE_HEADERS_ONLY) through pi v1.4.0 edit-diff.ts.
func TestGenerateUnifiedPatchZeroContextMatchesJsdiff(t *testing.T) {
	cases := []struct {
		name, oldContent, newContent, want string
	}{
		{
			"single change", "a\nb\nc\n", "a\nX\nc\n",
			"--- f.txt\n+++ f.txt\n@@ -2,1 +2,1 @@\n-b\n+X\n",
		},
		{
			"separate hunks", "one\ntwo\nthree\nfour\nfive\n", "ONE\ntwo\nthree\nfour\nFIVE\n",
			"--- f.txt\n+++ f.txt\n@@ -1,1 +1,1 @@\n-one\n+ONE\n@@ -5,1 +5,1 @@\n-five\n+FIVE\n",
		},
		{
			"no trailing newline", "a\nb", "a\nX",
			"--- f.txt\n+++ f.txt\n@@ -2,1 +2,1 @@\n-b\n\\ No newline at end of file\n+X\n\\ No newline at end of file\n",
		},
		{
			"from empty", "", "new\n",
			"--- f.txt\n+++ f.txt\n@@ -0,0 +1,1 @@\n+new\n",
		},
		{
			"to empty", "old\n", "",
			"--- f.txt\n+++ f.txt\n@@ -1,1 +0,0 @@\n-old\n",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := GenerateUnifiedPatch("f.txt", test.oldContent, test.newContent, 0); got != test.want {
				t.Fatalf("patch = %q; want %q", got, test.want)
			}
		})
	}
}

// Upstream generateDiffString passes contextLines to its renderer; an explicit
// zero hides every context line and leaves only the skipped-run markers.
// Expectations generated with the pinned module's generateDiffString.
func TestGenerateDiffStringZeroContextMatchesJsdiff(t *testing.T) {
	cases := []struct {
		name, oldContent, newContent, want string
		firstChanged                       int
	}{
		{"single change", "a\nb\nc\n", "a\nX\nc\n", "   ...\n-2 b\n+2 X\n   ...", 2},
		{"separate changes", "one\ntwo\nthree\nfour\nfive\n", "ONE\ntwo\nthree\nfour\nFIVE\n", "-1 one\n+1 ONE\n   ...\n-5 five\n+5 FIVE", 1},
		{"no trailing newline", "a\nb", "a\nX", "   ...\n-2 b\n+2 X", 2},
		{"from empty", "", "new\n", "+1 new", 1},
		{"to empty", "old\n", "", "-1 old", 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			diff, first, ok := GenerateDiffString(test.oldContent, test.newContent, 0)
			if !ok || diff != test.want || first != test.firstChanged {
				t.Fatalf("diff = %q first=%d ok=%v; want %q, %d, true", diff, first, ok, test.want, test.firstChanged)
			}
		})
	}
}

// Upstream generateDiffString returns no firstChangedLine for identical
// content; the port reports that as ok=false.
func TestGenerateDiffStringReportsUnchanged(t *testing.T) {
	diff, first, ok := GenerateDiffString("same\n", "same\n", 3)
	if diff != "" || first != -1 || ok {
		t.Fatalf("unchanged diff = %q first=%d ok=%v; want empty, -1, false", diff, first, ok)
	}
	if diff, first, ok := GenerateDiffString("old\n", "new\n", 3); diff == "" || first != 1 || !ok {
		t.Fatalf("changed diff = %q first=%d ok=%v", diff, first, ok)
	}
}
