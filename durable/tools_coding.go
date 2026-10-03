package durable

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/chord"
)

// Port of the durable coding tools (tools/read.ts, tools/write.ts, tools/env.ts).

// RequireEnv is the call's execution environment; a tool without one fails.
func RequireEnv(api ToolExecutionApi) (ExecutionEnv, error) {
	env := api.Env()
	if env == nil {
		return nil, fmt.Errorf("No execution environment is configured")
	}
	return env, nil
}

// ReadToolInput is the read tool's input.
type ReadToolInput struct {
	Path   string `json:"path"`
	Offset *int   `json:"offset,omitempty"`
	Limit  *int   `json:"limit,omitempty"`
}

// ReadTruncation describes how the shown text was cut (no content).
type ReadTruncation struct {
	Truncated             bool   `json:"truncated"`
	TruncatedBy           string `json:"truncatedBy,omitempty"`
	TotalLines            int    `json:"totalLines"`
	TotalBytes            int    `json:"totalBytes"`
	OutputLines           int    `json:"outputLines"`
	OutputBytes           int    `json:"outputBytes"`
	LastLinePartial       bool   `json:"lastLinePartial,omitempty"`
	FirstLineExceedsLimit bool   `json:"firstLineExceedsLimit,omitempty"`
	MaxLines              int    `json:"maxLines"`
	MaxBytes              int    `json:"maxBytes"`
}

// ReadToolDetails is the read tool's structured details.
type ReadToolDetails struct {
	Truncation *ReadTruncation `json:"truncation,omitempty"`
}

func readResultError(code, message string) ToolExecutionResult {
	isError := true
	return ToolExecutionResult{
		Content: []ai.UserContent{}, IsError: &isError,
		Diagnostics: []ToolDiagnostic{ToolDiagnosticOf(code, message)},
	}
}

// CreateReadTool builds the read tool: it reads text files and reports how the
// shown text was cut; image files are rejected.
func CreateReadTool() ToolRegistration {
	return ToolRegistration{
		Tool: ai.Tool{
			Name:        "read",
			Description: fmt.Sprintf("Read the contents of a text file. Output is truncated to %d lines or %dKB (whichever is hit first). Use offset/limit for large files. When you need the full file, continue with offset until complete.", DefaultMaxLines, DefaultMaxBytes/1024),
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to read (relative or absolute)"},"offset":{"type":"number","description":"Line number to start reading from (1-indexed)"},"limit":{"type":"number","description":"Maximum number of lines to read"}},"required":["path"]}`),
		},
		Execute: func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error) {
			var input ReadToolInput
			if err := decodeArgs(args, &input); err != nil {
				return ToolExecutionResult{}, err
			}
			env, err := RequireEnv(api)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			absolutePath, err := ResolveReadToolPath(ctx, env, input.Path)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			bytes, err := env.ReadBinaryFile(absolutePath, ctx)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			if mimeType := DetectSupportedImageMimeType(bytes); mimeType != "" {
				return readResultError("unsupported_image",
					fmt.Sprintf("%s is an image (%s); reading images is not supported", input.Path, mimeType)), nil
			}
			text := string(bytes)
			allLines := strings.Split(text, "\n")
			totalFileLines := len(allLines)
			startLine := 0
			if input.Offset != nil && *input.Offset-1 > 0 {
				startLine = *input.Offset - 1
			}
			startLineDisplay := startLine + 1
			if startLine >= len(allLines) {
				return ToolExecutionResult{}, fmt.Errorf("Offset %d is beyond end of file (%d lines total)", valueOr(input.Offset, 0), len(allLines))
			}
			selected := ""
			var userLimitedLines *int
			if input.Limit != nil {
				endLine := startLine + *input.Limit
				if endLine > len(allLines) {
					endLine = len(allLines)
				}
				selected = strings.Join(allLines[startLine:endLine], "\n")
				lines := endLine - startLine
				userLimitedLines = &lines
			} else {
				selected = strings.Join(allLines[startLine:], "\n")
			}
			truncation := TruncateHead(selected, TruncationOptions{})
			diagnostics := []ToolDiagnostic{}
			outputText := truncation.Content
			var details *ReadToolDetails
			if truncation.FirstLineExceedsLimit {
				lineBytes := []byte(allLines[startLine])
				end := CharacterEnd(lineBytes, DefaultMaxBytes)
				outputText = string(lineBytes[:end])
				diagnostics = append(diagnostics, ToolDiagnostic{
					Severity: DiagnosticWarn, Code: stringPointer("truncated"),
					Message: fmt.Sprintf("Line %d is %s, exceeds the %s limit; showing its first %s. Use bash: sed -n '%dp' %s | tail -c +%d",
						startLineDisplay, FormatSize(len(lineBytes)), FormatSize(DefaultMaxBytes), FormatSize(end), startLineDisplay, input.Path, end+1),
				})
				details = &ReadToolDetails{Truncation: &ReadTruncation{
					Truncated: truncation.Truncated, TruncatedBy: truncation.TruncatedBy,
					TotalLines: truncation.TotalLines, TotalBytes: truncation.TotalBytes,
					OutputLines: 1, OutputBytes: end, FirstLineExceedsLimit: true,
					MaxLines: truncation.MaxLines, MaxBytes: truncation.MaxBytes,
				}}
			} else if truncation.Truncated {
				endLineDisplay := startLineDisplay + truncation.OutputLines - 1
				nextOffset := endLineDisplay + 1
				limitText := ""
				if truncation.TruncatedBy != "lines" {
					limitText = " (" + FormatSize(DefaultMaxBytes) + " limit)"
				}
				diagnostics = append(diagnostics, ToolDiagnostic{
					Severity: DiagnosticInfo, Code: stringPointer("truncated"),
					Message: fmt.Sprintf("Showing lines %d-%d of %d%s. Use offset=%d to continue.",
						startLineDisplay, endLineDisplay, totalFileLines, limitText, nextOffset),
				})
				details = &ReadToolDetails{Truncation: readTruncationOf(truncation)}
			} else if userLimitedLines != nil && startLine+*userLimitedLines < len(allLines) {
				remaining := len(allLines) - (startLine + *userLimitedLines)
				nextOffset := startLine + *userLimitedLines + 1
				diagnostics = append(diagnostics, ToolDiagnostic{
					Severity: DiagnosticInfo,
					Message:  fmt.Sprintf("%d more lines in file. Use offset=%d to continue.", remaining, nextOffset),
				})
			}
			content := []ai.UserContent{}
			if outputText != "" {
				content = append(content, ai.TextContent{Text: outputText})
			}
			result := ToolExecutionResult{Content: content, Diagnostics: diagnostics}
			if details != nil {
				result.Details = *details
			}
			return result, nil
		},
	}
}

