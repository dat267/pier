package interactive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/coding"
	"github.com/dat267/pier/tui"
)

// TestBashToolRenderUpstreamParity pins the shell tool's rendered rows against
// pi v1.5.2 (b4afdddf), ToolExecutionComponent and tools/renderers/bash.ts, using
// pier's palette (D154). Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-bashtool-parity.mjs ./pi > coding/interactive/testdata/bashtool_parity_golden.txt
//
// It covers the call line (missing, empty, long and multiline commands, the
// timeout suffix), the collapsed preview and its expand hint, the expanded form,
// truncation warnings by lines and by bytes, the full-output path, an error
// result, tabs, a narrow width and the recorded duration line.
func TestBashToolRenderUpstreamParity(t *testing.T) {
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	// The collapsed preview's hint carries keyText("app.tools.expand"), so the
	// row needs the app keybindings; pin them here instead of inheriting the
	// package's, which another test's manager can replace.
	previous := tui.GetKeybindings()
	tui.SetKeybindings(NewAppKeybindingsManager(nil, "").KeybindingsManager)
	defer tui.SetKeybindings(previous)

	var corpus struct {
		Cwd   string `json:"cwd"`
		Cases map[string]struct {
			Args struct {
				Command *string  `json:"command"`
				Timeout *float64 `json:"timeout"`
			} `json:"args"`
			Width      int   `json:"width"`
			IsPartial  bool  `json:"isPartial"`
			Expanded   bool  `json:"expanded"`
			ShowImages *bool `json:"showImages"`
			Result     struct {
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					Data     string `json:"data"`
					MimeType string `json:"mimeType"`
				} `json:"content"`
				Details    *coding.BashToolDetails `json:"details"`
				IsError    bool                    `json:"isError"`
				DurationMs *int64                  `json:"durationMs"`
			} `json:"result"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/bashtool_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	golden := loadGoldenFile(t, "testdata/bashtool_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		args := map[string]any{}
		if spec.Args.Command != nil {
			args["command"] = *spec.Args.Command
		}
		if spec.Args.Timeout != nil {
			args["timeout"] = *spec.Args.Timeout
		}
		component := NewToolExecutionComponent("bash", "call-1", args,
			ToolExecutionOptions{ShowImages: spec.ShowImages}, &bashRenderers, nil, corpus.Cwd)
		if spec.Expanded {
			component.SetExpanded(true)
		}
		component.SetArgsComplete()
		if spec.Result.DurationMs != nil {
			component.SetResultDuration(spec.Result.DurationMs)
		}
		result := &SortToolResultContent{
			Content: make([]ToolResultContent, 0, len(spec.Result.Content)),
			Details: spec.Result.Details,
			IsError: spec.Result.IsError,
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
