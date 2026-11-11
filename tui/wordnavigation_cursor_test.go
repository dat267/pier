package tui

import "testing"

// TestWordNavigationNormalizesItsCursor pins A509. The functions take UTF-16
// code-unit offsets; a cursor inside a surrogate pair is not a boundary, and
// the pin would answer from a lone surrogate, which Go cannot represent. Pier
// normalizes such a cursor to the pair's end and always returns a valid offset.
// Reference: pi v1.5.0 (f9a26365) packages/tui/src/word-navigation.ts.
func TestWordNavigationNormalizesItsCursor(t *testing.T) {
	texts := []string{
		"🇯🇵 flag",
		"👨‍👩‍👧‍👦 fam",
		"日本語 テキスト",
		"a🇯🇵b",
		"😀",
		"plain ascii words",
	}
	for _, text := range texts {
		limit := jsLength(text)
		for cursor := 0; cursor <= limit; cursor++ {
			backward := FindWordBackward(text, cursor)
			if backward < 0 || backward > cursor {
				t.Errorf("FindWordBackward(%q, %d) = %d, want in [0,%d]", text, cursor, backward, cursor)
			}
			forward := FindWordForward(text, cursor)
			if forward < cursor || forward > limit {
				t.Errorf("FindWordForward(%q, %d) = %d, want in [%d,%d]", text, cursor, forward, cursor, limit)
			}
		}
	}

	// A cursor inside either pair answers like the pair's end; offsets 5 and up
	// are ordinary boundaries of the words that follow.
	for _, cursor := range []int{1, 3} {
		backward := FindWordBackward("🇯🇵 flag", cursor)
		if backward != FindWordBackward("🇯🇵 flag", 2) {
			t.Errorf("FindWordBackward at mid-pair cursor %d = %d, want the normalized value %d",
				cursor, backward, FindWordBackward("🇯🇵 flag", 2))
		}
	}
	if got := FindWordBackward("🇯🇵 flag", 1); got != 0 {
		t.Errorf("FindWordBackward(\"🇯🇵 flag\", 1) = %d, want 0", got)
	}
}
