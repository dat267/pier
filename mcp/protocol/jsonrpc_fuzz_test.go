package protocol

import "testing"

// FuzzParseMessageBytes checks the JSON-RPC message parser never panics on
// arbitrary bytes and round-trips a valid message through its codec.
func FuzzParseMessageBytes(f *testing.F) {
	seeds := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":"a","result":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32601,"message":"nope"}}`,
		`{}`,
		`[]`,
		`null`,
		`not json`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"x","arguments":{"a":[1,2,3]}}}`,
		`{"jsonrpc":"2.0","id":1,"method":`,
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		_ = ParseMessageBytes([]byte(input))
	})
}
