package coding

import (
	"encoding/json"
	"strings"
	"testing"
)

// fastScanJSONString is the candidate: try the escape-free scanner first and
// fall back to the strict one. Both must accept exactly the same inputs.
func fastScanJSONString(s []byte, start int) (int, bool) {
	if _, n, ok := scanPlainStringBytes(s[start:]); ok {
		return start + n, true
	}
	return scanJSONStringStrict(s, start)
}

// TestSessionJSONStringScanMatchesStrictScan pins the fast dispatch to the
// strict scanner's accept set (pi v1.5.0 session-manager.ts parseSessionEntryLine
// only ever sees strings JSON.parse accepts).
func TestSessionJSONStringScanMatchesStrictScan(t *testing.T) {
	inputs := []string{
		`"plain"`, `""`, `"a\"b"`, `"a\\b"`, `"a\/b"`, `"a\u0041b"`, `"a\u00e9"`,
		`"a\uZZZZb"`, `"a\u00"`, `"a\u"`, `"a\qb"`, `"a` + "\x00" + `b"`,
		`"a` + "\x1f" + `b"`, `"a` + "\x7f" + `b"`, `"a` + "\xff" + `b"`,
		`"a` + "\xed\xa0\x80" + `b"`, `"é"`, `"a` + "\t" + `b"`, `"a` + "\n" + `b"`,
		`"unterminated`, `"`, ``, `"a"b`, `"a"` + " ", `"-"`, `"null"`, `"x\\"`,
		`"a` + "\u0080" + `b"`, `"a` + "\u2028" + `b"`, strings.Repeat("é", 40),
	}
	for _, in := range inputs {
		wantN, wantOK := scanJSONStringStrict([]byte(in), 0)
		gotN, gotOK := fastScanJSONString([]byte(in), 0)
		if gotOK != wantOK || (gotOK && gotN != wantN) {
			t.Errorf("%q: fast=(%d,%v) want=(%d,%v)", in, gotN, gotOK, wantN, wantOK)
		}
		if wantOK && !json.Valid([]byte(in[:wantN])) {
			t.Errorf("%q: strict scanner accepted invalid JSON", in)
		}
	}
}

// BenchmarkSessionJSONStringScan measures the dispatch over a realistic mix.
func BenchmarkSessionJSONStringScan(b *testing.B) {
	line := []byte(`{"role":"assistant","content":[{"type":"text","text":"Here is a fairly long answer with ASCII only, which is the common case in these session files."}],"timestamp":1762336467000}`)
	b.SetBytes(int64(len(line)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := scanJSONValueAt(line, 0, 0); !ok {
			b.Fatal("scan failed")
		}
	}
}

func BenchmarkSessionJSONStringScanStrict(b *testing.B) {
	s := []byte(`"Here is a fairly long answer with ASCII only, which is the common case in these session files."`)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := scanJSONStringStrict(s, 0); !ok {
			b.Fatal("scan failed")
		}
	}
}

func BenchmarkSessionJSONStringScanPlain(b *testing.B) {
	s := []byte(`"Here is a fairly long answer with ASCII only, which is the common case in these session files."`)
	b.SetBytes(int64(len(s)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, ok := scanPlainStringBytes(s); !ok {
			b.Fatal("scan failed")
		}
	}
}
