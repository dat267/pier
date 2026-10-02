package coding

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Port of core/mcp-servers.ts: the `mcpServers` config shape, its validation
// and the per-tool exposure resolution. Upstream's registry of
// extension-registered servers has no port counterpart (extension mechanics
// are out of scope), so only the config side lives here; the MCP runtime
// consumes it.

// McpExposure is how a server's tools reach the model.
//
//   - codemode: callable from codemode scripts, not declared to the model.
//   - deferred: declared after tool_search loads them (needs codemode).
//   - direct: declared to the model like any built-in tool.
//   - hidden: registered but unreachable.
type McpExposure string

// The exposures (upstream MCP_EXPOSURES).
const (
	McpExposureCodemode McpExposure = "codemode"
	McpExposureDeferred McpExposure = "deferred"
	McpExposureDirect   McpExposure = "direct"
	McpExposureHidden   McpExposure = "hidden"
)

var mcpExposures = []McpExposure{
	McpExposureCodemode, McpExposureDeferred, McpExposureDirect, McpExposureHidden,
}

// mcpExposureAliases maps retired exposure names onto their current name.
var mcpExposureAliases = map[string]McpExposure{"codemode-deferred": McpExposureCodemode}

// McpServerConfigBase carries the fields every server config shares.
type McpServerConfigBase struct {
	// Exposure defaults to codemode at tool-exposure time.
	Exposure *McpExposure `json:"exposure,omitempty"`
	// ToolExposure overrides Exposure per tool name or `*` pattern.
	ToolExposure map[string]McpExposure `json:"toolExposure,omitempty"`
	// toolExposureOrder lists ToolExposure's keys in match order. Go maps have
	// no order and upstream's resolver takes the first matching pattern in
	// object order; the loader fills this from the file.
	toolExposureOrder []string
	// Description names what the server offers, for the system prompt section.
	Description *string `json:"description,omitempty"`
	// Enabled false keeps the entry without connecting.
	Enabled *bool `json:"enabled,omitempty"`
	// TimeoutSeconds is the per-request timeout (default 60).
	TimeoutSeconds *float64 `json:"timeout,omitempty"`
}

// McpOAuthConfig is the OAuth client configuration for a server without
// dynamic client registration.
type McpOAuthConfig struct {
	ClientID              *string `json:"clientId,omitempty"`
	ClientSecret          *string `json:"clientSecret,omitempty"`
	CallbackPort          *int    `json:"callbackPort,omitempty"`
	CallbackURL           *string `json:"callbackUrl,omitempty"`
	Scope                 *string `json:"scope,omitempty"`
	ClientName            *string `json:"clientName,omitempty"`
	AuthServerMetadataURL *string `json:"authServerMetadataUrl,omitempty"`
}

// McpProviderAuth sends the token of a `/login` provider instead of using
// OAuth. Only allowed in the global mcp.json.
type McpProviderAuth struct {
	Provider string `json:"provider"`
}

