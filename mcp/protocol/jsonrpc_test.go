package protocol

import "testing"

// TestParseMessageBytesRejectsTrailingData pins upstream's JSON.parse
// behavior: a line must hold exactly one JSON value, so bytes after the
// message are an error rather than silently ignored.
func TestParseMessageBytesRejectsTrailingData(t *testing.T) {
	valid := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if err := ParseMessageBytes([]byte(valid)); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
	for _, input := range []string{
		valid + `{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		valid + `xyz`,
		valid + `,`,
	} {
		if err := ParseMessageBytes([]byte(input)); err == nil {
			t.Errorf("ParseMessageBytes(%q) accepted trailing data", input)
		}
	}
}

// TestDecodeMessageRejectsTrailingData covers the decode path the transports
// use, which validates before building the concrete message shape.
func TestDecodeMessageRejectsTrailingData(t *testing.T) {
	valid := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	if _, err := DecodeMessage([]byte(valid)); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}
	if _, err := DecodeMessage([]byte(valid + `{}`)); err == nil {
		t.Error("DecodeMessage accepted trailing data")
	}
}
