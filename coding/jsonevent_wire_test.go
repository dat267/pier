package coding

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
)

// v1.3.0 modes/json-event.ts removes both the outer cumulative message and
// the inner partial. Wire size must not depend on the accumulated response.
func jsonEventGoldens(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile("testdata/json_events_golden.txt")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		label, wire, ok := strings.Cut(line, " => ")
		if !ok {
			t.Fatalf("invalid golden line: %s", line)
		}
		out[label] = wire
	}
	return out
}

func TestJSONAssistantEventsMatchUpstream(t *testing.T) {
	goldens := jsonEventGoldens(t)
	message := createAssistantMessageT("")
	message.Timestamp = 1
	message.Content = ai.ContentList{}
	call := ai.ToolCall{ID: "c", Name: "read", Arguments: json.RawMessage(`{"z":1,"a":2}`)}
	partial := *message
	partial.Content = ai.ContentList{call}
	for _, typ := range []string{"start", "text_start", "text_delta", "text_end", "thinking_start", "thinking_delta", "thinking_end", "toolcall_start", "toolcall_delta", "toolcall_end", "done", "error"} {
		t.Run(typ, func(t *testing.T) {
			inner := &ai.AssistantMessageEvent{Type: typ}
			switch typ {
			case "done":
				inner.Reason = ai.StopStop
				inner.Message = message
			case "error":
				inner.Reason = ai.StopError
				inner.Error = message
			default:
				inner.Partial = &partial
			}
			if typ == "toolcall_end" {
				inner.ToolCall = &call
			}
			event := &SessionEvent{Type: SessionMessageUpdate, Agent: &agent.AgentEvent{Message: message, AssistantMessageEvent: inner}}
			wire, err := MarshalJSONSessionEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			if string(wire) != goldens[typ] {
				t.Fatalf("wire = %s\nwant = %s", wire, goldens[typ])
			}
		})
	}
}

func TestJSONSessionEventsMatchUpstream(t *testing.T) {
	goldens := jsonEventGoldens(t)
	message := createAssistantMessageT("")
	message.Timestamp = 1
	message.Content = ai.ContentList{}
	args := json.RawMessage(`{"z":1,"a":2}`)
	duration := int64(0)
	usage := &ai.Usage{}
	result := agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: "ok"}, ai.ImageContent{Data: "AA==", MimeType: "image/png"}}, Details: args, Usage: usage, IsError: true, Terminate: true}
	tool := func() *agent.AgentEvent {
		return &agent.AgentEvent{ToolCallID: "c", ToolName: "read", Args: args, PartialResult: result, Result: result, DurationMs: &duration}
	}
	entry := &SessionEntry{Type: "custom", SessionEntryBase: SessionEntryBase{ID: "e", Timestamp: "2026-01-01T00:00:00Z"}, CustomType: "test", Data: args}
	compaction := &CompactionResult{Summary: "summary", FirstKeptEntryID: "e", TokensBefore: 10, EstimatedTokensAfter: 2, Usage: usage, Details: args}
	cases := []struct {
		label string
		event *SessionEvent
	}{
		{"agent_start", &SessionEvent{Type: SessionAgentStart}},
		{"agent_end", &SessionEvent{Type: SessionAgentEnd, Agent: &agent.AgentEvent{}}},
		{"agent_settled", &SessionEvent{Type: SessionAgentSettled}},
		{"turn_start", &SessionEvent{Type: SessionTurnStart}},
		{"turn_end", &SessionEvent{Type: SessionTurnEnd, Agent: &agent.AgentEvent{Message: message}}},
		{"message_start", &SessionEvent{Type: SessionMessageStart, Agent: &agent.AgentEvent{Message: message}}},
		{"message_end", &SessionEvent{Type: SessionMessageEnd, Agent: &agent.AgentEvent{Message: message}}},
		{"tool_execution_start", &SessionEvent{Type: SessionToolExecutionStart, Agent: tool()}},
		{"tool_execution_update", &SessionEvent{Type: SessionToolExecutionUpdate, Agent: tool()}},
		{"tool_execution_end", &SessionEvent{Type: SessionToolExecutionEnd, Agent: tool()}},
		{"queue_update", &SessionEvent{Type: SessionQueueUpdate}},
		{"compaction_start", &SessionEvent{Type: SessionCompactionStart, Reason: CompactionManual}},
		{"compaction_end", &SessionEvent{Type: SessionCompactionEnd, Reason: CompactionManual, Result: compaction, WillRetry: true}},
		{"compaction_aborted", &SessionEvent{Type: SessionCompactionEnd, Reason: CompactionManual, Aborted: true, ErrorMessage: "cancelled"}},
		{"entry_appended", &SessionEvent{Type: SessionEntryAppended, Entry: entry}},
		{"session_info_changed", &SessionEvent{Type: SessionInfoChanged}},
		{"thinking_level_changed", &SessionEvent{Type: SessionThinkingLevelChanged, Level: "high"}},
		{"auto_retry_start", &SessionEvent{Type: SessionAutoRetryStart, Attempt: 1, MaxAttempts: 3}},
		{"auto_retry_end", &SessionEvent{Type: SessionAutoRetryEnd, Attempt: 1, ErrorMessage: "failed"}},
		{"summarization_retry_scheduled", &SessionEvent{Type: SessionSummarizationRetryScheduled, Attempt: 1, MaxAttempts: 3}},
		{"summarization_retry_attempt_start", &SessionEvent{Type: SessionSummarizationRetryAttemptStart, Source: "compaction", Reason: CompactionManual}},
		{"summarization_retry_finished", &SessionEvent{Type: SessionSummarizationRetryFinished}},
		{"bash_execution_update", &SessionEvent{Type: SessionBashExecutionUpdate, ID: "b"}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			wire, err := MarshalJSONSessionEvent(c.event)
			if err != nil {
				t.Fatal(err)
			}
			if string(wire) != goldens[c.label] {
				t.Fatalf("wire = %s\nwant = %s", wire, goldens[c.label])
			}
			// The map API and actual stdout path must describe the same event.
			plain, err := ToJSONEvent(c.event)
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := ai.MarshalJSON(plain)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal(wire, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(want, got) {
				t.Fatal("map and wire APIs disagree")
			}
		})
	}
}

