package coding

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp/protocol"
)

// Port of the "MCP tools" cases of upstream
// packages/coding-agent/test/mcp-extension.test.ts.

// TestCreateMcpToolName covers provider-safe names and collisions.
func TestCreateMcpToolName(t *testing.T) {
	if got := CreateMcpToolName("docs", "search", nil); got != "mcp__docs__search" {
		t.Fatalf("name = %q", got)
	}
	if got := CreateMcpToolName("my-server", "get.item/v2", nil); got != "mcp__my_server__get_item_v2" {
		t.Fatalf("name = %q", got)
	}
	long := CreateMcpToolName("server", strings.Repeat("x", 100), nil)
	if len(long) != 64 {
		t.Fatalf("long = %q (%d)", long, len(long))
	}
	if !regexp.MustCompile(`^mcp__server__x+_[0-9a-f]{8}$`).MatchString(long) {
		t.Fatalf("long = %q", long)
	}
	if other := CreateMcpToolName("server", strings.Repeat("x", 100)+"y", nil); other == long {
		t.Fatalf("hash collision: %q", other)
	}
	// Names that sanitize to one already taken by another tool get a hash suffix.
	taken := CreateMcpToolName("s", "a_b", nil)
	second := CreateMcpToolName("s", "a-b", func(name string) bool { return name == taken })
	if !regexp.MustCompile(`^mcp__s__a_b_[0-9a-f]{8}$`).MatchString(second) {
		t.Fatalf("second = %q", second)
	}
}

