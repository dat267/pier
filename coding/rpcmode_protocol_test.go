package coding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/internal/offloop"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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

// pi v1.4.0 rpc-mode.ts acknowledges accepted prompts independently of the
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

// pi v1.4.0 rpc-mode.ts disposes on input EOF. The accepted user prompt
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

// pi v1.4.0 jsonl.ts uses LF-only framing, accepts a final unterminated
// record, and does not split payload strings at U+2028/U+2029.
func TestRPCJSONLFraming(t *testing.T) {
	id := strings.Repeat("界", 24000) + "\u2028\u2029"
	input := fmt.Sprintf("{\"id\":\"%s\",\"type\":\"get_messages\"}\r\n{\"id\":\"last\",\"type\":\"get_state\"}", id)
	records := runRPCScript(t, rpcTestSession(t), input)
	if len(records) != 2 || records[0]["id"] != id || records[1]["id"] != "last" {
		t.Fatalf("JSONL framing corrupted %d records", len(records))
	}
}

// pi v1.4.0 rpc-mode.ts sends one authoritative preflight failure, not a
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

// pi v1.4.0 rpc-mode.ts get_messages returns role-tagged messages, not
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

// pi v1.4.0 rpc-mode.ts reports malformed JSON and keeps reading commands.
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

// pi v1.4.0 rpc-mode.ts returns a correlated failure for unknown commands.
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

// Reference: pi v1.4.0 core/agent-session-runtime.ts extractUserMessageText
// joins text blocks without a separator for a "before" fork.
func TestRPCUserMessageEntryTextJoinsBlockContent(t *testing.T) {
	raw, err := ai.MarshalMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Blocks: ai.ContentList{
		ai.TextContent{Text: "first "}, ai.TextContent{Text: "question"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	text, ok := rpcUserMessageEntryText(&SessionEntry{Type: "message", Message: raw})
	if !ok || text != "first question" {
		t.Errorf("text = %q, ok = %v; want the joined block text", text, ok)
	}
}

// TestRPCResponseEchoesTheIdOnlyWhenPresent pins the documented correlation
// contract in ARCHITECTURE.md's "Supported commands": "Every command accepts an
// optional string `id`. Responses repeat it when present, including an empty
// string. Omitted ids remain omitted." The distinction matters to a client that
// matches responses to requests: a response must never invent an id, and an
// empty one is still a value the client chose.
func TestRPCResponseEchoesTheIdOnlyWhenPresent(t *testing.T) {
	session := rpcTestSession(t)
	defer session.Dispose()
	records := runRPCScript(t, session, `{"id":"","type":"get_state"}
{"type":"get_state"}
{"id":"abc","type":"get_state"}
`)

	var responses []map[string]any
	for _, record := range records {
		if record["type"] == "response" && record["command"] == "get_state" {
			responses = append(responses, record)
		}
	}
	if len(responses) != 3 {
		t.Fatalf("got %d responses, want 3: %v", len(responses), records)
	}

	id, present := responses[0]["id"]
	if !present || id != "" {
		t.Errorf(`the response to {"id":""} has id %v (present %v), want the empty string`, id, present)
	}
	if _, present := responses[1]["id"]; present {
		t.Errorf("the response to a command without an id carries id %v, want the property omitted", responses[1]["id"])
	}
	if id := responses[2]["id"]; id != "abc" {
		t.Errorf(`the response to {"id":"abc"} has id %v, want "abc"`, id)
	}
}
