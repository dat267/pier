package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// Upstream v1.4.0 caa6967b6 (#10940, #10942) test cases: tool changes after the
// first request, as the Responses transports send them.

func additionalToolsTestModel() *Model {
	model := testResponsesModel()
	model.ID = "gpt-5.4"
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
		SupportsMidConvoSystemMessages: boolPtrT(true),
		SupportsAdditionalTools:        boolPtrT(true),
	}}
	return model
}

func toolSearchTestModel() *Model {
	model := testResponsesModel()
	model.ID = "gpt-5.4"
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
		SupportsMidConvoSystemMessages: boolPtrT(true),
		SupportsToolSearch:             boolPtrT(true),
	}}
	return model
}

// toolChangeItem is one wire item of the Responses input array.
type toolChangeItem struct {
	Type    string            `json:"type"`
	Role    string            `json:"role"`
	Content json.RawMessage   `json:"content"`
	Tools   []json.RawMessage `json:"tools"`
}

func (item toolChangeItem) text() string {
	var text string
	if err := json.Unmarshal(item.Content, &text); err == nil {
		return text
	}
	// Content may be an array of parts; the notices are always plain strings.
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(item.Content, &parts); err == nil {
		var builder strings.Builder
		for _, part := range parts {
			builder.WriteString(part.Text)
		}
		return builder.String()
	}
	return ""
}

func (item toolChangeItem) toolNames(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, raw := range item.Tools {
		var tool struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(raw, &tool); err != nil {
			t.Fatalf("tool: %v", err)
		}
		names = append(names, tool.Name)
	}
	return names
}

// responsesPayload converts a request's input array and top-level tools.
func responsesPayload(t *testing.T, model *Model, messages []Message) ([]toolChangeItem, []string) {
	t.Helper()
	params, err := BuildOpenAIResponsesParams(model, NormalizeContext(Context{Messages: messages}),
		&OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "sk-test"}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var items []toolChangeItem
	if err := json.Unmarshal(params.Input, &items); err != nil {
		t.Fatalf("input: %v", err)
	}
	var tools []string
	for _, tool := range params.Tools {
		tools = append(tools, tool.Name)
	}
	return items, tools
}

// With "changes", every tool is declared in an additional_tools item at its
// system message, so no tool is in the top-level list.
func TestResponsesAdditionalToolsDeclareEveryToolInPlace(t *testing.T) {
	baseTool, lateTool := testTool("base_tool"), testTool("late_tool")
	items, tools := responsesPayload(t, additionalToolsTestModel(), []Message{
		&SystemMessage{Content: StringOrBlocks{Text: "base prompt"}, ToolsAdded: []Tool{baseTool}, Timestamp: 0},
		&UserMessage{Content: StringOrBlocks{Text: "before"}, Timestamp: 1},
		&SystemMessage{Content: StringOrBlocks{Text: "updated guidance"}, ToolsAdded: []Tool{lateTool}, Timestamp: 2},
	})
	if len(tools) != 0 {
		t.Fatalf("top-level tools = %v; want none", tools)
	}
	var declared [][]string
	var sequence []string
	for _, item := range items {
		if item.Type == "additional_tools" {
			declared = append(declared, item.toolNames(t))
			sequence = append(sequence, "additional_tools")
			continue
		}
		if item.Type != "" {
			sequence = append(sequence, item.Type)
			continue
		}
		sequence = append(sequence, item.text())
	}
	if len(declared) != 2 || declared[0][0] != "base_tool" || declared[1][0] != "late_tool" {
		t.Fatalf("additional_tools = %v", declared)
	}
	// The incremental notice precedes every later item, so a model does not read
	// the latest one as the complete tool set.
	want := []string{"additional_tools", "base prompt", "", "incremental tools update", "additional_tools", "updated guidance"}
	if len(sequence) != len(want) {
		t.Fatalf("sequence = %q; want %q", sequence, want)
	}
	for i, part := range want {
		switch {
		case part == "":
			continue
		case part == "incremental tools update":
			if !strings.Contains(sequence[i], "This is an incremental tools update.") {
				t.Fatalf("sequence[%d] = %q; want the incremental notice", i, sequence[i])
			}
		default:
			if sequence[i] != part {
				t.Fatalf("sequence[%d] = %q; want %q (full: %q)", i, sequence[i], part, sequence)
			}
		}
	}
}

