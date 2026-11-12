package coding

import (
	"testing"

	"github.com/dat267/pier/ai"
)

// FuzzSessionFileEntry feeds a session file's line through the entry decoder. A
// session file is user-editable and shareable, so a malformed line must be an
// error rather than a panic, and an entry that decodes must marshal back for
// the next save.
func FuzzSessionFileEntry(f *testing.F) {
	seeds := []string{
		`{"type":"session","version":3,"id":"s","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/w"}`,
		`{"type":"message","id":"a","parentId":null,"timestamp":"2026-01-01T00:00:00.000Z","message":{"role":"user","timestamp":1,"content":"hi"}}`,
		`{"type":"usage","id":"u","parentId":"a","timestamp":"2026-01-01T00:00:00.000Z","kind":"cache_warm","usage":{"cost":{"total":0.1}}}`,
		`{"type":"custom","id":"c","parentId":null,"timestamp":"2026-01-01T00:00:00.000Z","customType":"note","content":"x"}`,
		`{"type":"compaction","id":"k","parentId":null,"timestamp":"2026-01-01T00:00:00.000Z"}`,
		`{}`, `null`, `[]`, `{"type":42}`, `{"type":"message"}`,
		`{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":""}]}}`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, line string) {
		entry, err := UnmarshalFileEntry(line)
		if err != nil || entry == nil {
			return
		}
		if _, err := ai.MarshalJSON(entry); err != nil {
			t.Fatalf("decoded %q but could not marshal it: %v", line, err)
		}
	})
}
