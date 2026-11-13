package interactive

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/dat267/pier/coding"
)

// TestSearchToolRenderUpstreamParity pins the grep, find and ls rows against
// pi v1.5.2 (b4afdddf), ToolExecutionComponent and tools/renderers/{grep,find,ls}.ts,
// using pier's palette (D154). Regenerate from the repository root:
//
//	node --experimental-strip-types coding/interactive/testdata/gen-searchtool-parity.mjs ./pi > coding/interactive/testdata/searchtool_parity_golden.txt
//
// It covers the call lines (missing pattern, default and empty path, glob, limit),
// the collapsed and expanded result lists with their expand hint, every
// truncation warning (match limit, byte and line truncation, lines truncated,
// result limit, entry limit), an error result, an empty result and the recorded
// duration line.
func TestSearchToolRenderUpstreamParity(t *testing.T) {
	SetTrueColorSupport(true)
	SetStyleColorsEnabled(true)
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	InitTheme("dark", false)

	var corpus struct {
		Cwd   string `json:"cwd"`
		Cases map[string]struct {
			Tool      string          `json:"tool"`
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
	data, err := os.ReadFile("testdata/searchtool_corpus.json")
	if err != nil {
		t.Fatalf("read corpus: %v", err)
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("parse corpus: %v", err)
	}

	renderers := map[string]*ToolRenderers{
		"grep": &grepRenderers,
		"find": &findRenderers,
		"ls":   &lsRenderers,
	}
	golden := loadGoldenFile(t, "testdata/searchtool_parity_golden.txt")

	for _, label := range sortedKeys(corpus.Cases) {
		spec := corpus.Cases[label]
		definition, ok := renderers[spec.Tool]
		if !ok {
			t.Fatalf("%s: unknown tool %q", label, spec.Tool)
		}
		// The session hands the renderer the call's JSON (toolCall.Arguments), so
		// the corpus keeps that shape rather than a decoded map.
		args := json.RawMessage(spec.Args)
		component := NewToolExecutionComponent(spec.Tool, "call-1", args,
			ToolExecutionOptions{}, definition, nil, corpus.Cwd)
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
		if len(spec.Result.Details) > 0 {
			details, err := decodeSearchToolDetails(spec.Tool, spec.Result.Details)
			if err != nil {
				t.Fatalf("%s: parse details: %v", label, err)
			}
			result.Details = details
		}
		component.UpdateResult(result, spec.IsPartial)

		checkGolden(t, golden, label+"@"+itoa(spec.Width), normalizeRender(component.Render(spec.Width)))
	}
}

// decodeSearchToolDetails parses the corpus details into the tool's own type,
// because each renderer reads its details through that type.
func decodeSearchToolDetails(tool string, raw json.RawMessage) (any, error) {
	var target any
	switch tool {
	case "grep":
		target = &coding.GrepToolDetails{}
	case "find":
		target = &coding.FindToolDetails{}
	case "ls":
		target = &coding.LsToolDetails{}
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return nil, err
	}
	return target, nil
}
