package coding

import (
	"encoding/json"
	"testing"
)

// pi v1.4.0 packages/coding-agent/src/core/mcp-servers.ts validateOAuth
// rejects a present `oauth: null` because only undefined is treated as absent.
func TestValidateMcpServerConfigRejectsNullOAuth(t *testing.T) {
	var raw any
	if err := json.Unmarshal([]byte(`{"url":"https://example.com/mcp","oauth":null}`), &raw); err != nil {
		t.Fatal(err)
	}
	_, err := ValidateMcpServerConfig("srv", raw)
	if err == nil || err.Error() != `server "srv": oauth must be an object` {
		t.Fatalf("ValidateMcpServerConfig() error = %v, want oauth object validation error", err)
	}
}
