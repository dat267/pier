package protocol

import (
	"encoding/json"
	"testing"
)

// pi v1.4.0 packages/mcp/src/protocol/jsonrpc.ts isJsonRpcId rejects null.
// The public typed decoder must reject it too, rather than converting it to 0.
func TestJsonRpcIDUnmarshalRejectsNull(t *testing.T) {
	var id JsonRpcId
	if err := json.Unmarshal([]byte("null"), &id); err == nil {
		t.Fatalf("null decoded as request ID %+v", id)
	}
}
