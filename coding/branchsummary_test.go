package coding

import (
	ctxpkg "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// Round 106 tests: branch summarization and session tree navigation.

func branchEntries() []SessionEntry {
	parent := "root"
	return []SessionEntry{
		{Type: "message", ID: "root", Timestamp: "2024-01-01T00:00:00Z",
			Message: mustJSONMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "start"}})},
		{Type: "message", ID: "a1", ParentID: &parent, Timestamp: "2024-01-01T00:01:00Z",
			Message: mustJSONMessage(&ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
				Content: ai.ContentList{
					ai.TextContent{Text: "reading"},
					ai.ToolCall{ID: "call-1", Name: "read", Arguments: json.RawMessage(`{"path":"/tmp/a.txt"}`)},
				},
				StopReason: ai.StopStop,
			})},
		{Type: "message", ID: "a2", ParentID: new("a1"), Timestamp: "2024-01-01T00:02:00Z",
			Message: mustJSONMessage(&ai.ToolResultMessage{
				ToolCallID: "call-1", ToolName: "read",
				Content: ai.UserContentList{ai.TextContent{Text: "file body"}},
			})},
		{Type: "message", ID: "a3", ParentID: new("a2"), Timestamp: "2024-01-01T00:03:00Z",
			Message: mustJSONMessage(&ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
				Content: ai.ContentList{
					ai.ToolCall{ID: "call-2", Name: "edit", Arguments: json.RawMessage(`{"path":"/tmp/b.txt"}`)},
				},
				StopReason: ai.StopStop,
			})},
	}
}

func mustJSONMessage(message ai.Message) json.RawMessage {
	encoded, err := ai.MarshalMessage(message)
	if err != nil {
		panic(err)
	}
	return encoded
}

func TestCollectEntriesForBranchSummary(t *testing.T) {
	manager := newTestSessionManager(t)
	first := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "root"}})
	second := manager.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "branch one"}}, StopReason: ai.StopStop,
	})
	third := manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "leaf"}})

	// No old leaf means nothing to summarize.
	if result := CollectEntriesForBranchSummary(manager, "", third); len(result.Entries) != 0 || result.CommonAncestorID != "" {
		t.Fatalf("result = %+v", result)
	}

	// Walking from the leaf back to the root yields every entry chronologically.
	result := CollectEntriesForBranchSummary(manager, third, first)
	if len(result.Entries) != 2 {
		t.Fatalf("entries = %+v", result.Entries)
	}
	if result.Entries[0].ID != second || result.Entries[1].ID != third {
		t.Fatalf("entries = %+v", result.Entries)
	}
	if !result.HasAncestor || result.CommonAncestorID != first {
		t.Fatalf("result = %+v", result)
	}
}

func TestPrepareBranchEntries(t *testing.T) {
	entries := branchEntries()
	preparation := PrepareBranchEntries(entries, 0)
	// The tool result entry is skipped; the branch summary and compaction
	// summaries would be included when present.
	for _, message := range preparation.Messages {
		if _, ok := message.(*ai.ToolResultMessage); ok {
			t.Fatalf("tool results must be skipped: %+v", preparation.Messages)
		}
	}
	if len(preparation.Messages) != 3 {
		t.Fatalf("messages = %+v", preparation.Messages)
	}
	// File ops are collected from the tool calls.
	readFiles, modifiedFiles := ComputeFileLists(preparation.FileOps)
	if strings.Join(readFiles, ",") != "/tmp/a.txt" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if strings.Join(modifiedFiles, ",") != "/tmp/b.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}
	if preparation.TotalTokens <= 0 {
		t.Fatalf("totalTokens = %d", preparation.TotalTokens)
	}

	// A tiny budget drops older messages but keeps the newest.
	tiny := PrepareBranchEntries(entries, 1)
	if len(tiny.Messages) != 0 {
		t.Fatalf("messages = %+v", tiny.Messages)
	}
}

// pi v1.5.0 d0fe21308 (#11680): the branch summary input honors context edits,
// so omitted entries stay out of the summary and replaced entries arrive in
// their replacement form, file operations included.
func TestPrepareBranchEntriesAppliesContextEdits(t *testing.T) {
	entries := branchEntries()
	entries = append(entries,
		SessionEntry{Type: "context_edit", ID: "edit-omit", TargetID: "a1", Replacement: json.RawMessage(`null`)},
		SessionEntry{Type: "context_edit", ID: "edit-replace", TargetID: "root",
			Replacement: json.RawMessage(`{"content":"replaced start"}`)},
	)
	preparation := PrepareBranchEntries(entries, 0)
	if len(preparation.Messages) != 2 {
		t.Fatalf("messages = %+v", preparation.Messages)
	}
	user, ok := preparation.Messages[0].(*ai.UserMessage)
	if !ok || user.Content.Text != "replaced start" {
		t.Fatalf("first message = %+v", preparation.Messages[0])
	}
	assistant, ok := preparation.Messages[1].(*ai.AssistantMessage)
	if !ok || len(assistant.Content) != 1 {
		t.Fatalf("second message = %+v", preparation.Messages[1])
	}
	if _, ok := assistant.Content[0].(ai.ToolCall); !ok {
		t.Fatalf("second message = %+v", preparation.Messages[1])
	}
	// The omitted assistant message carried the read call, so its file op is gone.
	readFiles, modifiedFiles := ComputeFileLists(preparation.FileOps)
	if strings.Join(readFiles, ",") != "" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if strings.Join(modifiedFiles, ",") != "/tmp/b.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}
}

