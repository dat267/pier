package protocol

import (
	"encoding/json"
	"testing"

	ai "github.com/dat267/pier/ai"
)

// Port of packages/mcp/test/content.test.ts.

func TestToLlmContentPassesTextAndImages(t *testing.T) {
	blocks := []ContentBlock{
		{Type: "text", Text: "hello", Annotations: &ContentAnnotations{Priority: new(float64(1))}},
		{Type: "image", Data: "aW1n", MimeType: "image/png", Meta: json.RawMessage(`{"x":1}`)},
		{Type: "audio", Data: "YXVk", MimeType: "audio/wav"},
		{Type: "resource_link", URI: "file:///a.txt", Name: "a.txt"},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///b.txt", Text: "inline", TextArm: true}},
		{Type: "resource", Resource: &ResourceContents{URI: "file:///c.png", MimeType: new("image/png"), Blob: "Yw=="}},
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

// The resource arm is discriminated by key presence upstream ("text" in
// resource and "blob" in resource), so an empty payload keeps its arm (D181).
func TestResourceContentsArmByPresence(t *testing.T) {
	var contents ResourceContents
	if err := json.Unmarshal([]byte(`{"uri":"file:///e.txt","text":""}`), &contents); err != nil {
		t.Fatal(err)
	}
	if !contents.TextArm || contents.BlobArm {
		t.Fatal("empty text did not record only the text arm")
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
	if blob.TextArm || !blob.BlobArm {
		t.Fatal("empty blob did not record only the blob arm")
	}
	enc, err = json.Marshal(blob)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc) != `{"uri":"file:///f.bin","blob":""}` {
		t.Fatalf("marshal = %s", enc)
	}

	// A resource with neither member has neither arm; upstream's `"blob" in`
	// check is what decides the binary branch (D181).
	var neither ResourceContents
	if err := json.Unmarshal([]byte(`{"uri":"file:///g.bin"}`), &neither); err != nil {
		t.Fatal(err)
	}
	if neither.TextArm || neither.BlobArm {
		t.Fatalf("neither-arm resource recorded an arm: %+v", neither)
	}
}

// pi v1.5.2 (b4afdddf) packages/mcp/src/protocol/content.ts blockToLlmContent:
// the embedded-resource arm uses `resource.mimeType?.startsWith("image/")`, so a
// MIME type of exactly "image/" takes the image arm (the reference slices nothing
// and the port's `len >= 6 && [:6] == "image/"` must agree), while a shorter or
// different prefix takes the binary placeholder. The placeholder names the type,
// or "unknown type" when the field is absent.
func TestEmbeddedResourceImagePrefixBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mimeType *string
		image    bool
		text     string
	}{
		{name: "exactly the prefix", mimeType: new("image/"), image: true},
		{name: "a full image type", mimeType: new("image/png"), image: true},
		{name: "one byte short", mimeType: new("image"), text: "[binary resource file:///r (image) omitted]"},
		{name: "a different type", mimeType: new("text/plain"), text: "[binary resource file:///r (text/plain) omitted]"},
		{name: "an empty type", mimeType: new(""), text: "[binary resource file:///r () omitted]"},
		{name: "no type", mimeType: nil, text: "[binary resource file:///r (unknown type) omitted]"},
	} {
		block := ContentBlock{Type: "resource", Resource: &ResourceContents{
			URI: "file:///r", MimeType: tc.mimeType, Blob: "Yw==", BlobArm: true,
		}}
		got := Content(&CallToolResult{Content: []ContentBlock{block}})
		if len(got) != 1 {
			t.Fatalf("%s: got %+v", tc.name, got)
		}
		if tc.image {
			image, ok := got[0].(ai.ImageContent)
			if !ok || image.MimeType != *tc.mimeType {
				t.Errorf("%s: got %#v, want an image content of %q", tc.name, got[0], *tc.mimeType)
			}
			continue
		}
		text, ok := got[0].(ai.TextContent)
		if !ok || text.Text != tc.text {
			t.Errorf("%s: got %#v, want %q", tc.name, got[0], tc.text)
		}
	}

	// A nil result and a result with no blocks both carry no content.
	if got := Content(nil); len(got) != 0 {
		t.Errorf("Content(nil) = %+v", got)
	}
	if got := Content(&CallToolResult{}); len(got) != 0 {
		t.Errorf("Content(empty) = %+v", got)
	}
}
