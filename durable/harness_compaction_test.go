package durable

import (
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Port of harness/compaction.ts pure helpers.

func compactionView(head *EntryRecord, entries []EntryRecord, contributions [][]ai.Message) ContextView {
	messages := OrderToolResults(flattenMessages(contributions))
	return ContextView{Head: head, Entries: entries, Contributions: contributions, Messages: messages}
}

func TestSelectCutKeepsRecentTokens(t *testing.T) {
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop}
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}
	view := compactionView(nil,
		[]EntryRecord{{ID: 1}, {ID: 2}},
		[][]ai.Message{{assistant}, {user}},
	)
	cut := SelectCut(view, 1)
	if cut == nil || *cut != 1 {
		t.Fatalf("cut = %v", cut)
	}
	// When the entire context fits the keep window there is nothing to compact.
	if cut := SelectCut(view, 1_000_000); cut != nil {
		t.Fatalf("cut = %v", cut)
	}
}

func TestSelectCutWithHeadMarkerSkipsTheHead(t *testing.T) {
	head := EntryRecord{ID: 1}
	assistant := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop}
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}
	view := compactionView(&head,
		[]EntryRecord{{ID: 1}, {ID: 2}},
		[][]ai.Message{{assistant}, {user}},
	)
	// start is 1, so the cut at 1 leaves no non-empty contribution before it.
	if cut := SelectCut(view, 1); cut != nil {
		t.Fatalf("cut = %v", cut)
	}
}

func TestCutCandidateToolResultRule(t *testing.T) {
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{ai.ToolCall{ID: "c1", Name: "bash"}}, StopReason: ai.StopToolUse,
	}
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}
	result := &ai.ToolResultMessage{ToolCallID: "c1", ToolName: "bash", Timestamp: 1}
	contributions := [][]ai.Message{{assistant}, {user}, {result}}
	if !isCutCandidate(contributions, 0) {
		t.Fatal("an assistant start is a candidate")
	}
	// The user entry is followed by a result of the preceding assistant's call,
	// so it is not a candidate.
	if isCutCandidate(contributions, 1) {
		t.Fatal("a user entry before its matching result is not a candidate")
	}
	if isCutCandidate(contributions, 2) {
		t.Fatal("a tool result start is not a candidate")
	}
}

func TestEstimateContext(t *testing.T) {
	head := EntryRecord{ID: 1}
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop,
		Usage: ai.Usage{TotalTokens: 100},
	}
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}}
	view := compactionView(&head,
		[]EntryRecord{{ID: 1}, {ID: 2}},
		[][]ai.Message{{}, {assistant, user}},
	)
	expected := 100 + ai.EstimateMessageTokens(user)
	if tokens := EstimateContext(view, nil); tokens != expected {
		t.Fatalf("tokens = %d, want %d", tokens, expected)
	}
	extra := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "more"}}
	if tokens := EstimateContext(view, []ai.Message{extra}); tokens != expected+ai.EstimateMessageTokens(extra) {
		t.Fatalf("tokens = %d", tokens)
	}
	// Without a measured assistant every message is estimated.
	plain := compactionView(nil, []EntryRecord{{ID: 1}}, [][]ai.Message{{user}})
	if tokens := EstimateContext(plain, nil); tokens != ai.EstimateMessageTokens(user) {
		t.Fatalf("tokens = %d", tokens)
	}
}

func TestSerializeConversation(t *testing.T) {
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ThinkingContent{Thinking: "hmm"},
			ai.TextContent{Text: "ok"},
			ai.ToolCall{ID: "c1", Name: "read", Arguments: []byte(`{"path":"/a","n":1}`)},
		},
		StopReason: ai.StopToolUse,
	}
	result := &ai.ToolResultMessage{ToolCallID: "c1", ToolName: "read",
		Content: ai.UserContentList{ai.TextContent{Text: "content"}}}
	serialized := SerializeConversation([]ai.Message{user, assistant, result})
	for _, expected := range []string{
		"[User]: hi",
		"[Assistant thinking]: hmm",
		"[Assistant]: ok",
		`[Assistant tool calls]: read(path="/a", n=1)`,
		"[Tool result]: content",
	} {
		if !strings.Contains(serialized, expected) {
			t.Fatalf("serialized = %q missing %q", serialized, expected)
		}
	}
	// A long tool result is truncated at the character bound.
	long := &ai.ToolResultMessage{ToolCallID: "c2", ToolName: "read",
		Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", ToolResultMaxChars+10)}}}
	truncated := SerializeConversation([]ai.Message{long})
	if !strings.Contains(truncated, "more characters truncated]") {
		t.Fatalf("truncated = %q", truncated[len(truncated)-60:])
	}
}

func TestSummaryTextAndFailure(t *testing.T) {
	stop := &ai.AssistantMessage{Content: ai.ContentList{ai.TextContent{Text: "  done  "}}, StopReason: ai.StopStop}
	if text, ok := SummaryText(stop); !ok || text != "done" {
		t.Fatalf("text = %q, %v", text, ok)
	}
	toolCall := &ai.AssistantMessage{Content: ai.ContentList{ai.ToolCall{ID: "c"}}, StopReason: ai.StopToolUse}
	if _, ok := SummaryText(toolCall); ok {
		t.Fatal("a tool call is not a summary")
	}
	length := &ai.AssistantMessage{Content: ai.ContentList{}, StopReason: ai.StopLength}
	if SummaryFailure(length) != "Summarization hit the token limit; the summary is incomplete" {
		t.Fatalf("failure = %q", SummaryFailure(length))
	}
	message := "boom"
	failed := &ai.AssistantMessage{Content: ai.ContentList{}, StopReason: ai.StopError, ErrorMessage: &message}
	if SummaryFailure(failed) != "Summarization failed: boom" {
		t.Fatalf("failure = %q", SummaryFailure(failed))
	}
	if SummaryFailure(toolCall) != "Summarization attempted to call a tool" {
		t.Fatalf("failure = %q", SummaryFailure(toolCall))
	}
	empty := &ai.AssistantMessage{Content: ai.ContentList{}, StopReason: ai.StopStop}
	if SummaryFailure(empty) != "Summarization produced no text" {
		t.Fatalf("failure = %q", SummaryFailure(empty))
	}
}

func TestSummaryPrompt(t *testing.T) {
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}}
	prompt := SummaryPrompt([]ai.Message{user}, nil)
	if !strings.HasPrefix(prompt, "<conversation>\n[User]: hi\n</conversation>\n\n") ||
		!strings.Contains(prompt, SummarizationPrompt) {
		t.Fatalf("prompt = %q", prompt)
	}
	focus := "the build"
	focused := SummaryPrompt([]ai.Message{user}, &focus)
	if !strings.HasSuffix(focused, "\n\nAdditional focus: the build") {
		t.Fatalf("focused = %q", focused)
	}
}

func TestDefineTaskWrapsDefinition(t *testing.T) {
	task := DefineTask(TaskDefinition{Name: "custom", Version: 2})
	if task.Definition.Name != "custom" || task.Definition.Version != 2 {
		t.Fatalf("task = %+v", task.Definition)
	}
}
