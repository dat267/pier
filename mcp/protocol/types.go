package protocol

import "encoding/json"

// Protocol version constants (upstream types.ts). LATEST_PROTOCOL_VERSION is
// what the client requests; SUPPORTED_PROTOCOL_VERSIONS lists the versions
// the client accepts from a server. Servers that do not support the
// requested version answer with their own latest one, so older versions stay
// accepted for servers built on older SDKs.
const (
	LatestProtocolVersion = "2025-11-25"
)

// SupportedProtocolVersions mirrors SUPPORTED_PROTOCOL_VERSIONS.
var SupportedProtocolVersions = []string{LatestProtocolVersion, "2025-06-18", "2025-03-26", "2024-11-05"}

// Implementation identifies a client or server (upstream Implementation).
type Implementation struct {
	Name    string  `json:"name"`
	Version string  `json:"version"`
	Title   *string `json:"title,omitempty"`
}

// Root is a filesystem root the client exposes (upstream Root).
type Root struct {
	URI  string  `json:"uri"`
	Name *string `json:"name,omitempty"`
}

// ClientCapabilities are the client's declared capabilities (upstream
// ClientCapabilities).
type ClientCapabilities struct {
	Experimental json.RawMessage `json:"experimental,omitempty"` // Record<string, unknown>
	Roots        *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"roots,omitempty"`
	Sampling    json.RawMessage `json:"sampling,omitempty"`
	Elicitation json.RawMessage `json:"elicitation,omitempty"`
}

// ServerCapabilities are the server's declared capabilities (upstream
// ServerCapabilities).
type ServerCapabilities struct {
	Experimental json.RawMessage `json:"experimental,omitempty"`
	Logging      json.RawMessage `json:"logging,omitempty"`
	Prompts      *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"prompts,omitempty"`
	Resources *struct {
		Subscribe   bool `json:"subscribe,omitempty"`
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"resources,omitempty"`
	Tools *struct {
		ListChanged bool `json:"listChanged,omitempty"`
	} `json:"tools,omitempty"`
	Completions json.RawMessage `json:"completions,omitempty"`
}

// InitializeResult is the server's `initialize` result (upstream
// InitializeResult).
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      Implementation     `json:"serverInfo"`
	Instructions    *string            `json:"instructions,omitempty"`
}

// ProgressNotification reports long-running operation progress (upstream
// ProgressNotification).
type ProgressNotification struct {
	ProgressToken string   `json:"progressToken"` // string | number
	Progress      float64  `json:"progress"`
	Total         *float64 `json:"total,omitempty"`
	Message       *string  `json:"message,omitempty"`
}

// ToolAnnotations are UI hints about a tool (upstream ToolAnnotations).
type ToolAnnotations struct {
	Title           *string `json:"title,omitempty"`
	ReadOnlyHint    *bool   `json:"readOnlyHint,omitempty"`
	DestructiveHint *bool   `json:"destructiveHint,omitempty"`
	IdempotentHint  *bool   `json:"idempotentHint,omitempty"`
	OpenWorldHint   *bool   `json:"openWorldHint,omitempty"`
}

// ToolExecution describes task support for a tool (upstream ToolExecution).
type ToolExecution struct {
	TaskSupport *string `json:"taskSupport,omitempty"` // "forbidden" | "optional" | "required"
}

// Tool is a server tool (upstream Tool).
type Tool struct {
	Name         string           `json:"name"`
	Title        *string          `json:"title,omitempty"`
	Description  *string          `json:"description,omitempty"`
	InputSchema  json.RawMessage  `json:"inputSchema"` // Record<string, unknown>
	OutputSchema json.RawMessage  `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
	Execution    *ToolExecution   `json:"execution,omitempty"`
	Meta         json.RawMessage  `json:"_meta,omitempty"`
}

// Resource is a resource a server lists in `resources/list` (upstream
// Resource).
type Resource struct {
	URI         string              `json:"uri"`
	Name        string              `json:"name"`
	Title       *string             `json:"title,omitempty"`
	Description *string             `json:"description,omitempty"`
	MimeType    *string             `json:"mimeType,omitempty"`
	Size        *int64              `json:"size,omitempty"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// ResourceTemplate is a family of resources, addressed by an RFC 6570 URI
// template, from `resources/templates/list` (upstream ResourceTemplate).
type ResourceTemplate struct {
	URITemplate string              `json:"uriTemplate"`
	Name        string              `json:"name"`
	Title       *string             `json:"title,omitempty"`
	Description *string             `json:"description,omitempty"`
	MimeType    *string             `json:"mimeType,omitempty"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// ListResourcesResult is a `resources/list` page (upstream
// ListResourcesResult).
type ListResourcesResult struct {
	Resources  []Resource      `json:"resources"`
	NextCursor *string         `json:"nextCursor,omitempty"`
	Meta       json.RawMessage `json:"_meta,omitempty"`
}

// ListResourceTemplatesResult is a `resources/templates/list` page (upstream
// ListResourceTemplatesResult).
type ListResourceTemplatesResult struct {
	ResourceTemplates []ResourceTemplate `json:"resourceTemplates"`
	NextCursor        *string            `json:"nextCursor,omitempty"`
	Meta              json.RawMessage    `json:"_meta,omitempty"`
}

// ReadResourceResult is a `resources/read` result (upstream
// ReadResourceResult).
type ReadResourceResult struct {
	Contents []ResourceContents `json:"contents"`
	Meta     json.RawMessage    `json:"_meta,omitempty"`
}
