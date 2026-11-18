package coding

import (
	"encoding/json"
	"fmt"
	"github.com/dat267/pier/ai"
	"strings"
	"testing"
)

// Compaction tests keyed to upstream compaction.ts semantics.

// capModelStreamFn simulates a provider whose answer room is a message count: a
// prompt carrying more than maxMessages conversation lines stops at the token
// cap, anything smaller completes. It lets the escalation be tested against a
// monotone constraint instead of a hand-written attempt script.
func capModelStreamFn(t *testing.T, maxMessages int, requests *int) StreamFnFn {
	t.Helper()
	return func(model *ai.Model, context ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		if requests != nil {
			*requests++
		}
		prompt := ""
		for _, message := range context.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				prompt += ai.ContentText(user.Content, "")
			}
		}
		stream := ai.NewAssistantMessageEventStream()
		if strings.Count(prompt, "[User]: ") > maxMessages {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopLength, Message: &ai.AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "## Goal\npartial"}}, StopReason: ai.StopLength,
			}})
			return stream
		}
		text := "## Goal\n" + lastConversationMarker(prompt)
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
			API: model.API, Provider: model.Provider, Model: model.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
		}})
		return stream
	}
}

// lastConversationMarker extracts the last serialized user line so a test can
// see which chunk a summary came from.
func lastConversationMarker(prompt string) string {
	marker := ""
	for line := range strings.SplitSeq(prompt, "\n") {
		if after, ok := strings.CutPrefix(line, "[User]: "); ok {
			marker = after
		}
	}
	return marker
}

// A summarization whose input is the problem falls back to smaller chunks merged
// into the summary so far, instead of failing the compaction: each chunk gets
// the whole budget (D264).
func TestSummarizationSplitsTheInputWhenTheCapKeepsHitting(t *testing.T) {
	requests := 0
	messages := make([]ai.Message, 0, 8)
	for index := range 8 {
		messages = append(messages, &ai.UserMessage{
			Content: ai.StringOrBlocks{Text: fmt.Sprintf("chunk-%d %s", index, strings.Repeat("x", 200))},
		})
	}
	model := &ai.Model{ID: "mock", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 64000}
	result, err := generateSummaryWithUsage(messages, CompactionOptions{
		Model: model, StreamFn: capModelStreamFn(t, 4, &requests),
	}, "")
	if err != nil {
		t.Fatalf("err = %v; want the split fallback to complete the summary", err)
	}
	if result.text == "" || !strings.Contains(result.text, "chunk-7") {
		t.Fatalf("summary = %q; want the last chunk folded in", result.text)
	}
	if requests < 3 {
		t.Fatalf("requests = %d; want the whole-input attempts plus one split", requests)
	}
}

// The escalation is bounded: an input that never fits stops with upstream's
// message instead of re-sending the conversation forever.
func TestSummarizationSplitStaysBounded(t *testing.T) {
	requests := 0
	streamFn := func(model *ai.Model, _ ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		requests++
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopLength, Message: &ai.AssistantMessage{
			API: model.API, Provider: model.Provider, Model: model.ID,
			Content: ai.ContentList{ai.TextContent{Text: "## Goal\npartial"}}, StopReason: ai.StopLength,
		}})
		return stream
	}
	messages := make([]ai.Message, 0, 16)
	for index := range 16 {
		messages = append(messages, &ai.UserMessage{Content: ai.StringOrBlocks{Text: fmt.Sprintf("chunk-%d", index)}})
	}
	model := &ai.Model{ID: "mock", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 64000}
	_, err := generateSummaryWithUsage(messages, CompactionOptions{Model: model, StreamFn: streamFn}, "")
	if err == nil || !strings.Contains(err.Error(), "token cap") {
		t.Fatalf("err = %v; want the upstream failure", err)
	}
	if requests > maxSummarizationRequests {
		t.Fatalf("requests = %d; want at most %d", requests, maxSummarizationRequests)
	}
}

// heavyMessage is a message whose estimated weight dominates a probe slice, so
// the weight-only midpoint lands where the test wants it.
func heavyMessage(chars int) ai.StringOrBlocks {
	return ai.StringOrBlocks{Text: strings.Repeat("x", chars)}
}

