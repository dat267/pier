package coding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dat267/pier/ai"
)

var mcpConfigIndent = regexp.MustCompile(`(?m)^([ \t]+)\S`)

// AddMcpServerConfig ports extensions/mcp/config.ts addMcpServerConfig
// (pi v1.4.0, 4a080252). It keeps other entries, key order and indentation.
func AddMcpServerConfig(path, name string, config json.RawMessage) (bool, error) {
	text, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if os.IsNotExist(err) {
		text = []byte("{}")
	}
	text = []byte(strings.TrimPrefix(string(text), "\uFEFF"))
	// The ordered field reader is a projection, not a whole-document parser.
	// Reject trailing data before rewriting any user configuration.
	if !json.Valid(text) {
		return false, fmt.Errorf("%s: invalid JSON", path)
	}
	keys, values, err := readJSONObjectOrdered(text)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	serverKeys := []string{}
	servers := map[string]json.RawMessage{}
	if raw, ok := values["mcpServers"]; ok {
		serverKeys, servers, err = readJSONObjectOrdered(raw)
		if err != nil {
			return false, fmt.Errorf("%s: expected an object with an \"mcpServers\" object", path)
		}
	} else {
		keys = append(keys, "mcpServers")
	}
	_, replaced := servers[name]
	if !replaced {
		serverKeys = append(serverKeys, name)
	}
	servers[name] = config
	values["mcpServers"] = marshalOrderedObject(serverKeys, servers)
	encoded, err := ai.MarshalJSON(marshalOrderedObject(keys, values))
	if err != nil {
		return false, err
	}
	indent := "  "
	if match := mcpConfigIndent.FindSubmatch(text); match != nil {
		indent = string(match[1])
		if len(indent) > 10 {
			indent = indent[:10] // JSON.stringify's space argument is capped at 10.
		}
	}
	var output bytes.Buffer
	if err := json.Indent(&output, encoded, "", indent); err != nil {
		return false, err
	}
	output.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
		return false, err
	}
	return replaced, writePrivateFile(path, output.Bytes())
}
