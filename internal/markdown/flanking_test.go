package markdown

import "testing"

// Upstream v1.4.0 b2d0da6fe (#10154): emphasis delimiters follow CommonMark's
// flanking rules, with CJK characters counting as punctuation so markers next to
// CJK punctuation still pair.
func TestEmphasisFlanking(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string // flattened: S(strong) E(em), text otherwise
	}{
		{"punctuation before closer, letter after", "**end.**Then next", "**end.**Then next"},
		{"punctuation before closer, space after", "**a.** b", "S(a.) b"},
		{"closer before punctuation", "a **b**, c", "a S(b), c"},
		{"intraword asterisk pairs", "a**b**c", "aS(b)c"},
		{"intraword underscore stays literal", "a_b_c and a__b__c", "a_b_c and a__b__c"},
		{"underscore run closes at the end", "_foo_bar_baz_", "E(foo_bar_baz)"},
		{"double underscore run closes at the end", "__foo__bar__baz__", "S(foo__bar__baz)"},
		{"CJK punctuation before closer", "**这是测试。**后面", "S(这是测试。)后面"},
		{"CJK punctuation before closer then digits", "**要点到此为止。**20 小时", "S(要点到此为止。)20 小时"},
		{"CJK quotes inside", "字**「引用」**字", "字S(「引用」)字"},
		{"CJK after em closer", "*斜体。*后面", "E(斜体。)后面"},
		{"nested strong in strong", "**a **b** c**", "S(a S(b) c)"},
		{"strong inside em across runs", "*foo**bar**baz*", "E(fooS(bar)baz)"},
		{"triple then single closer", "***foo** bar*", "E(S(foo) bar)"},
		{"mixed run lengths", "**foo, **bar****", "S(foo, S(bar))"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			blocks := Lex(tc.src)
			var got string
			var flat func(Inline) string
			flat = func(in Inline) string {
				switch in.Kind {
				case InlineText:
					return in.Text
				case InlineStrong:
					out := "S("
					for _, child := range in.Children {
						out += flat(child)
					}
					return out + ")"
				case InlineEmphasis:
					out := "E("
					for _, child := range in.Children {
						out += flat(child)
					}
					return out + ")"
				}
				return "?" + itoaInline(in.Kind) + "?"
			}
			for _, block := range blocks {
				for _, in := range block.Inlines {
					got += flat(in)
				}
			}
			if got != tc.want {
				t.Fatalf("%q = %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

func itoaInline(kind InlineKind) string {
	return string(rune('0' + kind))
}