func TestJSONSessionNameChangeCarriesNormalizedName(t *testing.T) {
	// v1.3.0 agent-session.ts setSessionName emits getSessionName(), not raw input.
	session, _ := newPrintSession(t)
	var wires []string
	session.Subscribe(func(event *SessionEvent) {
		if event.Type == SessionInfoChanged {
			wire, err := MarshalJSONSessionEvent(event)
			if err != nil {
				t.Fatal(err)
			}
			wires = append(wires, string(wire))
		}
	})
	session.SetSessionName("  renamed\n session  ")
	session.SetSessionName("")
	want := []string{`{"type":"session_info_changed","name":"renamed  session"}`, `{"type":"session_info_changed"}`}
	if !reflect.DeepEqual(wires, want) {
		t.Fatalf("events = %q, want %q", wires, want)
	}
}

func TestJSONEventMapPreservesOptionalUsage(t *testing.T) {
	// v1.3.0 json-event.ts forwards the full Usage value, not a fixed subset.
	reasoning, cacheWrite1h := int64(7), int64(3)
	message := createAssistantMessageT("")
	message.Usage.Reasoning = &reasoning
	message.Usage.CacheWrite1h = &cacheWrite1h
	event := &SessionEvent{Type: SessionMessageUpdate, Agent: &agent.AgentEvent{Message: message, AssistantMessageEvent: &ai.AssistantMessageEvent{Type: ai.EventStart, Partial: message}}}
	value, err := ToJSONEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	usage := value["usage"].(map[string]any)
	if usage["reasoning"] != reasoning || usage["cacheWrite1h"] != cacheWrite1h {
		t.Fatalf("optional usage missing: %#v", usage)
	}
}

func TestPrintModeJSONPreservesTerminalPayloads(t *testing.T) {
	// v1.3.0 print-mode.ts serializes toJsonEvent directly. Check actual stdout,
	// not only the exported mapper, so the two paths cannot drift again.
	session, sessions := newPrintSession(t, createAssistantMessageT("reply"))
	var code int
	output := captureStdout(t, func() {
		code = RunPrintMode(session, sessions, PrintModeOptions{Mode: CLIModeJSON, InitialMessage: "hi"})
	})
	if code != 0 {
		t.Fatalf("exit = %d", code)
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var event map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		var typ string
		if err := json.Unmarshal(event["type"], &typ); err != nil {
			t.Fatal(err)
		}
		switch typ {
		case "message_update":
			if _, ok := event["message"]; ok {
				t.Fatalf("cumulative message leaked: %s", line)
			}
		case "turn_end":
			if string(event["toolResults"]) != "[]" {
				t.Fatalf("missing empty toolResults: %s", line)
			}
			seen[typ] = true
		case "agent_end":
			if string(event["willRetry"]) != "false" {
				t.Fatalf("missing willRetry=false: %s", line)
			}
			seen[typ] = true
		case "agent_settled":
			if string(event["aborted"]) != "false" {
				t.Fatalf("missing aborted=false: %s", line)
			}
			seen[typ] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("terminal events missing: %v", seen)
	}
}

func TestJSONWirePreservesUpstreamFieldOrder(t *testing.T) {
	// Literal from v1.3.0 modes/json-event.ts, JSON.stringify(toJsonEvent(event)).
	event := &SessionEvent{Type: SessionMessageUpdate, Agent: &agent.AgentEvent{
		Message:               createAssistantMessageT("ignored"),
		AssistantMessageEvent: &ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: 1, Delta: "<>&"},
	}}
	want := `{"type":"message_update","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"assistantMessageEvent":{"type":"text_delta","contentIndex":1,"delta":"<>&"}}`
	wire, err := MarshalJSONSessionEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != want {
		t.Fatalf("wire = %s\nwant = %s", wire, want)
	}
}

func TestJSONMessageUpdateDoesNotRepeatCumulativeContent(t *testing.T) {
	var first string
	for _, size := range []int{16, 4096} {
		message := createAssistantMessageT(strings.Repeat("x", size))
		event := &SessionEvent{Type: SessionMessageUpdate, Agent: &agent.AgentEvent{
			Type: agent.MessageUpdate, Message: message,
			AssistantMessageEvent: &ai.AssistantMessageEvent{Type: ai.EventTextDelta, ContentIndex: 1, Delta: "x", Partial: message},
		}}
		wire, err := MarshalJSONSessionEvent(event)
		if err != nil {
			t.Fatal(err)
		}
		var value map[string]any
		if err := json.Unmarshal(wire, &value); err != nil {
			t.Fatal(err)
		}
		if _, present := value["message"]; present {
			t.Fatalf("cumulative message leaked: %s", wire)
		}
		if first != "" && first != string(wire) {
			t.Fatal("delta wire size depends on accumulated content")
		}
		first = string(wire)
	}
}
