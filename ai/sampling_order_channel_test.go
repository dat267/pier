package ai

import (
	"encoding/json"
	"testing"
)

// D414: a Go map cannot carry JavaScript insertion order, so request-level
// samplingParams has an explicit order channel. Reference: pi v1.5.0
// (f9a26365) api/openai-completions.ts buildParams assigns the extras onto the
// body object, and JSON.stringify emits them in insertion order:
//
//	{"model":"gpt-4o","messages":[...],"stream":true,"stream_options":...,
//	 "store":false,"top_k":5,"min_p":0.1}
//
// A caller that only fills the map gets the port's sorted fallback; a caller
// that also fills SamplingParamsOrder (the model loader does, from the raw
// JSON keys) reproduces the reference bytes.
func TestRequestSamplingParamsOrderChannel(t *testing.T) {
	model := &Model{ID: "gpt-4o", Name: "GPT-4o", API: APIOpenAICompletions, Provider: "openai",
		BaseURL: "https://api.openai.com/v1", Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 8192}
	context := NormalizeContext(Context{Messages: []Message{
		&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
	}})
	params := SamplingParams{"top_k": json.RawMessage("5"), "min_p": json.RawMessage("0.1")}
	prefix := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}],"stream":true,` +
		`"stream_options":{"include_usage":true},"store":false,`

	// Without an order the port sorts, which is deterministic but not the
	// reference's insertion order.
	unsorted, err := BuildOpenAICompletionsParams(model, context, &OpenAICompletionsOptions{
		StreamOptions: StreamOptions{SamplingParams: params},
	}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(unsorted)
	if err != nil {
		t.Fatal(err)
	}
	if want := prefix + `"min_p":0.1,"top_k":5}`; string(encoded) != want {
		t.Fatalf("sorted fallback = %s\nwant %s", encoded, want)
	}

	// With the order channel the bytes match the reference.
	ordered, err := BuildOpenAICompletionsParams(model, context, &OpenAICompletionsOptions{
		StreamOptions: StreamOptions{
			SamplingParams:      params,
			SamplingParamsOrder: []string{"top_k", "min_p"},
		},
	}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = MarshalJSON(ordered)
	if err != nil {
		t.Fatal(err)
	}
	if want := prefix + `"top_k":5,"min_p":0.1}`; string(encoded) != want {
		t.Fatalf("ordered = %s\nwant %s", encoded, want)
	}

	// A request key that overrides a named field keeps the named field's
	// position, as JavaScript assignment does.
	temperature := 0.2
	override, err := BuildOpenAICompletionsParams(model, context, &OpenAICompletionsOptions{
		StreamOptions: StreamOptions{
			Temperature:         &temperature,
			SamplingParams:      SamplingParams{"temperature": json.RawMessage("0.9")},
			SamplingParamsOrder: []string{"temperature"},
		},
	}, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err = MarshalJSON(override)
	if err != nil {
		t.Fatal(err)
	}
	if want := prefix + `"temperature":0.9}`; string(encoded) != want {
		t.Fatalf("override = %s\nwant %s", encoded, want)
	}
}
