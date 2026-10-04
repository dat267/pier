package coding

import "testing"

// FuzzUnmarshalFileEntry checks a malformed session line never panics the
// loader.
func FuzzUnmarshalFileEntry(f *testing.F) {
	seeds := []string{
		`{}`,
		`{"type":"message","id":"a","timestamp":1}`,
		`{"type":"compaction","id":"b"}`,
		`{"type":"unknown","extra":1}`,
		`not json`,
		``,
		`{"type":"message","content":`,
		`null`,
		`[]`,
		`{"type":"message","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		_, _ = UnmarshalFileEntry(line)
	})
}
