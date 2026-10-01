// Package protocol is a Go port of @earendil-works/pi-mcp's protocol layer
// (pi/packages/mcp/src/protocol): JSON-RPC 2.0 message shapes, the MCP
// protocol types, and tool-result content blocks.
//
// Ground truth: pi/packages/mcp/src/protocol at the pinned upstream commit.
//
// Upstream's tagged unions (`type: "text"` discriminators) become Go structs
// with a Type field plus omitempty members; the JSON shape is identical in
// both directions and unknown block types survive a round trip.
package protocol

import (
	"bytes"
	"encoding/json"

	ai "github.com/dat267/pier/ai"
)

// ContentAnnotations describes who a block is for (upstream
// ContentAnnotations).
type ContentAnnotations struct {
	Audience     []string `json:"audience,omitempty"` // "user" | "assistant"
	Priority     *float64 `json:"priority,omitempty"`
	LastModified *string  `json:"lastModified,omitempty"`
}

// TextContent is a text block (upstream TextContent).
type TextContent struct {
	Text        string              `json:"text"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// ImageContent is a base64-encoded image block (upstream ImageContent).
type ImageContent struct {
	Data        string              `json:"data"`
	MimeType    string              `json:"mimeType"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// AudioContent is a base64-encoded audio block (upstream AudioContent).
type AudioContent struct {
	Data        string              `json:"data"`
	MimeType    string              `json:"mimeType"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// ResourceLinkContent links to a resource the server did not embed (upstream
// ResourceLinkContent).
type ResourceLinkContent struct {
	URI         string              `json:"uri"`
	Name        string              `json:"name"`
	Title       *string             `json:"title,omitempty"`
	Description *string             `json:"description,omitempty"`
	MimeType    *string             `json:"mimeType,omitempty"`
	Size        *int64              `json:"size,omitempty"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// TextResourceContents is inline text for a resource (upstream
// TextResourceContents).
type TextResourceContents struct {
	URI      string          `json:"uri"`
	MimeType *string         `json:"mimeType,omitempty"`
	Text     string          `json:"text"`
	Meta     json.RawMessage `json:"_meta,omitempty"`
}

// BlobResourceContents is inline base64 data for a resource (upstream
// BlobResourceContents).
type BlobResourceContents struct {
	URI      string          `json:"uri"`
	MimeType *string         `json:"mimeType,omitempty"`
	Blob     string          `json:"blob"`
	Meta     json.RawMessage `json:"_meta,omitempty"`
}

// ResourceContents is TextResourceContents or BlobResourceContents.
// Upstream discriminates the arm by key presence ("text" in resource), which
// matters for an empty text payload; TextArm records that (D181).
type ResourceContents struct {
	URI      string          `json:"uri"`
	MimeType *string         `json:"mimeType,omitempty"`
	Text     string          `json:"text,omitempty"`
	Blob     string          `json:"blob,omitempty"`
	Meta     json.RawMessage `json:"_meta,omitempty"`

	TextArm bool `json:"-"`
}

// UnmarshalJSON records whether the text member was present.
func (r *ResourceContents) UnmarshalJSON(data []byte) error {
	type alias ResourceContents
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	_, a.TextArm = probe["text"]
	*r = ResourceContents(a)
	return nil
}

// MarshalJSON emits the text member whenever the text arm is active, even
// when the text is empty, and always emits the blob member otherwise (the
// blob arm requires it).
func (r ResourceContents) MarshalJSON() ([]byte, error) {
	type alias struct {
		URI      string          `json:"uri"`
		MimeType *string         `json:"mimeType,omitempty"`
		Text     *string         `json:"text,omitempty"`
		Blob     *string         `json:"blob,omitempty"`
		Meta     json.RawMessage `json:"_meta,omitempty"`
	}
	var a alias
	a.URI, a.MimeType, a.Meta = r.URI, r.MimeType, r.Meta
	if r.TextArm {
		text := r.Text
		a.Text = &text
	} else {
		blob := r.Blob
		a.Blob = &blob
	}
	return json.Marshal(a)
}

// EmbeddedResourceContent is a resource the server embedded in a content
// block (upstream EmbeddedResourceContent).
type EmbeddedResourceContent struct {
	Resource    ResourceContents    `json:"resource"`
	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// Content block type discriminators.
const (
	ContentTypeText         = "text"
	ContentTypeImage        = "image"
	ContentTypeAudio        = "audio"
	ContentTypeResourceLink = "resource_link"
	ContentTypeResource     = "resource"
)

// ContentBlock is one tool-result block: TextContent, ImageContent,
// AudioContent, ResourceLinkContent, or EmbeddedResourceContent. The flat
// shape keeps unknown `type` values round-trippable.
type ContentBlock struct {
	Type string `json:"type"`
	// text / image / audio
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	// resource_link
	URI         string  `json:"uri,omitempty"`
	Name        string  `json:"name,omitempty"`
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Size        *int64  `json:"size,omitempty"`
	// resource
	Resource *ResourceContents `json:"resource,omitempty"`

	Annotations *ContentAnnotations `json:"annotations,omitempty"`
	Meta        json.RawMessage     `json:"_meta,omitempty"`
}

// CallToolResult is a tool invocation result (upstream CallToolResult).
type CallToolResult struct {
	Content           []ContentBlock  `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
	Meta              json.RawMessage `json:"_meta,omitempty"`
}

// blockToLlmContent is upstream's blockToLlmContent.
func blockToLlmContent(block ContentBlock) ai.UserContent {
	switch block.Type {
	case ContentTypeText:
		return ai.TextContent{Text: block.Text}
	case ContentTypeImage:
		return ai.ImageContent{Data: block.Data, MimeType: block.MimeType}
	case ContentTypeAudio:
		return ai.TextContent{Text: "[audio " + block.MimeType + " omitted]"}
	case ContentTypeResourceLink:
		return ai.TextContent{Text: block.Name + ": " + block.URI}
	case ContentTypeResource:
		resource := block.Resource
		if resource.TextArm {
			return ai.TextContent{Text: resource.Text}
		}
		if resource.MimeType != nil && len(*resource.MimeType) >= 6 && (*resource.MimeType)[:6] == "image/" {
			return ai.ImageContent{Data: resource.Blob, MimeType: *resource.MimeType}
		}
		kind := "unknown type"
		if resource.MimeType != nil {
			kind = *resource.MimeType
		}
		return ai.TextContent{Text: "[binary resource " + resource.URI + " (" + kind + ") omitted]"}
	default:
		return ai.TextContent{Text: "[unsupported MCP content " + block.Type + "]"}
	}
}

// Content converts a tool result to text and image content for a model
// (upstream toLlmContent). Text and images pass through, embedded text
// resources become text, embedded image resources become images, and other
// blocks (audio, resource links, binary resources) become a short text
// placeholder. A result without content blocks but with structuredContent
// becomes its JSON, since servers should, but do not always, mirror
// structured results as text.
func Content(result *CallToolResult) []ai.UserContent {
	var blocks []ContentBlock
	if result != nil {
		blocks = result.Content
	}
	out := make([]ai.UserContent, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, blockToLlmContent(block))
	}
	if len(out) == 0 && result != nil && len(result.StructuredContent) > 0 {
		out = append(out, ai.TextContent{Text: IndentJSON(result.StructuredContent)})
	}
	return out
}

// IndentJSON reformats compact JSON with the two-space shape of
// JSON.stringify(value, null, 2); byte order is preserved.
func IndentJSON(data []byte) string {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		return string(data)
	}
	return buf.String()
}
