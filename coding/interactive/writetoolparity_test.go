package interactive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/tui"
)

// TestWriteToolRenderUpstreamParity pins the write tool's rows against
// pi v1.5.2 (b4afdddf), ToolExecutionComponent and tools/renderers/write.ts, using
// pier's palette (D154). Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-writetool-parity.mjs ./pi > coding/interactive/testdata/writetool_parity_golden.txt
//
// It covers the highlighted and plain content previews collapsed and expanded
// with the "N total" expand hint, a missing or empty content argument, a missing
// path, tabs and CRLF, trailing blank lines, an error result with and without
// text, a streamed call, and the recorded duration line.
func TestWriteToolRenderUpstreamParity(t *testing.T) {
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
			Args      json.RawMessage `json:"args"`
			Width     int             `json:"width"`
			IsPartial bool            `json:"isPartial"`
			Expanded  bool            `json:"expanded"`
			Result    struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				IsError    bool   `json:"isError"`
				DurationMs *int64 `json:"durationMs"`
			} `json:"result"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/writetool_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	golden := loadGoldenFile(t, "testdata/writetool_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		// The session hands the renderer the call's JSON (toolCall.Arguments), so
		// the corpus keeps that shape rather than a decoded map.
		args := json.RawMessage(spec.Args)
		component := NewToolExecutionComponent("write", "call-1", args,
			ToolExecutionOptions{}, &writeRenderers, nil, corpus.Cwd)
		if spec.Expanded {
			component.SetExpanded(true)
		}
		component.SetArgsComplete()
		if spec.Result.DurationMs != nil {
			component.SetResultDuration(spec.Result.DurationMs)
		}
		result := &SortToolResultContent{IsError: spec.Result.IsError}
		for _, block := range spec.Result.Content {
			result.Content = append(result.Content, ToolResultContent{Type: block.Type, Text: block.Text})
		}
		component.UpdateResult(result, spec.IsPartial)

		checkGolden(t, golden, label+"@"+itoa(spec.Width), normalizeRender(component.Render(spec.Width)))
	}
}
