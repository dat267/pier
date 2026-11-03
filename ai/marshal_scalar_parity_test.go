package ai

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"testing"
)

// The scalar fast paths in MarshalJSON must equal the standard-library encode
// followed by canonicalization, including invalid UTF-8 and control escapes.
func TestMarshalJSONScalarsMatchEncoder(t *testing.T) {
	reference := func(value any) string {
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		encoded := buffer.Bytes()
		encoded = bytes.TrimSuffix(encoded, []byte("\n"))
		canonical, err := canonicalJavaScriptJSON(encoded)
		if err != nil {
			t.Fatal(err)
		}
		return string(canonical)
	}
	random := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ {
		length := random.Intn(40)
		raw := make([]byte, length)
		for j := range raw {
			raw[j] = byte(random.Intn(256))
		}
		value := string(raw)
		got, err := MarshalJSON(value)
		if err != nil || string(got) != reference(value) {
			t.Fatalf("string %q = %q/%v, want %q", value, got, err, reference(value))
		}
	}
	for _, value := range []any{nil, true, false, 0, int64(-1), int32(7), uint64(9), uint8(255),
		json.RawMessage(`{"10":1,"a":"\u2028"}`), json.RawMessage(nil)} {
		got, err := MarshalJSON(value)
		if err != nil || string(got) != reference(value) {
			t.Fatalf("%T %v = %q/%v, want %q", value, value, got, err, reference(value))
		}
	}
}
