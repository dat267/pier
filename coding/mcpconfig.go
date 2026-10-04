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
	// Override is the project `mcp.json` that overrides only this global
	// server's enabled/exposure/toolExposure (upstream McpServerEntry.override).
	Override string

	// raw is the entry's config object as written, kept so a project override
	// merges with the global entry before revalidation.
	raw json.RawMessage
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
	// ProjectConfig is the project `mcp.json` path when the project is trusted
	// (upstream LoadedMcpConfig.projectConfig).
	ProjectConfig string
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
	projectConfig := ""
	if options.ProjectTrusted {
		projectConfig = filepath.Join(options.Cwd, ConfigDirName, "mcp.json")
		readMcpConfigFile(projectConfig, McpScopeProject, state)
	}
	servers := make([]McpServerEntry, 0, len(state.order))
	for _, name := range state.order {
		servers = append(servers, state.servers[name])
	}
	return LoadedMcpConfig{Servers: servers, AutoEnableCodemode: state.autoEnableCodemode, Errors: state.errors, ProjectConfig: projectConfig}
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
		// A project entry without command, url, or type overrides only the
		// enabled/exposure/toolExposure of the global server with the same name.
		if scope == McpScopeProject && isMcpOverrideValue(value) {
			applyMcpOverride(path, name, raw, state)
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
		state.servers[name] = McpServerEntry{Name: name, Config: config, Source: path, Scope: scope, raw: raw}
	}
}

// isMcpOverrideValue reports whether a project entry overrides a server defined
// elsewhere instead of defining one (upstream isOverride).
func isMcpOverrideValue(value any) bool {
	record, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, hasCommand := record["command"]
	_, hasURL := record["url"]
	_, hasType := record["type"]
	return !hasCommand && !hasURL && !hasType
}

// mcpOverrideKeys are the only keys a project override may set.
var mcpOverrideKeys = map[string]bool{"enabled": true, "exposure": true, "toolExposure": true}

// applyMcpOverride merges a project override into the global entry with the
// same name (upstream readConfigFile's override branch).
func applyMcpOverride(path, name string, raw json.RawMessage, state *mcpConfigState) {
	base, ok := state.servers[name]
	if !ok {
		state.errors = append(state.errors, fmt.Sprintf("%s: server %q needs \"command\" or \"url\", or a global server to override", path, name))
		return
	}
	keys, _, err := readJSONObjectOrdered(raw)
	if err != nil {
		state.errors = append(state.errors, fmt.Sprintf("%s: server %q must be an object", path, name))
		return
	}
	for _, key := range keys {
		if !mcpOverrideKeys[key] {
			state.errors = append(state.errors, fmt.Sprintf("%s: server %q: an override can only set enabled, exposure, toolExposure", path, name))
			return
		}
	}
	merged, err := mergeJSONObjects(base.raw, raw)
	if err != nil {
		state.errors = append(state.errors, fmt.Sprintf("%s: server %q: %s", path, name, err.Error()))
		return
	}
	var mergedValue any
	if err := json.Unmarshal(merged, &mergedValue); err != nil {
		state.errors = append(state.errors, fmt.Sprintf("%s: server %q: %s", path, name, err.Error()))
		return
	}
	config, validationErr := ValidateMcpServerConfig(name, mergedValue)
	if validationErr != nil {
		state.errors = append(state.errors, fmt.Sprintf("%s: %s", path, validationErr.Error()))
		return
	}
	if order := jsonObjectKeyOrder(raw, "toolExposure"); order != nil {
		config.toolExposureOrder = order
	} else {
		config.toolExposureOrder = base.Config.toolExposureOrder
	}
	base.Config = config
	base.Override = path
	state.servers[name] = base
}

// mergeJSONObjects merges override's keys over base (shallow).
func mergeJSONObjects(base, override json.RawMessage) (json.RawMessage, error) {
	baseObject := map[string]any{}
	if len(base) > 0 {
		if err := json.Unmarshal(base, &baseObject); err != nil {
			return nil, err
		}
	}
	overrideObject := map[string]any{}
	if err := json.Unmarshal(override, &overrideObject); err != nil {
		return nil, err
	}
	for key, value := range overrideObject {
		baseObject[key] = value
	}
	return json.Marshal(baseObject)
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
