package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// pi v1.4.0 (4a080252), api/pi-messages.ts wraps onPayload, onResponse and
// onProviderStreamEvent in the stream's try/catch. A host callback failure must
// fail this stream, not terminate the application; re-exec isolates the panic.
func TestPiMessagesCallbackFailuresStayInStream(t *testing.T) {
	const probeEnv = "PIER_TEST_PI_MESSAGES_CALLBACK_FAILURE"
	if mode := os.Getenv(probeEnv); mode != "" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"type\":\"text_delta\",\"contentIndex\":0,\"delta\":\"ok\"}\n\ndata: {\"type\":\"done\",\"reason\":\"stop\"}\n\n"))
		}))
		defer server.Close()
		model := &Model{ID: "fixture", API: APIPiMessages, Provider: "pi", BaseURL: server.URL}
		options := &PiMessagesOptions{StreamOptions: StreamOptions{APIKey: "test-key"}}
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
		message, err := StreamPiMessages(model, mistralUserContext(), options).Result(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if mode == "control" {
			if message.StopReason == StopError {
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
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPiMessagesCallbackFailuresStayInStream$", "-test.timeout=10s")
			command.Env = append(os.Environ(), probeEnv+"="+mode)
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("callback %s terminated the process: %v\n%s", mode, err, output)
			}
		})
	}
}
