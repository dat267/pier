package ai

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// pi v1.4.0 (4a080252), api/simple-options.ts buildBaseOptions and provider
// streamSimple wrappers. Capture at the supported onPayload boundary and cancel
// before network I/O. The reference generator throws after the same capture.
// Regenerate:
//
//	node --experimental-strip-types ai/testdata/simple-max-tokens/generate.mjs ./pi > ai/testdata/simple-max-tokens/golden.json
func TestSimpleStreamsClampMaxTokensLikeUpstream(t *testing.T) {
	raw, err := os.ReadFile("testdata/simple-max-tokens/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name     string              `json:"name"`
		Model    Model               `json:"model"`
		Options  SimpleStreamOptions `json:"options"`
		Simple   bool                `json:"simple"`
		Expected *int                `json:"expectedMaxTokens"`
	}
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty simple max-tokens corpus")
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			fixture.Options.Ctx = ctx
			var got *int
			captured := false
			fixture.Options.APIKey = "test-key"
			fixture.Options.Env = ProviderEnv{"AWS_REGION": "us-east-1", "AWS_BEARER_TOKEN_BEDROCK": "test-key"}
			fixture.Options.OnPayload = func(payload json.RawMessage, model *Model) json.RawMessage {
				captured = true
				var value struct {
					MaxTokens       *int `json:"maxTokens"`
					MaxSnake        *int `json:"max_tokens"`
					MaxCompletion   *int `json:"max_completion_tokens"`
					MaxOutput       *int `json:"max_output_tokens"`
					InferenceConfig struct {
						MaxTokens *int `json:"maxTokens"`
					} `json:"inferenceConfig"`
					Config struct {
						MaxTokens *int `json:"maxOutputTokens"`
					} `json:"config"`
				}
				if err := json.Unmarshal(payload, &value); err != nil {
					t.Error(err)
				}
				for _, candidate := range []*int{value.MaxTokens, value.MaxSnake, value.MaxCompletion, value.MaxOutput, value.InferenceConfig.MaxTokens, value.Config.MaxTokens} {
					if candidate != nil {
						got = candidate
						break
					}
				}
				cancel()
				return nil
			}
			provider := builtinAPIStreams()[fixture.Model.API]
			if provider == nil {
				t.Fatalf("unknown API %s", fixture.Model.API)
			}
			transcript := NormalizeContext(Context{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1},
			}})
			var stream *AssistantMessageEventStream
			if fixture.Simple {
				stream = provider.StreamSimple(&fixture.Model, transcript, &fixture.Options)
			} else {
				stream = provider.Stream(&fixture.Model, transcript, &fixture.Options.StreamOptions)
			}
			message, err := stream.Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !captured || message.StopReason != StopAborted {
				t.Fatalf("did not cancel after onPayload: %+v", message)
			}
			if !reflect.DeepEqual(got, fixture.Expected) {
				actual, _ := json.Marshal(got)
				want, _ := json.Marshal(fixture.Expected)
				t.Fatalf("max tokens = %s, want %s", actual, want)
			}
		})
	}
}

// pi v1.5.2 (b4afdddf) api/simple-options.ts clampMaxTokensToContext: the
// safety margin is 15% of the counted input when that exceeds 4096 tokens
// (OpenRouter counted prose up to 12% above the provider's count, #10497), and
// below a 32768-token output room only the fixed margin is kept, with output
// capped at that size. The golden holds the reference's own results.
func TestClampMaxTokensMatchesReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/clamp-max-tokens/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Transcript    string `json:"transcript"`
		ContextWindow int    `json:"contextWindow"`
		MaxTokens     int    `json:"maxTokens"`
		Want          int    `json:"want"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) != 144 {
		t.Fatalf("corpus has %d cases, want 144", len(cases))
	}
	lengths := map[string]int{"empty": 0, "small": 400, "medium": 40000, "large": 400000}
	for _, test := range cases {
		t.Run(strings.Join([]string{test.Transcript, strconv.Itoa(test.ContextWindow), strconv.Itoa(test.MaxTokens)}, "-"), func(t *testing.T) {
			var messages []Message
			if length := lengths[test.Transcript]; length > 0 {
				messages = []Message{&UserMessage{Content: StringOrBlocks{Text: strings.Repeat("x", length)}, Timestamp: 1}}
			}
			model := &Model{ContextWindow: int64(test.ContextWindow), MaxTokens: 64000}
			got := ClampMaxTokensToContext(model, TranscriptContext{Messages: messages}, test.MaxTokens)
			if got != test.Want {
				t.Fatalf("ClampMaxTokensToContext = %d, want %d", got, test.Want)
			}
		})
	}
}
