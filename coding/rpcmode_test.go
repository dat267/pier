package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
)

func rpcTestSession(t *testing.T) *AgentSession {
	t.Helper()
	persist := false
	session, err := NewAgentSession(&SessionConfig{
		Cwd: t.TempDir(), StreamFn: stubStreamFn,
		Sessions: NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist}),
	})
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func runRPCScript(t *testing.T, session *AgentSession, input string) []map[string]any {
	t.Helper()
	var output bytes.Buffer
	if err := RunRPCMode(context.Background(), session, RPCModeOptions{Input: io.NopCloser(strings.NewReader(input)), Output: &output}); err != nil {
		t.Fatal(err)
	}
	var records []map[string]any
	decoder := json.NewDecoder(&output)
	for decoder.More() {
		var record map[string]any
		if err := decoder.Decode(&record); err != nil {
			t.Fatal(err)
		}
		records = append(records, record)
	}
	return records
}

type rpcTestOutput chan map[string]any

func (output rpcTestOutput) Write(data []byte) (int, error) {
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		return 0, err
	}
	output <- record
	return len(data), nil
}

type rpcTestClient struct {
	input  *io.PipeWriter
	output rpcTestOutput
	done   chan error
}

func startRPCTest(t *testing.T, session *AgentSession) *rpcTestClient {
	return startRPCTestWith(t, session, RPCModeOptions{})
}

func startRPCTestWith(t *testing.T, session *AgentSession, options RPCModeOptions) *rpcTestClient {
	t.Helper()
	input, writer := io.Pipe()
	client := &rpcTestClient{writer, make(rpcTestOutput, 128), make(chan error, 1)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	options.Input = input
	options.Output = client.output
	go func() { client.done <- RunRPCMode(ctx, session, options) }()
	t.Cleanup(func() {
		writer.Close()
		cancel()
		select {
		case <-client.done:
		case <-time.After(6 * time.Second):
			t.Error("RPC did not stop")
		}
	})
	return client
}

func (client *rpcTestClient) send(t *testing.T, command string) {
	t.Helper()
	if _, err := io.WriteString(client.input, command+"\n"); err != nil {
		t.Fatal(err)
	}
}

func (client *rpcTestClient) until(t *testing.T, match func(map[string]any) bool) map[string]any {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case record := <-client.output:
			if match(record) {
				return record
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for RPC record")
			return nil
		}
	}
}

// pi v1.3.0 rpc-mode.ts acknowledges accepted prompts independently of the
// eventual run; events continue streaming while commands remain readable.
func blockingRPCSession(t *testing.T) *AgentSession {
	t.Helper()
	session := rpcTestSession(t)
	session.Agent.SetModel(&ai.Model{ID: "m", Provider: "test", API: ai.APIAnthropicMessages, ContextWindow: 100000})
	session.Agent.StreamFunction = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		stream := ai.NewAssistantMessageEventStream()
		go func() {
			<-options.Ctx.Done()
			message := &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopAborted, Content: ai.ContentList{}}
			stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: message})
		}()
		return stream
	}
	return session
}

func TestRPCPromptAcknowledgedBeforeCompletion(t *testing.T) {
	session := blockingRPCSession(t)
	client := startRPCTest(t, session)
	client.send(t, `{"id":"prompt","type":"prompt","message":"hello"}`)
	response := client.until(t, func(record map[string]any) bool { return record["id"] == "prompt" })
	if response["success"] != true || response["data"].(map[string]any)["disposition"] != "started" {
		t.Fatalf("response = %+v", response)
	}
	client.until(t, func(record map[string]any) bool { return record["type"] == "agent_start" })
	client.send(t, `{"id":"state","type":"get_state"}`)
	state := client.until(t, func(record map[string]any) bool { return record["id"] == "state" })
	if state["data"].(map[string]any)["isStreaming"] != true {
		t.Fatalf("state = %+v", state)
	}
}

