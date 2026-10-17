package coding

// Second RPC tranche: session introspection, compaction/retry controls and
// bash execution (pi v1.3.0 modes/rpc/rpc-mode.ts).

import (
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dat267/pier/ai"
)

// rpcResponseData returns the sole response's data object, ignoring any session
// events the command streamed before it.
func rpcResponseData(t *testing.T, records []map[string]any) map[string]any {
	t.Helper()
	responses := rpcResponses(records)
	if len(responses) != 1 {
		t.Fatalf("want one response, got %+v", responses)
	}
	if responses[0]["success"] != true {
		t.Fatalf("command failed: %+v", responses[0])
	}
	data, ok := responses[0]["data"].(map[string]any)
	if !ok {
		t.Fatalf("data = %#v", responses[0]["data"])
	}
	return data
}

// pi v1.3.0 rpc-mode.ts returns the last assistant text, omitting the field
// when there is none (JSON.stringify drops the undefined value).
func TestRPCGetLastAssistantText(t *testing.T) {
	session := rpcTestSession(t)
	records := runRPCScript(t, session, `{"id":"t","type":"get_last_assistant_text"}`+"\n")
	if data := rpcResponseData(t, records); len(data) != 0 {
		t.Fatalf("no assistant message should omit text: %+v", data)
	}

	session.Agent.SetMessages([]ai.Message{&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content:    ai.ContentList{ai.TextContent{Text: "  answer  "}},
		StopReason: ai.StopStop,
	}})
	records = runRPCScript(t, session, `{"id":"t","type":"get_last_assistant_text"}`+"\n")
	if got := rpcResponseData(t, records)["text"]; got != "answer" {
		t.Fatalf("text = %#v, want trimmed answer", got)
	}
}

// pi v1.3.0 rpc-mode.ts set_session_name trims and rejects an empty name.
func TestRPCSessionName(t *testing.T) {
	session := rpcTestSession(t)
	records := runRPCScript(t, session, `{"id":"a","type":"set_session_name","name":"   "}`+"\n"+
		`{"id":"b","type":"set_session_name","name":"  My Session \n"}`+"\n")
	responses := rpcResponses(records)
	if len(responses) != 2 {
		t.Fatalf("responses = %+v", responses)
	}
	if responses[0]["success"] != false || responses[0]["error"] != "Session name cannot be empty" {
		t.Fatalf("empty name response = %+v", responses[0])
	}
	if responses[1]["success"] != true {
		t.Fatalf("valid name response = %+v", responses[1])
	}
	if got := session.SessionName(); got != "My Session" {
		t.Fatalf("session name = %q", got)
	}
}

// pi v1.3.0 get_fork_messages lists the user messages with their entry ids.
func TestRPCForkMessages(t *testing.T) {
	session := rpcTestSession(t)
	firstID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	session.Sessions.AppendMessage(&ai.AssistantMessage{
		API: ai.APIAnthropicMessages, Provider: "test", Model: "m",
		Content: ai.ContentList{ai.TextContent{Text: "answer"}}, StopReason: ai.StopStop,
	})
	secondID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})

	records := runRPCScript(t, session, `{"id":"f","type":"get_fork_messages"}`+"\n")
	messages := rpcResponseData(t, records)["messages"].([]any)
	want := []any{
		map[string]any{"entryId": firstID, "text": "first"},
		map[string]any{"entryId": secondID, "text": "second"},
	}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("messages = %+v, want %+v", messages, want)
	}
}

// pi v1.3.0 get_entries returns every entry plus the leaf id, and supports a
// `since` cursor that excludes the named entry.
func TestRPCGetEntries(t *testing.T) {
	session := rpcTestSession(t)
	firstID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	secondID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})

	records := runRPCScript(t, session, `{"id":"all","type":"get_entries"}`+"\n")
	data := rpcResponseData(t, records)
	if len(data["entries"].([]any)) != 2 {
		t.Fatalf("entries = %+v", data["entries"])
	}
	if data["leafId"] != secondID {
		t.Fatalf("leafId = %#v, want %q", data["leafId"], secondID)
	}

	records = runRPCScript(t, session, `{"id":"after","type":"get_entries","since":"`+firstID+`"}`+"\n")
	entries := rpcResponseData(t, records)["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["id"] != secondID {
		t.Fatalf("since entries = %+v", entries)
	}

	records = runRPCScript(t, session, `{"id":"missing","type":"get_entries","since":"nope"}`+"\n")
	response := rpcResponses(records)[0]
	if response["success"] != false || response["error"] != "Entry not found: nope" {
		t.Fatalf("missing since response = %+v", response)
	}
}

