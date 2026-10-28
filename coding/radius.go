package coding

import (
	"encoding/json"
	"strings"

	"github.com/dat267/pier/ai"
)

// RadiusMcpURL is core/radius.ts RADIUS_MCP_URL (pi v1.4.0, 4a080252).
// Like upstream, it targets the hosted gateway, not PI_RADIUS_GATEWAY.
const RadiusMcpURL = ai.DefaultRadiusGateway + "/mcp"

// PrepareRadiusMcpServerConfig returns the global Radius server update offered
// by interactive-mode.ts offerRadiusMcpServer. A nil config means it already
// uses the Radius login. Project config is deliberately excluded.
func PrepareRadiusMcpServerConfig(agentDir string) (string, json.RawMessage) {
	loaded := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir})
	name := "radius"
	existing := false
	keys := []string{"url"}
	values := map[string]json.RawMessage{"url": mustMarshalJSON(RadiusMcpURL)}
	for _, server := range loaded.Servers {
		if server.Config.IsHTTP() && strings.TrimRight(server.Config.URL, "/") == RadiusMcpURL {
			if server.Config.Auth != nil && server.Config.Auth.Provider == "radius" {
				return "", nil
			}
			name = server.Name
			existing = true
			// Keep the original object, including unknown fields and key order,
			// as upstream spreads the validated (but unprojected) config.
			keys, values, _ = readJSONObjectOrdered(server.raw)
			break
		}
	}
	if !existing {
		for _, server := range loaded.Servers {
			if server.Name == name {
				name = "radius-mcp"
				break
			}
		}
	}
	if _, exists := values["auth"]; !exists {
		keys = append(keys, "auth")
	}
	values["auth"] = json.RawMessage(`{"provider":"radius"}`)
	delete(values, "oauth")
	return name, marshalOrderedObject(keys, values)
}
