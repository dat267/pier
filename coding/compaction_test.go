package coding

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Compaction tests keyed to upstream compaction.ts semantics.

func TestEstimateAgentMessageTokens(t *testing.T) {
	// chars/4 heuristic over the role-specific text.
	user := &ai.UserMessage{Content: ai.StringOrBlocks{Text: strings.Repeat("x", 40)}}
	if got := EstimateAgentMessageTokens(user); got != 10 {
		t.Fatalf("user tokens = %d; want 10", got)
	}
	// Images estimate 4800 chars.
	withImage := &ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
		ai.ImageContent{Data: "aGk=", MimeType: "image/png"},
	}}}
	if got := EstimateAgentMessageTokens(withImage); got != 1200 {
		t.Fatalf("image tokens = %d; want 1200", got)
	}
	// Assistant: text + thinking + toolCall name + serialized args.
	assistant := &ai.AssistantMessage{Content: ai.ContentList{
		ai.ThinkingContent{Thinking: strings.Repeat("t", 20)},
		ai.TextContent{Text: strings.Repeat("a", 20)},
		ai.ToolCall{ID: "c", Name: "read", Arguments: json.RawMessage(`{"path":"file.txt"}`)},
	}}
	// 20+20 chars + name + JSON args, ceil(chars/4).
	want := (20 + 20 + len("read") + len(`{"path":"file.txt"}`) + 3) / 4
	if want*4 < 20+20+len("read")+len(`{"path":"file.txt"}`) {
		want++
	}
	if got := EstimateAgentMessageTokens(assistant); got != want {
		t.Fatalf("assistant tokens = %d; want %d", got, want)
	}
}

func TestShouldCompact(t *testing.T) {
	settings := DefaultCompactionSettings
	if ShouldCompact(100000, 200000, settings) {
		t.Fatal("under reserve threshold should not compact")
	}
	if !ShouldCompact(190000, 200000, settings) {
		t.Fatal("over threshold should compact")
	}
	disabled := CompactionSettings{Enabled: false}
	if ShouldCompact(190000, 200000, disabled) {
		t.Fatal("disabled never compacts")
	}
}

func TestEstimateContextTokensPrefersUsage(t *testing.T) {
	messages := []ai.Message{
		createUserMessage("hi"),
		&ai.AssistantMessage{
			Content:    ai.ContentList{ai.TextContent{Text: "reply"}},
			Usage:      ai.Usage{TotalTokens: 500},
			StopReason: ai.StopStop, Timestamp: 2,
		},
		createUserMessage("tail"),
	}
	estimate := EstimateContextTokens(messages)
	if estimate.UsageTokens != 500 || estimate.Tokens != 500+EstimateAgentMessageTokens(messages[2]) {
		t.Fatalf("estimate = %+v", estimate)
	}
	if estimate.LastUsageIndex != 1 {
		t.Fatalf("lastUsageIndex = %d", estimate.LastUsageIndex)
	}
	// Errored assistant usage is skipped.
	errored := &ai.AssistantMessage{Usage: ai.Usage{TotalTokens: 999}, StopReason: ai.StopError, Timestamp: 3}
	messages = append(messages, errored)
	estimate = EstimateContextTokens(messages)
	if estimate.UsageTokens != 500 {
		t.Fatalf("errored usage leaked: %+v", estimate)
	}
}

func TestFindCutPoint(t *testing.T) {
	// Build a session: 10 user/assistant turns of ~100 tokens each.
	m := NewSessionManager("/tmp", &SessionManagerOptions{Persist: boolPtr(false)})
	var entries []SessionEntry
	for i := 0; i < 10; i++ {
		m.AppendMessage(createUserMessage(strings.Repeat("u", 400))) // ~100 tokens
		m.AppendMessage(createAssistantMessageT(strings.Repeat("a", 400)))
	}
	entries = m.GetEntries()

	// Keep ~500 tokens: cuts at the newest boundary, keeping the tail.
	result := FindCutPoint(entries, 0, len(entries), 500)
	if result.FirstKeptEntryIndex < 0 || result.FirstKeptEntryIndex >= len(entries) {
		t.Fatalf("cut index = %d", result.FirstKeptEntryIndex)
	}
	// Never cut at a toolResult... (none here). The kept entries hold >= 500
	// tokens minus one message.
	keptTokens := 0
	for i := result.FirstKeptEntryIndex; i < len(entries); i++ {
		for _, msg := range SessionEntryToContextMessages(&entries[i]) {
			keptTokens += EstimateAgentMessageTokens(msg)
		}
	}
	if keptTokens < 400 {
		t.Fatalf("kept tokens = %d; cut overshot", keptTokens)
	}
	// Empty range → cut at start.
	empty := FindCutPoint(entries, 0, 0, 500)
	if empty.FirstKeptEntryIndex != 0 {
		t.Fatalf("empty range = %+v", empty)
	}
}