// output-files.ts uses the wx flag so a random-name collision never overwrites an existing file.
func TestSaveMcpToTempFileDoesNotOverwriteCollision(t *testing.T) {
	random := bytes.NewReader(make([]byte, 8))
	readRandom := func(buffer []byte) (int, error) { return io.ReadFull(random, buffer) }
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	path := filepath.Join(dir, "pi-mcp-0000000000000000.txt")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := saveMcpToTempFile([]byte("replace"), ".txt", readRandom); err == nil {
		t.Fatal("expected exclusive create to reject the existing path")
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "keep" {
		t.Fatalf("existing file = %q, err = %v; want keep", got, err)
	}
}

// mcp/tools.ts converts text resource blobs with Buffer.toString("utf8").
func TestMcpTextResourceReplacesMalformedUTF8(t *testing.T) {
	blob := base64.StdEncoding.EncodeToString([]byte{0xff, 'x'})
	mimeType := "text/plain"
	content, err := ToModelContent("docs", []protocol.ContentBlock{{
		Type:     protocol.ContentTypeResource,
		Resource: &protocol.ResourceContents{URI: "docs://invalid", MimeType: &mimeType, Blob: blob, BlobArm: true},
	}}, ConvertMcpResultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 {
		t.Fatalf("content = %+v", content)
	}
	text, ok := content[0].(ai.TextContent)
	if !ok || text.Text != "\uFFFDx" {
		t.Fatalf("resource text = %+v; want replacement-decoded text", content[0])
	}
}

// TestConvertMcpResult covers block conversion, the structured-content
// fallback and error results.
func TestConvertMcpResult(t *testing.T) {
	blocks := []protocol.ContentBlock{
		{Type: protocol.ContentTypeResourceLink, URI: "file:///a", Name: "a"},
		{Type: protocol.ContentTypeResource, Resource: &protocol.ResourceContents{URI: "file:///b", Text: "b text", TextArm: true}},
		{Type: protocol.ContentTypeAudio, Data: "", MimeType: "audio/wav"},
	}
	result, isError, err := ConvertMcpResult("docs", "t", &protocol.CallToolResult{
		Content: blocks, StructuredContent: json.RawMessage(`{"ok":true}`), Meta: json.RawMessage(`{"trace":"x"}`),
	}, ConvertMcpResultOptions{})
	if err != nil || isError {
		t.Fatalf("err=%v isError=%v", err, isError)
	}
	wantContent := []string{"[Resource file:///a \"a\"]", "b text", "[audio audio/wav omitted]"}
	if len(result.Content) != len(wantContent) {
		t.Fatalf("content = %+v", result.Content)
	}
	for index, want := range wantContent {
		text, ok := result.Content[index].(ai.TextContent)
		if !ok || text.Text != want {
			t.Fatalf("content[%d] = %+v, want %q", index, result.Content[index], want)
		}
	}
	var details map[string]any
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["server"] != "docs" || details["tool"] != "t" {
		t.Fatalf("details = %v", details)
	}
	if _, hasPath := details["fullOutputPath"]; hasPath {
		t.Fatalf("unexpected fullOutputPath: %v", details)
	}

	// Without content blocks the structured content becomes its JSON.
	structured, _, err := ConvertMcpResult("docs", "t", &protocol.CallToolResult{
		StructuredContent: json.RawMessage(`{"n":1}`),
	}, ConvertMcpResultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(structured.Content) != 1 || structured.Content[0].(ai.TextContent).Text != "{\n  \"n\": 1\n}" {
		t.Fatalf("content = %+v", structured.Content)
	}

	// isError keeps the text and flags the result.
	flagged, isError, err := ConvertMcpResult("docs", "t", &protocol.CallToolResult{
		Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "nope"}}, IsError: true,
	}, ConvertMcpResultOptions{})
	if err != nil || !isError {
		t.Fatalf("err=%v isError=%v", err, isError)
	}
	if len(flagged.Content) != 1 || flagged.Content[0].(ai.TextContent).Text != "nope" {
		t.Fatalf("content = %+v", flagged.Content)
	}
	if !flagged.IsError {
		t.Fatal("result.IsError = false; want true")
	}
	empty, _, _ := ConvertMcpResult("docs", "t", &protocol.CallToolResult{IsError: true}, ConvertMcpResultOptions{})
	if len(empty.Content) != 1 || empty.Content[0].(ai.TextContent).Text != "MCP tool docs/t returned an error" {
		t.Fatalf("content = %+v", empty.Content)
	}
}

// TestConvertMcpResultResourceLinksAndBinaries covers the read hint and saved
// binary resources.
func TestConvertMcpResultResourceLinksAndBinaries(t *testing.T) {
	type savedEntry struct {
		data      []byte
		extension string
	}
	saved := []savedEntry{}
	saveOutput := func(data []byte, extension string) (string, error) {
		saved = append(saved, savedEntry{data, extension})
		return "/tmp/saved" + extension, nil
	}
	size := int64(2048)
	description := "How to use it"
	result, _, err := ConvertMcpResult("docs", "t", &protocol.CallToolResult{
		Content: []protocol.ContentBlock{
			{
				Type: protocol.ContentTypeResourceLink, URI: "docs://guide", Name: "guide",
				Title: new("The Guide"), MimeType: "text/markdown", Size: &size, Description: &description,
			},
			{
				Type: protocol.ContentTypeResource,
				Resource: &protocol.ResourceContents{
					URI: "file:///r/report.pdf", MimeType: new("application/pdf"), Blob: "JVBERg==", BlobArm: true,
				},
			},
			{
				Type: protocol.ContentTypeResource,
				Resource: &protocol.ResourceContents{
					URI: "docs://logo", MimeType: new("image/png"), Blob: "AAAA", BlobArm: true,
				},
			},
		},
	}, ConvertMcpResultOptions{SaveOutput: saveOutput, ReadableResources: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"[Resource docs://guide \"The Guide\" (text/markdown, 2.0KB): How to use it. Read it with read_mcp_resource (server \"docs\")]",
		"[Binary resource file:///r/report.pdf (application/pdf, 4B) saved to /tmp/saved.pdf]",
	}
	if len(result.Content) != 3 {
		t.Fatalf("content = %+v", result.Content)
	}
	for index, expected := range want {
		if text := result.Content[index].(ai.TextContent).Text; text != expected {
			t.Fatalf("content[%d] = %q, want %q", index, text, expected)
		}
	}
	image, ok := result.Content[2].(ai.ImageContent)
	if !ok || image.Data != "AAAA" || image.MimeType != "image/png" {
		t.Fatalf("image = %+v", result.Content[2])
	}
	if len(saved) != 1 || string(saved[0].data) != "%PDF" || saved[0].extension != ".pdf" {
		t.Fatalf("saved = %+v", saved)
	}
}

// TestConvertMcpResultTruncatesMiddle covers the 20KB limit and the full-output
// file.
func TestConvertMcpResultTruncatesMiddle(t *testing.T) {
	full := strings.Builder{}
	for index := 1; index <= 3000; index++ {
		if index > 1 {
			full.WriteString("\n")
		}
		full.WriteString("line ")
		full.WriteString(strconv.Itoa(index))
	}
	text := full.String()
	saved := []string{}
	saveOutput := func(data []byte, extension string) (string, error) {
		saved = append(saved, string(data))
		return "/tmp/full.txt", nil
	}
	result, _, err := ConvertMcpResult("docs", "snapshot", &protocol.CallToolResult{
		Content: []protocol.ContentBlock{
			{Type: protocol.ContentTypeText, Text: text},
			{Type: protocol.ContentTypeImage, Data: "AAAA", MimeType: "image/png"},
		},
	}, ConvertMcpResultOptions{SaveOutput: saveOutput})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 2 {
		t.Fatalf("content = %+v", result.Content)
	}
	truncated := result.Content[0].(ai.TextContent).Text
	tokens := (len(text) + 3) / 4
	prefix := "Warning: truncated output (original token count: " + strconv.Itoa(tokens) + ")\nTotal output lines: 3000\n\nline 1\nline 2\n"
	if !strings.HasPrefix(truncated, prefix) {
		t.Fatalf("text = %q", truncated[:min(len(truncated), 200)])
	}
	if !regexp.MustCompile(`…\d+ chars truncated…`).MatchString(truncated) {
		t.Fatalf("no truncation marker: %q", truncated)
	}
	if !strings.HasSuffix(truncated, "line 3000\n\n[Full output: /tmp/full.txt (read it with offset/limit)]") {
		t.Fatalf("tail = %q", truncated[len(truncated)-80:])
	}
	if len(truncated) >= 21*1024 {
		t.Fatalf("truncated text is %d bytes", len(truncated))
	}
	if image, ok := result.Content[1].(ai.ImageContent); !ok || image.Data != "AAAA" {
		t.Fatalf("image = %+v", result.Content[1])
	}
	if len(saved) != 1 || saved[0] != text {
		t.Fatalf("saved = %d entries", len(saved))
	}
	var details map[string]any
	if err := json.Unmarshal(result.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details["server"] != "docs" || details["tool"] != "snapshot" || details["fullOutputPath"] != "/tmp/full.txt" {
		t.Fatalf("details = %v", details)
	}

	// Text within the limit is not saved.
	if _, _, err := ConvertMcpResult("docs", "small", &protocol.CallToolResult{
		Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "ok"}},
	}, ConvertMcpResultOptions{SaveOutput: saveOutput}); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 {
		t.Fatalf("saved = %d entries", len(saved))
	}
}

// TestTruncateMiddleMultiByte covers the character-boundary case.
func TestTruncateMiddleMultiByte(t *testing.T) {
	text := strings.Repeat("é", 20000) + "end"
	result := TruncateMiddle(text, 1001)
	if !result.Truncated {
		t.Fatal("not truncated")
	}
	if strings.Contains(result.Content, "\uFFFD") {
		t.Fatal("replacement character in output")
	}
	if !strings.HasSuffix(result.Content, "end") {
		t.Fatalf("tail = %q", result.Content[len(result.Content)-10:])
	}
	parts := regexp.MustCompile(`…\d+ chars truncated…`).Split(result.Content, -1)
	if len(parts) != 2 {
		t.Fatalf("parts = %d", len(parts))
	}
	if len([]byte(parts[0])) > 500 || len([]byte(parts[1])) > 501 {
		t.Fatalf("head=%d tail=%d bytes", len([]byte(parts[0])), len([]byte(parts[1])))
	}
	if len([]rune(parts[0]))+len([]rune(parts[1]))+result.RemovedChars != len([]rune(text)) {
		t.Fatalf("rune accounting: %d+%d+%d != %d", len([]rune(parts[0])), len([]rune(parts[1])), result.RemovedChars, len([]rune(text)))
	}
}

// tools.ts decodes blobs with Buffer.from(blob, "base64"), which ignores
// whitespace and missing padding; StdEncoding rejected both and dropped the
// resource.
func TestMcpBase64ResourceToleratesWhitespaceAndMissingPadding(t *testing.T) {
	mimeType := "text/plain"
	content, err := ToModelContent("docs", []protocol.ContentBlock{{
		Type:     protocol.ContentTypeResource,
		Resource: &protocol.ResourceContents{URI: "docs://wrapped", MimeType: &mimeType, Blob: "aGVs\nbG8", BlobArm: true},
	}}, ConvertMcpResultOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 {
		t.Fatalf("content = %+v", content)
	}
	text, ok := content[0].(ai.TextContent)
	if !ok || text.Text != "hello" {
		t.Fatalf("resource text = %+v; want hello", content[0])
	}
}
