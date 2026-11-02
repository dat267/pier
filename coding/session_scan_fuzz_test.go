package coding

import (
	"encoding/json"
	"testing"
)

// scanSessionLine is the loader's fast shell over a session line; anything it
// accepts must be valid JSON with the same shell fields the decoder sees.
func TestSessionScanAcceptsOnlyValidJSON(t *testing.T) {
	cases := []string{
		`{"type":"message","id":"a","timestamp":"t","message":{"x":1}}`,
		`{"type":"message","id":"a","timestamp":"t","message":null}`,
		`{"type":"message","id":"a","timestamp":"t","message":"s"}`,
		`{"type":"message","id":"a","timestamp":"t","parentId":null,"message":[]}`,
		`{"type":"message","id":"a","timestamp":"t","parentId":"p","message":{"s":"\u0041"}}`,
		`{"type":"message","id":"a","timestamp":"t","message":{"s":"\q"}}`,
		`{"type":"message","id":"a","timestamp":1,"message":{}}`,
		`{"type":"message","id":"a","timestamp":"t","message":{}} trailing`,
		`{"type":"message","id":"a","id":"b","timestamp":"t","message":{}}`,
		`{"type":"message","id":"a","timestamp":"t","message":{}}`,
		`{"type":"message","id":"a","timestamp":"t","message":{"n":01}}`,
		`{"type":"message","id":"a","timestamp":"t","message":{"s":"a` + "\x01" + `b"}}`,
		`{"type":"message","id":"a","timestamp":"t","message":[1,2,]}`,
	}
	for _, line := range cases {
		meta, ok := scanSessionLine([]byte(line))
		var parsed map[string]json.RawMessage
		valid := json.Unmarshal([]byte(line), &parsed) == nil
		if ok && !valid {
			t.Errorf("scanner accepted invalid JSON %q: %+v", line, meta)
			continue
		}
		if !ok || !valid {
			continue
		}
		var typ string
		_ = json.Unmarshal(parsed["type"], &typ)
		if meta.typ != typ {
			t.Errorf("%q type = %q, want %q", line, meta.typ, typ)
		}
		var id, ts string
		_ = json.Unmarshal(parsed["id"], &id)
		_ = json.Unmarshal(parsed["timestamp"], &ts)
		if meta.id != id || meta.timestamp != ts {
			t.Errorf("%q id/timestamp = %q/%q, want %q/%q", line, meta.id, meta.timestamp, id, ts)
		}
		if parsed["message"] != nil && !json.Valid(meta.message) {
			t.Errorf("%q message slice is invalid: %q", line, meta.message)
		}
	}
}

func FuzzSessionScanAcceptsOnlyValidJSON(f *testing.F) {
	for _, seed := range []string{
		`{"type":"message","id":"a","timestamp":"t","message":{"x":[1,{"y":null}]}}`,
		`{"type":"message","id":"a","timestamp":"t","message":"s"}`,
		`{"type":"message","id":"a","timestamp":"t","parentId":"p","message":{}}`,
		`{"type":"message","id":"a","timestamp":1,"message":{}}`,
		`{"type":"message","id":"a","timestamp":"t","message":{}} junk`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, line []byte) {
		if len(line) > 2048 {
			return
		}
		meta, ok := scanSessionLine(line)
		if !ok {
			return
		}
		var parsed map[string]json.RawMessage
		if err := json.Unmarshal(line, &parsed); err != nil {
			t.Fatalf("scanner accepted invalid JSON %q: %v (%+v)", line, err, meta)
		}
		if meta.message != nil && !json.Valid(meta.message) {
			t.Fatalf("scanner accepted invalid message %q in %q", meta.message, line)
		}
	})
}

func TestSessionScanRejectsRawControlInStrings(t *testing.T) {
	for _, line := range []string{
		"{\"type\":\"mes\x01sage\",\"id\":\"a\",\"timestamp\":\"t\",\"message\":{}}",
		"{\"type\":\"message\",\"id\":\"a\x01\",\"timestamp\":\"t\",\"message\":{}}",
		"{\"type\":\"message\",\"id\":\"a\",\"timestamp\":\"t\x01\",\"message\":{}}",
	} {
		if meta, ok := scanSessionLine([]byte(line)); ok {
			t.Fatalf("scanner accepted raw control character in %q: %+v", line, meta)
		}
	}
}
