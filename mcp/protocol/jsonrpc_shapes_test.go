package protocol

import (
	"encoding/json"
	"testing"
)

// jsonrpc.ts isJsonRpcRequest / isJsonRpcNotification / isJsonRpcResponse: a
// request and a response carry a valid JSON-RPC id (string, or a finite
// number), a notification carries no id at all, and a response carries exactly
// one of result and error with a numeric code and a string message.
func TestJsonRpcMessageShapes(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		message any
		request bool
		notice  bool
		reponse bool
	}{
		{"string id request", map[string]any{"jsonrpc": "2.0", "id": "1", "method": "x"}, true, false, false},
		{"number id request", map[string]any{"jsonrpc": "2.0", "id": json.Number("7"), "method": "x"}, true, false, false},
		{"object id request", map[string]any{"jsonrpc": "2.0", "id": map[string]any{}, "method": "x"}, false, false, false},
		{"array id request", map[string]any{"jsonrpc": "2.0", "id": []any{1}, "method": "x"}, false, false, false},
		{"null id request", map[string]any{"jsonrpc": "2.0", "id": nil, "method": "x"}, false, false, false},
		{"method without id", map[string]any{"jsonrpc": "2.0", "method": "x"}, false, true, false},
		{"notification", map[string]any{"jsonrpc": "2.0", "method": "x"}, false, true, false},
		{"notification with id", map[string]any{"jsonrpc": "2.0", "id": "1", "method": "x"}, true, false, false},
		{"result response", map[string]any{"jsonrpc": "2.0", "id": "1", "result": map[string]any{}}, false, false, true},
		{"object id response", map[string]any{"jsonrpc": "2.0", "id": map[string]any{}, "result": 1}, false, false, false},
		{"error response", map[string]any{"jsonrpc": "2.0", "id": "1", "error": map[string]any{"code": json.Number("-32601"), "message": "no"}}, false, false, true},
		{"both result and error", map[string]any{"jsonrpc": "2.0", "id": "1", "result": 1, "error": map[string]any{"code": json.Number("-1"), "message": "no"}}, false, false, false},
		{"non-numeric error code", map[string]any{"jsonrpc": "2.0", "id": "1", "error": map[string]any{"code": "x", "message": "no"}}, false, false, false},
		{"error without message", map[string]any{"jsonrpc": "2.0", "id": "1", "error": map[string]any{"code": json.Number("-1")}}, false, false, false},
		{"bad version", map[string]any{"jsonrpc": "1.0", "id": "1", "method": "x"}, false, false, false},
		{"non-object", "not a message", false, false, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsJsonRpcRequest(testCase.message); got != testCase.request {
				t.Fatalf("IsJsonRpcRequest = %v, want %v", got, testCase.request)
			}
			if got := IsJsonRpcNotification(testCase.message); got != testCase.notice {
				t.Fatalf("IsJsonRpcNotification = %v, want %v", got, testCase.notice)
			}
			if got := IsJsonRpcResponse(testCase.message); got != testCase.reponse {
				t.Fatalf("IsJsonRpcResponse = %v, want %v", got, testCase.reponse)
			}
		})
	}
}