// A removal cannot be expressed on the wire, so it is announced as text.
func TestResponsesAnnounceRemovalsAsText(t *testing.T) {
	items, _ := responsesPayload(t, additionalToolsTestModel(), []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool")},
			Timestamp:  0,
		},
		&UserMessage{Content: StringOrBlocks{Text: "before"}, Timestamp: 1},
		&SystemMessage{
			Content:      StringOrBlocks{Text: "updated guidance"},
			ToolsRemoved: []ToolReference{{Name: "base_tool"}},
			ToolsAdded:   []Tool{namedTestTool("late_tool", "late_tool tool")},
			Timestamp:    2,
		},
	})
	if len(items) == 0 {
		t.Fatal("no items")
	}
	update := items[len(items)-1].text()
	if !strings.Contains(update, "updated guidance") {
		t.Fatalf("update = %q; want the system text", update)
	}
	want := "The following tools are no longer available. Do not call them:\n- base_tool"
	if !strings.Contains(update, want) {
		t.Fatalf("update = %q; want the removal notice", update)
	}
}

// A redefinition arrives as a removal plus an addition of the same name, so it
// is not announced as a removal.
func TestResponsesRedefinitionIsNotAnnouncedAsRemoval(t *testing.T) {
	items, _ := responsesPayload(t, additionalToolsTestModel(), []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool")},
			Timestamp:  0,
		},
		&UserMessage{Content: StringOrBlocks{Text: "before"}, Timestamp: 1},
		&SystemMessage{
			Content:      StringOrBlocks{Text: ""},
			ToolsRemoved: []ToolReference{{Name: "base_tool"}},
			ToolsAdded:   []Tool{namedTestTool("base_tool", "changed")},
			Timestamp:    2,
		},
	})
	if len(items) != 5 {
		t.Fatalf("items = %d; want 5", len(items))
	}
	if items[0].Type != "additional_tools" {
		t.Fatalf("first item = %+v", items[0])
	}
	if descriptions := items[0].toolNames(t); len(descriptions) != 1 || descriptions[0] != "base_tool" {
		t.Fatalf("declared = %v", descriptions)
	}
	if notice := items[3].text(); !strings.Contains(notice, "latest definition replaces the earlier one") {
		t.Fatalf("notice = %q", notice)
	}
	for _, item := range items {
		if strings.Contains(item.text(), "no longer available") {
			t.Fatalf("redefinition announced a removal: %q", item.text())
		}
	}
}

// captureToolHistory replays a tool removal followed by two loads and returns
// each request's items: initial tools, then the removal, then two loads.
func captureToolHistory(t *testing.T, model *Model) [][]toolChangeItem {
	t.Helper()
	messages := []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool"), namedTestTool("drop_tool", "drop_tool tool")},
			Timestamp:  0,
		},
	}
	steps := [][]Message{
		{&UserMessage{Content: StringOrBlocks{Text: "first"}, Timestamp: 1}},
		{
			&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsRemoved: []ToolReference{{Name: "drop_tool"}}, Timestamp: 2},
			&UserMessage{Content: StringOrBlocks{Text: "after removal"}, Timestamp: 3},
		},
		{
			&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsAdded: []Tool{namedTestTool("late_tool", "late_tool tool")}, Timestamp: 4},
			&UserMessage{Content: StringOrBlocks{Text: "after first load"}, Timestamp: 5},
		},
		{
			&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsAdded: []Tool{namedTestTool("later_tool", "later_tool tool")}, Timestamp: 6},
			&UserMessage{Content: StringOrBlocks{Text: "after second load"}, Timestamp: 7},
		},
	}
	var payloads [][]toolChangeItem
	for _, step := range steps {
		messages = append(messages, step...)
		items, _ := responsesPayload(t, model, messages)
		payloads = append(payloads, items)
	}
	return payloads
}

// Items grow by prefix: every request starts with the previous request's items,
// so the cached prefix survives a removal and later loads.
func requireGrowingPrefix(t *testing.T, payloads [][]toolChangeItem) {
	t.Helper()
	for index := 1; index < len(payloads); index++ {
		previous, current := payloads[index-1], payloads[index]
		if len(current) < len(previous) {
			t.Fatalf("request %d has %d items, fewer than the previous %d", index, len(current), len(previous))
		}
		for i := range previous {
			before, _ := json.Marshal(previous[i])
			after, _ := json.Marshal(current[i])
			if string(before) != string(after) {
				t.Fatalf("request %d item %d changed:\n got %s\nwant %s", index, i, after, before)
			}
		}
	}
}

