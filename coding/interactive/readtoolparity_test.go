package interactive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/tui"
)

// TestReadToolRenderUpstreamParity pins the read tool's rows against
// pi v1.5.2 (b4afdddf), ToolExecutionComponent and tools/renderers/read.ts, using
// pier's palette (D154). Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-readtool-parity.mjs ./pi > coding/interactive/testdata/readtool_parity_golden.txt
//
// It covers the compact call lines (skill and resource), the plain call line with
// an offset and limit, the highlighted and plain result bodies collapsed and
// expanded with their expand hint, every truncation notice (lines, bytes, first
// line over the limit), an error result, tabs, a hidden image and the recorded
// duration line.
func TestReadToolRenderUpstreamParity(t *testing.T) {
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
			Args       json.RawMessage `json:"args"`
			Width      int             `json:"width"`
			IsPartial  bool            `json:"isPartial"`
			Expanded   bool            `json:"expanded"`
			ShowImages *bool           `json:"showImages"`
			Result     struct {
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Data     string `json:"data"`
					MimeType string `json:"mimeType"`
				} `json:"content"`
				Details    json.RawMessage `json:"details"`
				IsError    bool            `json:"isError"`
				DurationMs *int64          `json:"durationMs"`
			} `json:"result"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/readtool_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	golden := loadGoldenFile(t, "testdata/readtool_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		// The session hands the renderer the call's JSON (toolCall.Arguments), so
		// the corpus keeps that shape rather than a decoded map.
		args := json.RawMessage(spec.Args)
		component := NewToolExecutionComponent("read", "call-1", args,
			ToolExecutionOptions{ShowImages: spec.ShowImages}, &readRenderers, nil, corpus.Cwd)
		if spec.Expanded {
			component.SetExpanded(true)
		}
		component.SetArgsComplete()
		if spec.Result.DurationMs != nil {
			component.SetResultDuration(spec.Result.DurationMs)
		}
		// The session stores details as the tool's JSON (sortToolResultFromAgent),
		// so the corpus keeps that shape rather than a typed struct.
		result := &SortToolResultContent{IsError: spec.Result.IsError}
		if details := spec.Result.Details; len(details) > 0 && string(details) != "null" {
			result.Details = details
		}
		for _, block := range spec.Result.Content {
			content := ToolResultContent{Type: block.Type}
			switch block.Type {
			case "text":
				content.Text = block.Text
			case "image":
				content.Data = block.Data
				content.MimeType = block.MimeType
			}
			result.Content = append(result.Content, content)
		}
		component.UpdateResult(result, spec.IsPartial)

		checkGolden(t, golden, label+"@"+itoa(spec.Width), normalizeRender(component.Render(spec.Width)))
	}
}
