package highlight

import (
	"os"
	"testing"
	"unicode/utf8"
)

// TestJavaMethodBeginMatchesTheRegexp pins D523's hand-written Java method
// begin to highlight.js 10.7.3's `(GENERIC_IDENT_RE\s+)+UNDERSCORE_IDENT_RE\s*\(`
// (pi v1.6.0, faac5858, languages/java.js): at every position of the golden
// sample and of inputs built around generics, `$`, digits, U+00C0-U+02B8
// identifiers, JavaScript-only whitespace and line terminators, the match
// end is the regexp's.
func TestJavaMethodBeginMatchesTheRegexp(t *testing.T) {
	const ident = `[À-ʸa-zA-Z_$][À-ʸa-zA-Z_$0-9]*`
	source := `(?:` + ident + `(<` + ident + `(\s*,\s*` + ident + `)*>)?\s+)+` + UnderscoreIdentRe + `\s*\(`
	re, err := compileEncodedPattern(`\A(?:`+encodeCodeUnitsString(source)+`)`, "m")
	if err != nil {
		t.Fatal(err)
	}
	sample, err := os.ReadFile("testdata/sample.java")
	if err != nil {
		t.Fatal(err)
	}
	inputs := []string{
		string(sample),
		"public static void main(String[] args) {}\nprivate List<String> names (int a);\n",
		"Map<K,V> get(\tMap<K , V> put(\nMap<K ,V > bad( List<A,B>x( a<b>c ( $x _y(",
		"int f(　x g h  (",
		"àé ñ( ʸx y( ˹z w( 9a b( a9 b9 ( a $ ( a b c d e (",
		"a\r\nb\r\n(  x\u000by\u000c(  final int count = 0;",
		"Foo<Bar<Baz>> f( Foo<> g( Foo<Bar> (",
	}
	var ws jsWorkspace
	checked, matched := 0, 0
	for _, input := range inputs {
		code := encodeCodeUnitsString(input)
		jsLines := nonLFLineEnds(code)
		for pos := 0; pos <= len(code); pos++ {
			if pos < len(code) && !utf8.RuneStart(code[pos]) {
				continue
			}
			want := -1
			if loc := re.findIndexWS(code[pos:], jsLines, &ws); loc != nil && loc[0] == 0 {
				want = pos + loc[1]
			}
			got := -1
			if end, ok := javaMethodBegin(code, pos); ok {
				got = end
				matched++
			}
			checked++
			if got != want {
				t.Fatalf("at %d of %q: javaMethodBegin ends at %d, the regexp at %d", pos, code, got, want)
			}
		}
	}
	if matched < 20 {
		t.Fatalf("only %d of %d positions matched", matched, checked)
	}
}