// pi v1.3.0 rpc-mode.ts awaits session.abort before acknowledging it.
func TestRPCAbortAcknowledgesAfterSettled(t *testing.T) {
	client := startRPCTest(t, blockingRPCSession(t))
	client.send(t, `{"id":"prompt","type":"prompt","message":"hello"}`)
	client.until(t, func(record map[string]any) bool { return record["id"] == "prompt" })
	client.send(t, `{"id":"abort","type":"abort"}`)
	settled := false
	response := client.until(t, func(record map[string]any) bool {
		if record["type"] == "agent_settled" {
			settled = true
		}
		return record["id"] == "abort"
	})
	if response["success"] != true || !settled {
		t.Fatalf("abort response = %+v; settled = %v", response, settled)
	}
	client.send(t, `{"id":"idle","type":"get_state"}`)
	state := client.until(t, func(record map[string]any) bool { return record["id"] == "idle" })
	if state["data"].(map[string]any)["isStreaming"] != false {
		t.Fatalf("state = %+v", state)
	}
}

// pi v1.3.0 rpc-mode.ts returns queued dispositions and clear_queue
// returns expanded texts for both queues, including empty arrays.
func TestRPCQueuedInputControl(t *testing.T) {
	for _, command := range []string{
		`{"id":"queue","type":"steer","message":"/greet friend"}`,
		`{"id":"queue","type":"follow_up","message":"/greet friend"}`,
		`{"id":"queue","type":"prompt","message":"/greet friend","streamingBehavior":"followUp"}`,
	} {
		t.Run(command, func(t *testing.T) {
			session := blockingRPCSession(t)
			started := make(chan struct{})
			stream := session.Agent.StreamFunction
			session.Agent.StreamFunction = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
				result := stream(model, transcript, options)
				close(started)
				return result
			}
			session.control.PromptTemplates = []PromptTemplate{{Name: "greet", Content: "Hello $1"}}
			client := startRPCTest(t, session)
			client.send(t, `{"id":"start","type":"prompt","message":"hello"}`)
			client.until(t, func(record map[string]any) bool { return record["id"] == "start" })
			// Wait past the initial steering poll so the queued input remains
			// pending until clear_queue, rather than being delivered immediately.
			<-started
			client.send(t, command)
			response := client.until(t, func(record map[string]any) bool { return record["id"] == "queue" })
			if response["success"] != true || response["data"].(map[string]any)["disposition"] != "queued" {
				t.Fatalf("response = %+v", response)
			}
			client.send(t, `{"id":"clear","type":"clear_queue"}`)
			cleared := client.until(t, func(record map[string]any) bool { return record["id"] == "clear" })
			if cleared["success"] != true {
				t.Fatal(cleared)
			}
			data := cleared["data"].(map[string]any)
			steering, followUp := data["steering"].([]any), data["followUp"].([]any)
			if len(steering)+len(followUp) != 1 {
				t.Fatalf("queues = %+v", data)
			}
			if append(steering, followUp...)[0] != "Hello friend" {
				t.Fatalf("queues = %+v", data)
			}
			client.send(t, `{"id":"state","type":"get_state"}`)
			state := client.until(t, func(record map[string]any) bool { return record["id"] == "state" })
			if state["data"].(map[string]any)["pendingMessageCount"] != float64(0) {
				t.Fatal(state)
			}
		})
	}
}

// pi v1.3.0 rpc-mode.ts queue mode setters affect get_state without
// persisting global settings.
func TestRPCQueueModes(t *testing.T) {
	records := runRPCScript(t, rpcTestSession(t), `{"type":"set_steering_mode","mode":"all"}
{"type":"set_follow_up_mode","mode":"all"}
{"type":"get_state"}
`)
	if records[0]["success"] != true || records[1]["success"] != true {
		t.Fatal(records)
	}
	state := records[2]["data"].(map[string]any)
	if state["steeringMode"] != "all" || state["followUpMode"] != "all" {
		t.Fatal(state)
	}
}

func rpcResponses(records []map[string]any) []map[string]any {
	var responses []map[string]any
	for _, record := range records {
		if record["type"] == "response" {
			responses = append(responses, record)
		}
	}
	return responses
}