// pi v1.3.0 get_tree returns the entry tree and the leaf id; labels are
// attached only when present.
func TestRPCGetTree(t *testing.T) {
	session := rpcTestSession(t)
	rootID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "first"}})
	leafID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "second"}})
	if _, err := session.Sessions.AppendLabelChange(leafID, strPtr("checkpoint")); err != nil {
		t.Fatal(err)
	}

	records := runRPCScript(t, session, `{"id":"t","type":"get_tree"}`+"\n")
	data := rpcResponseData(t, records)
	if current := session.Sessions.GetLeafID(); current == nil || data["leafId"] != *current {
		t.Fatalf("leafId = %#v, want %v", data["leafId"], current)
	}
	roots := data["tree"].([]any)
	if len(roots) != 1 {
		t.Fatalf("roots = %+v", roots)
	}
	root := roots[0].(map[string]any)
	if root["entry"].(map[string]any)["id"] != rootID {
		t.Fatalf("root entry = %+v", root["entry"])
	}
	if _, present := root["label"]; present {
		t.Fatalf("unlabeled root should omit label: %+v", root)
	}
	children := root["children"].([]any)
	if len(children) != 1 {
		t.Fatalf("children = %+v", children)
	}
	labeled := children[0].(map[string]any)
	if labeled["entry"].(map[string]any)["id"] != leafID || labeled["label"] != "checkpoint" {
		t.Fatalf("labeled node = %+v", labeled)
	}
	if _, present := labeled["labelTimestamp"]; !present {
		t.Fatalf("labeled entry should include labelTimestamp: %+v", labeled)
	}
}

// pi v1.3.0 get_session_stats uses the camelCase SessionStats shape.
func TestRPCGetSessionStats(t *testing.T) {
	session := rpcTestSession(t)
	session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "hello"}})
	session.Agent.SetModel(&ai.Model{ID: "m", Provider: "test", API: ai.APIAnthropicMessages, ContextWindow: 1000})

	records := runRPCScript(t, session, `{"id":"s","type":"get_session_stats"}`+"\n")
	data := rpcResponseData(t, records)
	for _, key := range []string{"sessionId", "userMessages", "assistantMessages", "toolCalls", "toolResults", "totalMessages", "tokens", "cost", "contextUsage"} {
		if _, present := data[key]; !present {
			t.Fatalf("missing %q in %+v", key, data)
		}
	}
	if data["userMessages"].(float64) != 1 || data["totalMessages"].(float64) != 1 {
		t.Fatalf("counts = %+v", data)
	}
	tokens := data["tokens"].(map[string]any)
	for _, key := range []string{"input", "output", "cacheRead", "cacheWrite", "total"} {
		if _, present := tokens[key]; !present {
			t.Fatalf("missing token %q in %+v", key, tokens)
		}
	}
	usage := data["contextUsage"].(map[string]any)
	for _, key := range []string{"tokens", "contextWindow", "percent"} {
		if _, present := usage[key]; !present {
			t.Fatalf("missing contextUsage %q in %+v", key, usage)
		}
	}
}

