package coding

import (
	"bytes"
	"encoding/base64"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// A wide image is resized on the way to the model, with the dimension note in the
// text. The resize used to be left for later, and the tool sent the original
// bytes at whatever size they were.
func TestReadToolResizesWideImages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wide.png")
	if err := os.WriteFile(path, solidPNG(t, 2100, 4), 0o644); err != nil {
		t.Fatal(err)
	}

	result := execTool(t, CreateReadTool(dir, nil), `{"path":"wide.png"}`)
	if len(result.Content) != 2 {
		t.Fatalf("content = %#v, want text and image", result.Content)
	}
	text := result.Content[0].(ai.TextContent).Text
	if !strings.HasPrefix(text, "Read image file [image/png]") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "[Image: original 2100x4, displayed at 2000x4.") {
		t.Errorf("the dimension note is missing from %q", text)
	}
	imageContent := result.Content[1].(ai.ImageContent)
	data, err := base64.StdEncoding.DecodeString(imageContent.Data)
	if err != nil {
		t.Fatalf("image data is not base64: %v", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("the attached image does not decode: %v", err)
	}
	if config.Width != 2000 || config.Height != 4 {
		t.Errorf("attached image is %dx%d, want 2000x4", config.Width, config.Height)
	}
}

// A model that cannot accept images still receives the image content, and the
// fact that the model cannot take it travels in the result details, not in the
// text the model reads: the provider adds its own placeholder per request, so the
// stored result stays right after a model switch (read.ts, v1.6.0, #12139).
func TestReadToolNotesNonVisionModelsInDetails(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.png"), solidPNG(t, 20, 20), 0o644); err != nil {
		t.Fatal(err)
	}
	textOnly := &ai.Model{Input: []string{"text"}}
	tool := CreateReadTool(dir, &ReadToolOptions{Model: func() *ai.Model { return textOnly }})

	result := execTool(t, tool, `{"path":"small.png"}`)
	if len(result.Content) != 2 {
		t.Fatalf("content = %#v", result.Content)
	}
	text := result.Content[0].(ai.TextContent).Text
	if strings.Contains(text, "does not support images") {
		t.Errorf("the non-vision note reached the model content: %q", text)
	}
	if !strings.Contains(string(result.Details), `"imageUnsupportedByModel":`) {
		t.Errorf("details = %s, want the model id", result.Details)
	}
}

// A format the sniffer accepts but the standard library cannot decode degrades to
// a text-only result carrying the omission message, rather than attaching bytes
// the model cannot read.
func TestReadToolReportsUndecodableImages(t *testing.T) {
	dir := t.TempDir()
	// A WebP header is enough for the sniffer, but the payload is truncated, so
	// the registered WebP decoder rejects it: exactly the case being pinned.
	webp := append([]byte("RIFF"), make([]byte, 4)...)
	webp = append(webp, []byte("WEBPVP8 ")...)
	webp = append(webp, make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(dir, "shot.webp"), webp, 0o644); err != nil {
		t.Fatal(err)
	}

	result := execTool(t, CreateReadTool(dir, nil), `{"path":"shot.webp"}`)
	if len(result.Content) != 1 {
		t.Fatalf("content = %#v, want the text only", result.Content)
	}
	text := result.Content[0].(ai.TextContent).Text
	if !strings.Contains(text, "Read image file [image/webp]") {
		t.Errorf("text = %q", text)
	}
	if !strings.Contains(text, "[Image omitted:") {
		t.Errorf("the omission message is missing from %q", text)
	}
}

// The model is read per call, so a session that switches models mid-run does not
// keep the previous model's capabilities (upstream reads ctx.model each time).
func TestReadToolReadsTheModelPerCall(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "small.png"), solidPNG(t, 20, 20), 0o644); err != nil {
		t.Fatal(err)
	}
	current := &ai.Model{Input: []string{"image"}}
	tool := CreateReadTool(dir, &ReadToolOptions{Model: func() *ai.Model { return current }})

	first := execTool(t, tool, `{"path":"small.png"}`)
	if strings.Contains(string(first.Details), "imageUnsupportedByModel") {
		t.Fatalf("a vision model got the non-vision note: %s", first.Details)
	}
	current = &ai.Model{ID: "text-only", Input: []string{"text"}}
	switched := execTool(t, tool, `{"path":"small.png"}`)
	if !strings.Contains(string(switched.Details), `"imageUnsupportedByModel":"text-only"`) {
		t.Fatalf("the switched-to model did not take effect: %s", switched.Details)
	}
}

// A filename with a space is not special: the path travels as one JSON string,
// nothing in the pipeline splits on spaces, and ResolveReadPath only folds the
// unicode variants (macOS screenshots) on top of the plain form.
func TestReadToolHandlesSpacedImageNames(t *testing.T) {
	dir := t.TempDir()
	name := "my screenshot 2026.png"
	if err := os.WriteFile(filepath.Join(dir, name), solidPNG(t, 24, 16), 0o644); err != nil {
		t.Fatal(err)
	}
	result := execTool(t, CreateReadTool(dir, nil), `{"path":"my screenshot 2026.png"}`)
	if len(result.Content) != 2 {
		t.Fatalf("content = %#v, want text and image", result.Content)
	}
	if text := result.Content[0].(ai.TextContent).Text; !strings.HasPrefix(text, "Read image file [image/png]") {
		t.Errorf("text = %q", text)
	}
	if _, ok := result.Content[1].(ai.ImageContent); !ok {
		t.Errorf("content[1] = %T, want an image", result.Content[1])
	}

	// The narrow-no-break-space form macOS screenshots use resolves to the same
	// file, which is what the AM/PM variant is for.
	narrow := "my screenshot\u202f2026.png"
	result = execTool(t, CreateReadTool(dir, nil), `{"path":"`+narrow+`"}`)
	if len(result.Content) != 2 {
		t.Fatalf("the narrow-space variant did not resolve: %#v", result.Content)
	}
}
