package coding

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Port of core/tools/{read,write,ls}.ts (minus the extension wrapper and UI
// renderers, which are host surface). Schemas match the TypeBox-generated
// JSON.

func numSchema(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

// --- read ---

// ReadToolOptions tune the read tool.
type ReadToolOptions struct {
	// AutoResizeImages resizes images to 2000x2000 max. Default true.
	AutoResizeImages bool
	// Model returns the model in play, read per call the way upstream reads
	// ctx.model: a session can switch models, and both the image resize limits and
	// the non-vision note depend on the current one. Nil means unknown, which
	// gives the default limits and no note.
	Model func() *ai.Model
}

// ReadToolDetails carries the truncation info.
type ReadToolDetails struct {
	Truncation *TruncationResult `json:"truncation,omitempty"`
}

// readSchemaJSON mirrors the TypeBox schema.
var readSchemaJSON = mustSchemaJSON(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":   map[string]any{"type": "string", "description": "Path to the file to read (relative or absolute)"},
		"offset": numSchema("Line number to start reading from (1-indexed)"),
		"limit":  numSchema("Maximum number of lines to read"),
	},
	"required": []string{"path"},
})

func mustSchemaJSON(v any) json.RawMessage {
	enc, err := ai.MarshalJSON(v)
	if err != nil {
		panic(err)
	}
	return enc
}

// ReadToolDescription is upstream's read description.
const ReadToolDescription = "Read the contents of a file. Supports text files and images (jpg, png, gif, webp, bmp). Images are sent as attachments. For text files, output is truncated to 2000 lines or 50KB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete."

// CreateReadTool builds the read tool.
func CreateReadTool(cwd string, options *ReadToolOptions) agent.AgentTool {
	autoResize := options == nil || options.AutoResizeImages
	var modelFor func() *ai.Model
	if options != nil {
		modelFor = options.Model
	}
	return agent.AgentTool{
		Name:        "read",
		Description: ReadToolDescription,
		Parameters:  readSchemaJSON,
		Label:       "read",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}
			var input struct {
				Path   string   `json:"path"`
				Offset *float64 `json:"offset"`
				Limit  *float64 `json:"limit"`
			}
			if err := jsonUnmarshalStrictTool(params, &input); err != nil {
				return agent.AgentToolResult{}, err
			}
			absolutePath := ResolveReadPath(input.Path, cwd)
			if _, err := os.Stat(absolutePath); err != nil {
				return agent.AgentToolResult{}, err
			}

			if mimeType := DetectImageMimeTypeFromFile(absolutePath); mimeType != "" {
				return readImageFile(absolutePath, mimeType, input.Path, autoResize, modelFor)
			}

			data, err := os.ReadFile(absolutePath)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			textContent := DecodeUTF8WithReplacement(data)
			allLines := strings.Split(textContent, "\n")
			totalFileLines := len(allLines)
			startLine := 0.0
			if input.Offset != nil {
				startLine = math.Max(0, *input.Offset-1)
			}
			startLineDisplay := startLine + 1
			if startLine >= float64(len(allLines)) {
				return agent.AgentToolResult{}, fmt.Errorf("Offset %s is beyond end of file (%d lines total)", formatToolNumber(*input.Offset), len(allLines))
			}
			sliceStart := jsArraySliceIndex(startLine, len(allLines))
			var selectedContent string
			userLimitedLines := -1.0
			if input.Limit != nil {
				endLine := math.Min(startLine+*input.Limit, float64(len(allLines)))
				sliceEnd := jsArraySliceIndex(endLine, len(allLines))
				if sliceEnd < sliceStart {
					sliceEnd = sliceStart
				}
				selectedContent = strings.Join(allLines[sliceStart:sliceEnd], "\n")
				userLimitedLines = endLine - startLine
			} else {
				selectedContent = strings.Join(allLines[sliceStart:], "\n")
			}
			truncation := TruncateHead(selectedContent, TruncationOptions{})
			var outputText string
			if truncation.FirstLineExceedsLimit {
				firstLineSize := FormatSize(int64(len(allLines[sliceStart])))
				outputText = fmt.Sprintf("[Line %s is %s, exceeds %s limit. Use bash: sed -n '%sp' %s | head -c %d]",
					formatToolNumber(startLineDisplay), firstLineSize, FormatSize(DefaultMaxBytes), formatToolNumber(startLineDisplay), input.Path, DefaultMaxBytes)
			} else if truncation.Truncated {
				endLineDisplay := startLineDisplay + float64(truncation.OutputLines) - 1
				nextOffset := endLineDisplay + 1
				outputText = truncation.Content
				if truncation.TruncatedBy == TruncatedByLines {
					outputText += fmt.Sprintf("\n\n[Showing lines %s-%s of %d. Use offset=%s to continue.]",
						formatToolNumber(startLineDisplay), formatToolNumber(endLineDisplay), totalFileLines, formatToolNumber(nextOffset))
				} else {
					outputText += fmt.Sprintf("\n\n[Showing lines %s-%s of %d (%s limit). Use offset=%s to continue.]",
						formatToolNumber(startLineDisplay), formatToolNumber(endLineDisplay), totalFileLines, FormatSize(DefaultMaxBytes), formatToolNumber(nextOffset))
				}
			} else if userLimitedLines >= 0 && startLine+userLimitedLines < float64(len(allLines)) {
				remaining := float64(len(allLines)) - (startLine + userLimitedLines)
				nextOffset := startLine + userLimitedLines + 1
				outputText = fmt.Sprintf("%s\n\n[%s more lines in file. Use offset=%s to continue.]",
					truncation.Content, formatToolNumber(remaining), formatToolNumber(nextOffset))
			} else {
				outputText = truncation.Content
			}
			var details json.RawMessage
			if truncation.Truncated || truncation.FirstLineExceedsLimit {
				details, _ = ai.MarshalJSON(ReadToolDetails{Truncation: &truncation})
			}
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: outputText}},
				Details: details,
			}, nil
		},
	}
}

