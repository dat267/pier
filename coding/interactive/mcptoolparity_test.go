package interactive

import (
	"encoding/json"
	"os"
	"regexp"
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
		// D397: the expand hint leads the preview, right under the arguments,
		// where the reference puts it below the preview. Drop hint rows from both
		// sides so the preview rows line up; the hint's own rows are pinned by
		// TestMcpResultPreviewHintCountsVisualLines and
		// TestMcpToolHintTruncatesToTheFrameWidth.
		got = withoutHintRows(got)
		var want []string
		if err := json.Unmarshal([]byte(golden[key]), &want); err != nil {
			t.Fatalf("%s: parse golden: %v", label, err)
		}
		want = withoutHintRows(want)
		// D397: pier renders one uncut pairs header in both states. The reference
		// cuts the collapsed pairs at 100 units and, expanded, writes one
		// `key: value` row per argument, so the header differs in shape and can be
		// a different number of rows. Everything from the result's first text row
		// on must match; the header is checked by its own shape below.
		anchor := ""
		for _, block := range spec.Result.Content {
			if block.Type == "text" && block.Text != "" {
				anchor = strings.Split(block.Text, "\n")[0]
				break
			}
		}
		wantStart := indexOfRow(want, anchor, 2)
		gotStart := indexOfRow(got, anchor, 2)
		if anchor == "" || wantStart < 0 || gotStart < 0 {
			checkGolden(t, golden, key, got)
			continue
		}
		// D493: pier appends the call's elapsed/took row, which the reference's MCP
		// renderer omits, so the shared rows come first and any extra rows at the
		// end must be that line.
		shared := len(want) - wantStart
		if len(got)-gotStart < shared {
			t.Fatalf("%s: %d result rows, want at least %d", label, len(got)-gotStart, shared)
		}
		checkGolden(t, map[string]string{key: mustJSON(want[wantStart:])}, key, got[gotStart:gotStart+shared])
		for _, extra := range got[gotStart+shared:] {
			row := paritySGR.ReplaceAllString(extra, "")
			if strings.TrimSpace(row) != "" && !strings.Contains(row, "Took ") && !strings.Contains(row, "Elapsed ") {
				t.Fatalf("%s: unexpected row after the result: %q", label, row)
			}
		}
		header := paritySGR.ReplaceAllString(got[2], "")
		hasArgs := len(spec.Args) > 0 && string(spec.Args) != "null" && string(spec.Args) != "{}"
		if !strings.Contains(header, spec.Label) || (hasArgs && !strings.Contains(header, "=")) {
			t.Fatalf("%s: header is not the uncut pairs line: %q", label, got[2])
		}
	}
}

// withoutHintRows drops the expand-hint rows, which D397 moves above the preview.
func withoutHintRows(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		if strings.Contains(paritySGR.ReplaceAllString(row, ""), "more lines,") {
			continue
		}
		out = append(out, row)
	}
	return out
}

// indexOfRow returns the first row at or after from whose text is exactly the
// given line, or -1.
func indexOfRow(rows []string, text string, from int) int {
	want := strings.TrimSpace(text)
	if want == "" {
		return -1
	}
	exact := len(want) < 8
	if !exact && len(want) > 24 {
		want = want[:24]
	}
	for index := from; index < len(rows); index++ {
		row := strings.TrimSpace(paritySGR.ReplaceAllString(rows[index], ""))
		if (exact && row == want) || (!exact && strings.Contains(row, want)) {
			return index
		}
	}
	return -1
}

var paritySGR = regexp.MustCompile("\x1b\\[[0-9;]*m")
