package interactive

import (
	"os"
	"strings"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// Port of the render-utils helpers shared by the tool renderers
// (`tool-execution.ts` and the per-tool renderers). The manual `!` frame renders
// through the shell tool's frame, so upstream's bash-execution component is not
// ported (D246).

// ShortenPath replaces the home directory prefix with "~".
func ShortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// Str normalizes a value to a string: nil becomes "", other non-strings fail.
func Str(value any) (string, bool) {
	if value == nil {
		return "", true
	}
	text, ok := value.(string)
	return text, ok
}

// NormalizeDisplayText removes carriage returns.
func NormalizeDisplayText(text string) string { return strings.ReplaceAll(text, "\r", "") }

// ToolResultContent is one result content block.
type ToolResultContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// SortToolResultContent is the result payload the components render.
type SortToolResultContent struct {
	Content []ToolResultContent
	Details any
	IsError bool
}

// GetTextOutput joins the text blocks and appends image indicators when images
// are unavailable or hidden.
func GetTextOutput(result *SortToolResultContent, showImages bool) string {
	if result == nil {
		return ""
	}
	var textBlocks []string
	var imageBlocks []ToolResultContent
	for _, block := range result.Content {
		switch block.Type {
		case "text":
			textBlocks = append(textBlocks, strings.ReplaceAll(coding.SanitizeBinaryOutput(coding.StripAnsi(block.Text)), "\r", ""))
		case "image":
			imageBlocks = append(imageBlocks, block)
		}
	}
	output := strings.Join(textBlocks, "\n")

	if len(imageBlocks) > 0 && (!showImages || tui.GetTerminalCapabilities().Images == "") {
		indicators := make([]string, 0, len(imageBlocks))
		for _, block := range imageBlocks {
			mimeType := block.MimeType
			if mimeType == "" {
				mimeType = "image/unknown"
			}
			var dimensions *tui.ImageDimensions
			if block.Data != "" && block.MimeType != "" {
				if parsed, ok := tui.GetImageDimensions(block.Data, block.MimeType); ok {
					dimensions = &parsed
				}
			}
			indicators = append(indicators, tui.ImageFallback(mimeType, dimensions, ""))
		}
		joined := strings.Join(indicators, "\n")
		if output != "" {
			output = output + "\n" + joined
		} else {
			output = joined
		}
	}
	return output
}

// InvalidArgText renders the invalid-argument marker.
func InvalidArgText(theme *Theme) string { return theme.Fg("error", "[invalid arg]") }

// RenderToolPath renders a tool path argument.
func RenderToolPath(rawPath *string, theme *Theme, cwd string, emptyFallback string) string {
	if rawPath == nil {
		return InvalidArgText(theme)
	}
	value := *rawPath
	if value == "" {
		value = emptyFallback
	}
	if value == "" {
		return theme.Fg("toolOutput", "...")
	}
	// Hyperlinks need the path-to-file-URL conversion; with capabilities off
	// this is the plain styled path (same as upstream's fallback).
	return theme.Fg("accent", ShortenPath(value))
}