// pi v1.3.0 rpc-mode.ts delegates thinking selection and cycling to the
// session; the setters do not persist global defaults.
func TestRPCThinkingSelection(t *testing.T) {
	session := rpcTestSession(t)
	session.Agent.SetModel(&ai.Model{ID: "m", Provider: "test", Reasoning: true})
	records := rpcResponses(runRPCScript(t, session, `{"type":"get_available_thinking_levels"}
{"type":"set_thinking_level","level":"low"}
{"type":"cycle_thinking_level"}
{"type":"get_state"}
`))
	for _, response := range records {
		if response["success"] != true {
			t.Fatal(response)
		}
	}
	levels := records[0]["data"].(map[string]any)["levels"]
	if !reflect.DeepEqual(levels, []any{"off", "minimal", "low", "medium", "high"}) {
		t.Fatalf("levels = %+v", levels)
	}
	if records[2]["data"].(map[string]any)["level"] != "medium" || records[3]["data"].(map[string]any)["thinkingLevel"] != "medium" {
		t.Fatal(records)
	}
}

// pi v1.3.0 rpc-mode.ts uses the available (authenticated) model snapshot.
func TestRPCModelSelection(t *testing.T) {
	session := rpcTestSession(t)
	runtime := runtimeWithProviders(t, stubProvider("alpha"), stubProvider("beta"))
	for _, provider := range []string{"alpha", "beta"} {
		if err := runtime.SetRuntimeAPIKey(provider, "test-key", context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	session.control.ModelRuntime = runtime
	records := rpcResponses(runRPCScript(t, session, `{"type":"get_available_models"}
{"type":"set_model","provider":"beta","modelId":"beta-model"}
{"type":"cycle_model"}
{"type":"get_state"}
{"id":"missing","type":"set_model","provider":"missing","modelId":"none"}
`))
	for _, response := range records[:4] {
		if response["success"] != true {
			t.Fatal(response)
		}
	}
	if len(records[0]["data"].(map[string]any)["models"].([]any)) != 2 {
		t.Fatal(records[0])
	}
	if records[1]["data"].(map[string]any)["provider"] != "beta" {
		t.Fatal(records[1])
	}
	cycle := records[2]["data"].(map[string]any)
	if cycle["model"].(map[string]any)["provider"] != "alpha" || cycle["isScoped"] != false {
		t.Fatal(cycle)
	}
	if records[3]["data"].(map[string]any)["model"].(map[string]any)["provider"] != "alpha" {
		t.Fatal(records[3])
	}
	if records[4]["success"] != false || records[4]["error"] != "Model not found: missing/none" {
		t.Fatal(records[4])
	}
}

type rpcFailEventWriter struct{ err error }

func (writer rpcFailEventWriter) Write(data []byte) (int, error) {
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		return 0, err
	}
	if record["type"] != "response" {
		return 0, writer.err
	}
	return len(data), nil
}

// A vanished stdout consumer must stop the run and report the I/O error,
// rather than leaving a headless agent running after its host disconnects.
func TestRPCEventWriteFailureStopsSession(t *testing.T) {
	input, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	failure := errors.New("peer disconnected")
	done := make(chan error, 1)
	session := blockingRPCSession(t)
	go func() {
		done <- RunRPCMode(ctx, session, RPCModeOptions{Input: input, Output: rpcFailEventWriter{failure}})
	}()
	if _, err := io.WriteString(writer, "{\"type\":\"prompt\",\"message\":\"hello\"}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, failure) {
			t.Fatalf("error = %v, want %v", err, failure)
		}
	case <-ctx.Done():
		t.Fatal("RPC survived an output failure")
	}
}

type rpcGatedWriter struct{ started, release chan struct{} }

func (writer rpcGatedWriter) Write(data []byte) (int, error) {
	close(writer.started)
	<-writer.release
	return len(data), nil
}

// D235: a slow stdout consumer may delay bytes, never cancellation or
// accepted-save cleanup. Only the byte pump may remain in an OS write.
func TestRPCCancelUnblocksSlowOutput(t *testing.T) {
	input, writer := io.Pipe()
	defer writer.Close()
	output := rpcGatedWriter{make(chan struct{}), make(chan struct{})}
	defer close(output.release)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	session := rpcTestSession(t)
	go func() { done <- RunRPCMode(ctx, session, RPCModeOptions{Input: input, Output: output}) }()
	if _, err := io.WriteString(writer, "{\"type\":\"get_state\"}\n"); err != nil {
		t.Fatal(err)
	}
	<-output.started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow output prevented cancellation")
	}
}

