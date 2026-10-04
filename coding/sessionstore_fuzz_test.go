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
		entry, err := UnmarshalFileEntry(line)
		if err != nil || entry == nil {
			return
		}
		// Re-marshaling a parsed entry and parsing it back must reproduce the
		// same bytes: fork and branch rewrites depend on the codec being a fixed
		// point. (The struct's unexported raw cache is excluded on purpose.)
		encoded, err := MarshalFileEntry(*entry)
		if err != nil {
			t.Fatalf("MarshalFileEntry failed for %q: %v", line, err)
		}
		again, err := UnmarshalFileEntry(encoded)
		if err != nil || again == nil {
			t.Fatalf("re-parse of %q failed: %v (%q)", line, err, encoded)
		}
		reencoded, err := MarshalFileEntry(*again)
		if err != nil || reencoded != encoded {
			t.Fatalf("entry codec is not a fixed point for %q:\n first = %q\n again = %q (%v)", line, encoded, reencoded, err)
		}
	})
}
