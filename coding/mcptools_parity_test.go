package coding

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp/protocol"
)

type mcpToolsFixture struct {
	ToolNames []struct {
		Name     string `json:"name"`
		Server   string `json:"server"`
		Tool     string `json:"tool"`
		Taken    bool   `json:"taken"`
		Expected string `json:"expected"`
	} `json:"toolNames"`
	Content map[string]struct {
		Blocks  json.RawMessage `json:"blocks"`
		Content []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			MimeType string `json:"mimeType"`
			Data     string `json:"data"`
		} `json:"content"`
		SaveCalls []struct {
			Bytes     int    `json:"bytes"`
			Extension string `json:"extension"`
		} `json:"saveCalls"`
		Error string `json:"error"`
	} `json:"content"`
}

// loadMcpToolsFixture reads the source-generated pinned fixtures.
// Regenerate with
// node coding/testdata/mcp-tools/generate.mjs ./pi > coding/testdata/mcp-tools/golden.json
func loadMcpToolsFixture(t *testing.T) mcpToolsFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/mcp-tools/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture mcpToolsFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.ToolNames) == 0 || len(fixture.Content) == 0 {
		t.Fatal("fixture is empty")
	}
	return fixture
}

// TestMcpToolNamesMatchUpstream pins createMcpToolName, whose sanitizer works
// on UTF-16 code units: an astral character becomes two underscores.
func TestMcpToolNamesMatchUpstream(t *testing.T) {
	fixture := loadMcpToolsFixture(t)
	for _, test := range fixture.ToolNames {
		t.Run(test.Name, func(t *testing.T) {
			taken := func(string) bool { return test.Taken }
			if got := CreateMcpToolName(test.Server, test.Tool, taken); got != test.Expected {
				t.Errorf("name = %q, want %q", got, test.Expected)
			}
		})
	}
}

// TestMcpToolContentMatchesUpstream pins toModelContent for resource links,
// blob resources (base64url and opaque URIs) and the neither-arm resource.
func TestMcpToolContentMatchesUpstream(t *testing.T) {
	fixture := loadMcpToolsFixture(t)
	for name, test := range fixture.Content {
		t.Run(name, func(t *testing.T) {
			var blocks []protocol.ContentBlock
			if err := json.Unmarshal(test.Blocks, &blocks); err != nil {
				t.Fatal(err)
			}
			var saveCalls []map[string]any
			content, err := ToModelContent("srv", blocks, ConvertMcpResultOptions{
				ReadableResources: true,
				SaveOutput: func(data []byte, extension string) (string, error) {
					saveCalls = append(saveCalls, map[string]any{"bytes": len(data), "extension": extension})
					if extension == ".fail" {
						return "", fmt.Errorf("disk full")
					}
					return "/tmp/full" + extension, nil
				},
			})
			if test.Error != "" {
				if err == nil {
					t.Fatalf("want error %q, got %+v", test.Error, content)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := make([]map[string]any, 0, len(content))
			for _, block := range content {
				switch typed := block.(type) {
				case ai.TextContent:
					got = append(got, map[string]any{"type": "text", "text": typed.Text})
				case ai.ImageContent:
					got = append(got, map[string]any{"type": "image", "mimeType": typed.MimeType, "data": typed.Data})
				default:
					got = append(got, map[string]any{"type": fmt.Sprintf("unsupported %T", block)})
				}
			}
			want := make([]map[string]any, 0, len(test.Content))
			for _, block := range test.Content {
				switch block.Type {
				case "text":
					want = append(want, map[string]any{"type": "text", "text": block.Text})
				case "image":
					want = append(want, map[string]any{"type": "image", "mimeType": block.MimeType, "data": block.Data})
				default:
					t.Fatalf("unexpected fixture block type %q", block.Type)
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("content = %v, want %v", got, want)
			}
			calls := make([]map[string]any, 0, len(test.SaveCalls))
			for _, call := range test.SaveCalls {
				calls = append(calls, map[string]any{"bytes": call.Bytes, "extension": call.Extension})
			}
			if len(saveCalls) != len(calls) || len(calls) > 0 && !reflect.DeepEqual(saveCalls, calls) {
				t.Errorf("save calls = %v, want %v", saveCalls, calls)
			}
		})
	}
}
