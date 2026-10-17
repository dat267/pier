package rpcclient

// Unit tests for the command mapping. They mirror upstream's mocked-send tests
// (rpc-client-clear-queue.test.ts, rpc-client-clone.test.ts) and pin the wire
// command each method builds, plus the response decoding.

import (
	"encoding/json"
	"reflect"
	"testing"
)

func stubClient(t *testing.T, responses map[string]any) (*RpcClient, *[]map[string]any) {
	t.Helper()
	sent := &[]map[string]any{}
	client := New(Options{})
	client.sendFunc = func(command map[string]any) (*response, error) {
		*sent = append(*sent, command)
		name, _ := command["type"].(string)
		value, ok := responses[name]
		if !ok {
			return &response{Type: "response", Command: name, Success: false, Error: "Unknown command: " + name}, nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return &response{Type: "response", Command: name, Success: true, Data: data}, nil
	}
	return client, sent
}

func TestClearQueue(t *testing.T) {
	client, sent := stubClient(t, map[string]any{
		"clear_queue": map[string]any{"steering": []string{"Change direction"}, "followUp": []string{"Summarize when finished"}},
	})
	result, err := client.ClearQueue()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*sent, []map[string]any{{"type": "clear_queue"}}) {
		t.Fatalf("sent = %+v", *sent)
	}
	want := &ClearQueueResult{Steering: []string{"Change direction"}, FollowUp: []string{"Summarize when finished"}}
	if !reflect.DeepEqual(result, want) {
		t.Fatalf("result = %+v want %+v", result, want)
	}
}

func TestClone(t *testing.T) {
	client, sent := stubClient(t, map[string]any{"clone": map[string]any{"cancelled": false}})
	result, err := client.Clone()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*sent, []map[string]any{{"type": "clone"}}) {
		t.Fatalf("sent = %+v", *sent)
	}
	if result == nil || result.Cancelled {
		t.Fatalf("result = %+v", result)
	}
}

func TestExportHTMLOmitsEmptyPath(t *testing.T) {
	client, sent := stubClient(t, map[string]any{"export_html": map[string]any{"path": "/tmp/x.html"}})
	result, err := client.ExportHTML("")
	if err != nil || result.Path != "/tmp/x.html" {
		t.Fatalf("result = %+v err = %v", result, err)
	}
	if !reflect.DeepEqual(*sent, []map[string]any{{"type": "export_html"}}) {
		t.Fatalf("sent = %+v", *sent)
	}
	if _, err := client.ExportHTML("/tmp/out.html"); err != nil {
		t.Fatal(err)
	}
	if got := (*sent)[1]["outputPath"]; got != "/tmp/out.html" {
		t.Fatalf("outputPath = %v", got)
	}
}

func TestPromptBuildsCommand(t *testing.T) {
	client, sent := stubClient(t, map[string]any{
		"prompt": map[string]any{"disposition": "started"},
	})
	disposition, err := client.Prompt("hi", nil, "steer")
	if err != nil || disposition != "started" {
		t.Fatalf("disposition = %q err = %v", disposition, err)
	}
	command := (*sent)[0]
	if command["type"] != "prompt" || command["message"] != "hi" || command["streamingBehavior"] != "steer" {
		t.Fatalf("command = %+v", command)
	}
	if _, hasImages := command["images"]; hasImages {
		t.Fatalf("nil images must be omitted: %+v", command)
	}
}

func TestStateDecodesWireShape(t *testing.T) {
	client, _ := stubClient(t, map[string]any{
		"get_state": map[string]any{
			"thinkingLevel": "high", "isStreaming": true, "isCompacting": false,
			"steeringMode": "all", "followUpMode": "one-at-a-time", "sessionId": "s1",
			"autoCompactionEnabled": true, "messageCount": 3, "pendingMessageCount": 1,
		},
	})
	state, err := client.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "s1" || state.ThinkingLevel != "high" || !state.IsStreaming ||
		state.SteeringMode != "all" || state.FollowUpMode != "one-at-a-time" ||
		!state.AutoCompactionEnabled || state.MessageCount != 3 || state.PendingMessageCount != 1 {
		t.Fatalf("state = %+v", state)
	}
}

func TestCycleThinkingLevelNull(t *testing.T) {
	client, _ := stubClient(t, map[string]any{"cycle_thinking_level": nil})
	level, err := client.CycleThinkingLevel()
	if err != nil || level != nil {
		t.Fatalf("level = %v err = %v", level, err)
	}
	client, _ = stubClient(t, map[string]any{"cycle_thinking_level": map[string]any{"level": "high"}})
	level, err = client.CycleThinkingLevel()
	if err != nil || level == nil || *level != "high" {
		t.Fatalf("level = %v err = %v", level, err)
	}
}

func TestCommandErrorPropagates(t *testing.T) {
	client, _ := stubClient(t, map[string]any{
		"get_entries": map[string]any{},
	})
	client.sendFunc = func(command map[string]any) (*response, error) {
		return &response{Type: "response", Command: "get_entries", Success: false, Error: "Entry not found: nope"}, nil
	}
	if _, err := client.Entries("nope"); err == nil || err.Error() != "Entry not found: nope" {
		t.Fatalf("err = %v", err)
	}
}

func TestLastAssistantTextMissing(t *testing.T) {
	client, _ := stubClient(t, map[string]any{"get_last_assistant_text": map[string]any{}})
	text, err := client.LastAssistantText()
	if err != nil || text != nil {
		t.Fatalf("text = %v err = %v", text, err)
	}
	client, _ = stubClient(t, map[string]any{"get_last_assistant_text": map[string]any{"text": "hello"}})
	text, err = client.LastAssistantText()
	if err != nil || text == nil || *text != "hello" {
		t.Fatalf("text = %v err = %v", text, err)
	}
}

func TestSendWithoutStart(t *testing.T) {
	client := New(Options{})
	if _, err := client.State(); err == nil || err.Error() != "Client not started" {
		t.Fatalf("err = %v", err)
	}
}

func TestSetModelDecodesModel(t *testing.T) {
	client, sent := stubClient(t, map[string]any{
		"set_model": map[string]any{"provider": "openai", "id": "gpt-4o", "contextWindow": 128000},
	})
	model, err := client.SetModel("openai", "gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	if model.Provider != "openai" || model.ID != "gpt-4o" {
		t.Fatalf("model = %+v", model)
	}
	if got := (*sent)[0]; got["provider"] != "openai" || got["modelId"] != "gpt-4o" {
		t.Fatalf("command = %+v", got)
	}
}