// nonVisionImageNote is upstream getNonVisionImageNote: a note appended when the
// model cannot accept images at all, so the text says the image will be omitted
// rather than implying it was sent.
func nonVisionImageNote(model *ai.Model) string {
	if model == nil {
		return ""
	}
	for _, input := range model.Input {
		if input == "image" {
			return ""
		}
	}
	return "[Current model does not support images. The image will be omitted from this request.]"
}

// imageResizeOptionsFor resolves the model's resize limits over the defaults, the
// way upstream spreads model.inputLimits.images.resize over DEFAULT_OPTIONS: a
// partially specified block keeps the default for every key it leaves out.
func imageResizeOptionsFor(model *ai.Model) ImageResizeOptions {
	options := DefaultImageResizeOptions
	if model == nil || model.InputLimits == nil || model.InputLimits.Images == nil {
		return options
	}
	resize := model.InputLimits.Images.Resize
	if resize == nil {
		return options
	}
	if resize.MaxWidth > 0 {
		options.MaxWidth = int(resize.MaxWidth)
	}
	if resize.MaxHeight > 0 {
		options.MaxHeight = int(resize.MaxHeight)
	}
	if resize.MaxBytes > 0 {
		options.MaxBytes = resize.MaxBytes
	}
	if resize.JPEGQuality > 0 {
		options.JPEGQuality = int(resize.JPEGQuality)
	}
	return options
}

