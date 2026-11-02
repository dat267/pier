package coding

import (
	"encoding/json"
	"testing"
)

// scanListingMessage is the session picker's fast view of a stored message.
// When it accepts a message the typed decoder also accepts, every field it
// reports must match; for messages the decoder rejects it may still report
// metadata (D225), so those are skipped.
func FuzzScanListingMessageMatchesDecode(f *testing.F) {
	for _, seed := range []string{
		`{"role":"user","content":"hi","timestamp":1}`,
		`{"role":"user","content":[{"type":"text","text":"hi"}],"timestamp":1}`,
		`{"role":"assistant","content":[{"type":"text","text":"hi"}],"timestamp":1}`,
		`{"role":"toolResult","content":[{"type":"text","text":"hi"}],"timestamp":1}`,
		`{"role":"user","content":"\u2028","timestamp":1}`,
		`{"role":"user","content":"\q","timestamp":1}`,
		`{"role":"user","content":{"x":1},"timestamp":1}`,
		`{"timestamp":1,"role":"user","content":"hi"}`,
		"{\"role\":\"user\",\"content\":\"\x95\",\"timestamp\":1}",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1024 {
			return
		}
		listing := scanListingMessage(data)
		// Compare against an exact-case object, like the JavaScript properties
		// upstream reads; the Go decoder's case-insensitive field matching is a
		// separate artifact.
		var object map[string]json.RawMessage
		if err := json.Unmarshal(data, &object); err != nil {
			if listing.ok {
				t.Fatalf("listing accepted invalid JSON %q: %+v", data, listing)
			}
			return
		}
		if !listing.ok {
			return
		}
		var role string
		if raw, ok := object["role"]; ok {
			_ = json.Unmarshal(raw, &role)
		}
		if listing.role != role {
			t.Fatalf("role for %q = %q want %q", data, listing.role, role)
		}
		var timestamp int64
		hasTimestamp := false
		if raw, ok := object["timestamp"]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, &timestamp); err == nil {
				hasTimestamp = true
			}
		}
		if listing.hasTimestamp != hasTimestamp || (hasTimestamp && listing.timestamp != timestamp) {
			t.Fatalf("timestamp for %q = %d/%v want %d/%v", data, listing.timestamp, listing.hasTimestamp, timestamp, hasTimestamp)
		}
		if role == "user" {
			if raw, ok := object["content"]; ok && len(raw) > 0 && raw[0] == '"' {
				var text string
				if err := json.Unmarshal(raw, &text); err == nil && listing.text != text {
					t.Fatalf("text for %q = %q want %q", data, listing.text, text)
				}
			}
		}
	})
}