// pi v1.3.0 get_commands lists prompt templates, then skills, each with
// source metadata. Extensions are absent in this port.
func TestRPCGetCommands(t *testing.T) {
	persist := false
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), StreamFn: stubStreamFn,
		Sessions: NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist}),
		Control: &AgentSessionControl{PromptTemplates: []PromptTemplate{{
			Name: "review", Description: "Review a change",
			SourceInfo: SourceInfo{Path: "/tmp/review.md", Source: "prompt", Scope: SourceScopeProject, Origin: SourceOriginTopLevel},
		}}},
		Skills: []Skill{{
			Name: "summarize", Description: "Summarize text",
			SourceInfo: SourceInfo{Path: "/tmp/skill.md", Source: "skill", Scope: SourceScopeUser, Origin: SourceOriginTopLevel},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	records := runRPCScript(t, session, `{"id":"c","type":"get_commands"}`+"\n")
	commands := rpcResponseData(t, records)["commands"].([]any)
	if len(commands) != 2 {
		t.Fatalf("commands = %+v", commands)
	}
	first := commands[0].(map[string]any)
	if first["name"] != "review" || first["description"] != "Review a change" || first["source"] != "prompt" {
		t.Fatalf("prompt command = %+v", first)
	}
	info := first["sourceInfo"].(map[string]any)
	if info["path"] != "/tmp/review.md" || info["source"] != "prompt" || info["scope"] != "project" || info["origin"] != "top-level" {
		t.Fatalf("prompt sourceInfo = %+v", info)
	}
	if _, present := info["baseDir"]; present {
		t.Fatalf("empty baseDir should be omitted: %+v", info)
	}
	second := commands[1].(map[string]any)
	if second["name"] != "skill:summarize" || second["source"] != "skill" {
		t.Fatalf("skill command = %+v", second)
	}
}

// pi v1.3.0 set_auto_compaction / set_auto_retry / abort_retry toggle session
// state without requiring a model.
func TestRPCCompactionAndRetryControls(t *testing.T) {
	session := rpcTestSession(t)
	records := runRPCScript(t, session,
		`{"id":"ac","type":"set_auto_compaction","enabled":true}`+"\n"+
			`{"id":"ar","type":"set_auto_retry","enabled":true}`+"\n"+
			`{"id":"ab","type":"abort_retry"}`+"\n"+
			`{"id":"state","type":"get_state"}`+"\n")
	responses := rpcResponses(records)
	if len(responses) != 4 || responses[0]["success"] != true || responses[1]["success"] != true || responses[2]["success"] != true {
		t.Fatalf("control responses = %+v", responses)
	}
	if !session.AutoCompactionEnabled() || !session.RetryEnabled() {
		t.Fatalf("autoCompaction=%v autoRetry=%v", session.AutoCompactionEnabled(), session.RetryEnabled())
	}
	state := responses[3]["data"].(map[string]any)
	if state["autoCompactionEnabled"] != true {
		t.Fatalf("get_state = %+v", state)
	}
}

// pi v1.3.0 compact returns the CompactionResult fields.
func TestRPCCompact(t *testing.T) {
	var runs atomic.Int64
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	session := compactionTestSession(t, model, summarizingStreamFn(t, &runs, "summary text that is long enough to pass the usability check"), smallCompactionSettings())
	seedBranch(t, session)

	client := startRPCTest(t, session)
	client.send(t, `{"id":"c","type":"compact"}`)
	response := client.until(t, func(record map[string]any) bool {
		return record["type"] == "response" && record["id"] == "c"
	})
	if response["success"] != true {
		t.Fatalf("compact response = %+v", response)
	}
	data := response["data"].(map[string]any)
	for _, key := range []string{"summary", "firstKeptEntryId", "tokensBefore"} {
		if _, present := data[key]; !present {
			t.Fatalf("missing %q in %+v", key, data)
		}
	}
	if data["summary"] != "summary text that is long enough to pass the usability check" {
		t.Fatalf("summary = %#v", data["summary"])
	}
}

// pi v1.3.0 compact forwards customInstructions into the summarization prompt.
func TestRPCCompactCustomInstructions(t *testing.T) {
	var mu sync.Mutex
	var captured string
	model := &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 1000, MaxTokens: 100}
	streamFn := func(model *ai.Model, context ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		encoded, _ := ai.MarshalJSON(context.Messages)
		mu.Lock()
		captured = string(encoded)
		mu.Unlock()
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventDone, Reason: ai.StopStop, Message: &ai.AssistantMessage{
				API: ai.APIAnthropicMessages, Provider: "anthropic", Model: "m",
				Content: ai.ContentList{ai.TextContent{Text: "summary text that is long enough to pass the usability check"}}, StopReason: ai.StopStop,
			}})
		}()
		return stream
	}
	session := compactionTestSession(t, model, streamFn, smallCompactionSettings())
	seedBranch(t, session)

	client := startRPCTest(t, session)
	client.send(t, `{"id":"c","type":"compact","customInstructions":"focus the test note"}`)
	response := client.until(t, func(record map[string]any) bool {
		return record["type"] == "response" && record["id"] == "c"
	})
	if response["success"] != true {
		t.Fatalf("compact response = %+v", response)
	}
	mu.Lock()
	prompt := captured
	mu.Unlock()
	if !strings.Contains(prompt, "Additional focus: focus the test note") {
		t.Fatalf("custom instructions missing from summarization prompt: %s", prompt)
	}
}