// Auth preflight may perform I/O. pi's awaited checks yield its event loop,
// so abort must remain readable before a prompt has been acknowledged.
func preflightRPCSession(t *testing.T) (*AgentSession, <-chan struct{}) {
	t.Helper()
	provider := stubProvider("gate")
	var enabled atomic.Bool
	started := make(chan struct{}, 1)
	provider.Auth.APIKey.Resolve = func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
		if enabled.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
			<-input.Ctx2.Done()
			return nil, input.Ctx2.Err()
		}
		return nil, nil
	}
	runtime := runtimeWithProviders(t, provider)
	session := rpcTestSession(t)
	session.control.ModelRuntime = runtime
	session.Agent.SetModel(runtime.GetModel("gate", "gate-model"))
	enabled.Store(true)
	return session, started
}

func TestRPCAbortDuringPromptPreflight(t *testing.T) {
	session, started := preflightRPCSession(t)
	client := startRPCTest(t, session)
	client.send(t, `{"id":"prompt","type":"prompt","message":"hello"}`)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("preflight did not start")
	}
	client.send(t, `{"id":"abort","type":"abort"}`)
	response := client.until(t, func(record map[string]any) bool { return record["id"] == "abort" })
	if response["success"] != true {
		t.Fatal(response)
	}
}

// D235 validates command fields at the wire boundary without losing a valid
// id/type, and malformed requests must not mutate the session.
func TestRPCInvalidFieldsPreserveCorrelation(t *testing.T) {
	for _, tc := range []struct{ command, fragment string }{
		{`{"id":"bad","type":"prompt"}`, "message"},
		{`{"id":"bad","type":"prompt","message":false}`, "message"},
		{`{"id":"bad","type":"prompt","message":"hi","streamingBehavior":"invalid"}`, "streamingBehavior"},
		{`{"id":"bad","type":"set_thinking_level","level":"invalid"}`, "level"},
		{`{"id":"bad","type":"set_model","provider":"test"}`, "modelId"},
	} {
		t.Run(tc.command, func(t *testing.T) {
			client := startRPCTest(t, rpcTestSession(t))
			client.send(t, tc.command)
			response := client.until(t, func(record map[string]any) bool { return record["id"] == "bad" })
			if response["success"] != false || !strings.Contains(response["error"].(string), tc.fragment) {
				t.Fatalf("response = %+v", response)
			}
		})
	}
}

// D235 bounds pending auth/preflight work, with a visible rejection rather
// than an unbounded goroutine per input record.
func TestRPCPromptAdmissionIsBounded(t *testing.T) {
	session, started := preflightRPCSession(t)
	client := startRPCTest(t, session)
	client.send(t, `{"id":"0","type":"prompt","message":"hello"}`)
	<-started
	for i := 1; i <= 4; i++ {
		client.send(t, fmt.Sprintf(`{"id":"%d","type":"prompt","message":"hello"}`, i))
	}
	response := client.until(t, func(record map[string]any) bool { return record["id"] == "4" })
	if response["success"] != false || !strings.Contains(response["error"].(string), "pending prompt") {
		t.Fatal(response)
	}
	client.send(t, `{"id":"abort","type":"abort"}`)
	response = client.until(t, func(record map[string]any) bool { return record["id"] == "abort" })
	if response["success"] != true {
		t.Fatal(response)
	}
}

// RpcSessionState's optional model is absent when no model was selected;
// internal placeholder models and empty thinking levels are not wire values.
func TestRPCStateWithoutSelectedModel(t *testing.T) {
	response := runRPCScript(t, rpcTestSession(t), "{\"type\":\"get_state\"}\n")[0]
	state := response["data"].(map[string]any)
	for _, key := range []string{"model", "sessionFile", "sessionName"} {
		if value, exists := state[key]; exists {
			t.Fatalf("unexpected %s = %+v", key, value)
		}
	}
	if state["thinkingLevel"] != "off" {
		t.Fatalf("thinking level = %v", state["thinkingLevel"])
	}
}

