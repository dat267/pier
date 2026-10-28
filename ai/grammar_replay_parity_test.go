package ai

import (
	"encoding/json"
	"os"
	"testing"
)

// pi v1.4.0 (4a080252), api/constrained-sampling.ts permits an empty schema
// property name. The Responses and Completions converters test Map membership,
// not truthiness, so such calls keep their custom-tool wire type during replay.
// Regenerate: node --experimental-strip-types testdata/grammar-replay/generate.mjs ../pi > testdata/grammar-replay/golden.json
func TestGrammarToolReplayMatchesUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/grammar-replay/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string
		Model    Model
		Messages []json.RawMessage
		Payload  string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			var messages []Message
			for _, raw := range fixture.Messages {
				message, err := UnmarshalMessage(raw)
				if err != nil {
					t.Fatal(err)
				}
				messages = append(messages, message)
			}
			transcript := TranscriptContext{Messages: messages}
			var params any
			var err error
			switch fixture.Model.API {
			case APIOpenAIResponses:
				params, err = BuildOpenAIResponsesParams(&fixture.Model, transcript, nil, nil, nil)
			case APIOpenAICompletions:
				params, err = BuildOpenAICompletionsParams(&fixture.Model, transcript, nil, nil, "", nil)
			default:
				t.Fatalf("unexpected API: %s", fixture.Model.API)
			}
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := MarshalJSON(params)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != fixture.Payload {
				t.Fatalf("payload = %s\nwant      %s", encoded, fixture.Payload)
			}
		})
	}
}
