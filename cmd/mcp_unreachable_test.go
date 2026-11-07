package cmd

import (
	"strings"
	"testing"

	"github.com/dat267/pier/coding"
)

// D185: startup reports the tools pier cannot expose with the MCP config's
// load errors (upstream ensureDiscoveryActive warns once the config is
// loaded). The warning follows the load errors and must not write into the
// config's own Errors slice.
func TestStartupMCPMessagesAppendTheUnreachableWarning(t *testing.T) {
	errs := make([]string, 1, 4)
	errs[0] = "mcp.json: bad entry"
	config := coding.LoadedMcpConfig{
		Servers: []coding.McpServerEntry{{Name: "gopls", Config: &coding.McpServerConfig{}}},
		Errors:  errs,
	}
	messages := appendMCPUnreachableWarning(config)
	if len(messages) != 2 || messages[0] != "mcp.json: bad entry" ||
		!strings.HasPrefix(messages[1], `MCP server "gopls" has tools with codemode`) {
		t.Fatalf("messages = %q, want the load error then the warning", messages)
	}
	if extended := errs[:2]; extended[1] != "" {
		t.Fatalf("the warning was written into the config's Errors backing array: %q", extended)
	}

	direct := coding.McpExposureDirect
	config.Servers[0].Config.Exposure = &direct
	if messages := appendMCPUnreachableWarning(config); len(messages) != 1 {
		t.Fatalf("direct-exposure messages = %q, want only the load error", messages)
	}
}