// A chunk is summarized with the calls it answers: the split never lands between
// a tool call and its result, which would leave one half with a call it cannot
// explain and the other with an unexplained result.
func TestSummarizationSplitKeepsToolCallsWithTheirResults(t *testing.T) {
	// Small, small, call, result, small, small: the weight-only midpoint is 3,
	// which is exactly the gap between the call and the result.
	pair := []ai.Message{
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.AssistantMessage{
			Content: ai.ContentList{
				ai.TextContent{Text: strings.Repeat("x", 4000)},
				ai.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
			},
			StopReason: ai.StopToolUse,
		},
		&ai.ToolResultMessage{ToolCallID: "call-1", ToolName: "bash", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 4000)}}},
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(40)},
	}
	if midpoint := tokenMidpoint(pair); midpoint != 3 {
		t.Fatalf("probe midpoint = %d; want 3 for the test to exercise the gap", midpoint)
	}
	if got := summarizationSplitPoint(pair); got == 3 {
		t.Fatalf("split = 3; the call and its result must stay in one chunk")
	} else if got != 2 && got != 4 {
		t.Fatalf("split = %d; want the nearest boundary outside the pair (2 or 4)", got)
	}

	// A parallel batch keeps the whole batch together.
	batch := []ai.Message{
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.AssistantMessage{Content: ai.ContentList{
			ai.TextContent{Text: strings.Repeat("x", 2000)},
			ai.ToolCall{ID: "a", Name: "read", Arguments: json.RawMessage(`{}`)},
			ai.ToolCall{ID: "b", Name: "read", Arguments: json.RawMessage(`{}`)},
		}},
		&ai.ToolResultMessage{ToolCallID: "a", ToolName: "read", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 2000)}}},
		&ai.ToolResultMessage{ToolCallID: "b", ToolName: "read", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 2000)}}},
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(40)},
	}
	if got := summarizationSplitPoint(batch); got != 2 && got != 5 {
		t.Fatalf("split = %d; want 2 or 5, outside the whole batch", got)
	}

	// A result whose call was summarized earlier imposes nothing, so the split
	// is the plain midpoint by weight.
	orphan := []ai.Message{
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(4000)},
		&ai.ToolResultMessage{ToolCallID: "gone", ToolName: "read", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 4000)}}},
		&ai.UserMessage{Content: heavyMessage(4000)},
	}
	if got, want := summarizationSplitPoint(orphan), tokenMidpoint(orphan); got != want {
		t.Fatalf("split = %d; want the plain midpoint %d", got, want)
	}
}

// tokenMidpoint is the weight-only split the escalation used before tool calls
// had to stay with their results.
func tokenMidpoint(messages []ai.Message) int {
	total := 0
	for _, message := range messages {
		total += EstimateAgentMessageTokens(message)
	}
	half := total / 2
	sum := 0
	for index, message := range messages {
		sum += EstimateAgentMessageTokens(message)
		if sum >= half {
			return index + 1
		}
	}
	return len(messages)
}

// The split fallback folds the call and its result into one chunk in practice.
func TestSummarizationSplitSummarizesACallWithItsResult(t *testing.T) {
	prompts := make([]string, 0, 8)
	model := &ai.Model{ID: "mock", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 64000}
	streamFn := func(model *ai.Model, context ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		prompt := ""
		for _, message := range context.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				prompt += ai.ContentText(user.Content, "")
			}
		}
		prompts = append(prompts, prompt)
		stream := ai.NewAssistantMessageEventStream()
		reason := ai.StopStop
		text := "## Goal\nsummary " + itoa(len(prompts))
		// The answer room is three messages, so the six-message input must split.
		if strings.Count(prompt, "[User]: ") > 3 {
			reason = ai.StopLength
			text = "## Goal\npartial"
		}
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: reason, Message: &ai.AssistantMessage{
			API: model.API, Provider: model.Provider, Model: model.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: reason,
		}})
		return stream
	}
	// Small, small, call, result, small, small: the weight midpoint is the gap
	// between the call and the result.
	messages := []ai.Message{
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.AssistantMessage{
			Content: ai.ContentList{
				ai.TextContent{Text: strings.Repeat("x", 4000)},
				ai.ToolCall{ID: "call-1", Name: "bash", Arguments: json.RawMessage(`{"command":"ls"}`)},
			},
			StopReason: ai.StopToolUse,
		},
		&ai.ToolResultMessage{ToolCallID: "call-1", ToolName: "bash", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 4000)}}},
		&ai.UserMessage{Content: heavyMessage(40)},
		&ai.UserMessage{Content: heavyMessage(40)},
	}
	if _, err := generateSummaryWithUsage(messages, CompactionOptions{Model: model, StreamFn: streamFn}, ""); err != nil {
		t.Fatal(err)
	}
	for _, prompt := range prompts {
		hasCall := strings.Contains(prompt, "[Assistant tool calls]: bash(")
		hasResult := strings.Contains(prompt, "[Tool result]: ")
		if hasCall != hasResult {
			t.Fatalf("chunk has call=%v result=%v; want them together:\n%s", hasCall, hasResult, prompt)
		}
	}
}