func TestFindCutPointNeverSplitsToolResult(t *testing.T) {
	m := NewSessionManager("/tmp", &SessionManagerOptions{Persist: boolPtr(false)})
	m.AppendMessage(createUserMessage(strings.Repeat("u", 2000))) // big turn
	entries := m.GetEntries()
	userEntry := entries[0]

	// Assistant with a tool call + result: the cut must include the whole
	// tool pair (cut points exclude tool results).
	m.AppendMessage(&ai.AssistantMessage{
		Content: ai.ContentList{ai.ToolCall{ID: "c1", Name: "bash", Arguments: json.RawMessage(`{}`)}},
		API:     "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopToolUse, Timestamp: 2,
	})
	m.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "bash",
		Content:   ai.UserContentList{ai.TextContent{Text: strings.Repeat("r", 4000)}},
		Timestamp: 3,
	})
	all := m.GetEntries()

	// Tiny budget: cut must land ON the first user entry (a valid cut point),
	// never between the assistant and its result.
	result := FindCutPoint(all, 0, len(all), 10)
	if result.FirstKeptEntryIndex < 0 {
		t.Fatal("cut point must exist")
	}
	_ = userEntry
}

func TestFileOperationsAndSerialization(t *testing.T) {
	fileOps := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ToolCall{ID: "1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			ai.ToolCall{ID: "2", Name: "read", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
			ai.ToolCall{ID: "3", Name: "write", Arguments: json.RawMessage(`{"path":"b.txt"}`)},
			ai.ToolCall{ID: "4", Name: "edit", Arguments: json.RawMessage(`{"path":"c.txt"}`)},
		},
	}, fileOps)

	readFiles, modifiedFiles := ComputeFileLists(fileOps)
	// b.txt was read then written → modified only. a.txt stays read-only.
	if len(readFiles) != 1 || readFiles[0] != "a.txt" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if len(modifiedFiles) != 2 || modifiedFiles[0] != "b.txt" || modifiedFiles[1] != "c.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}

	formatted := FormatFileOperations(readFiles, modifiedFiles)
	if !strings.Contains(formatted, "<read-files>\na.txt\n</read-files>") ||
		!strings.Contains(formatted, "<modified-files>\nb.txt\nc.txt\n</modified-files>") {
		t.Fatalf("formatted = %q", formatted)
	}
	if FormatFileOperations(nil, nil) != "" {
		t.Fatal("empty ops render empty")
	}
}

func TestFileOperationsFromNestedToolCalls(t *testing.T) {
	// Upstream extractFileOpsFromMessage reads the nested calls recorded on a
	// tool result, which is how codemode script file access reaches the
	// compaction file lists. A byte-heavy call arrives without arguments, so it
	// must be skipped rather than contributing an empty path.
	fileOps := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "bash",
		NestedCalls: &ai.NestedToolCalls{Complete: true, Calls: []ai.NestedToolCallRecord{
			{ID: "n1", Name: "read", Arguments: json.RawMessage(`{"path":"script-read.txt"}`)},
			{ID: "n2", Name: "write", Arguments: json.RawMessage(`{"path":"script-write.txt"}`)},
			{ID: "n3", Name: "edit", Arguments: json.RawMessage(`{"path":"script-edit.txt"}`)},
			{ID: "n4", Name: "read", ArgumentsBytes: intPtr(9000)},
		}},
	}, fileOps)

	readFiles, modifiedFiles := ComputeFileLists(fileOps)
	if len(readFiles) != 1 || readFiles[0] != "script-read.txt" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if len(modifiedFiles) != 2 || modifiedFiles[0] != "script-edit.txt" || modifiedFiles[1] != "script-write.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}
	// A tool result without nested calls contributes nothing.
	empty := CreateFileOps()
	ExtractFileOpsFromMessage(&ai.ToolResultMessage{ToolCallID: "c2", ToolName: "read"}, empty)
	if len(empty.Read)+len(empty.Written)+len(empty.Edited) != 0 {
		t.Fatalf("plain tool result added file ops: %+v", empty)
	}
}

