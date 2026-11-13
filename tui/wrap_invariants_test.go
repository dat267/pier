package tui

import (
	"math/rand"
	"strings"
	"testing"
)

// stripEscapes removes every escape sequence, leaving the text a terminal
// would show. The test only needs the printable content.
func stripEscapes(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if code, length := ExtractANSICode(text, i); code != "" {
			i += length
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// Wrapping and truncation see text the UI did not compose: assistant output,
// tool results, session titles. Wide graphemes, ZWJ sequences, regional
// indicators, combining marks and inline styles all reach them, so the
// invariants are checked over a seeded corpus rather than a few fixtures.
//
// The width bound allows one grapheme wider than the requested width, because a
// two-column character cannot be narrowed to one; that matches the reference,
// which emits the segment too (pi v1.5.2 packages/tui/src/utils.ts wrapText).
func TestWrapAndTruncateKeepTheirInvariants(t *testing.T) {
	alphabet := []string{"a", "b", " ", "漢", "👨‍👩‍👧", "e\u0301", "🇦🇧", "\x1b[31m", "\x1b[39m", "\x1b]8;;https://x\x1b\\", "→"}
	random := rand.New(rand.NewSource(7))
	for iteration := 0; iteration < 4000; iteration++ {
		var text strings.Builder
		for range random.Intn(30) {
			text.WriteString(alphabet[random.Intn(len(alphabet))])
		}
		width := 1 + random.Intn(20)
		limit := max(width, 2)
		lines := WrapTextWithAnsi(text.String(), width)
		for _, line := range lines {
			if got := VisibleWidth(line); got > limit {
				t.Fatalf("wrap %q at %d produced width %d line %q", text.String(), width, got, line)
			}
		}
		// Wrapping moves text to other lines; it must not drop or reorder any.
		want := strings.ReplaceAll(stripEscapes(text.String()), " ", "")
		got := strings.ReplaceAll(stripEscapes(strings.Join(lines, "")), " ", "")
		if want != got {
			t.Fatalf("wrap %q at %d changed the content: %q vs %q", text.String(), width, want, got)
		}
		truncated := TruncateToWidth(text.String(), width, "…", false)
		if got := VisibleWidth(truncated); got > limit {
			t.Fatalf("truncate %q at %d produced width %d: %q", text.String(), width, got, truncated)
		}
		// A truncation cuts between graphemes, never inside one.
		kept := strings.TrimSuffix(strings.ReplaceAll(stripEscapes(truncated), " ", ""), "…")
		if !strings.HasPrefix(want, kept) {
			t.Fatalf("truncate %q at %d split a grapheme: %q", text.String(), width, truncated)
		}
	}
}
