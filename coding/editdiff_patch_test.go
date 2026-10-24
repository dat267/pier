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
