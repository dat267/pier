package protocol

import (
	"encoding/json"
	"testing"

	ai "github.com/dat267/pier/ai"
)

// Port of packages/mcp/test/content.test.ts.

func TestToLlmContentPassesTextAndImages(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "hello", Annotations: &ContentAnnotations{Priority: floatPtr(1)}},
		{Type: "image", Data: "aW1n", MimeType: "image/png", Meta: json.RawMessage(`{"x":1}`)},
		{Type: "audio", Data: "YXVk", MimeType: "audio/wav"},
		{Type: "resource_link", URI: "file:///a.txt", Name: "a.txt"},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///b.txt", Text: "inline", TextArm: true}},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///c.png", MimeType: strPtr("image/png"), Blob: "Yw=="}},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///d.bin", Blob: "ZA=="}},
	}
	got := Content(&CallToolResult{Content: blocks})
	want := []ai.UserContent{
		ai.TextContent{Text: "hello"},
		ai.ImageContent{Data: "aW1n", MimeType: "image/png"},
		ai.TextContent{Text: "[audio audio/wav omitted]"},
		ai.TextContent{Text: "a.txt: file:///a.txt"},
		ai.TextContent{Text: "inline"},
		ai.ImageContent{Data: "Yw==", MimeType: "image/png"},
		ai.TextContent{Text: "[binary resource file:///d.bin (unknown type) omitted]"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d blocks, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("block %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestToLlmContentFallsBackToStructuredJSON(t *testing.T) {
	got := Content(&CallToolResult{Content: nil, StructuredContent: json.RawMessage(`{"n":1}`)})
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	text, ok := got[0].(ai.TextContent)
	if !ok || text.Text != "{\n  \"n\": 1\n}" {
		t.Fatalf("structured fallback = %#v", got[0])
	}

	got = Content(&CallToolResult{Content: []ContentBlock{{Type: "text", Text: "n=1"}}, StructuredContent: json.RawMessage(`{"n":1}`)})
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	text, ok = got[0].(ai.TextContent)
	if !ok || text.Text != "n=1" {
		t.Fatalf("with blocks = %#v", got[0])
	}
}

func floatPtr(f float64) *float64 { return &f }
func strPtr(s string) *string     { return &s }

// The resource arm is discriminated by key presence upstream ("text" in
// resource), so an empty text payload stays the text arm (D181).
func TestResourceContentsArmByPresence(t *testing.T) {
	var contents ResourceContents
	if err := json.Unmarshal([]byte(`{"uri":"file:///e.txt","text":""}`), &contents); err != nil {
		t.Fatal(err)
	}
	if !contents.TextArm {
		t.Fatal("empty text did not record the text arm")
	}
	enc, err := json.Marshal(contents)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != `{"uri":"file:///e.txt","text":""}` {
		t.Fatalf("marshal = %s", enc)
	}

	var blob ResourceContents
	if err := json.Unmarshal([]byte(`{"uri":"file:///f.bin","blob":""}`), &blob); err != nil {
		t.Fatal(err)
	}
	if blob.TextArm {
		t.Fatal("blob arm recorded as text")
	}
	enc, err = json.Marshal(blob)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != `{"uri":"file:///f.bin","blob":""}` {
		t.Fatalf("marshal = %s", enc)
	}
}