func readTruncationOf(truncation TruncationResult) *ReadTruncation {
	return &ReadTruncation{
		Truncated: truncation.Truncated, TruncatedBy: truncation.TruncatedBy,
		TotalLines: truncation.TotalLines, TotalBytes: truncation.TotalBytes,
		OutputLines: truncation.OutputLines, OutputBytes: truncation.OutputBytes,
		LastLinePartial: truncation.LastLinePartial, FirstLineExceedsLimit: truncation.FirstLineExceedsLimit,
		MaxLines: truncation.MaxLines, MaxBytes: truncation.MaxBytes,
	}
}

// WriteToolInput is the write tool's input.
type WriteToolInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// CreateWriteTool builds the write tool: it creates or overwrites a file,
// creating parent directories.
func CreateWriteTool() ToolRegistration {
	return ToolRegistration{
		Tool: ai.Tool{
			Name:        "write",
			Description: "Write content to a file. Creates the file if it doesn't exist, overwrites if it does. Automatically creates parent directories.",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Path to the file to write (relative or absolute)"},"content":{"type":"string","description":"Content to write to the file"}},"required":["path","content"]}`),
		},
		Execute: func(args chord.JsonValue, api ToolExecutionApi, ctx chord.Context) (ToolExecutionResult, error) {
			var input WriteToolInput
			if err := decodeArgs(args, &input); err != nil {
				return ToolExecutionResult{}, err
			}
			env, err := RequireEnv(api)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			absolutePath, err := ResolveToolPath(ctx, env, input.Path)
			if err != nil {
				return ToolExecutionResult{}, err
			}
			return WithFileMutationQueue(ctx, env, absolutePath, func() (ToolExecutionResult, error) {
				if ctx != nil && ctx.Err() != nil {
					return ToolExecutionResult{}, fmt.Errorf("Operation aborted")
				}
				if err := env.WriteFile(absolutePath, []byte(input.Content), ctx); err != nil {
					return ToolExecutionResult{}, err
				}
				if ctx != nil && ctx.Err() != nil {
					return ToolExecutionResult{}, fmt.Errorf("Operation aborted")
				}
				return ToolExecutionResult{Content: []ai.UserContent{
					ai.TextContent{Text: "Successfully wrote to " + input.Path},
				}}, nil
			})
		},
	}
}

func decodeArgs(args chord.JsonValue, target any) error {
	encoded, err := marshalJSONValue(args)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(encoded), target)
}

func valueOr(value *int, fallback int) int {
	if value == nil {
		return fallback
	}
	return *value
}