// readImageFile prepares an image the way upstream's read tool does. The bytes go
// through ProcessImage, which resizes within the limits (D26: bilinear resampling
// rather than Photon's Lanczos3) or reports an omission message,
// which resizes within the limits or reports an omission message; the processing
// hints and the non-vision note are folded into the text, and the image content
// is attached only when the processing succeeded.
func readImageFile(absolutePath, mimeType, originalPath string, autoResize bool, modelFor func() *ai.Model) (agent.AgentToolResult, error) {
	data, err := os.ReadFile(absolutePath)
	if err != nil {
		return agent.AgentToolResult{}, err
	}
	var model *ai.Model
	if modelFor != nil {
		model = modelFor()
	}
	resizeOptions := imageResizeOptionsFor(model)
	processed := ProcessImage(data, mimeType, &ProcessImageOptions{
		AutoResizeImages: &autoResize,
		ResizeOptions:    &resizeOptions,
	})
	note := nonVisionImageNote(model)
	if !processed.OK {
		text := fmt.Sprintf("Read image file [%s]\n%s", mimeType, processed.Message)
		if note != "" {
			text += "\n" + note
		}
		return agent.AgentToolResult{
			Content: []ai.Content{ai.TextContent{Text: text}},
		}, nil
	}
	text := fmt.Sprintf("Read image file [%s]", processed.MimeType)
	if len(processed.Hints) > 0 {
		text += "\n" + strings.Join(processed.Hints, "\n")
	}
	if note != "" {
		text += "\n" + note
	}
	return agent.AgentToolResult{
		Content: []ai.Content{
			ai.TextContent{Text: text},
			ai.ImageContent{Data: processed.Data, MimeType: processed.MimeType},
		},
	}, nil
}

// DetectImageMimeTypeFromFile sniffs image MIME from magic bytes.
func DetectImageMimeTypeFromFile(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	magic := make([]byte, 16)
	n, _ := file.Read(magic)
	magic = magic[:n]
	switch {
	case len(magic) >= 8 && string(magic[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(magic) >= 3 && magic[0] == 0xFF && magic[1] == 0xD8:
		return "image/jpeg"
	case len(magic) >= 6 && string(magic[:3]) == "GIF" && (magic[3] == '8'):
		return "image/gif"
	case len(magic) >= 12 && string(magic[8:12]) == "WEBP":
		return "image/webp"
	case len(magic) >= 2 && string(magic[:2]) == "BM":
		return "image/bmp"
	default:
		return ""
	}
}

// --- write ---

// WriteToolDescription is upstream's write description.
const WriteToolDescription = "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories."

var writeSchemaJSON = mustSchemaJSON(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":    map[string]any{"type": "string", "description": "Path to the file to write (relative or absolute)"},
		"content": map[string]any{"type": "string", "description": "Content to write to the file"},
	},
	"required": []string{"path", "content"},
})

// CreateWriteTool builds the write tool.
func CreateWriteTool(cwd string) agent.AgentTool {
	return agent.AgentTool{
		Name:        "write",
		Description: WriteToolDescription,
		Parameters:  writeSchemaJSON,
		Label:       "write",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			var input struct {
				Path    string `json:"path"`
				Content string `json:"content"`
			}
			if err := jsonUnmarshalStrictTool(params, &input); err != nil {
				return agent.AgentToolResult{}, err
			}
			absolutePath := ResolveToCwd(input.Path, cwd)
			dir := filepath.Dir(absolutePath)
			err := WithFileMutationQueue(absolutePath, func() error {
				if ctxErrOf(ctx) != nil {
					return fmt.Errorf("Operation aborted")
				}
				if err := os.MkdirAll(dir, 0o777); err != nil {
					return err
				}
				if ctxErrOf(ctx) != nil {
					return fmt.Errorf("Operation aborted")
				}
				if err := os.WriteFile(absolutePath, []byte(input.Content), 0o666); err != nil {
					return err
				}
				if ctxErrOf(ctx) != nil {
					return fmt.Errorf("Operation aborted")
				}
				return nil
			})
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: "Successfully wrote to " + input.Path}},
			}, nil
		},
	}
}

// --- ls ---

// LsToolDetails carries ls truncation info.
type LsToolDetails struct {
	Truncation        *TruncationResult `json:"truncation,omitempty"`
	EntryLimitReached *float64          `json:"entryLimitReached,omitempty"`
}

// LsToolDescription is upstream's ls description.
const LsToolDescription = "List directory contents. Returns entries sorted alphabetically, with '/' suffix for directories. Includes dotfiles. Output is truncated to 500 entries or 50KB (whichever is hit first)."

var lsSchemaJSON = mustSchemaJSON(map[string]any{
	"type": "object",
	"properties": map[string]any{
		"path":  map[string]any{"type": "string", "description": "Directory to list (default: current directory)"},
		"limit": numSchema("Maximum number of entries to return (default: 500)"),
	},
})