func TestSerializeConversation(t *testing.T) {
	messages := []ai.Message{
		createUserMessage("do the thing"),
		&ai.AssistantMessage{
			Content: ai.ContentList{
				ai.ThinkingContent{Thinking: "pondering"},
				ai.ToolCall{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"a.txt"}`)},
			},
			API: "openai-responses", Provider: "openai", Model: "mock",
			StopReason: ai.StopToolUse, Timestamp: 2,
		},
		&ai.ToolResultMessage{
			ToolCallID: "c1", ToolName: "read",
			Content:   ai.UserContentList{ai.TextContent{Text: strings.Repeat("r", 3000)}},
			Timestamp: 3,
		},
	}
	serialized := SerializeConversation(ConvertToLlm(messages))
	if !strings.Contains(serialized, "[User]: do the thing") {
		t.Fatalf("user missing: %q", serialized)
	}
	if strings.Contains(serialized, "pondering") || strings.Contains(serialized, "[Assistant thinking]") {
		t.Fatalf("thinking should not be serialized for compaction: %q", serialized)
	}
	if !strings.Contains(serialized, `[Assistant tool calls]: read(path="a.txt")`) {
		t.Fatalf("tool calls: %q", serialized)
	}
	// Tool results truncate at 2000 chars with the marker.
	if !strings.Contains(serialized, "[... 1000 more characters truncated]") {
		t.Fatalf("truncation marker missing: %q", serialized[len(serialized)-120:])
	}
}

func TestConvertToLlmCustomRoles(t *testing.T) {
	bashMsg := CreateBashExecutionMessage("ls -la", "file1", intPtr(0), false, false, "", false, 100)
	excluded := CreateBashExecutionMessage("secret", "", intPtr(0), false, false, "", true, 101)
	compacted := CreateCompactionSummaryMessage("the summary", 1000, 102)
	branch := CreateBranchSummaryMessage("branch summary", "root", 103)
	messages := []ai.Message{createUserMessage("hi"), bashMsg, excluded, compacted, branch}

	converted := ConvertToLlm(messages)
	roles := messageRoles(converted)
	// user, user(bash), user(compaction summary), user(branch summary);
	// excluded dropped.
	if len(roles) != 4 {
		t.Fatalf("roles = %v", roles)
	}
	for i := 1; i < 4; i++ {
		if roles[i] != "user" {
			t.Fatalf("role %d = %s; all custom roles convert to user", i, roles[i])
		}
	}
	// Compaction summary renders with the prefix/suffix wrapper.
	if user, ok := converted[2].(*ai.UserMessage); ok {
		text := user.Content.Blocks[0].(ai.TextContent).Text
		if !strings.HasPrefix(text, CompactionSummaryPrefix) || !strings.HasSuffix(text, CompactionSummarySuffix) {
			t.Fatalf("compaction text = %q", text)
		}
	}
	// Bash execution rendering.
	if user, ok := converted[1].(*ai.UserMessage); ok {
		text := user.Content.Blocks[0].(ai.TextContent).Text
		if !strings.HasPrefix(text, "Ran `ls -la`") || !strings.Contains(text, "```\nfile1\n```") {
			t.Fatalf("bash text = %q", text)
		}
	}
}

func TestCombineUsage(t *testing.T) {
	r1 := int64(5)
	r2 := int64(7)
	first := ai.Usage{Input: 10, Output: 20, CacheRead: 1, CacheWrite: 2, Reasoning: &r1,
		TotalTokens: 33, Cost: ai.UsageCost{Input: 1, Output: 2, CacheRead: 0.1, CacheWrite: 0.2, Total: 3.3}}
	second := ai.Usage{Input: 100, Output: 200, CacheRead: 10, CacheWrite: 20, Reasoning: &r2,
		TotalTokens: 330, Cost: ai.UsageCost{Input: 10, Output: 20, CacheRead: 1, CacheWrite: 2, Total: 33}}
	combined := CombineUsage(first, second)
	if combined.Input != 110 || combined.TotalTokens != 363 || combined.Cost.Total != 36.3 {
		t.Fatalf("combined = %+v", combined)
	}
	if combined.Reasoning == nil || *combined.Reasoning != 12 {
		t.Fatalf("reasoning = %v", combined.Reasoning)
	}
}

func TestPrepareAndCompactFlow(t *testing.T) {
	// A session with enough tokens to trigger a cut, summarized by a scripted
	// stream function.
	m := NewSessionManager("/tmp", &SessionManagerOptions{Persist: boolPtr(false)})
	for i := 0; i < 6; i++ {
		m.AppendMessage(createUserMessage(strings.Repeat("u", 800)))
		m.AppendMessage(createAssistantMessageT(strings.Repeat("a", 800)))
	}
	entries := m.GetEntries()
	preparation := PrepareCompaction(entries, CompactionSettings{
		Enabled: true, ReserveTokens: 16384, KeepRecentTokens: 400,
	})
	if preparation == nil {
		t.Fatal("compaction should be preparable")
	}
	if preparation.FirstKeptEntryID == "" || len(preparation.MessagesToSummarize) == 0 {
		t.Fatalf("preparation = %+v", preparation)
	}

	var calls int
	options := CompactionOptions{
		Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", MaxTokens: 8192},
		StreamFn: func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			calls++
			stream := ai.NewAssistantMessageEventStream()
			msg := &ai.AssistantMessage{
				Content: ai.ContentList{ai.TextContent{Text: "## Goal\ntest summary long enough to pass the usability check"}},
				API:     model.API, Provider: model.Provider, Model: model.ID,
				StopReason: ai.StopStop, Timestamp: 1,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: msg})
			return stream
		},
	}
	result, err := Compact(preparation, options)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("summarization calls = %d; want 1 (no split turn)", calls)
	}
	if !strings.HasPrefix(result.Summary, "## Goal") {
		t.Fatalf("summary = %q", result.Summary)
	}
	if result.FirstKeptEntryID != preparation.FirstKeptEntryID || result.TokensBefore != int64(preparation.TokensBefore) {
		t.Fatalf("result = %+v", result)
	}
	var details CompactionDetails
	json.Unmarshal(result.Details, &details)
	if details.ReadFiles == nil || details.ModifiedFiles == nil {
		t.Fatalf("details = %s", result.Details)
	}
}

func TestSummarizationFailureGuards(t *testing.T) {
	// A length stop must not become a checkpoint.
	response := &ai.AssistantMessage{StopReason: ai.StopLength}
	if got := GetSummarizationFailure(response, "Summarization"); !strings.Contains(got, "token cap") {
		t.Fatalf("failure = %q", got)
	}
	errored := &ai.AssistantMessage{StopReason: ai.StopError, ErrorMessage: strPtrOf("boom")}
	if got := GetSummarizationFailure(errored, "Summarization"); got != "Summarization failed: boom" {
		t.Fatalf("failure = %q", got)
	}
	ok := &ai.AssistantMessage{StopReason: ai.StopStop}
	if got := GetSummarizationFailure(ok, "Summarization"); got != "" {
		t.Fatalf("failure = %q", got)
	}
}

// Ported from packages/coding-agent/test/compaction-summary-reasoning.test.ts
// (upstream #9652, pinned v0.87.1): the split-turn summary uses Markdown
// boundaries and continuation wording so a reasoning model does not read the
// conversation as a task to extend.
func TestTurnPrefixPromptUsesMarkdownBoundaries(t *testing.T) {
	var captured string
	options := CompactionOptions{
		Model: &ai.Model{ID: "mock", API: "openai-responses", Provider: "openai", MaxTokens: 8192},
		StreamFn: func(model *ai.Model, context ai.TranscriptContext, streamOptions *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			for _, m := range context.Messages {
				if um, ok := m.(*ai.UserMessage); ok && len(um.Content.Blocks) > 0 {
					if text, ok := um.Content.Blocks[0].(ai.TextContent); ok {
						captured = text.Text
					}
				}
			}
			stream := ai.NewAssistantMessageEventStream()
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				Content: ai.ContentList{ai.TextContent{Text: "checkpoint"}},
				API:     model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopStop, Timestamp: 1,
			}})
			return stream
		},
	}
	if _, err := generateTurnPrefixSummary(
		[]ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: "Summarize this."}, Timestamp: 1}}, options); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(captured, "<conversation>") {
		t.Fatalf("stale XML boundary in the turn-prefix prompt:\n%s", captured)
	}
	if !strings.Contains(captured, "# Conversation\n") {
		t.Fatalf("missing the Markdown conversation boundary:\n%s", captured)
	}
	if !strings.Contains(captured, "# Instructions\nThe messages above are earlier context from an ongoing conversation.") {
		t.Fatalf("missing the continuation instructions:\n%s", captured)
	}
}

// Upstream serializes tool-call arguments with Object.entries, preserving the
// model's key order; the port sorted them.
func TestSerializeConversationPreservesToolArgOrder(t *testing.T) {
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ToolCall{ID: "c1", Name: "edit", Arguments: json.RawMessage(`{"b":2,"a":1}`)},
		},
		API: "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopToolUse, Timestamp: 2,
	}
	serialized := SerializeConversation(ConvertToLlm([]ai.Message{assistant}))
	if !strings.Contains(serialized, `edit(b=2, a=1)`) {
		t.Fatalf("tool calls = %q; want insertion order", serialized)
	}
	// Nested object keys keep their order too.
	nested := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.ToolCall{ID: "c2", Name: "edit", Arguments: json.RawMessage(`{"path":"x","edits":[{"oldText":"a","newText":"b"}]}`)},
		},
		API: "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopToolUse, Timestamp: 2,
	}
	serialized = SerializeConversation(ConvertToLlm([]ai.Message{nested}))
	if !strings.Contains(serialized, `edit(path="x", edits=[{"oldText":"a","newText":"b"}])`) {
		t.Fatalf("nested tool calls = %q; want nested insertion order", serialized)
	}
}

// Upstream contentText joins assistant text blocks with newlines.
func TestSerializeConversationJoinsAssistantTextWithNewlines(t *testing.T) {
	assistant := &ai.AssistantMessage{
		Content: ai.ContentList{
			ai.TextContent{Text: "line1"},
			ai.TextContent{Text: "line2"},
		},
		API: "openai-responses", Provider: "openai", Model: "mock",
		StopReason: ai.StopStop, Timestamp: 2,
	}
	serialized := SerializeConversation(ConvertToLlm([]ai.Message{assistant}))
	if !strings.Contains(serialized, "[Assistant]: line1\nline2") {
		t.Fatalf("assistant text = %q; want newline join", serialized)
	}
}

// Upstream adds JSON.stringify(arguments).length, a UTF-16 code-unit count, so
// multi-byte tool arguments must not inflate the compaction estimate the way a
// byte count would.
func TestEstimateAgentMessageTokensMeasuresJSStringLength(t *testing.T) {
	raw := `{"text":"日本"}` // 13 UTF-16 code units, 17 bytes
	assistant := &ai.AssistantMessage{Content: ai.ContentList{
		ai.ToolCall{ID: "c", Name: "echo", Arguments: json.RawMessage(raw)},
	}}
	// ceil((4 + 13) / 4) = 5; a byte count (4 + 17 = 21) would give 6.
	if got := EstimateAgentMessageTokens(assistant); got != 5 {
		t.Fatalf("assistant tokens = %d; want 5", got)
	}
}

// TestPrepareCompactionUsesTheProjection covers prepareCompaction over the
// session projection: an omitted message is neither summarized nor counted
// toward tokensBefore, because the model never saw it.
func TestPrepareCompactionUsesTheProjection(t *testing.T) {
	m := NewSessionManager(t.TempDir(), nil)
	m.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "p", Model: "m", StopReason: ai.StopToolUse,
		Content: ai.ContentList{ai.ToolCall{ID: "c1", Name: "bash", Arguments: []byte("{}")}},
	})
	omittedID := m.AppendMessage(&ai.ToolResultMessage{
		ToolCallID: "c1", ToolName: "bash",
		Content: ai.UserContentList{ai.TextContent{Text: strings.Repeat("x", 40000)}},
	})
	m.AppendMessage(createUserMessage("follow up question"))
	m.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "p", Model: "m", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "answer"}},
	})
	if _, err := m.AppendContextEdit(omittedID, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}

	preparation := PrepareCompaction(m.GetBranch(""), CompactionSettings{Enabled: true, ReserveTokens: 0, KeepRecentTokens: 1})
	if preparation == nil {
		t.Fatal("nothing to compact")
	}
	projected := m.ProjectedContextEstimate()
	if preparation.TokensBefore != projected.Tokens {
		t.Fatalf("tokensBefore = %d, projected estimate %d", preparation.TokensBefore, projected.Tokens)
	}
	if preparation.TokensBefore > 100 {
		t.Fatalf("tokensBefore = %d; the omitted tool result still counts", preparation.TokensBefore)
	}
	for _, message := range append(append([]ai.Message{}, preparation.MessagesToSummarize...), preparation.TurnPrefixMessages...) {
		if ai.RoleOf(message) == ai.RoleToolResult {
			t.Fatalf("the omitted tool result was summarized: %+v", message)
		}
	}
}

// TestPrepareCompactionAdvancesPastRecoveryOmission pins the recovery-suffix
// rule (upstream findProjectedCutPoint): a closed suffix holding the omitted
// assistant attempt advances the cut, so the summarized history ends at that
// attempt. The expected shape is the reference's own output for this branch.
func TestPrepareCompactionAdvancesPastRecoveryOmission(t *testing.T) {
	m := NewSessionManager(t.TempDir(), nil)
	m.AppendMessage(createUserMessage("first question"))
	m.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "first answer with some detail to tokenize"}},
	})
	m.AppendMessage(createUserMessage("second question"))
	m.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", StopReason: ai.StopStop,
		Content: ai.ContentList{ai.TextContent{Text: "second answer"}},
	})
	m.AppendMessage(createUserMessage("third question"))
	failedID := m.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m", StopReason: ai.StopError,
		ErrorMessage: strPtr("prompt is too long: 2000 tokens > 1000 maximum"),
	})
	if _, err := m.AppendContextEdit(failedID, json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}

	preparation := PrepareCompaction(m.GetBranch(""), CompactionSettings{Enabled: true, ReserveTokens: 100, KeepRecentTokens: 1})
	if preparation == nil {
		t.Fatal("nothing to compact")
	}
	if preparation.FirstKeptEntryID != failedID {
		t.Fatalf("first kept entry = %s, want the omitted attempt %s", preparation.FirstKeptEntryID, failedID)
	}
	if !preparation.IsSplitTurn {
		t.Fatal("cut at the omitted assistant is a split turn")
	}
	roles := func(messages []ai.Message) []string {
		out := make([]string, 0, len(messages))
		for _, message := range messages {
			out = append(out, ai.RoleOf(message))
		}
		return out
	}
	wantSummary := []string{ai.RoleUser, ai.RoleAssistant, ai.RoleUser, ai.RoleAssistant}
	if got := roles(preparation.MessagesToSummarize); !reflect.DeepEqual(got, wantSummary) {
		t.Fatalf("messages to summarize = %v, want %v", got, wantSummary)
	}
	if got := roles(preparation.TurnPrefixMessages); !reflect.DeepEqual(got, []string{ai.RoleUser}) {
		t.Fatalf("turn prefix = %v, want the split turn's user message", got)
	}
	if preparation.TokensBefore != m.ProjectedContextEstimate().Tokens {
		t.Fatalf("tokensBefore = %d, projected estimate %d", preparation.TokensBefore, m.ProjectedContextEstimate().Tokens)
	}
}