func TestResponsesAdditionalToolsKeepThePrefixAcrossLoads(t *testing.T) {
	payloads := captureToolHistory(t, additionalToolsTestModel())
	requireGrowingPrefix(t, payloads)
	last := payloads[len(payloads)-1]
	var declared [][]string
	var notices []string
	for _, item := range last {
		if item.Type == "additional_tools" {
			declared = append(declared, item.toolNames(t))
		}
		if strings.Contains(item.text(), "incremental tools update") {
			notices = append(notices, item.text())
		}
	}
	want := [][]string{{"base_tool", "drop_tool"}, {"late_tool"}, {"later_tool"}}
	if len(declared) != len(want) {
		t.Fatalf("additional_tools = %v; want %v", declared, want)
	}
	for i := range want {
		if strings.Join(declared[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("additional_tools = %v; want %v", declared, want)
		}
	}
	if len(notices) != 2 {
		t.Fatalf("incremental notices = %d; want one per later load", len(notices))
	}
	for _, notice := range notices {
		if !strings.Contains(notice, "\nStill unavailable: drop_tool.") {
			t.Fatalf("notice = %q; want the still-unavailable restatement", notice)
		}
	}
	announced := false
	for _, item := range last {
		if strings.Contains(item.text(), "no longer available. Do not call them:\n- drop_tool") {
			announced = true
		}
	}
	if !announced {
		t.Fatal("removal was never announced")
	}
}

// A tool declared again is available again, so it stops being listed.
func TestResponsesRedeclaredToolStopsBeingUnavailable(t *testing.T) {
	items, _ := responsesPayload(t, additionalToolsTestModel(), []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool"), namedTestTool("drop_tool", "drop_tool tool")},
			Timestamp:  0,
		},
		&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsRemoved: []ToolReference{{Name: "drop_tool"}}, Timestamp: 1},
		&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsAdded: []Tool{namedTestTool("drop_tool", "drop_tool tool")}, Timestamp: 2},
		&UserMessage{Content: StringOrBlocks{Text: "go"}, Timestamp: 3},
	})
	var notice string
	for _, item := range items {
		if strings.Contains(item.text(), "incremental tools update") {
			notice = item.text()
		}
	}
	if notice == "" {
		t.Fatal("no incremental notice")
	}
	if strings.Contains(notice, "Still unavailable") {
		t.Fatalf("notice = %q; want no unavailable restatement", notice)
	}
	if last := items[len(items)-2]; last.Type != "additional_tools" || last.toolNames(t)[0] != "drop_tool" {
		t.Fatalf("last declaration = %+v", last)
	}
}

// Synthetic tool search cannot remove a tool, so a removal rewrites the prefix
// once and later loads stay anchored.
func TestResponsesToolSearchKeepsThePrefixAfterRemoval(t *testing.T) {
	model := toolSearchTestModel()
	base := []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool"), namedTestTool("drop_tool", "drop_tool tool")},
			Timestamp:  0,
		},
		&UserMessage{Content: StringOrBlocks{Text: "first"}, Timestamp: 1},
	}
	if _, tools := responsesPayload(t, model, base); strings.Join(tools, ",") != "base_tool,drop_tool" {
		t.Fatalf("top-level tools = %v; want both initial tools", tools)
	}

	// After the removal the top-level list is the folded state, and the later
	// loads arrive as in-place tool_search_output items.
	afterRemoval := append(append([]Message{}, base...),
		&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsRemoved: []ToolReference{{Name: "drop_tool"}}, Timestamp: 2},
		&UserMessage{Content: StringOrBlocks{Text: "after removal"}, Timestamp: 3},
	)
	if _, tools := responsesPayload(t, model, afterRemoval); strings.Join(tools, ",") != "base_tool" {
		t.Fatalf("top-level tools after the removal = %v; want [base_tool]", tools)
	}

	payloads := captureToolHistory(t, model)
	requireGrowingPrefix(t, payloads[1:])
	last := payloads[len(payloads)-1]
	var loaded []string
	for _, item := range last {
		if item.Type == "tool_search_output" {
			loaded = append(loaded, item.toolNames(t)...)
		}
	}
	if strings.Join(loaded, ",") != "late_tool,later_tool" {
		t.Fatalf("tool_search_output = %v; want the two later loads", loaded)
	}
}

func namedTestTool(name string, description string) Tool {
	return Tool{Name: name, Description: description, Parameters: json.RawMessage(`{"type":"object"}`)}
}

