package coding

import (
	"encoding/json"
	"testing"
)

// FuzzValidateMcpServerConfig checks the config validator never panics on
// arbitrary JSON values or names.
func FuzzValidateMcpServerConfig(f *testing.F) {
	seeds := []string{
		`{"command":"npx","args":["-y","server"]}`,
		`{"url":"https://example.com/mcp","headers":{"A":"B"}}`,
		`{"type":"sse","url":"https://example.com/sse"}`,
		`{"exposure":"direct","toolExposure":{"read*":"direct","*":"codemode"}}`,
		`{"url":"nope"}`,
		`[]`,
		`null`,
		`{"auth":{"provider":"p"},"oauth":{"clientId":"c"}}`,
	}
	for _, seed := range seeds {
		f.Add("server-name", seed)
	}
	f.Fuzz(func(t *testing.T, name string, raw string) {
		var value any
		_ = json.Unmarshal([]byte(raw), &value)
		_, _ = ValidateMcpServerConfig(name, value)
	})
}