func TestPrepareBranchEntriesCumulativeFileOps(t *testing.T) {
	details, err := ai.MarshalJSON(BranchSummaryDetails{
		ReadFiles: []string{"/tmp/nested.txt"}, ModifiedFiles: []string{"/tmp/written.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entries := []SessionEntry{
		{Type: "branch_summary", Summary: "nested", Details: details},
	}
	preparation := PrepareBranchEntries(entries, 0)
	readFiles, modifiedFiles := ComputeFileLists(preparation.FileOps)
	if strings.Join(readFiles, ",") != "/tmp/nested.txt" {
		t.Fatalf("readFiles = %v", readFiles)
	}
	if strings.Join(modifiedFiles, ",") != "/tmp/written.txt" {
		t.Fatalf("modifiedFiles = %v", modifiedFiles)
	}

	// Extension-generated summaries do not contribute file ops.
	fromHook := true
	entries = []SessionEntry{
		{Type: "branch_summary", Summary: "nested", Details: details, FromHook: &fromHook},
	}
	preparation = PrepareBranchEntries(entries, 0)
	readFiles, modifiedFiles = ComputeFileLists(preparation.FileOps)
	if len(readFiles) != 0 || len(modifiedFiles) != 0 {
		t.Fatalf("readFiles = %v modifiedFiles = %v", readFiles, modifiedFiles)
	}
}

func TestGenerateBranchSummary(t *testing.T) {
	entries := branchEntries()
	calls := 0
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		calls++
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				Content:    ai.ContentList{ai.TextContent{Text: "## Goal\ndo the thing"}},
				StopReason: ai.StopStop,
				Usage:      ai.Usage{Input: 100, Output: 20, TotalTokens: 120},
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192}
	result, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{
		Model: model, StreamFn: streamFn, Ctx: ctxpkg.Background(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if !strings.HasPrefix(result.Summary, "The user explored a different conversation branch") ||
		!strings.Contains(result.Summary, "## Goal") {
		t.Fatalf("summary = %q", result.Summary)
	}
	if !strings.Contains(result.Summary, "/tmp/a.txt") || !strings.Contains(result.Summary, "/tmp/b.txt") {
		t.Fatalf("summary = %q", result.Summary)
	}
	if result.Usage == nil || result.Usage.Input != 100 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if strings.Join(result.ReadFiles, ",") != "/tmp/a.txt" || strings.Join(result.ModifiedFiles, ",") != "/tmp/b.txt" {
		t.Fatalf("files = %v %v", result.ReadFiles, result.ModifiedFiles)
	}

	// No content to summarize.
	empty, err := GenerateBranchSummary(nil, GenerateBranchSummaryOptions{Model: model, StreamFn: streamFn})
	if err != nil || empty.Summary != "No content to summarize" {
		t.Fatalf("result = %+v err = %v", empty, err)
	}

	// Custom instructions are appended, or replace the prompt when asked.
	var promptText string
	capture := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		for _, message := range context.Messages {
			if user, ok := message.(*ai.UserMessage); ok {
				promptText = contentTextJoinedNoSep(user.Content)
			}
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				Content: ai.ContentList{ai.TextContent{Text: "body"}}, StopReason: ai.StopStop,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: message})
		}()
		return stream
	}
	if _, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{
		Model: model, StreamFn: capture, CustomInstructions: "focus on tests",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(promptText, "Additional focus: focus on tests") {
		t.Fatalf("prompt = %q", promptText)
	}
	if _, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{
		Model: model, StreamFn: capture, CustomInstructions: "only this", ReplaceInstructions: true,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(promptText, "\n\nonly this") {
		t.Fatalf("prompt = %q", promptText)
	}

	// An aborted response reports the abort.
	abortFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				StopReason: ai.StopAborted,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: message})
		}()
		return stream
	}
	aborted, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{Model: model, StreamFn: abortFn})
	if err != nil || !aborted.Aborted {
		t.Fatalf("result = %+v err = %v", aborted, err)
	}

	// A tool call in the response is an error.
	toolFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				Content:    ai.ContentList{ai.ToolCall{ID: "t", Name: "read", Arguments: json.RawMessage(`{}`)}},
				StopReason: ai.StopToolUse,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopToolUse, Message: message})
		}()
		return stream
	}
	toolResult, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{Model: model, StreamFn: toolFn})
	if err != nil || toolResult.Error != "Branch summarization attempted to call a tool" {
		t.Fatalf("result = %+v err = %v", toolResult, err)
	}

	// An errored response reports the summarization failure.
	errorFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			errorText := "boom"
			message := &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				StopReason: ai.StopError, ErrorMessage: &errorText,
			}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopError, Error: message})
		}()
		return stream
	}
	failed, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{Model: model, StreamFn: errorFn})
	if err != nil || failed.Error == "" || !strings.Contains(failed.Error, "Branch summarization") {
		t.Fatalf("result = %+v err = %v", failed, err)
	}
}

