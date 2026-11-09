package coding

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

// D419: the session remembers the request it last sent, so a compaction
// summarizes from that request instead of reserializing the conversation.
func TestSessionCompactionReplaysTheLastTurnPrefix(t *testing.T) {
	dir := t.TempDir()
	manager := NewSessionManager(dir, nil)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 8192}
	answer := func(requestModel *ai.Model, text string) *ai.AssistantMessageEventStream {
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
			Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: 10, TotalTokens: 10},
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	var turnPrefix []ai.Message
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, Model: model,
		StreamFn: func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			turnPrefix = append([]ai.Message{}, transcript.Messages...)
			return answer(requestModel, "done")
		},
		ConvertToLlm: ConvertToLlm, Settings: SessionSettings{Compaction: smallCompactionSettings()},
		Control: &AgentSessionControl{autoCompaction: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Prompt(context.Background(), "start", &PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(turnPrefix) == 0 {
		t.Fatal("no turn request was recorded")
	}

	var summaryRequests []ai.TranscriptContext
	session.CompactionStreamFn = func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		summaryRequests = append(summaryRequests, transcript)
		return answer(requestModel, "## Goal\ncontinue")
	}
	if _, err := session.CompactSession(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(summaryRequests) != 1 {
		t.Fatalf("summary requests = %d", len(summaryRequests))
	}
	sent := summaryRequests[0].Messages
	if len(sent) < len(turnPrefix)+1 {
		t.Fatalf("summary request sent %d messages, want the %d-message turn prefix plus the instruction", len(sent), len(turnPrefix))
	}
	for i, message := range turnPrefix {
		if sent[i] != message {
			t.Fatalf("message %d is not the last turn's", i)
		}
	}
	// The turn's own answer, appended after that request, joins the replay so
	// the summary covers it (D419).
	if tail := prefixMessageText(sent[len(sent)-2]); tail != "done" {
		t.Fatalf("replayed tail = %q, want the turn's answer", tail)
	}
}

// The captured prefix is only the session's own conversation while no other
// context replaced it: navigating to an earlier entry or clearing the session
// leaves a request behind that speaks for a branch the user left, so a
// compaction must fall back to the stock summary instead of replaying it (D419).
func TestSessionCompactionSkipsThePrefixAfterBranchNavigation(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), nil)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 8192}
	answer := func(requestModel *ai.Model, text string) *ai.AssistantMessageEventStream {
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
			Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: 10, TotalTokens: 10},
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager, Model: model, ConvertToLlm: ConvertToLlm,
		StreamFn: func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			return answer(requestModel, "done")
		},
		Settings: SessionSettings{Compaction: smallCompactionSettings()},
		Control:  &AgentSessionControl{autoCompaction: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"first", "abandoned branch"} {
		if err := session.Prompt(context.Background(), prompt, &PromptOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	// Back to the answer that followed the first prompt.
	target := ""
	for _, entry := range session.Sessions.ProjectedEntries() {
		for _, message := range entry.Messages {
			if _, ok := message.(*ai.AssistantMessage); ok && target == "" {
				target = entry.Source.ID
			}
		}
	}
	if target == "" {
		t.Fatal("no assistant entry to navigate to")
	}
	if _, err := session.NavigateTree(context.Background(), target, NavigateTreeOptions{}); err != nil {
		t.Fatal(err)
	}

	var summaryRequests []ai.TranscriptContext
	session.CompactionStreamFn = func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		summaryRequests = append(summaryRequests, transcript)
		return answer(requestModel, "## Goal\ncontinue")
	}
	if _, err := session.CompactSession(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(summaryRequests) != 1 {
		t.Fatalf("summary requests = %d", len(summaryRequests))
	}
	for _, message := range summaryRequests[0].Messages {
		text := prefixMessageText(message)
		if strings.Contains(text, "abandoned branch") {
			t.Fatalf("the summary request replayed the branch the user left: %q", text)
		}
	}
}

// prefixMessageText is the text a replayed message carries, for tests that
// follow what a summary request contained.
func prefixMessageText(message ai.Message) string {
	var content ai.ContentList
	switch typed := message.(type) {
	case *ai.UserMessage:
		return ai.ContentText(typed.Content, "")
	case *ai.AssistantMessage:
		content = typed.Content
	default:
		return ""
	}
	var parts []string
	for _, block := range content {
		if text, ok := block.(ai.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, " ")
}

// The captured request predates whatever the turn appended after it (the
// assistant's final answer, a bash execution, a custom message). A summary that
// replays the bare request would drop that content, which the stock path
// serializes, so the replay extends the captured prefix with the current
// context instead of stopping at it (D419).
func TestSessionCompactionReplaysAppendedContext(t *testing.T) {
	manager := NewSessionManager(t.TempDir(), nil)
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 200000, MaxTokens: 8192}
	var turnPrefix []ai.Message
	answer := func(requestModel *ai.Model, text string) *ai.AssistantMessageEventStream {
		message := &ai.AssistantMessage{
			API: ai.APIAnthropicMessages, Provider: requestModel.Provider, Model: requestModel.ID,
			Content: ai.ContentList{ai.TextContent{Text: text}}, StopReason: ai.StopStop,
			Timestamp: time.Now().UnixMilli(), Usage: ai.Usage{Input: 10, TotalTokens: 10},
		}
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: message.StopReason, Message: message})
		}()
		return stream
	}
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), Sessions: manager, Model: model, ConvertToLlm: ConvertToLlm,
		StreamFn: func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			turnPrefix = append([]ai.Message{}, transcript.Messages...)
			return answer(requestModel, "done")
		},
		Settings: SessionSettings{Compaction: smallCompactionSettings()},
		Control:  &AgentSessionControl{autoCompaction: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Prompt(context.Background(), "please work", &PromptOptions{}); err != nil {
		t.Fatal(err)
	}
	// A bash execution between turns reaches the context without a request.
	session.recordBashResultAt("echo marker", BashResult{Output: "appended-bash-output"}, false, time.Now().UnixMilli())

	var summaryRequests []ai.TranscriptContext
	session.CompactionStreamFn = func(requestModel *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		summaryRequests = append(summaryRequests, transcript)
		return answer(requestModel, "## Goal\ncontinue")
	}
	if _, err := session.CompactSession(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if len(summaryRequests) != 1 {
		t.Fatalf("summary requests = %d", len(summaryRequests))
	}
	sent := summaryRequests[0].Messages
	// The cached prefix is untouched, so the provider still serves it as a hit.
	for i, message := range turnPrefix {
		if i >= len(sent) || sent[i] != message {
			t.Fatalf("message %d is not the turn's", i)
		}
	}
	appended := false
	for _, message := range sent {
		if strings.Contains(prefixMessageText(message), "appended-bash-output") {
			appended = true
		}
	}
	if !appended {
		t.Fatalf("the summary request dropped the context appended after the turn: %d messages", len(sent))
	}
}
