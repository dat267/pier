package coding

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/compaction/utils.ts serializeConversation.
// Regenerate:
//
//	node --experimental-strip-types coding/testdata/serialize-conversation/generate.mjs ./pi > coding/testdata/serialize-conversation/golden.json
func TestSerializeConversationMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/serialize-conversation/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string            `json:"name"`
		Messages []json.RawMessage `json:"messages"`
		Expected string            `json:"expected"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty serialize-conversation corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			messages := make([]ai.Message, 0, len(fixture.Messages))
			for _, encoded := range fixture.Messages {
				message, err := ai.UnmarshalMessage(encoded)
				if err != nil {
					t.Fatalf("decode message: %v", err)
				}
				messages = append(messages, message)
			}
			if got := SerializeConversation(messages); got != fixture.Expected {
				t.Fatalf("serialized conversation differs\n got: %q\nwant: %q", got, fixture.Expected)
			}
		})
	}
}

// pi v1.4.0 core/agent-session.ts getLastAssistantText uses JS trim; rpc-mode.ts
// exposes that value and omits text when the result is undefined. Regenerate:
// node coding/testdata/last-assistant-text/generate.mjs ./pi > coding/testdata/last-assistant-text/golden.json
func TestLastAssistantTextMatchesUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/last-assistant-text/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name         string            `json:"name"`
		Messages     []json.RawMessage `json:"messages"`
		ExpectedText string            `json:"expectedText"`
		RPCData      map[string]any    `json:"rpcData"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("empty last-assistant-text corpus")
	}
	for _, test := range cases {
		t.Run(test.Name, func(t *testing.T) {
			messages, err := ai.UnmarshalMessages(test.Messages)
			if err != nil {
				t.Fatal(err)
			}
			session := rpcTestSession(t)
			session.Agent.SetMessages(messages)
			if text := session.GetLastAssistantText(); text != test.ExpectedText {
				t.Errorf("last text = %q, want %q", text, test.ExpectedText)
			}
			data := rpcResponseData(t, runRPCScript(t, session, "{\"type\":\"get_last_assistant_text\"}\n"))
			if !reflect.DeepEqual(data, test.RPCData) {
				t.Errorf("RPC data = %#v, want %#v", data, test.RPCData)
			}
		})
	}
}