// A branch summary takes no output limit either: upstream v1.4.0 6fcb48670
// removed the hardcoded 4096 cap so the provider sizes the answer from the
// request, like every other summary.
// pi v1.5.0 269afc66a (#11482): a reasoning model summarizes a branch at the
// session's thinking level, and an off level sends no reasoning at all.
func TestBranchSummarySendsTheSessionThinkingLevel(t *testing.T) {
	entries := branchEntries()
	reasoningModel := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic",
		ContextWindow: 100000, MaxTokens: 8192, Reasoning: true}
	run := func(model *ai.Model, level ai.ThinkingLevel) ai.ThinkingLevel {
		var seen ai.ThinkingLevel
		streamFn := func(called *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			seen = options.Reasoning
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
					API: ai.APIAnthropicMessages, Provider: "anthropic", Model: called.ID,
					Content:    ai.ContentList{ai.TextContent{Text: "## Goal\ndo the thing"}},
					StopReason: ai.StopStop,
				}})
			}()
			return stream
		}
		if _, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{
			Model: model, StreamFn: streamFn, Ctx: ctxpkg.Background(), ThinkingLevel: level,
		}); err != nil {
			t.Fatal(err)
		}
		return seen
	}
	if level := run(reasoningModel, ai.ThinkHigh); level != ai.ThinkHigh {
		t.Fatalf("reasoning = %q, want %q", level, ai.ThinkHigh)
	}
	if level := run(reasoningModel, ai.ThinkOff); level != "" {
		t.Fatalf("off level sent reasoning %q", level)
	}
	plainModel := *reasoningModel
	plainModel.Reasoning = false
	if level := run(&plainModel, ai.ThinkHigh); level != "" {
		t.Fatalf("non-reasoning model sent reasoning %q", level)
	}
}

func TestBranchSummarySendsNoOutputLimit(t *testing.T) {
	entries := branchEntries()
	var limit *int
	var seen bool
	streamFn := func(model *ai.Model, _ ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		seen = true
		limit = options.MaxTokens
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: model.ID,
				Content:    ai.ContentList{ai.TextContent{Text: "## Goal\ndo the thing"}},
				StopReason: ai.StopStop,
			}})
		}()
		return stream
	}
	if _, err := GenerateBranchSummary(entries, GenerateBranchSummaryOptions{
		Model:    &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000, MaxTokens: 8192},
		StreamFn: streamFn, Ctx: ctxpkg.Background(),
	}); err != nil {
		t.Fatal(err)
	}
	if !seen {
		t.Fatal("no branch summary request was sent")
	}
	if limit != nil {
		t.Fatalf("branch summary sent maxTokens %d; want none", *limit)
	}
}

// Reference: pi v1.4.0 (4a080252), core/compaction/branch-summarization.ts
// prepareBranchEntries admits newest first but returns chronological messages.
// Compare allocation growth to catch Go prepends copying the growing slice.
// Decoder and race-instrumentation costs can differ, so an absolute per-message
// ceiling tests the environment rather than the collection's scaling (CR-075).
func TestBranchPreparationAllocationsStayLinear(t *testing.T) {
	const smallCount, largeCount = 512, 2048
	entries := make([]SessionEntry, largeCount)
	for i := range entries {
		entries[i] = SessionEntry{Type: "message", Message: json.RawMessage(fmt.Sprintf(`{"role":"user","content":"message-%d","timestamp":1}`, i))}
	}
	// Keep every message: the test measures allocation growth, not truncation.
	const tokenBudget = 1_000_000
	PrepareBranchEntries(entries[:1], tokenBudget)
	measure := func(input []SessionEntry) uint64 {
		t.Helper()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		preparation := PrepareBranchEntries(input, tokenBudget)
		runtime.ReadMemStats(&after)
		if len(preparation.Messages) != len(input) {
			t.Fatalf("retained %d messages, want %d", len(preparation.Messages), len(input))
		}
		for i, message := range preparation.Messages {
			user, ok := message.(*ai.UserMessage)
			if !ok || user.Content.Text != fmt.Sprintf("message-%d", i) {
				t.Fatalf("message %d out of chronological order: %#v", i, message)
			}
		}
		return after.TotalAlloc - before.TotalAlloc
	}
	small := measure(entries[:smallCount])
	large := measure(entries)
	// Four times the input may use up to six times the allocations, leaving
	// headroom for decoder/runtime variance. The former quadratic prepends
	// grow much faster at these sizes, even with linear decoding overhead.
	if small == 0 || large > small*6 {
		t.Fatalf("allocation growth: %d messages = %d bytes, %d messages = %d bytes; want at most 6x growth for 4x input", smallCount, small, largeCount, large)
	}
	t.Logf("allocation growth: %d messages = %d bytes, %d messages = %d bytes", smallCount, small, largeCount, large)
}