// pi v1.5.2 (b4afdddf) core/compaction/utils.ts truncateForSummary slices with
// sliceHead, which never splits a surrogate pair. JSSlice cuts between the two
// halves, and the lone surrogate decodes to U+FFFD in the summary.
func TestTruncateForSummaryKeepsSurrogatePairsWhole(t *testing.T) {
	// 1999 ASCII units then an emoji: the 2000th unit is its high surrogate.
	text := strings.Repeat("a", 1999) + "😀" + strings.Repeat("b", 10)
	got := truncateForSummary(text, 2000)
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatalf("summary holds a replacement character: %q", got[len(got)-80:])
	}
	if !strings.HasPrefix(got, strings.Repeat("a", 1999)) {
		t.Fatalf("summary lost its head: %q", got[:20])
	}
	if strings.Contains(got, "😀") {
		t.Fatalf("the cut kept the whole emoji: %q", got[len(got)-80:])
	}
}

// D264 keeps the pinned compaction prompts but escalates length stops by
// splitting. When every boundary separates a call/result pair, its documented
// fallback still needs two nonempty chunks, even if the result dominates weight.
// Reference: pi v1.4.0 (4a080252), core/compaction/compaction.ts compact.
func TestCompactionSplitsResultHeavyToolPairAtLastResort(t *testing.T) {
	model := &ai.Model{ID: "m", API: ai.APIOpenAICompletions, Provider: "test", ContextWindow: 100000}
	var prompts []string
	streamFn := func(model *ai.Model, transcript ai.TranscriptContext, _ *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		prompt := ""
		for _, message := range transcript.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				prompt = ai.ContentText(user.Content, "")
			}
		}
		prompts = append(prompts, prompt)
		reason := ai.StopStop
		text := "checkpoint"
		// A provider with room for one source message can summarize either chunk,
		// but returns an incomplete answer when both are sent at once.
		if strings.Contains(prompt, "[Assistant tool calls]:") && strings.Contains(prompt, "[Tool result]:") {
			reason = ai.StopLength
			text = "partial"
		}
		stream := ai.NewAssistantMessageEventStream()
		stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: reason, Message: &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: reason, Content: ai.ContentList{ai.TextContent{Text: text}}, Usage: ai.Usage{Input: 1, TotalTokens: 1}}})
		return stream
	}
	preparation := &CompactionPreparation{
		FirstKeptEntryID: "kept", FileOps: CreateFileOps(),
		MessagesToSummarize: []ai.Message{
			&ai.AssistantMessage{Content: ai.ContentList{ai.ToolCall{ID: "call", Name: "read", Arguments: json.RawMessage(`{"path":"file"}`)}}, StopReason: ai.StopToolUse},
			&ai.ToolResultMessage{ToolCallID: "call", ToolName: "read", Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 4000)}}},
		},
	}
	result, err := Compact(preparation, CompactionOptions{Model: model, StreamFn: streamFn})
	if err != nil {
		t.Fatalf("result-heavy pair could be summarized as two chunks: %v (requests=%d)", err, len(prompts))
	}
	if len(prompts) != 3 {
		t.Fatalf("requests = %d, want whole input then two chunks", len(prompts))
	}
	if !strings.Contains(prompts[2], "<previous-summary>\ncheckpoint\n</previous-summary>") {
		t.Fatal("last chunk lost the first checkpoint")
	}
	if result.Usage == nil || result.Usage.Input != 3 {
		t.Fatalf("usage = %+v, want all three requests", result.Usage)
	}
}