// pi v1.3.0 bash executes in the session cwd and returns a BashResult.
func TestRPCBash(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	client := startRPCTest(t, rpcTestSession(t))
	client.send(t, `{"id":"b","type":"bash","command":"printf rpc-ok"}`)
	response := client.until(t, func(record map[string]any) bool {
		return record["type"] == "response" && record["id"] == "b"
	})
	if response["success"] != true {
		t.Fatalf("bash response = %+v", response)
	}
	data := response["data"].(map[string]any)
	if data["output"] != "rpc-ok" {
		t.Fatalf("output = %#v", data["output"])
	}
	if data["exitCode"].(float64) != 0 || data["cancelled"] != false || data["truncated"] != false {
		t.Fatalf("bash result = %+v", data)
	}
	if _, present := data["fullOutputPath"]; present {
		t.Fatalf("untruncated output should omit fullOutputPath: %+v", data)
	}
}

// pi v1.3.0 abort_bash succeeds even when no command is running.
func TestRPCAbortBash(t *testing.T) {
	records := runRPCScript(t, rpcTestSession(t), `{"id":"a","type":"abort_bash"}`+"\n")
	if response := rpcResponses(records)[0]; response["success"] != true {
		t.Fatalf("abort_bash response = %+v", response)
	}
}

// pi v1.3.0 handles commands without blocking the input loop, so abort_bash can
// cancel a running bash command through the tracked run context.
func TestRPCAbortBashCancelsRunningCommand(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no shell available")
	}
	client := startRPCTest(t, rpcTestSession(t))
	client.send(t, `{"id":"run","type":"bash","command":"printf ready; sleep 30"}`)
	// The first output chunk proves the run is tracked and cancellable.
	client.until(t, func(record map[string]any) bool {
		return record["type"] == "bash_execution_update" && record["id"] == "run"
	})
	client.send(t, `{"id":"stop","type":"abort_bash"}`)
	stop := client.until(t, func(record map[string]any) bool {
		return record["type"] == "response" && record["id"] == "stop"
	})
	if stop["success"] != true {
		t.Fatalf("abort_bash response = %+v", stop)
	}
	response := client.until(t, func(record map[string]any) bool {
		return record["type"] == "response" && record["id"] == "run"
	})
	data, ok := response["data"].(map[string]any)
	if !ok || response["success"] != true || data["cancelled"] != true {
		t.Fatalf("cancelled bash response = %+v", response)
	}
	if _, present := data["exitCode"]; present {
		t.Fatalf("cancelled bash should omit exitCode: %+v", data)
	}
}

// pi v1.3.0 requires the new command fields to have the right types.
func TestRPCNewCommandFieldValidation(t *testing.T) {
	records := runRPCScript(t, rpcTestSession(t),
		`{"id":"a","type":"bash","command":5}`+"\n"+
			`{"id":"b","type":"set_auto_compaction","enabled":"yes"}`+"\n"+
			`{"id":"c","type":"get_entries","since":3}`+"\n"+
			`{"id":"d","type":"set_session_name"}`+"\n")
	wantIDs := []string{"a", "b", "c", "d"}
	for i, record := range rpcResponses(records) {
		if record["success"] != false {
			t.Fatalf("record %d should fail: %+v", i, record)
		}
		if record["id"] != wantIDs[i] {
			t.Fatalf("record %d lost its id: %+v", i, record)
		}
	}
}