// pi v1.3.0 rpc-mode.ts disposes on input EOF. The accepted user prompt
// must survive even when its session write is still queued at shutdown.
func TestRPCDrainsAcceptedSavesOnEOF(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", t.TempDir())
	queue := offloop.New()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer func() { unblock(); queue.Stop() }()
	queue.Go(func() { <-release })
	sessions := NewSessionManager(dir, &SessionManagerOptions{SessionDir: dir, WriteQueue: queue})
	started := make(chan struct{})
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: sessions, Model: &ai.Model{ID: "m", Provider: "test", API: ai.APIAnthropicMessages, ContextWindow: 100000},
		StreamFn: func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
			stream := ai.NewAssistantMessageEventStream()
			go func() {
				close(started)
				<-options.Ctx.Done()
				unblock()
				message := &ai.AssistantMessage{API: model.API, Provider: model.Provider, Model: model.ID, StopReason: ai.StopAborted, Content: ai.ContentList{}}
				stream.Push(ai.AssistantMessageEvent{Type: ai.EventError, Reason: ai.StopAborted, Error: message})
			}()
			return stream
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, writer := io.Pipe()
	defer writer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunRPCMode(ctx, session, RPCModeOptions{Input: input, Output: io.Discard}) }()
	if _, err := io.WriteString(writer, "{\"type\":\"prompt\",\"message\":\"keep accepted prompt\"}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("provider did not start")
	}
	writer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("EOF did not drain accepted saves")
	}
	data, err := os.ReadFile(sessions.GetSessionFile())
	if err != nil || !bytes.Contains(data, []byte("keep accepted prompt")) {
		t.Fatalf("accepted prompt was lost: %q, %v", data, err)
	}
}

// pi v1.3.0 jsonl.ts uses LF-only framing, accepts a final unterminated
// record, and does not split payload strings at U+2028/U+2029.
func TestRPCJSONLFraming(t *testing.T) {
	id := strings.Repeat("界", 24000) + "\u2028\u2029"
	input := fmt.Sprintf("{\"id\":\"%s\",\"type\":\"get_messages\"}\r\n{\"id\":\"last\",\"type\":\"get_state\"}", id)
	records := runRPCScript(t, rpcTestSession(t), input)
	if len(records) != 2 || records[0]["id"] != id || records[1]["id"] != "last" {
		t.Fatalf("JSONL framing corrupted %d records", len(records))
	}
}

// pi v1.3.0 rpc-mode.ts sends one authoritative preflight failure, not a
// success followed by an error response for the same prompt.
func TestRPCPromptPreflightFailure(t *testing.T) {
	client := startRPCTest(t, rpcTestSession(t))
	client.send(t, `{"id":"prompt","type":"prompt","message":"hello"}`)
	response := client.until(t, func(record map[string]any) bool { return record["id"] == "prompt" })
	if response["success"] != false || !strings.Contains(response["error"].(string), "No model selected.") {
		t.Fatal(response)
	}
	client.send(t, `{"id":"state","type":"get_state"}`)
	client.until(t, func(record map[string]any) bool {
		if record["id"] == "prompt" {
			t.Error("duplicate prompt response")
		}
		if record["type"] == "agent_start" {
			t.Error("preflight failure started a run")
		}
		return record["id"] == "state"
	})
}

func TestRPCPromptDuringRunRequiresStreamingBehavior(t *testing.T) {
	client := startRPCTest(t, blockingRPCSession(t))
	client.send(t, `{"id":"start","type":"prompt","message":"hello"}`)
	client.until(t, func(record map[string]any) bool { return record["id"] == "start" })
	client.send(t, `{"id":"second","type":"prompt","message":"hello"}`)
	response := client.until(t, func(record map[string]any) bool { return record["id"] == "second" })
	if response["success"] != false || !strings.Contains(response["error"].(string), "Specify streamingBehavior") {
		t.Fatal(response)
	}
}