// pi v1.5.2 (b4afdddf) core/session-manager.ts branchWithSummary takes
// `branchFromId: string | null` and writes it straight into `parentId` and
// `this.leafId`, so branching to the root persists `"parentId":null`, not "".
// A "" parent also stayed the leaf, so every later append inherited it.
func TestBranchSummaryToRootPersistsNullParent(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir})
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hi"}})
	summaryID := manager.BranchWithSummary("", "summary", nil, false, nil)
	if summaryID == "" {
		t.Fatal("branch summary not appended")
	}
	if entry := manager.GetEntry(summaryID); entry == nil || entry.ParentID != nil {
		t.Fatalf("summary parent = %v, want nil", entry)
	}
	manager.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "after"}})

	raw, err := os.ReadFile(filepath.Join(manager.GetSessionDir(), filepath.Base(manager.GetSessionFile())))
	if err != nil {
		t.Fatal(err)
	}
	// Only the summary itself is a root: appendEntry makes it the new leaf, so
	// the next message is its child (upstream does the same).
	found := false
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		var entry map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %q: %v", line, err)
		}
		if string(entry["type"]) != `"branch_summary"` {
			continue
		}
		found = true
		if string(entry["parentId"]) != "null" {
			t.Fatalf("persisted parentId = %s in %q, want null", entry["parentId"], line)
		}
	}
	if !found {
		t.Fatal("branch_summary line not persisted")
	}
}

// Reference: pi v1.6.0 core/branch-summary.ts prepareBranchEntries reads
// `entry.message` for every message entry on the branch, and upstream keeps
// parsed messages in memory. `GetBranch` hands out index-only shells after a
// file load (D488), so `branchMessageFromEntry` unmarshalled an empty member,
// returned nil for every message, and a /tree summarize over a resumed session
// saw no conversation at all: on the 248MB session, 69,868 message entries
// produced zero summary messages and only the 13 compaction/branch summaries
// remained.
func TestPrepareBranchEntriesReadsLoadedMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}`,
		`{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":"first question","timestamp":1}}`,
		`{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-01-01T00:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"an answer"}],"api":"openai-completions","provider":"acme","model":"m1","usage":{},"stopReason":"stop","timestamp":2}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)

	branch := manager.GetBranch("")
	if prepared := PrepareBranchEntries(branch, 100000); len(prepared.Messages) != 0 {
		t.Fatalf("the unhydrated branch prepared %d messages; the shells carry no bytes", len(prepared.Messages))
	}
	prepared := manager.PrepareBranchEntries(100000)
	if len(prepared.Messages) != 2 {
		t.Fatalf("prepared messages = %d, want the two entries", len(prepared.Messages))
	}
}

// GenerateBranchSummary prepares its input through prepareBranchEntriesWith, so
// the collected entries of a resumed session reach the summary. That step is
// pinned by TestPrepareBranchEntriesReadsLoadedMessages; assert here that the
// option the real caller fills is the one the preparation uses.
func TestGenerateBranchSummaryOptionsCarryThePayloadReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	lines := []string{
		`{"type":"session","version":3,"id":"s1","timestamp":"2026-01-01T00:00:00.000Z","cwd":"/tmp"}`,
		`{"type":"message","id":"m1","parentId":null,"timestamp":"2026-01-01T00:00:01.000Z","message":{"role":"user","content":"first question","timestamp":1}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := OpenSession(path, dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	if manager.payloadsForRead() == nil {
		t.Fatal("a loaded session has no payload reader to hand to the summary")
	}
	prepared := prepareBranchEntriesWith(manager.GetBranch(""), 100000, manager.payloadsForRead())
	if len(prepared.Messages) != 1 {
		t.Fatalf("prepared messages = %d, want 1", len(prepared.Messages))
	}
}