const lsDefaultLimit = 500

// CreateLsTool builds the ls tool.
func CreateLsTool(cwd string) agent.AgentTool {
	return agent.AgentTool{
		Name:        "ls",
		Description: LsToolDescription,
		Parameters:  lsSchemaJSON,
		Label:       "ls",
		Execute: func(toolCallID string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			if ctxErrOf(ctx) != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Operation aborted")
			}
			var input struct {
				Path  string   `json:"path"`
				Limit *float64 `json:"limit"`
			}
			if len(params) > 0 {
				if err := jsonUnmarshalStrictTool(params, &input); err != nil {
					return agent.AgentToolResult{}, err
				}
			}
			dirPath := ResolveToCwd(orDefault(input.Path, "."), cwd)
			effectiveLimit := float64(lsDefaultLimit)
			if input.Limit != nil {
				effectiveLimit = *input.Limit
			}

			info, err := os.Stat(dirPath)
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Path not found: %s", dirPath)
			}
			if !info.IsDir() {
				return agent.AgentToolResult{}, fmt.Errorf("Not a directory: %s", dirPath)
			}
			entries, err := os.ReadDir(dirPath)
			if err != nil {
				return agent.AgentToolResult{}, fmt.Errorf("Cannot read directory: %s", err.Error())
			}

			// Sort alphabetically, case-insensitively.
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, DecodeUTF8WithReplacement([]byte(entry.Name())))
			}
			sortCaseInsensitive(names)

			var results []string
			entryLimitReached := false
			for _, entry := range names {
				if float64(len(results)) >= effectiveLimit {
					entryLimitReached = true
					break
				}
				fullPath := filepath.Join(dirPath, entry)
				entryInfo, err := os.Stat(fullPath)
				if err != nil {
					continue // skip entries we cannot stat
				}
				suffix := ""
				if entryInfo.IsDir() {
					suffix = "/"
				}
				results = append(results, entry+suffix)
			}

			if len(results) == 0 {
				return agent.AgentToolResult{
					Content: []ai.Content{ai.TextContent{Text: "(empty directory)"}},
				}, nil
			}

			rawOutput := strings.Join(results, "\n")
			truncation := TruncateHead(rawOutput, TruncationOptions{MaxLines: uncappedLines})
			output := truncation.Content
			var notices []string
			details := LsToolDetails{}
			if entryLimitReached {
				notices = append(notices, fmt.Sprintf("%s entries limit reached. Use limit=%s for more",
					formatToolNumber(effectiveLimit), formatToolNumber(effectiveLimit*2)))
				details.EntryLimitReached = &effectiveLimit
			}
			if truncation.Truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", FormatSize(DefaultMaxBytes)))
				details.Truncation = &truncation
			}
			if len(notices) > 0 {
				output += "\n\n[" + strings.Join(notices, ". ") + "]"
			}
			var detailsJSON json.RawMessage
			if len(notices) > 0 {
				detailsJSON, _ = ai.MarshalJSON(details)
			}
			return agent.AgentToolResult{
				Content: []ai.Content{ai.TextContent{Text: output}},
				Details: detailsJSON,
			}, nil
		},
	}
}

func formatToolNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func jsArraySliceIndex(index float64, length int) int {
	index = math.Trunc(index)
	if index < 0 {
		index = math.Max(0, float64(length)+index)
	} else if index > float64(length) {
		index = float64(length)
	}
	return int(index)
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func sortCaseInsensitive(names []string) {
	collator := collate.New(language.Und)
	sort.SliceStable(names, func(i, j int) bool {
		return collator.CompareString(strings.ToLower(names[i]), strings.ToLower(names[j])) < 0
	})
}

func jsonUnmarshalStrictTool(data json.RawMessage, v any) error {
	return json.Unmarshal(data, v)
}

func ctxErrOf(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// mustMarshalJSON panics on marshal failure (schemas are static).
func mustMarshalJSON(v any) json.RawMessage {
	enc, err := ai.MarshalJSON(v)
	if err != nil {
		panic(err)
	}
	return enc
}

func boolPtr(b bool) *bool { return &b }
