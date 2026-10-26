package ai

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), api/bedrock-converse-stream.ts wraps payload, response
// and provider-event callbacks in the stream's try/catch. A host callback error
// fails this stream rather than terminating the application. Re-exec isolates
// the previously unrecovered provider-goroutine panic from the test runner.
func TestBedrockCallbackFailuresStayInStream(t *testing.T) {
	const probeEnv = "PIER_TEST_BEDROCK_CALLBACK_FAILURE"
	if mode := os.Getenv(probeEnv); mode != "" {
		frames := [][]byte{
			bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
			bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
		}
		server := bedrockTestServer(t, frames, nil, nil)
		options := &BedrockOptions{
			StreamOptions: StreamOptions{APIKey: "test-key", Env: ProviderEnv{"AWS_REGION": "us-east-1"}},
			BearerToken:   "test-key",
		}
		switch mode {
		case "payload":
			options.OnPayload = func(json.RawMessage, *Model) json.RawMessage { panic(errors.New("hook failed")) }
		case "response":
			options.OnResponse = func(ProviderResponse, *Model) { panic(errors.New("hook failed")) }
		case "event":
			options.OnProviderStreamEvent = func(json.RawMessage, *Model) { panic(errors.New("hook failed")) }
		case "control":
		default:
			t.Fatalf("unknown probe mode %q", mode)
		}
		message, err := StreamBedrockConverse(bedrockTestModel(server.URL), mistralUserContext(), options).Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if mode == "control" {
			if message.StopReason != StopStop {
				t.Fatalf("control stream failed: %+v", message)
			}
		} else if message.StopReason != StopError || message.ErrorMessage == nil || *message.ErrorMessage != "hook failed" {
			t.Fatalf("callback failure did not become a stream error: %+v", message)
		}
		return
	}
	for _, mode := range []string{"control", "payload", "response", "event"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBedrockCallbackFailuresStayInStream$", "-test.timeout=10s")
			command.Env = append(os.Environ(), probeEnv+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("callback %s terminated the process: %v\n%s", mode, err, output)
			}
		})
	}
}