// pi v1.3.0 agent-session.ts _queueSteer/_queueFollowUp preserve image
// blocks and stamp the queued user message before it enters the transcript.
func TestRPCQueuedImagesReachTranscript(t *testing.T) {
	for _, kind := range []string{"steer", "follow_up"} {
		t.Run(kind, func(t *testing.T) {
			session := rpcTestSession(t)
			session.Agent.SetModel(&ai.Model{ID: "m", Provider: "test", API: ai.APIAnthropicMessages, ContextWindow: 100000})
			var prompts atomic.Int64
			session.Agent.StreamFunction = recordingStreamFn(t, &prompts)
			client := startRPCTest(t, session)
			client.send(t, fmt.Sprintf(`{"id":"queue","type":"%s","message":"look","images":[{"type":"image","data":"YQ==","mimeType":"image/png"}]}`, kind))
			client.until(t, func(record map[string]any) bool { return record["id"] == "queue" })
			client.send(t, `{"id":"start","type":"prompt","message":"go"}`)
			client.until(t, func(record map[string]any) bool { return record["type"] == "agent_settled" })
			client.send(t, `{"id":"messages","type":"get_messages"}`)
			response := client.until(t, func(record map[string]any) bool { return record["id"] == "messages" })
			found := false
			for _, value := range response["data"].(map[string]any)["messages"].([]any) {
				message := value.(map[string]any)
				if message["role"] != "user" {
					continue
				}
				blocks, ok := message["content"].([]any)
				if !ok || len(blocks) != 2 || blocks[0].(map[string]any)["text"] != "look" {
					continue
				}
				found = true
				if message["timestamp"].(float64) <= 0 {
					t.Fatal("queued message has no creation timestamp")
				}
				image := blocks[1].(map[string]any)
				if image["type"] != "image" || image["data"] != "YQ==" || image["mimeType"] != "image/png" {
					t.Fatal(image)
				}
			}
			if !found {
				t.Fatalf("queued image message missing: %+v", response)
			}
		})
	}
}

// Empty queued texts are still messages in pi v1.3.0, including image-only
// input. clear_queue returns them rather than confusing them with no input.
func TestRPCEmptyQueuedMessageIsRetained(t *testing.T) {
	for _, kind := range []string{"steer", "follow_up"} {
		t.Run(kind, func(t *testing.T) {
			records := rpcResponses(runRPCScript(t, rpcTestSession(t), fmt.Sprintf("{\"type\":\"%s\",\"message\":\"\"}\n{\"type\":\"clear_queue\"}\n", kind)))
			data := records[1]["data"].(map[string]any)
			key := "steering"
			if kind == "follow_up" {
				key = "followUp"
			}
			if !reflect.DeepEqual(data[key], []any{""}) {
				t.Fatalf("queues = %+v", data)
			}
		})
	}
}

