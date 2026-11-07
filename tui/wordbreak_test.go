package tui

import "testing"

type wantSegment struct {
	text     string
	wordLike bool
}

// Upstream segments with Intl.Segmenter (pi v1.5.0 packages/tui/src/utils.ts
// getWordSegmenter), which is ICU 78's word break iterator: UAX #29 tailored
// by icu4c/source/data/brkitr/rules/word.txt, on Unicode 17. Expectations are
// Node 26's Intl.Segmenter output (ICU 78.3). They cover each tailored rule:
// WB3c-WB4 (ZWJ, Format and Extend, also at the start of the text), WB6/WB7
// MidLetter and MidNumLet (":", "·", "’"), WB11/WB12, WB13a/b (an underscore
// alone is not a word, two are), WB15/WB16 flag pairs, Hangul syllables
// chaining only with each other, Katakana leaving ALetter, Number_Letter
// letters and the rule statuses that decide isWordLike.
func TestWordSegmentsMatchIntlSegmenter(t *testing.T) {
	cases := []struct {
		text string
		want []wantSegment
	}{
		{"_", []wantSegment{{"_", false}}},
		{"__", []wantSegment{{"__", true}}},
		{"_a", []wantSegment{{"_a", true}}},
		{"a_", []wantSegment{{"a_", true}}},
		{"a_b", []wantSegment{{"a_b", true}}},
		{"._", []wantSegment{{".", false}, {"_", false}}},
		{"_1", []wantSegment{{"_1", true}}},
		{"1_", []wantSegment{{"1_", true}}},
		{"_한", []wantSegment{{"_", false}, {"한", true}}},
		{"한_", []wantSegment{{"한", true}, {"_", false}}},
		{"한국어", []wantSegment{{"한국어", true}}},
		{"é한", []wantSegment{{"é", true}, {"한", true}}},
		{"한1", []wantSegment{{"한", true}, {"1", true}}},
		{"1한", []wantSegment{{"1", true}, {"한", true}}},
		{"a한", []wantSegment{{"a", true}, {"한", true}}},
		{"한a", []wantSegment{{"한", true}, {"a", true}}},
		{"ｶﾀｶﾅ", []wantSegment{{"ｶﾀｶﾅ", true}}},
		{"αβγ", []wantSegment{{"αβγ", true}}},
		{"a.b", []wantSegment{{"a.b", true}}},
		{"a'b", []wantSegment{{"a'b", true}}},
		{"1.5", []wantSegment{{"1.5", true}}},
		{"1,5", []wantSegment{{"1,5", true}}},
		{"a:b", []wantSegment{{"a:b", true}}},
		{"a·b", []wantSegment{{"a·b", true}}},
		{"a’b", []wantSegment{{"a’b", true}}},
		{"1’000", []wantSegment{{"1’000", true}}},
		{"1.a", []wantSegment{{"1", true}, {".", false}, {"a", true}}},
		{"a.1", []wantSegment{{"a", true}, {".", false}, {"1", true}}},
		{"a\u200db", []wantSegment{{"a\u200db", true}}},
		{"😀😀", []wantSegment{{"😀", false}, {"😀", false}}},
		{"🇻🇳🇻🇳", []wantSegment{{"🇻🇳", false}, {"🇻🇳", false}}},
		{"a😀", []wantSegment{{"a", true}, {"😀", false}}},
		{"a\u0301b", []wantSegment{{"a\u0301b", true}}},
		{"\u0301a", []wantSegment{{"\u0301", false}, {"a", true}}},
		{"a\u00adb", []wantSegment{{"a\u00adb", true}}},
		{"a·b", []wantSegment{{"a·b", true}}},
		{"1_000", []wantSegment{{"1_000", true}}},
		{"a-b", []wantSegment{{"a", true}, {"-", false}, {"b", true}}},
		{"x2", []wantSegment{{"x2", true}}},
		{"2x", []wantSegment{{"2x", true}}},
		{"a'", []wantSegment{{"a", true}, {"'", false}}},
		{"'a", []wantSegment{{"'", false}, {"a", true}}},
		{"'", []wantSegment{{"'", false}}},
		{"a..b", []wantSegment{{"a", true}, {".", false}, {".", false}, {"b", true}}},
		{"a.'b", []wantSegment{{"a", true}, {".", false}, {"'", false}, {"b", true}}},
		{"١٢٣", []wantSegment{{"١٢٣", true}}},
		{"a١", []wantSegment{{"a١", true}}},
		{"₂", []wantSegment{{"₂", false}}},
		{"Ⅻ", []wantSegment{{"Ⅻ", true}}},
		{"ß", []wantSegment{{"ß", true}}},
		{"ǅx", []wantSegment{{"ǅx", true}}},
		{"i\u0307", []wantSegment{{"i\u0307", true}}},
		{"a\u2060b", []wantSegment{{"a\u2060b", true}}},
		{"ab\u200b cd", []wantSegment{{"ab", true}, {"\u200b", false}, {" ", false}, {"cd", true}}},
		{"한국어 문장3", []wantSegment{{"한국어", true}, {" ", false}, {"문장", true}, {"3", true}}},
	}
	for _, test := range cases {
		got := WordSegments(test.text)
		if len(got) != len(test.want) {
			t.Fatalf("WordSegments(%q) = %+v; want %+v", test.text, got, test.want)
		}
		for i, segment := range got {
			if segment.Segment != test.want[i].text || segment.IsWordLike != test.want[i].wordLike {
				t.Fatalf("WordSegments(%q) = %+v; want %+v", test.text, got, test.want)
			}
		}
	}
}

