package coding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Port of extensions/mcp/config.ts's read side: MCP servers come from `mcp.json`
// in the agent directory and, for trusted projects, from `<project>/.pi/mcp.json`
// (project entries replace global ones with the same name). Both use the
// `mcpServers` shape shared by other MCP clients.
//
// The `/mcp` config writers (upstream updateMcpServerConfig,
// addMcpServerConfig, removeMcpServerConfig) belong to the `/mcp` command,
// which the port does not have yet; only loading is ported here.

// McpServerScope names where an entry came from.
type McpServerScope string

// The config sources.
const (
	McpScopeGlobal    McpServerScope = "global"
	McpScopeProject   McpServerScope = "project"
	McpScopeExtension McpServerScope = "extension"
)

// McpServerEntry is one configured server.
type McpServerEntry struct {
	Name   string
	Config *McpServerConfig
	// Source is the config file that defined the entry (or an extension path).
	Source string
	Scope  McpServerScope
}

// LoadedMcpConfig is the merged configuration (upstream LoadedMcpConfig).
type LoadedMcpConfig struct {
	// Servers includes disabled entries, so they can be enabled again.
	Servers []McpServerEntry
	// AutoEnableCodemode activates codemode when a codemode server connects;
	// nil means the default (true). A project value overrides the global one.
	AutoEnableCodemode *bool
	// Errors are per-file load and validation failures.
	Errors []string
}

// McpConfigLoadOptions are LoadMcpConfig inputs.
type McpConfigLoadOptions struct {
	AgentDir       string
	Cwd            string
	ProjectTrusted bool
}

// LoadMcpConfig reads the global (and, when trusted, project) MCP
// configuration (upstream loadMcpConfig).
func LoadMcpConfig(options McpConfigLoadOptions) LoadedMcpConfig {
	state := &mcpConfigState{servers: map[string]McpServerEntry{}, order: []string{}}
	readMcpConfigFile(filepath.Join(options.AgentDir, "mcp.json"), McpScopeGlobal, state)
	if options.ProjectTrusted {
		readMcpConfigFile(filepath.Join(options.Cwd, ConfigDirName, "mcp.json"), McpScopeProject, state)
	}
	servers := make([]McpServerEntry, 0, len(state.order))
	for _, name := range state.order {
		servers = append(servers, state.servers[name])
	}
	return LoadedMcpConfig{Servers: servers, AutoEnableCodemode: state.autoEnableCodemode, Errors: state.errors}
}

// mcpConfigState accumulates entries in first-definition order: a project entry
// replaces a global one in place, a new name appends (upstream Map.set order).
type mcpConfigState struct {
	servers            map[string]McpServerEntry
	order              []string
	autoEnableCodemode *bool
	errors             []string
}

// readMcpConfigFile merges one file into the state, recording errors instead of
// failing.
func readMcpConfigFile(path string, scope McpServerScope, state *mcpConfigState) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	keys, values, err := readJSONObjectOrdered(data)
	if err != nil {
		state.errors = append(state.errors, fmt.Sprintf("%s: %s", path, err.Error()))
		return
	}
	serversValue, hasServers := values["mcpServers"]
	var serverKeys []string
	serverValues := map[string]json.RawMessage{}
	if hasServers {
		if !isJSONObject(serversValue) {
			state.errors = append(state.errors, fmt.Sprintf("%s: expected an object with an \"mcpServers\" object", path))
			return
		}
		serverKeys, serverValues, err = readJSONObjectOrdered(serversValue)
		if err != nil {
			state.errors = append(state.errors, fmt.Sprintf("%s: %s", path, err.Error()))
			return
		}
	}
	_ = keys

	if raw, ok := values["autoEnableCodemode"]; ok {
		var enabled bool
		if err := json.Unmarshal(raw, &enabled); err == nil {
			state.autoEnableCodemode = &enabled
		} else {
			state.errors = append(state.errors, fmt.Sprintf("%s: autoEnableCodemode must be a boolean", path))
		}
	}

	for _, name := range serverKeys {
		raw := serverValues[name]
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			state.errors = append(state.errors, fmt.Sprintf("%s: server %q must be an object", path, name))
			continue
		}
		config, validationErr := ValidateMcpServerConfig(name, value)
		if validationErr != nil {
			state.errors = append(state.errors, fmt.Sprintf("%s: %s", path, validationErr.Error()))
			continue
		}
		// Names that differ only in `-` and `_` would share a namespace.
		for _, other := range state.order {
			if other != name && McpNamespace(other) == McpNamespace(name) {
				state.errors = append(state.errors, fmt.Sprintf("%s: server %q conflicts with %q", path, name, other))
				config = nil
				break
			}
		}
		if config == nil {
			continue
		}
		if scope == McpScopeProject && config.IsHTTP() && config.Auth != nil {
			state.errors = append(state.errors, fmt.Sprintf("%s: server %q: auth is only allowed in the global mcp.json", path, name))
			continue
		}
		// Keep the file's toolExposure pattern order (upstream walks the object
		// in insertion order; Go maps do not).
		if order := jsonObjectKeyOrder(raw, "toolExposure"); order != nil {
			config.toolExposureOrder = order
		}
		if _, exists := state.servers[name]; !exists {
			state.order = append(state.order, name)
		}
		state.servers[name] = McpServerEntry{Name: name, Config: config, Source: path, Scope: scope}
	}
}

// readJSONObjectOrdered decodes a JSON object, returning its keys in document
// order with each value preserved as raw JSON.
func readJSONObjectOrdered(data []byte) ([]string, map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, nil, fmt.Errorf("expected a JSON object")
	}
	keys := []string{}
	values := map[string]json.RawMessage{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, nil, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, nil, fmt.Errorf("expected a JSON object key")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		values[key] = value
	}
	return keys, values, nil
}

// isJSONObject reports whether raw is a JSON object.
func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == '{'
}

// jsonObjectKeyOrder returns the keys of raw's named object field in document
// order, or nil when the field is absent or not an object.
func jsonObjectKeyOrder(raw json.RawMessage, field string) []string {
	_, values, err := readJSONObjectOrdered(raw)
	if err != nil {
		return nil
	}
	value, ok := values[field]
	if !ok || !isJSONObject(value) {
		return nil
	}
	keys, _, err := readJSONObjectOrdered(value)
	if err != nil {
		return nil
	}
	return keys
}
