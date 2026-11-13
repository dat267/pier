package interactive

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/dat267/pier/tui"
)

// TestMcpToolRenderUpstreamParity pins the MCP tool rows against
// pi v1.5.2 (b4afdddf), ToolExecutionComponent and extensions/mcp/tools.ts
// createMcpToolRenderers, using pier's palette (D154). Regenerate from the
// repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-mcptool-parity.mjs ./pi > coding/interactive/testdata/mcptool_parity_golden.txt
//
// It covers the collapsed header with no arguments, a long argument list and a
// nested object, the visual-line preview of a long minified JSON result and of
// many lines collapsed and expanded, an error result, an empty result, the full
// output path collapsed and expanded, tabs and the recorded duration line.
func TestMcpToolRenderUpstreamParity(t *testing.T) {
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	previous := tui.GetKeybindings()
	tui.SetKeybindings(NewAppKeybindingsManager(nil, "").KeybindingsManager)
	defer tui.SetKeybindings(previous)

	var corpus struct {
		Cwd   string `json:"cwd"`
		Cases map[string]struct {
			Label     string          `json:"label"`
			Args      json.RawMessage `json:"args"`
			Width     int             `json:"width"`
			IsPartial bool            `json:"isPartial"`
			Expanded  bool            `json:"expanded"`
			Result    struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Details    json.RawMessage `json:"details"`
				IsError    bool            `json:"isError"`
				DurationMs *int64          `json:"durationMs"`
			} `json:"result"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/mcptool_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	golden := loadGoldenFile(t, "testdata/mcptool_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		// The session hands the renderer the call's JSON (toolCall.Arguments), so the
		// corpus keeps that shape: decoding into a map would sort the keys and hide
		// an order difference.
		args := json.RawMessage(spec.Args)
		definition := mcpRenderers(spec.Label)
		component := NewToolExecutionComponent(spec.Label, "call-1", args,
			ToolExecutionOptions{}, &definition, nil, corpus.Cwd)
		if spec.Expanded {
			component.SetExpanded(true)
		}
		component.SetArgsComplete()
		if spec.Result.DurationMs != nil {
			component.SetResultDuration(spec.Result.DurationMs)
		}
		result := &SortToolResultContent{IsError: spec.Result.IsError}
		if details := spec.Result.Details; len(details) > 0 && string(details) != "null" {
			result.Details = details
		}
		for _, block := range spec.Result.Content {
			result.Content = append(result.Content, ToolResultContent{Type: block.Type, Text: block.Text})
		}
		component.UpdateResult(result, spec.IsPartial)

		key := label + "@" + itoa(spec.Width)
		got := normalizeRender(component.Render(spec.Width))
		if !spec.Expanded {
			checkGolden(t, golden, key, got)
			continue
		}
		// D397: pier keeps the call header on one line in both states, so an
		// expanded frame shows the collapsed `key=value` header where the
		// reference reflows it into one `key: value` line per argument. Compare
		// every other row, and check the header really is the collapsed form.
		var want []string
		if err := json.Unmarshal([]byte(golden[key]), &want); err != nil {
			t.Fatalf("%s: parse golden: %v", label, err)
		}
		if len(want) != len(got)+1 {
			t.Fatalf("%s: %d rows, want %d (the reference's expanded header is one row taller)", label, len(got), len(want))
		}
		// Rows 0 and 1 are the frame's leading blank row and border; row 2 is the
		// header (one row here, two there); the rest is the body.
		checkGolden(t, map[string]string{key: mustJSON(append(want[:2], want[4:]...))}, key,
			append(append([]string{}, got[:2]...), got[3:]...))
		if !strings.Contains(got[2], "=") || strings.Contains(got[2], ": ") {
			t.Fatalf("%s: expanded header is not the collapsed form: %q", label, got[2])
		}
	}
}
