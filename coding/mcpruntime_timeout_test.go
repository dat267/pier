package coding

import (
	"math"
	"testing"
)

// pi v1.4.0 coding-agent/extensions/mcp/runtime.ts converts timeout seconds to
// milliseconds; mcp/client.ts keeps positive deadlines and the timer clamps
// sub-millisecond or oversized delays rather than disabling them.
func TestMcpServerTimeoutMsClampsConfiguredSeconds(t *testing.T) {
	for _, test := range []struct {
		name    string
		seconds float64
		want    int64
	}{
		{name: "sub-millisecond", seconds: 0.000001, want: 1},
		{name: "ordinary fractional", seconds: 1.5, want: 1500},
		{name: "above int64 range", seconds: 1e16, want: math.MaxInt64},
		{name: "non-finite milliseconds from finite seconds", seconds: 1e308, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := NewMcpServerConnection(McpServerConnectionOptions{Entry: McpServerEntry{
				Name: "test",
				Config: &McpServerConfig{
					McpServerConfigBase: McpServerConfigBase{TimeoutSeconds: &test.seconds},
				},
			}})
			if got := connection.TimeoutMs(); got != test.want {
				t.Fatalf("TimeoutMs() = %d, want %d", got, test.want)
			}
		})
	}
}
