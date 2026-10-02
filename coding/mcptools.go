package coding

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp/protocol"
)

// Port of extensions/mcp/tools.ts: MCP tool names and result conversion.
//
// Upstream's tool renderers (renderCall/renderResult) belong to the extension
// tool definition, which the port's agent.AgentTool does not carry; the
// interactive layer renders MCP calls with its generic fallback.

// MaxMcpToolNameLength is the provider tool-name limit (upstream
// MAX_TOOL_NAME_LENGTH).
const MaxMcpToolNameLength = 64

// McpOutputMaxBytes caps the model-facing text of an MCP result (20KB).
const McpOutputMaxBytes = 20 * 1024

// ReadMcpResourceTool is the tool that reads resources named by resource links.
const ReadMcpResourceTool = "read_mcp_resource"

// McpOutputSaver saves truncated text or a binary resource and returns the path.
type McpOutputSaver func(data []byte, extension string) (string, error)

// SaveMcpToTempFile writes data to a private temp file (upstream saveToTempFile).
func SaveMcpToTempFile(data []byte, extension string) (string, error) {
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	path := filepath.Join(os.TempDir(), "pi-mcp-"+hex.EncodeToString(random)+extension)
	// Results can carry private data, so only the user may read the file.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// CreateMcpToolName builds `mcp__<server>__<tool>`, sanitized and shortened with
// a hash suffix when too long (upstream createMcpToolName). `isTaken` reports
// names used by another MCP tool: sanitizing can map two tools to one name
// (`a-b` and `a_b`), which then get the hash suffix.
func CreateMcpToolName(server string, tool string, isTaken func(string) bool) string {
	if isTaken == nil {
		isTaken = func(string) bool { return false }
	}
	name := sanitizeMcpToolName("mcp__" + server + "__" + tool)
	if len(name) <= MaxMcpToolNameLength && !isTaken(name) {
		return name
	}
	sum := sha256.Sum256([]byte(server + "\x00" + tool))
	hash := hex.EncodeToString(sum[:])[:8]
	prefix := name
	if len(prefix) > MaxMcpToolNameLength-len(hash)-1 {
		prefix = prefix[:MaxMcpToolNameLength-len(hash)-1]
	}
	return prefix + "_" + hash
}

// sanitizeMcpToolName replaces everything outside [A-Za-z0-9_] with `_`.
func sanitizeMcpToolName(name string) string {
	var builder strings.Builder
	builder.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

// mcpTextOf joins the text content of a result, like upstream textOf.
func mcpTextOf(content []ai.Content) string {
	texts := make([]string, 0, len(content))
	for _, block := range content {
		if text, ok := block.(ai.TextContent); ok {
			texts = append(texts, text.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// LimitMcpContent keeps model-facing text within McpOutputMaxBytes. Longer text
// becomes one text block in Codex's truncation format, followed by the path of
// the file with the full text; images follow it (upstream limitMcpContent).
func LimitMcpContent(content []ai.Content, saveOutput McpOutputSaver) ([]ai.Content, *string) {
	if saveOutput == nil {
		saveOutput = SaveMcpToTempFile
	}
	combined := mcpTextOf(content)
	truncation := TruncateMiddle(combined, McpOutputMaxBytes)
	if !truncation.Truncated {
		return content, nil
	}
	var fullOutputPath *string
	where := ""
	if path, err := saveOutput([]byte(combined), ".txt"); err != nil {
		where = "[Could not save the full output: " + err.Error() + "]"
	} else {
		fullOutputPath = &path
		where = "[Full output: " + path + " (read it with offset/limit)]"
	}
	tokens := (truncation.TotalBytes + 3) / 4
	text := fmt.Sprintf("Warning: truncated output (original token count: %d)\nTotal output lines: %d\n\n%s\n\n%s",
		tokens, truncation.TotalLines, truncation.Content, where)
	limited := make([]ai.Content, 0, len(content))
	limited = append(limited, ai.TextContent{Text: text})
	for _, block := range content {
		if _, isImage := block.(ai.ImageContent); isImage {
			limited = append(limited, block)
		}
	}
	return limited, fullOutputPath
}

// ConvertMcpResultOptions are ConvertMcpResult inputs.
type ConvertMcpResultOptions struct {
	// SaveOutput saves truncated text and binary resources (default: a temp file).
	SaveOutput McpOutputSaver
	// ReadableResources reports whether the server's resources can be read with
	// ReadMcpResourceTool, which resource links then name.
	ReadableResources bool
}

// ConvertMcpResult converts an MCP result to an agent tool result (upstream
// convertMcpResult). The boolean reports upstream's `isError`; the port's tool
// layer returns an error for it, since agent.AgentToolResult carries no error
// flag (the model-facing text is the same).
func ConvertMcpResult(server string, tool string, result *protocol.CallToolResult, options ConvertMcpResultOptions) (agent.AgentToolResult, bool, error) {
	if result == nil {
		result = &protocol.CallToolResult{}
	}
	var converted []ai.Content
	if len(result.Content) > 0 {
		blocks, err := ToModelContent(server, result.Content, options)
		if err != nil {
			return agent.AgentToolResult{}, false, err
		}
		converted = blocks
	} else {
		converted = userContentToContent(protocol.Content(result))
	}
	if result.IsError && mcpTextOf(converted) == "" {
		converted = append(converted, ai.TextContent{Text: "MCP tool " + server + "/" + tool + " returned an error"})
	}
	content, fullOutputPath := LimitMcpContent(converted, options.SaveOutput)
	details := mcpToolDetails{Server: server, Tool: tool, FullOutputPath: fullOutputPath}
	encodedDetails, err := ai.MarshalJSON(details)
	if err != nil {
		return agent.AgentToolResult{}, false, err
	}
	return agent.AgentToolResult{Content: content, Details: encodedDetails}, result.IsError, nil
}

// mcpToolDetails is the persisted tool-result detail shape (upstream
// McpToolDetails): key order server, tool, fullOutputPath.
type mcpToolDetails struct {
	Server         string  `json:"server"`
	Tool           string  `json:"tool"`
	FullOutputPath *string `json:"fullOutputPath,omitempty"`
}

// ToModelContent converts the content blocks of one server result (upstream
// toModelContent).
func ToModelContent(server string, blocks []protocol.ContentBlock, options ConvertMcpResultOptions) ([]ai.Content, error) {
	out := make([]ai.Content, 0, len(blocks))
	for _, block := range blocks {
		converted, err := mcpBlockToContent(server, block, options)
		if err != nil {
			return nil, err
		}
		out = append(out, converted...)
	}
	return out, nil
}

// mcpBlockToContent is upstream blockToContent.
func mcpBlockToContent(server string, block protocol.ContentBlock, options ConvertMcpResultOptions) ([]ai.Content, error) {
	if block.Type == protocol.ContentTypeResourceLink {
		details := make([]string, 0, 2)
		if block.MimeType != "" {
			details = append(details, block.MimeType)
		}
		if block.Size != nil {
			details = append(details, FormatSize(*block.Size))
		}
		title := block.Name
		if block.Title != nil {
			title = *block.Title
		}
		read := ""
		if options.ReadableResources {
			read = ". Read it with " + ReadMcpResourceTool + " (server \"" + server + "\")"
		}
		description := ""
		if block.Description != nil {
			description = ": " + *block.Description
		}
		detailText := ""
		if len(details) > 0 {
			detailText = " (" + strings.Join(details, ", ") + ")"
		}
		return []ai.Content{ai.TextContent{Text: "[Resource " + block.URI + " \"" + title + "\"" + detailText + description + read + "]"}}, nil
	}
	if block.Type == protocol.ContentTypeResource && block.Resource != nil && !block.Resource.TextArm &&
		!strings.HasPrefix(mcpStringValue(block.Resource.MimeType), "image/") {
		resource := block.Resource
		data, err := base64.StdEncoding.DecodeString(resource.Blob)
		if err != nil {
			return []ai.Content{ai.TextContent{Text: "[binary resource " + resource.URI + " (invalid base64) omitted]"}}, nil
		}
		if isTextMimeType(mcpStringValue(resource.MimeType)) {
			return []ai.Content{ai.TextContent{Text: string(data)}}, nil
		}
		kind := mcpStringValue(resource.MimeType)
		if kind == "" {
			kind = "unknown type"
		}
		kind = kind + ", " + FormatSize(int64(len(data)))
		saveOutput := options.SaveOutput
		if saveOutput == nil {
			saveOutput = SaveMcpToTempFile
		}
		path, saveErr := saveOutput(data, mcpExtensionOf(resource.URI))
		if saveErr != nil {
			return []ai.Content{ai.TextContent{Text: "[Binary resource " + resource.URI + " (" + kind + ") could not be saved: " + saveErr.Error() + "]"}}, nil
		}
		return []ai.Content{ai.TextContent{Text: "[Binary resource " + resource.URI + " (" + kind + ") saved to " + path + "]"}}, nil
	}
	return userContentToContent(protocol.Content(&protocol.CallToolResult{Content: []protocol.ContentBlock{block}})), nil
}

// mcpStringValue dereferences an optional string ("" when absent).
func mcpStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// mcpExtensionOf is the saved binary resource's extension: the one its URI ends
// in, else `.bin` (upstream extensionOf).
func mcpExtensionOf(uri string) string {
	path := uri
	if parsed, err := url.Parse(uri); err == nil && parsed.Scheme != "" {
		path = parsed.Path
	}
	base := path
	if index := strings.LastIndexByte(base, '/'); index >= 0 {
		base = base[index+1:]
	}
	if index := strings.LastIndexByte(base, '.'); index >= 0 {
		extension := base[index:]
		if len(extension) >= 2 && len(extension) <= 9 && isMcpExtensionChars(extension[1:]) {
			return extension
		}
	}
	return ".bin"
}

func isMcpExtensionChars(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// isTextMimeType reports mime types whose blobs are shown as text.
func isTextMimeType(mimeType string) bool {
	typeValue := mimeType
	if index := strings.IndexByte(typeValue, ';'); index >= 0 {
		typeValue = typeValue[:index]
	}
	typeValue = strings.ToLower(strings.TrimSpace(typeValue))
	return strings.HasPrefix(typeValue, "text/") || typeValue == "application/json" ||
		strings.HasSuffix(typeValue, "+json") || strings.HasSuffix(typeValue, "+xml")
}

// userContentToContent narrows user content to the model content slice.
func userContentToContent(content []ai.UserContent) []ai.Content {
	out := make([]ai.Content, 0, len(content))
	for _, block := range content {
		out = append(out, block)
	}
	return out
}

// McpToolError converts an MCP `isError` result into the port's error shape:
// the joined text is what the model sees (upstream keeps the blocks and sets
// isError; agent.AgentToolResult has no error flag).
func McpToolError(result agent.AgentToolResult) error {
	text := mcpTextOf(result.Content)
	if text == "" {
		text = "MCP tool returned an error"
	}
	return errors.New(text)
}