// pi v1.3.0 uses null for unavailable cycles, but empty arrays for
// collection-valued responses; neither optional data shape may disappear.
func TestRPCUnavailableSelections(t *testing.T) {
	var output bytes.Buffer
	err := RunRPCMode(context.Background(), rpcTestSession(t), RPCModeOptions{
		Input: io.NopCloser(strings.NewReader("{\"type\":\"get_available_models\"}\n{\"type\":\"cycle_model\"}\n{\"type\":\"cycle_thinking_level\"}\n")), Output: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"type\":\"response\",\"command\":\"get_available_models\",\"success\":true,\"data\":{\"models\":[]}}\n" +
		"{\"type\":\"response\",\"command\":\"cycle_model\",\"success\":true,\"data\":null}\n" +
		"{\"type\":\"response\",\"command\":\"cycle_thinking_level\",\"success\":true,\"data\":null}\n"
	if output.String() != want {
		t.Fatalf("responses = %s, want %s", output.String(), want)
	}
}

// JSON property names follow JavaScript's case-sensitive lookup, not Go's
// case-insensitive struct-field aliases.
func TestRPCFieldNamesAreCaseSensitive(t *testing.T) {
	records := runRPCScript(t, rpcTestSession(t), `{"Type":"get_state"}
{"id":"bad","type":"prompt","Message":"hello"}
{"ID":"ignored","type":"get_messages"}
`)
	if len(records) != 3 || records[0]["success"] != false || records[0]["command"] != "parse" || records[1]["id"] != "bad" || records[1]["success"] != false {
		t.Fatal(records)
	}
	if _, exists := records[2]["id"]; exists {
		t.Fatal("uppercase ID became a correlation id")
	}
}

// pi v1.3.0 rpc-mode.ts get_messages returns role-tagged messages, not
// session entries, and an empty transcript is an array rather than null.
func TestRPCGetMessages(t *testing.T) {
	for _, text := range []string{"", "hello <world>"} {
		t.Run(text, func(t *testing.T) {
			session := rpcTestSession(t)
			if text != "" {
				session.Agent.SetMessages([]ai.Message{&ai.UserMessage{Content: ai.StringOrBlocks{Text: text}}})
			}
			records := runRPCScript(t, session, "{\"type\":\"get_messages\"}\n")
			response := records[0]
			if response["success"] != true {
				t.Fatalf("response = %+v", response)
			}
			if _, hasID := response["id"]; hasID {
				t.Fatal("omitted id must remain omitted")
			}
			messages, ok := response["data"].(map[string]any)["messages"].([]any)
			if !ok {
				t.Fatalf("messages = %+v", response)
			}
			if text == "" {
				if len(messages) != 0 {
					t.Fatal(messages)
				}
				return
			}
			message := messages[0].(map[string]any)
			if message["role"] != "user" || message["content"] != text {
				t.Fatalf("message = %+v", message)
			}
		})
	}
}

// pi v1.3.0 rpc-mode.ts reports malformed JSON and keeps reading commands.
func TestRPCMalformedInputDoesNotCloseSession(t *testing.T) {
	for _, input := range []string{"{", "null", "[]", "42", `{"type":42}`, `{"type":"prompt","message":false}`} {
		t.Run(input, func(t *testing.T) {
			records := runRPCScript(t, rpcTestSession(t), input+"\n{\"id\":\"next\",\"type\":\"get_state\"}\n")
			if len(records) != 2 || records[0]["success"] != false || records[1]["id"] != "next" || records[1]["success"] != true {
				t.Fatalf("responses = %+v", records)
			}
		})
	}
}

// pi v1.3.0 rpc-mode.ts returns a correlated failure for unknown commands.
func TestRPCUnknownCommand(t *testing.T) {
	var output bytes.Buffer
	err := RunRPCMode(context.Background(), rpcTestSession(t), RPCModeOptions{
		Input: io.NopCloser(strings.NewReader("{\"id\":\"<&>\",\"type\":\"missing\"}\n")), Output: &output,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\"id\":\"<&>\",\"type\":\"response\",\"command\":\"missing\",\"success\":false,\"error\":\"Unknown command: missing\"}\n"
	if output.String() != want {
		t.Fatalf("response = %s, want %s", output.String(), want)
	}
}

// pi v1.3.0 rpc-mode.ts export_html calls
// pi v1.3.0 rpc-mode.ts export_html calls agent-session.exportToHtml with the
// optional outputPath and responds { path }. The renderer lives in the
// interactive package, so the host injects it as a seam.
func TestRPCExportHTML(t *testing.T) {
	session := rpcTestSession(t)
	type call struct {
		session    *AgentSession
		outputPath string
	}
	calls := make(chan call, 4)
	stubPath := filepath.Join(t.TempDir(), "session.html")
	client := startRPCTestWith(t, session, RPCModeOptions{
		ExportToHtml: func(called *AgentSession, outputPath string) (string, error) {
			calls <- call{called, outputPath}
			return stubPath, nil
		},
	})
	assertPath := func(id string) {
		t.Helper()
		record := client.until(t, func(record map[string]any) bool { return record["id"] == id })
		if record["type"] != "response" || record["command"] != "export_html" || record["success"] != true {
			t.Fatalf("response = %+v", record)
		}
		if data, ok := record["data"].(map[string]any); !ok || data["path"] != stubPath {
			t.Fatalf("data = %+v", record["data"])
		}
	}
	client.send(t, `{"id":"e1","type":"export_html","outputPath":"/tmp/out.html"}`)
	assertPath("e1")
	if got := <-calls; got.session != session || got.outputPath != "/tmp/out.html" {
		t.Fatalf("seam call = %+v", got)
	}
	client.send(t, `{"id":"e2","type":"export_html"}`)
	assertPath("e2")
	if got := <-calls; got.session != session || got.outputPath != "" {
		t.Fatalf("omitted outputPath reached the seam as %q", got.outputPath)
	}
}

// Without an injected renderer export_html fails explicitly, like the other
// host-provided surfaces.
func TestRPCExportHTMLWithoutRenderer(t *testing.T) {
	client := startRPCTest(t, rpcTestSession(t))
	client.send(t, `{"id":"e","type":"export_html"}`)
	record := client.until(t, func(record map[string]any) bool { return record["id"] == "e" })
	if record["success"] != false || record["error"] != "HTML export is not available in this build" {
		t.Fatalf("response = %+v", record)
	}
}
