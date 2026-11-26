package protocol

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"github.com/dat267/pier/ai"
)

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

// pi v1.4.0 packages/mcp/src/protocol/jsonrpc.ts isJsonRpcId rejects null.
// The public typed decoder must reject it too, rather than converting it to 0.
func TestJsonRpcIDUnmarshalRejectsNull(t *testing.T) {
	var id JsonRpcId
	if err := json.Unmarshal([]byte("null"), &id); err == nil {
		t.Fatalf("null decoded as request ID %+v", id)
	}
}

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

// pi v1.4.0 protocol/content.ts calls JSON.stringify on the decoded structured
// value. Regenerate: node mcp/protocol/testdata/structured/generate.mjs ./pi > mcp/protocol/testdata/structured/fixtures.json
func TestStructuredToolContentAgainstUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/structured/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct{ Input, Text string }
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		content := Content(&CallToolResult{StructuredContent: json.RawMessage(fixture.Input)})
		if len(content) != 1 {
			t.Fatalf("input %s: got %d blocks, want one", fixture.Input, len(content))
		}
		text, ok := content[0].(ai.TextContent)
		if !ok || text.Text != fixture.Text {
			t.Errorf("input %s: got %#v, upstream text = %q", fixture.Input, content[0], fixture.Text)
		}
	}
}

// pi v1.5.2 packages/mcp/src/protocol/types.ts and content.ts type a resource
// or resource link's `size` as a JavaScript number, so a fractional or large
// value is valid. Go's int64 rejected both at decode time.
func TestContentSizesAcceptJavaScriptNumbers(t *testing.T) {
	var resource Resource
	if err := json.Unmarshal([]byte(`{"uri":"file:///x","name":"x","size":1.5}`), &resource); err != nil {
		t.Fatalf("fractional resource size: %v", err)
	}
	if resource.Size == nil || *resource.Size != 1.5 {
		t.Fatalf("resource size = %v, want 1.5", resource.Size)
	}
	var result CallToolResult
	if err := json.Unmarshal([]byte(`{"content":[{"type":"resource_link","uri":"file:///x","name":"x","size":1e20}]}`), &result); err != nil {
		t.Fatalf("large resource link size: %v", err)
	}
	if len(result.Content) != 1 || result.Content[0].Size == nil || *result.Content[0].Size != 1e20 {
		t.Fatalf("resource link size = %v, want 1e20", result.Content)
	}
	// Control: an integer size still decodes to the same number.
	if err := json.Unmarshal([]byte(`{"uri":"file:///x","name":"x","size":2048}`), &resource); err != nil || *resource.Size != 2048 {
		t.Fatalf("integer size = %v err=%v", resource.Size, err)
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/protocol/jsonrpc.ts isJsonRpcId:
// `typeof value === "string" || (typeof value === "number" &&
// Number.isFinite(value))`. Number.isFinite is false for NaN and both
// infinities, so the float64 arm needs the conjunction: a caller that builds the
// map itself (rather than decoding it) can put a non-finite number in `id`.
func TestIsJsonRpcIdAcceptsStringsAndFiniteNumbersOnly(t *testing.T) {
	for _, tc := range []struct {
		value any
		want  bool
	}{
		{"abc", true},
		{"", true},
		{1.5, true},
		{0.0, true},
		{json.Number("2"), true},
		{json.Number("not-a-number"), false},
		{math.NaN(), false},
		{math.Inf(1), false},
		{math.Inf(-1), false},
		{nil, false},
		{true, false},
		{map[string]any{}, false},
	} {
		if got := IsJsonRpcId(tc.value); got != tc.want {
			t.Errorf("IsJsonRpcId(%#v) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

// JSON.stringify(-0) is "0", so a peer that sends the negative zero id gets "0"
// back. Go's encoder keeps the sign, and String() is also what the HTTP transport
// compares to decide a response answered its request, so -0 and 0 answered each
// other's streams. (jsonrpc.ts isJsonRpcId accepts any finite number.)
func TestJsonRpcIdNegativeZeroRendersAsZero(t *testing.T) {
	var id JsonRpcId
	if err := json.Unmarshal([]byte(`-0`), &id); err != nil {
		t.Fatal(err)
	}
	if got := id.String(); got != "0" {
		t.Fatalf("String() = %q, want %q", got, "0")
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != "0" {
		t.Fatalf("MarshalJSON = %s, want 0", encoded)
	}
	// It therefore matches a plain 0 id, which is what the transport needs.
	var zero JsonRpcId
	if err := json.Unmarshal([]byte(`0`), &zero); err != nil {
		t.Fatal(err)
	}
	if zero.String() != id.String() {
		t.Fatal("-0 and 0 must render the same")
	}
}

// A request needs an id, so a message without one is neither a request nor a
// response; it is a notification only when it also has no id key at all.
func TestJSONRPCShapeClassification(t *testing.T) {
	request := map[string]any{"jsonrpc": "2.0", "id": "1", "method": "ping"}
	notification := map[string]any{"jsonrpc": "2.0", "method": "ping"}
	response := map[string]any{"jsonrpc": "2.0", "id": "1", "result": map[string]any{}}
	noID := map[string]any{"jsonrpc": "2.0", "method": "ping"}
	both := map[string]any{"jsonrpc": "2.0", "id": "1", "result": nil, "error": map[string]any{"code": 1.0, "message": "m"}}
	badID := map[string]any{"jsonrpc": "2.0", "id": math.Inf(1), "method": "ping"}

	if !IsJsonRpcRequest(request) || IsJsonRpcNotification(request) {
		t.Error("a request with an id is a request and not a notification")
	}
	if !IsJsonRpcNotification(notification) || IsJsonRpcRequest(notification) {
		t.Error("a message without an id is a notification and not a request")
	}
	if !IsJsonRpcResponse(response) {
		t.Error("a result message is a response")
	}
	if IsJsonRpcRequest(noID) || IsJsonRpcResponse(noID) {
		t.Error("a message with no id is neither a request nor a response")
	}
	if IsJsonRpcResponse(both) {
		t.Error("a message with both result and error is not a response")
	}
	if IsJsonRpcRequest(badID) || IsJsonRpcNotification(badID) {
		t.Error("a non-finite id is not a valid id")
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/protocol/jsonrpc.ts isJsonRpcResponse
// accepts any `typeof message.error.code === "number"`, and JSON numbers are
// doubles there, so a fractional code is a valid response. `DecodeMessage` on the
// port reported a raw Go unmarshal error instead, which escapes the McpError
// contract every other rejection follows.
func TestDecodeMessageAcceptsAFractionalErrorCode(t *testing.T) {
	message, err := DecodeMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":1.5,"message":"m"}}`))
	if err != nil {
		t.Fatalf("DecodeMessage: %v", err)
	}
	response, ok := message.(*JsonRpcResponse)
	if !ok || response.Error == nil {
		t.Fatalf("message = %#v", message)
	}
	// The port models the code as an int (McpError.Code is an int too), so a
	// fractional code truncates. What matters is that it reaches this point: the
	// reference rejects nothing here, and the port must not leak a raw Go
	// unmarshal error out of DecodeMessage.
	if response.Error.Code != 1 {
		t.Fatalf("code = %v, want 1 (truncated)", response.Error.Code)
	}
}