// The completions transport anchors Kimi tool messages the same way: the
// folded prefix holds the tools up to the last removal and later loads stay in
// place (upstream caa6967b6).
func TestCompletionsKimiToolMessagesAnchorAfterFolding(t *testing.T) {
	model := &Model{
		ID: "kimi-k3", Name: "Kimi K3", API: APIOpenAICompletions, Provider: "moonshotai",
		BaseURL: "https://api.moonshot.ai/v1", Input: []string{"text"},
		ContextWindow: 256000, MaxTokens: 8000,
		Compat: &ModelCompat{OpenAICompletions: &OpenAICompletionsCompat{
			SupportsMidConvoSystemMessages: boolPtrT(true),
			SupportsMidConvoToolAdditions:  boolPtrT(true),
		}},
	}
	params, err := BuildOpenAICompletionsParams(model, NormalizeContext(Context{Messages: []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool"), namedTestTool("drop_tool", "drop_tool tool")},
			Timestamp:  0,
		},
		&UserMessage{Content: StringOrBlocks{Text: "first"}, Timestamp: 1},
		&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsRemoved: []ToolReference{{Name: "drop_tool"}}, Timestamp: 2},
		&UserMessage{Content: StringOrBlocks{Text: "after removal"}, Timestamp: 3},
		&SystemMessage{Content: StringOrBlocks{Text: ""}, ToolsAdded: []Tool{namedTestTool("late_tool", "late_tool tool")}, Timestamp: 4},
		&UserMessage{Content: StringOrBlocks{Text: "after load"}, Timestamp: 5},
	}}), nil, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	var topLevel []string
	for _, tool := range params.Tools {
		topLevel = append(topLevel, tool.Function.Name)
	}
	// The removal folds both leading system messages into the top-level list.
	if strings.Join(topLevel, ",") != "base_tool" {
		t.Fatalf("top-level tools = %v; want [base_tool]", topLevel)
	}
	var anchored []string
	for _, message := range params.Messages {
		if message.Role == "system" && len(message.Tools) > 0 {
			for _, tool := range message.Tools {
				anchored = append(anchored, tool.Function.Name)
			}
		}
	}
	if strings.Join(anchored, ",") != "late_tool" {
		t.Fatalf("Kimi tool messages = %v; want the later load only", anchored)
	}
}

// Upstream v1.4.0 530015d93: the incremental notice precedes the removal notice
// of the same message, so it restates only removals announced by earlier
// messages.
func TestResponsesNoticeIgnoresTheRemovalItPrecedes(t *testing.T) {
	items, _ := responsesPayload(t, additionalToolsTestModel(), []Message{
		&SystemMessage{
			Content:    StringOrBlocks{Text: "base prompt"},
			ToolsAdded: []Tool{namedTestTool("base_tool", "base_tool tool"), namedTestTool("drop_tool", "drop_tool tool")},
			Timestamp:  0,
		},
		&SystemMessage{
			Content:      StringOrBlocks{Text: ""},
			ToolsAdded:   []Tool{namedTestTool("late_tool", "late_tool tool")},
			ToolsRemoved: []ToolReference{{Name: "drop_tool"}},
			Timestamp:    1,
		},
		&UserMessage{Content: StringOrBlocks{Text: "go"}, Timestamp: 2},
	})
	// Like Codex: notice, then the tools, then the removal notice.
	if len(items) < 4 {
		t.Fatalf("items = %d; want notice, tools, removal notice and the user message", len(items))
	}
	tail := items[len(items)-4:]
	if !strings.Contains(tail[0].text(), "This is an incremental tools update.") {
		t.Fatalf("tail[0] = %q; want the incremental notice", tail[0].text())
	}
	if strings.Contains(tail[0].text(), "Still unavailable") {
		t.Fatalf("notice = %q; the removal it precedes is not announced yet", tail[0].text())
	}
	if tail[1].Type != "additional_tools" || strings.Join(tail[1].toolNames(t), ",") != "late_tool" {
		t.Fatalf("tail[1] = %+v; want the late_tool declaration", tail[1])
	}
	if !strings.Contains(tail[2].text(), "no longer available. Do not call them:\n- drop_tool") {
		t.Fatalf("tail[2] = %q; want the removal notice", tail[2].text())
	}
	if tail[3].Role != "user" {
		t.Fatalf("tail[3] = %+v; want the user message", tail[3])
	}
}
