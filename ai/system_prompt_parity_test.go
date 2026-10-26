package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), ai/src/utils/text.ts (getSystemMessageText,
// renderSystemMessageUpdate) and transcript.ts (getCurrentSystemMessage,
// getCurrentSystemPrompt, getCurrentTools). Regenerate:
//
//	node --experimental-strip-types ai/testdata/system-prompt/generate.mjs ./pi > ai/testdata/system-prompt/golden.json
func TestSystemPromptHelpersMatchUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/system-prompt/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Render []struct {
			Name     string          `json:"name"`
			Message  json.RawMessage `json:"message"`
			Expected string          `json:"expected"`
		} `json:"render"`
		Update []struct {
			Name     string          `json:"name"`
			Message  json.RawMessage `json:"message"`
			Expected string          `json:"expected"`
		} `json:"update"`
		Current []struct {
			Name            string            `json:"name"`
			Messages        []json.RawMessage `json:"messages"`
			ExpectedMessage json.RawMessage   `json:"expectedMessage"`
			ExpectedPrompt  string            `json:"expectedPrompt"`
			ExpectedTools   json.RawMessage   `json:"expectedTools"`
		} `json:"current"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Render) == 0 || len(corpus.Update) == 0 || len(corpus.Current) == 0 {
		t.Fatal("incomplete system-prompt corpus")
	}
	decodedSystem := func(t *testing.T, encoded json.RawMessage) *SystemMessage {
		t.Helper()
		message, err := UnmarshalMessage(encoded)
		if err != nil {
			t.Fatalf("decode system message: %v", err)
		}
		system, ok := message.(*SystemMessage)
		if !ok {
			t.Fatalf("decoded %T, want *SystemMessage", message)
		}
		return system
	}
	for _, fixture := range corpus.Render {
		if got := GetSystemMessageText(decodedSystem(t, fixture.Message)); got != fixture.Expected {
			t.Errorf("%s: text = %q, want %q", fixture.Name, got, fixture.Expected)
		}
	}
	for _, fixture := range corpus.Update {
		if got := RenderSystemMessageUpdate(decodedSystem(t, fixture.Message)); got != fixture.Expected {
			t.Errorf("%s: update = %q, want %q", fixture.Name, got, fixture.Expected)
		}
	}
	for _, fixture := range corpus.Current {
		messages := make([]Message, 0, len(fixture.Messages))
		for _, encoded := range fixture.Messages {
			message, err := UnmarshalMessage(encoded)
			if err != nil {
				t.Fatalf("%s: decode message: %v", fixture.Name, err)
			}
			messages = append(messages, message)
		}
		current := GetCurrentSystemMessage(messages)
		if (current == nil) != (len(fixture.ExpectedMessage) == 0 || string(fixture.ExpectedMessage) == "null") {
			t.Fatalf("%s: current message presence = %v, want %s", fixture.Name, current != nil, fixture.ExpectedMessage)
		}
		if current != nil {
			encoded, err := MarshalMessage(current)
			if err != nil {
				t.Fatal(err)
			}
			want, err := MarshalJSON(json.RawMessage(fixture.ExpectedMessage))
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != string(want) {
				t.Errorf("%s: current message = %s, want %s", fixture.Name, encoded, want)
			}
		}
		if got := GetCurrentSystemPrompt(messages); got != fixture.ExpectedPrompt {
			t.Errorf("%s: prompt = %q, want %q", fixture.Name, got, fixture.ExpectedPrompt)
		}
		tools := GetCurrentTools(messages)
		encodedTools, err := MarshalJSON(tools)
		if err != nil {
			t.Fatal(err)
		}
		wantTools, err := MarshalJSON(json.RawMessage(fixture.ExpectedTools))
		if err != nil {
			t.Fatal(err)
		}
		if string(encodedTools) != string(wantTools) {
			t.Errorf("%s: tools = %s, want %s", fixture.Name, encodedTools, wantTools)
		}
	}
}