// McpServerConfig is one validated `mcpServers` entry. Exactly one transport is
// set: URL (streamable HTTP) or Command (stdio).
type McpServerConfig struct {
	McpServerConfigBase

	// HTTP transport.
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	OAuth   *McpOAuthConfig   `json:"oauth,omitempty"`
	Auth    *McpProviderAuth  `json:"auth,omitempty"`

	// Stdio transport.
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`
}

// IsHTTP reports the streamable-HTTP transport.
func (c *McpServerConfig) IsHTTP() bool { return c.URL != "" }

// mcpServerNamePattern is upstream SERVER_NAME.
var mcpServerNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// mcpLoopbackHosts are the hosts on which http is acceptable. Go's
// url.Hostname strips the brackets from an IPv6 literal, so both spellings are
// listed (upstream compares WHATWG hostnames, which keep them).
var mcpLoopbackHosts = []string{"localhost", "127.0.0.1", "[::1]", "::1"}

// McpNamespace is a server's tool namespace: `mcp__<server>` with `-` replaced
// by `_` (upstream mcpNamespace).
func McpNamespace(server string) string {
	return "mcp__" + strings.ReplaceAll(server, "-", "_")
}

// IsLoopbackRedirectURI reports whether a redirect URI can be served by the
// loopback callback server (upstream isLoopbackRedirectUri).
func IsLoopbackRedirectURI(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	if !isMcpLoopbackHost(parsed.Hostname()) {
		return false
	}
	return parsed.RawQuery == "" && parsed.Fragment == ""
}

func isMcpLoopbackHost(host string) bool {
	for _, candidate := range mcpLoopbackHosts {
		if host == candidate {
			return true
		}
	}
	return false
}

// mcpToolPatternRegexp compiles a `*`-pattern into an anchored regexp (upstream
// toolPatternRegExp). Only `*` is special.
func mcpToolPatternRegexp(pattern string) *regexp.Regexp {
	var builder strings.Builder
	for index, part := range strings.Split(pattern, "*") {
		if index > 0 {
			builder.WriteString(".*")
		}
		builder.WriteString(regexp.QuoteMeta(part))
	}
	return regexp.MustCompile("^" + builder.String() + "$")
}

// McpToolExposure resolves one tool's exposure: an exact `toolExposure` entry,
// else the first matching pattern in object order, else the server's exposure,
// else codemode (upstream getMcpToolExposure).
func McpToolExposure(config *McpServerConfig, toolName string) McpExposure {
	if config == nil {
		return McpExposureCodemode
	}
	if exact, ok := config.ToolExposure[toolName]; ok {
		return exact
	}
	// Go maps have no insertion order, so the patterns are matched in the
	// order the config file listed them (preserved in toolExposureOrder).
	for _, pattern := range config.toolExposureOrder {
		exposure, ok := config.ToolExposure[pattern]
		if !ok || !strings.Contains(pattern, "*") {
			continue
		}
		if mcpToolPatternRegexp(pattern).MatchString(toolName) {
			return exposure
		}
	}
	if config.Exposure != nil {
		return *config.Exposure
	}
	return McpExposureCodemode
}

// ValidateMcpServerConfig validates one `mcpServers` entry (upstream
// validateMcpServerConfig). It returns the config with exposure aliases
// resolved, or an error whose message matches upstream's.
func ValidateMcpServerConfig(name string, raw any) (*McpServerConfig, error) {
	if !mcpServerNamePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid server name %q (use letters, digits, \"_\" and \"-\")", name)
	}
	value, ok := mcpRecord(raw)
	if !ok {
		return nil, fmt.Errorf("server %q must be an object", name)
	}
	value = resolveMcpExposureAliases(value)

	serverType, _ := value["type"].(string)
	exposureNames := make([]string, 0, len(mcpExposures))
	for _, exposure := range mcpExposures {
		exposureNames = append(exposureNames, fmt.Sprintf("%q", exposure))
	}
	exposures := strings.Join(exposureNames, ", ")

	if exposure, ok := value["exposure"]; ok && !isMcpExposure(exposure) {
		return nil, fmt.Errorf("server %q: exposure must be one of %s", name, exposures)
	}
	if toolExposure, ok := value["toolExposure"]; ok {
		record, isRecord := mcpRecord(toolExposure)
		if !isRecord {
			return nil, fmt.Errorf("server %q: toolExposure must map tool names to exposures", name)
		}
		for _, tool := range sortedKeys(record) {
			if !isMcpExposure(record[tool]) {
				return nil, fmt.Errorf("server %q: toolExposure %q must be one of %s", name, tool, exposures)
			}
		}
	}
	if enabled, ok := value["enabled"]; ok && !isBool(enabled) {
		return nil, fmt.Errorf("server %q: enabled must be a boolean", name)
	}
	if _, ok := value["description"]; ok && !isString(value["description"]) {
		return nil, fmt.Errorf("server %q: description must be a string", name)
	}
	if timeout, ok := value["timeout"]; ok {
		number, isNumber := mcpNumber(timeout)
		if !isNumber || number <= 0 {
			return nil, fmt.Errorf("server %q: timeout must be a positive number of seconds", name)
		}
	}
	if serverType == "sse" {
		return nil, fmt.Errorf("server %q: legacy SSE transport is not supported; use the streamable HTTP URL", name)
	}

	config := &McpServerConfig{}
	if err := applyMcpServerConfigBase(config, value); err != nil {
		return nil, fmt.Errorf("server %q: %w", name, err)
	}

	if serverURL, ok := value["url"].(string); ok && (serverType == "" || serverType == "http" || serverType == "streamable-http") {
		parsed, err := url.Parse(serverURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("server %q: url must be an http or https URL", name)
		}
		if headers, ok := value["headers"]; ok {
			record, isRecord := mcpStringRecord(headers)
			if !isRecord {
				return nil, fmt.Errorf("server %q: headers must map names to strings", name)
			}
			config.Headers = record
		}
		oauth, oauthError := validateMcpOAuth(value["oauth"])
		if oauthError != "" {
			return nil, fmt.Errorf("server %q: %s", name, oauthError)
		}
		config.OAuth = oauth
		if authValue, ok := value["auth"]; ok {
			record, isRecord := mcpRecord(authValue)
			provider, isString := record["provider"].(string)
			if !isRecord || !isString || provider == "" {
				return nil, fmt.Errorf("server %q: auth.provider must be a provider name", name)
			}
			if parsed.Scheme != "https" && !isMcpLoopbackHost(parsed.Hostname()) {
				return nil, fmt.Errorf("server %q: auth requires an https URL, or http on localhost, 127.0.0.1, or [::1]", name)
			}
			config.Auth = &McpProviderAuth{Provider: provider}
		}
		config.URL = serverURL
		return config, nil
	}

	if command, ok := value["command"].(string); ok && (serverType == "" || serverType == "stdio") {
		if argsValue, ok := value["args"]; ok {
			args, isStringArray := mcpStringArray(argsValue)
			if !isStringArray {
				return nil, fmt.Errorf("server %q: args must be an array of strings", name)
			}
			config.Args = args
		}
		if envValue, ok := value["env"]; ok {
			record, isRecord := mcpStringRecord(envValue)
			if !isRecord {
				return nil, fmt.Errorf("server %q: env must map names to strings", name)
			}
			config.Env = record
		}
		if cwd, ok := value["cwd"]; ok && !isString(cwd) {
			return nil, fmt.Errorf("server %q: cwd must be a string", name)
		}
		if cwd, ok := value["cwd"].(string); ok {
			config.Cwd = cwd
		}
		config.Command = command
		return config, nil
	}
	return nil, fmt.Errorf("server %q needs either \"command\" (stdio) or \"url\" (streamable HTTP)", name)
}

// applyMcpServerConfigBase copies the shared fields.
func applyMcpServerConfigBase(config *McpServerConfig, value map[string]any) error {
	if exposure, ok := value["exposure"].(string); ok {
		resolved := McpExposure(exposure)
		config.Exposure = &resolved
	}
	if toolExposure, ok := mcpRecord(value["toolExposure"]); ok {
		config.ToolExposure = map[string]McpExposure{}
		for _, tool := range sortedKeys(toolExposure) {
			exposure, _ := toolExposure[tool].(string)
			config.ToolExposure[tool] = McpExposure(exposure)
		}
		// Deterministic fallback: the loader replaces this with the file's own
		// pattern order (upstream walks the object in insertion order).
		config.toolExposureOrder = sortedKeys(toolExposure)
	}
	if description, ok := value["description"].(string); ok {
		config.Description = &description
	}
	if enabled, ok := value["enabled"].(bool); ok {
		config.Enabled = &enabled
	}
	if timeout, ok := mcpNumber(value["timeout"]); ok {
		config.TimeoutSeconds = &timeout
	}
	return nil
}

// validateMcpOAuth validates the oauth object (upstream validateOAuth). An
// empty string means valid.
func validateMcpOAuth(value any) (*McpOAuthConfig, string) {
	if value == nil {
		return nil, ""
	}
	record, ok := mcpRecord(value)
	if !ok {
		return nil, "oauth must be an object"
	}
	config := &McpOAuthConfig{}
	if clientID, ok := record["clientId"]; ok {
		text, isString := clientID.(string)
		if !isString {
			return nil, "oauth.clientId must be a string"
		}
		config.ClientID = &text
	}
	if clientSecret, ok := record["clientSecret"]; ok {
		text, isString := clientSecret.(string)
		if !isString {
			return nil, "oauth.clientSecret must be a string"
		}
		config.ClientSecret = &text
	}
	var port *int
	if portValue, ok := record["callbackPort"]; ok {
		number, isNumber := mcpNumber(portValue)
		if !isNumber || number != float64(int(number)) || number < 1 || number > 65535 {
			return nil, "oauth.callbackPort must be a port number"
		}
		converted := int(number)
		port = &converted
		config.CallbackPort = port
	}
	if callbackURL, ok := record["callbackUrl"]; ok {
		text, isString := callbackURL.(string)
		if !isString || !IsLoopbackRedirectURI(text) {
			return nil, "oauth.callbackUrl must be an http URI on localhost, 127.0.0.1, or [::1] without query or fragment"
		}
		if parsed, err := url.Parse(text); err == nil && parsed.Port() != "" && port != nil {
			urlPort, conversionErr := strconv.Atoi(parsed.Port())
			if conversionErr != nil || urlPort != *port {
				return nil, "oauth.callbackUrl and oauth.callbackPort name different ports"
			}
		}
		config.CallbackURL = &text
	}
	if scope, ok := record["scope"]; ok {
		text, isString := scope.(string)
		if !isString {
			return nil, "oauth.scope must be a string"
		}
		config.Scope = &text
	}
	if clientName, ok := record["clientName"]; ok {
		text, isString := clientName.(string)
		if !isString || strings.TrimSpace(text) == "" {
			return nil, "oauth.clientName must be a non-empty string"
		}
		config.ClientName = &text
	}
	if metadataURL, ok := record["authServerMetadataUrl"]; ok {
		text, isString := metadataURL.(string)
		parsed, err := url.Parse(text)
		valid := isString && err == nil && (parsed.Scheme == "https" || (parsed.Scheme == "http" && isMcpLoopbackHost(parsed.Hostname())))
		if !valid {
			return nil, "oauth.authServerMetadataUrl must be an https URL, or http on localhost, 127.0.0.1, or [::1]"
		}
		config.AuthServerMetadataURL = &text
	}
	return config, ""
}

// resolveMcpExposureAliases replaces retired exposure names (upstream
// resolveExposureAliases).
func resolveMcpExposureAliases(value map[string]any) map[string]any {
	resolved := make(map[string]any, len(value))
	for key, entry := range value {
		resolved[key] = entry
	}
	if exposure, ok := value["exposure"]; ok {
		resolved["exposure"] = resolveMcpExposureAlias(exposure)
	}
	if toolExposure, ok := mcpRecord(value["toolExposure"]); ok {
		converted := make(map[string]any, len(toolExposure))
		for tool, entry := range toolExposure {
			converted[tool] = resolveMcpExposureAlias(entry)
		}
		resolved["toolExposure"] = converted
	}
	return resolved
}

// resolveMcpExposureAlias maps an alias to its current name; other values are
// returned unchanged.
func resolveMcpExposureAlias(value any) any {
	text, ok := value.(string)
	if !ok {
		return value
	}
	if resolved, ok := mcpExposureAliases[text]; ok {
		return string(resolved)
	}
	return value
}

func isMcpExposure(value any) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, exposure := range mcpExposures {
		if string(exposure) == text {
			return true
		}
	}
	return false
}

// --- raw-value helpers (upstream isRecord / isStringRecord / type checks) ---

func mcpRecord(value any) (map[string]any, bool) {
	record, ok := value.(map[string]any)
	if !ok || record == nil {
		return nil, false
	}
	return record, true
}

func mcpStringRecord(value any) (map[string]string, bool) {
	record, ok := mcpRecord(value)
	if !ok {
		return nil, false
	}
	out := make(map[string]string, len(record))
	for key, entry := range record {
		text, isString := entry.(string)
		if !isString {
			return nil, false
		}
		out[key] = text
	}
	return out, true
}

func mcpStringArray(value any) ([]string, bool) {
	entries, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		text, isString := entry.(string)
		if !isString {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func mcpNumber(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	}
	return 0, false
}

func isString(value any) bool {
	_, ok := value.(string)
	return ok
}

func isBool(value any) bool {
	_, ok := value.(bool)
	return ok
}

func sortedKeys[V any](record map[string]V) []string {
	keys := make([]string, 0, len(record))
	for key := range record {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
