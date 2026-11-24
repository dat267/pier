package coding

import (
	"slices"
	"strings"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// Port of utils/tool-result-images.ts: normalize image blocks returned by tool
// results as they enter session history.
//
// The `read` tool and `@file` CLI attachments run their images through
// ProcessImage, but tools that produce images themselves (extensions, MCP
// bridges, screenshot tools) hand back arbitrary base64 payloads that go
// straight into session history and every subsequent provider request.
// Oversized images make the provider reject the whole conversation, not just
// the offending turn, so normalize them once on arrival.
//
// D408: upstream resizes with Photon; the Go image pipeline reports the upstream
// omission message when an image needs resizing, and NormalizedToolResultImages
// keeps the original block in that case so a tool's output is never silently
// deleted (matching upstream's processImage-failure handling).

// NormalizeToolResultImagesOptions configure the normalization.
type NormalizeToolResultImagesOptions struct {
	// AutoResizeImages resizes oversized images to the inline provider limits
	// (default true).
	AutoResizeImages *bool
	// ResizeOptions is the model's resize profile; the defaults apply when nil.
	ResizeOptions *ImageResizeOptions
}

// NormalizeToolResultImages normalizes image blocks in a tool result's content.
// The original slice is returned when nothing changed so callers can skip
// rewriting the result.
func NormalizeToolResultImages(content ai.UserContentList, options *NormalizeToolResultImagesOptions) (ai.UserContentList, bool) {
	hasImage := slices.ContainsFunc(content, func(block ai.UserContent) bool {
		_, ok := block.(ai.ImageContent)
		return ok
	})
	if !hasImage {
		return content, false
	}

	autoResize := true
	var resizeOptions *ImageResizeOptions
	if options != nil {
		if options.AutoResizeImages != nil {
			autoResize = *options.AutoResizeImages
		}
		resizeOptions = options.ResizeOptions
	}

	normalized := make(ai.UserContentList, 0, len(content))
	changed := false
	for _, block := range content {
		image, ok := block.(ai.ImageContent)
		if !ok {
			normalized = append(normalized, block)
			continue
		}
		// Upstream decodes with Buffer.from(data, "base64"), which tolerates an
		// unpadded, URL-safe or wrapped payload (M290).
		data, ok := DecodeImageBase64(image.Data)
		if !ok {
			// Not decodable base64: keep the original block.
			normalized = append(normalized, block)
			continue
		}
		processed := ProcessImage(data, image.MimeType, &ProcessImageOptions{
			AutoResizeImages: &autoResize,
			ResizeOptions:    resizeOptions,
		})
		if !processed.OK {
			// Unlike `read`, keep the original block: the tool already produced
			// this image and the failure may just be an unavailable image
			// backend, so passing it through preserves the behavior tools have
			// today instead of silently deleting their output.
			normalized = append(normalized, block)
			continue
		}
		if processed.Data == image.Data && processed.MimeType == image.MimeType && len(processed.Hints) == 0 {
			normalized = append(normalized, block)
			continue
		}
		normalized = append(normalized, ai.ImageContent{Data: processed.Data, MimeType: processed.MimeType})
		if len(processed.Hints) > 0 {
			var hints strings.Builder
			for index, hint := range processed.Hints {
				if index > 0 {
					hints.WriteString("\n")
				}
				hints.WriteString(hint)
			}
			normalized = append(normalized, ai.TextContent{Text: hints.String()})
		}
		changed = true
	}
	if !changed {
		return content, false
	}
	return normalized, true
}

// normalizeToolResultContent applies the inline image limits to one tool
// result's content and reports whether it changed (upstream agent-session.ts
// _toolResultMessage runs every result through normalizeToolResultImages with
// the session model's resize profile and the imageAutoResize setting). A result
// without images, or one whose images are already within the limits, returns
// nil so the agent keeps the tool's own result.
func (s *AgentSession) normalizeToolResultContent(content []ai.Content) *agent.AfterToolCallResult {
	hasImage := slices.ContainsFunc(content, func(block ai.Content) bool {
		_, ok := block.(ai.ImageContent)
		return ok
	})
	if !hasImage {
		return nil
	}
	userContent := make(ai.UserContentList, 0, len(content))
	for _, block := range content {
		typed, ok := block.(ai.UserContent)
		if !ok {
			// A block kind the normalizer cannot carry through (upstream's tool
			// result is text or image only): leave the result untouched.
			return nil
		}
		userContent = append(userContent, typed)
	}
	autoResize := true
	if s.control != nil && s.control.Settings != nil {
		autoResize = s.control.Settings.GetImageAutoResize()
	}
	resizeOptions := imageResizeOptionsFor(s.Model())
	normalized, changed := NormalizeToolResultImages(userContent, &NormalizeToolResultImagesOptions{
		AutoResizeImages: &autoResize,
		ResizeOptions:    &resizeOptions,
	})
	if !changed {
		return nil
	}
	normalizedContent := make([]ai.Content, 0, len(normalized))
	for _, block := range normalized {
		normalizedContent = append(normalizedContent, block)
	}
	return &agent.AfterToolCallResult{Content: normalizedContent, HasContent: true}
}