// Segments are contiguous: each Index is the byte offset of its text.
func TestWordSegmentsIndexIsTheByteOffset(t *testing.T) {
	text := "don’t 문장3.14 🇻🇳🇻🇳 áb"
	offset := 0
	for _, segment := range WordSegments(text) {
		if segment.Index != offset || text[offset:offset+len(segment.Segment)] != segment.Segment {
			t.Fatalf("segment %+v at offset %d", segment, offset)
		}
		offset += len(segment.Segment)
	}
	if offset != len(text) {
		t.Fatalf("segments cover %d of %d bytes", offset, len(text))
	}
}

// Word navigation (pi v1.5.0 packages/tui/src/word-navigation.ts) moves over
// the segments above; expectations from findWordBackward/findWordForward in
// Node 26. Cursors are UTF-16 offsets.
func TestWordNavigationFollowsIntlSegmenterWords(t *testing.T) {
	cases := []struct {
		text                      string
		cursor, backward, forward int
	}{
		{"don’t stop", 0, 0, 5},
		{"don’t stop", 10, 6, 10},
		{"foo._ bar", 9, 6, 9},
		{"._", 0, 0, 2},
		{"._", 2, 0, 2},
		{"문장3.14 x", 0, 0, 2},
		{"문장3.14 x", 7, 4, 8},
		{"été한국어", 0, 0, 3},
		{"a·b c", 0, 0, 3},
		{"🇻🇳🇻🇳 x", 0, 0, 8},
		{"🇻🇳🇻🇳 x", 8, 0, 10},
	}
	for _, test := range cases {
		if got := FindWordBackward(test.text, test.cursor); got != test.backward {
			t.Errorf("FindWordBackward(%q, %d) = %d; want %d", test.text, test.cursor, got, test.backward)
		}
		if got := FindWordForward(test.text, test.cursor); got != test.forward {
			t.Errorf("FindWordForward(%q, %d) = %d; want %d", test.text, test.cursor, got, test.forward)
		}
	}
}

// D399: ICU splits runs of Han and kana (and Thai, Lao, Khmer, Myanmar) with
// its dictionary break engines; Intl.Segmenter gives "日本語" "の" "テキスト".
// Pier keeps the run word.txt's $KanaKanji chain produces as one word.
func TestWordSegmentsKeepDictionaryRunsWhole(t *testing.T) {
	got := WordSegments("日本語のテキスト x")
	want := []wantSegment{{"日本語のテキスト", true}, {" ", false}, {"x", true}}
	if len(got) != len(want) {
		t.Fatalf("WordSegments = %+v; want %+v", got, want)
	}
	for i, segment := range got {
		if segment.Segment != want[i].text || segment.IsWordLike != want[i].wordLike {
			t.Fatalf("WordSegments = %+v; want %+v", got, want)
		}
	}
}
