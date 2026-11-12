package protocol

import "testing"

// FuzzDecodeMessage feeds an MCP peer's bytes through the decoder. A server is
// untrusted input: a panic here ends the session, and a message that decodes
// must marshal back (the client echoes ids and stacks error data).
func FuzzDecodeMessage(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":"a","result":{"content":[]}}`,
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"missing"}}`,
		`{"jsonrpc":"2.0","method":"notifications/progress","params":{"progress":1}}`,
		`{}`, `[]`, `null`, `{"jsonrpc":"2.0"}`, `{"jsonrpc":"2.0","id":null,"result":1}`,
		`{"jsonrpc":"2.0","id":1.5,"method":"m"}`, `{"jsonrpc":"2.0","id":-0,"method":"m"}`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data string) {
		message, err := DecodeMessage([]byte(data))
		if err != nil {
			return
		}
		if _, err := MarshalJSON(message); err != nil {
			t.Fatalf("decoded %q but could not marshal it: %v", data, err)
		}
	})
}
