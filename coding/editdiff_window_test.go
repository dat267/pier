package coding

import (
	"fmt"
	"strings"
	"testing"
)

// numberedLines builds the fixture the window tests edit: count lines of
// "line N: the quick brown fox jumps over the lazy dog N".
func numberedLines(count int) string {
	var builder strings.Builder
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&builder, "line %d: the quick brown fox jumps over the lazy dog %d\n", index, index)
	}
	return builder.String()
}

// editOneLine replaces the body of the given 1-based line.
func editOneLine(content string, line int) (string, string, []textReplacement) {
	target := fmt.Sprintf("line %d: the quick brown fox jumps over the lazy dog %d\n", line, line)
	replacement := fmt.Sprintf("line %d: CHANGED\n", line)
	index := strings.Index(content, target)
	return content, strings.Replace(content, target, replacement, 1),
		[]textReplacement{{matchIndex: index, matchLength: len(target), newText: replacement}}
}

// TestGenerateEditDiffMatchesWholeFileDiff pins pi v1.5.2 (b4afdddf)
// core/tools/edit-diff.ts generateEditDiff: diffing only the window around a
// replacement gives the same display diff, patch and first changed line as the
// whole-content functions, so the faster path cannot change the edit tool's
// output.
func TestGenerateEditDiffMatchesWholeFileDiff(t *testing.T) {
	for _, lines := range []int{1, 2, 5, 40, 200, 3000} {
		for _, changed := range []int{1, lines / 2, lines} {
			if changed < 1 || changed > lines {
				continue
			}
			base, next, replacements := editOneLine(numberedLines(lines), changed)
			editDiff := GenerateEditDiff("f.txt", base, next, replacements, 4)
			wantDiff, wantFirst, _ := GenerateDiffString(base, next, 4)
			if editDiff.Diff != wantDiff {
				t.Fatalf("%d lines, line %d: diff =\n%s\nwant\n%s", lines, changed, editDiff.Diff, wantDiff)
			}
			if wantPatch := GenerateUnifiedPatch("f.txt", base, next, 4); editDiff.Patch != wantPatch {
				t.Fatalf("%d lines, line %d: patch =\n%s\nwant\n%s", lines, changed, editDiff.Patch, wantPatch)
			}
			if editDiff.FirstChangedLine != wantFirst {
				t.Fatalf("%d lines, line %d: firstChangedLine = %d, want %d", lines, changed, editDiff.FirstChangedLine, wantFirst)
			}
		}
	}
}

// TestGenerateEditDiffMergesAndSplitsWindows covers the window bookkeeping:
// two edits closer than the margin share one window, two edits beyond it split
// into two windows that each mark the gap as skipped and join with one marker.
func TestGenerateEditDiffMergesAndSplitsWindows(t *testing.T) {
	base := numberedLines(120)
	split := func(pairs ...[2]int) (string, []textReplacement) {
		content := base
		var replacements []textReplacement
		for _, pair := range pairs {
			target := fmt.Sprintf("line %d: the quick brown fox jumps over the lazy dog %d\n", pair[0], pair[0])
			replacement := fmt.Sprintf("line %d: CHANGED\n", pair[0])
			index := strings.Index(content, target)
			if index < 0 {
				t.Fatalf("line %d not found", pair[0])
			}
			replacements = append(replacements, textReplacement{matchIndex: index, matchLength: len(target), newText: replacement})
			content = content[:index] + replacement + content[index+len(target):]
		}
		return content, replacements
	}

	near := func(t *testing.T) {
		next, replacements := split([2]int{10, 1}, [2]int{20, 1})
		if got := len(getDiffWindows(base, replacements)); got != 1 {
			t.Errorf("near edits: %d windows, want 1", got)
		}
		editDiff := GenerateEditDiff("f.txt", base, next, replacements, 4)
		if want := GenerateUnifiedPatch("f.txt", base, next, 4); editDiff.Patch != want {
			t.Errorf("near edits: patch =\n%s\nwant\n%s", editDiff.Patch, want)
		}
	}
	far := func(t *testing.T) {
		next, replacements := split([2]int{3, 1}, [2]int{97, 1})
		if got := len(getDiffWindows(base, replacements)); got != 2 {
			t.Errorf("far edits: %d windows, want 2", got)
		}
		editDiff := GenerateEditDiff("f.txt", base, next, replacements, 4)
		// The two windows each mark the gap as skipped; the joined diff keeps one
		// marker there, so no marker directly follows another.
		if strings.Contains(editDiff.Diff, " ...\n     ...") {
			t.Errorf("far edits: doubled skip marker in\n%s", editDiff.Diff)
		}
		if wantDiff, first, _ := GenerateDiffString(base, next, 4); editDiff.Diff != wantDiff {
			t.Errorf("far edits: diff =\n%s\nwant\n%s", editDiff.Diff, wantDiff)
			_ = first
		}
		if want := GenerateUnifiedPatch("f.txt", base, next, 4); editDiff.Patch != want {
			t.Errorf("far edits: patch =\n%s\nwant\n%s", editDiff.Patch, want)
		}
	}
	t.Run("near", near)
	t.Run("far", far)
}

// TestGetDiffWindowsCoversWholeLines checks the window edges: each window starts
// at a line start, keeps the margin on both sides and stays within the content.
func TestGetDiffWindowsCoversWholeLines(t *testing.T) {
	base := numberedLines(200)
	target := "line 100: the quick brown fox jumps over the lazy dog 100\n"
	index := strings.Index(base, target)
	windows := getDiffWindows(base, []textReplacement{{matchIndex: index, matchLength: len(target), newText: "CHANGED\n"}})
	if len(windows) != 1 {
		t.Fatalf("%d windows, want 1", len(windows))
	}
	window := windows[0]
	if window.oldStart != 0 && base[window.oldStart-1] != '\n' {
		t.Errorf("oldStart %d is not a line start", window.oldStart)
	}
	if window.oldStart > index || window.oldEnd < index+len(target) {
		t.Errorf("window [%d,%d) does not cover the replacement at %d", window.oldStart, window.oldEnd, index)
	}
	startLine := strings.Count(base[:index], "\n")
	marginStart := strings.Count(base[:window.oldStart], "\n")
	if startLine-marginStart != diffWindowMarginLines {
		t.Errorf("leading margin %d lines, want %d", startLine-marginStart, diffWindowMarginLines)
	}
	if window.oldLine != marginStart || window.newLine != marginStart {
		t.Errorf("window lines = %d/%d, want %d", window.oldLine, window.newLine, marginStart)
	}
}

// BenchmarkEditDiffLargeFile records D475's baseline: the windowed diff keeps a
// small file edit at a cost that does not follow the file size.
func BenchmarkEditDiffLargeFile(b *testing.B) {
	for _, lines := range []int{2000, 20000, 100000} {
		base := numberedLines(lines)
		_, next, replacements := editOneLine(base, lines/2)
		b.Run(fmt.Sprintf("lines-%d/windowed", lines), func(b *testing.B) {
			for b.Loop() {
				if editDiff := GenerateEditDiff("f.go", base, next, replacements, 4); editDiff.Patch == "" {
					b.Fatal("empty patch")
				}
			}
		})
		b.Run(fmt.Sprintf("lines-%d/whole-file", lines), func(b *testing.B) {
			for b.Loop() {
				diff, _, _ := GenerateDiffString(base, next, 4)
				patch := GenerateUnifiedPatch("f.go", base, next, 4)
				if diff == "" || patch == "" {
					b.Fatal("empty")
				}
			}
		})
	}
}
