package coding

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/dat267/pier/ai"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// untilResponses waits for a response to every id without discarding the ones
// that arrive first. Two commands can respond in either order (abort_bash and
// the bash command it cancels both write asynchronously), so awaiting them one
// at a time through until would drop a response that arrived early.
func (client *rpcTestClient) untilResponses(t *testing.T, ids ...string) map[string]map[string]any {
	t.Helper()
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	found := make(map[string]map[string]any, len(ids))
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	for len(found) < len(wanted) {
		select {
		case record := <-client.output:
			id, ok := record["id"].(string)
			if !ok || record["type"] != "response" || !wanted[id] {
				continue
			}
			found[id] = record
		case <-deadline.C:
			t.Fatalf("timed out waiting for responses %v (got %v)", ids, found)
			return nil
		}
	}
	return found
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

// pi v1.4.0 rpc-mode.ts awaits session.abort before acknowledging it.
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

// pi v1.4.0 rpc-mode.ts returns queued dispositions and clear_queue
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

// pi v1.4.0 rpc-mode.ts queue mode setters affect get_state without
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

// pi v1.4.0 rpc-mode.ts delegates thinking selection and cycling to the
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

// pi v1.4.0 rpc-mode.ts uses the available (authenticated) model snapshot.
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

// pi v1.4.0 agent-session.ts _queueSteer/_queueFollowUp preserve image
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

// Empty queued texts are still messages in pi v1.4.0, including image-only
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

// pi v1.4.0 uses null for unavailable cycles, but empty arrays for
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

// Reference: pi v1.5.0 (f9a26365), modes/rpc/rpc-mode.ts awaits
// session.abort. D235 also includes admitted auth-preflight workers in the Go
// abort contract, without waiting for independently running user bash.
func TestRPCAbortWaitsForCanceledPreflightToSettle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		provider := stubProvider("gate")
		var enabled atomic.Bool
		started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var startedOnce, canceledOnce, releaseOnce sync.Once
		unblock := func() { releaseOnce.Do(func() { close(release) }) }
		defer unblock()
		provider.Auth.APIKey.Resolve = func(input ai.AuthResolveInput) (*ai.AuthResult, error) {
			if enabled.Load() {
				startedOnce.Do(func() { close(started) })
				<-input.Ctx2.Done()
				canceledOnce.Do(func() { close(canceled) })
				<-release // Cancellation was observed, but cleanup is not done.
				return nil, input.Ctx2.Err()
			}
			return nil, nil
		}
		runtime := runtimeWithProviders(t, provider)
		session := rpcTestSession(t)
		session.control.ModelRuntime = runtime
		session.Agent.SetModel(runtime.GetModel("gate", "gate-model"))
		enabled.Store(true)
		client := startRPCTest(t, session)
		client.send(t, `{"id":"prompt","type":"prompt","message":"hello"}`)
		<-started
		client.send(t, `{"id":"abort","type":"abort"}`)
		<-canceled
		synctest.Wait()
		select {
		case record := <-client.output:
			if record["id"] == "abort" {
				t.Errorf("abort acknowledged before canceled prompt preflight settled: %+v", record)
			}
			client.output <- record
		default:
		}
		unblock()
		responses := client.untilResponses(t, "prompt", "abort")
		if responses["abort"]["success"] != true || responses["prompt"]["success"] != false {
			t.Fatalf("responses after preflight settlement = %+v", responses)
		}
	})
}

// RPC reports the session's prompt and skill diagnostics on stderr, never on
// stdout, which carries the protocol; a replacement session reports the ones it
// adds and repeats none (upstream writes the diagnostics it takes from the
// runtime after a rebind, v1.6.0, #12115).
func TestRPCReportsSessionDiagnosticsOnStderr(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.promptDiagnostics = []ResourceDiagnostic{{Type: "warning", Message: "prompt conflict: two files"}}
	var stderr bytes.Buffer
	startRPCTestWith(t, session, RPCModeOptions{Stderr: &stderr})
	// The boot reports it before any command arrives.
	deadline := time.Now().Add(2 * time.Second)
	for stderr.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(stderr.String(), "prompt conflict: two files") {
		t.Fatalf("stderr = %q, want the prompt diagnostic", stderr.String())
	}
	if !strings.Contains(stderr.String(), "Warning: prompt conflict") {
		t.Fatalf("stderr = %q, want the CLI warning shape", stderr.String())
	}
}

// A session whose stored model could not be restored reports the fallback on
// stderr in RPC mode too, not only in interactive mode: every mode reports the
// fallback as a diagnostic at startup and after a replacement (main.ts, v1.6.0,
// #12234).
func TestRPCReportsTheModelFallbackOnStderr(t *testing.T) {
	manager, dir := newTestSession(t)
	session, err := NewAgentSession(&SessionConfig{
		Cwd: dir, Sessions: manager, StreamFn: stubStreamFn,
		Model: &ai.Model{ID: "m", API: ai.APIAnthropicMessages, Provider: "anthropic", ContextWindow: 100000},
	})
	if err != nil {
		t.Fatal(err)
	}
	session.modelFallbackMessage = "Could not restore model nope/nope. Using anthropic/m."
	var stderr bytes.Buffer
	startRPCTestWith(t, session, RPCModeOptions{Stderr: &stderr})
	deadline := time.Now().Add(2 * time.Second)
	for stderr.Len() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !strings.Contains(stderr.String(), "Could not restore model nope/nope. Using anthropic/m.") {
		t.Fatalf("stderr = %q, want the model fallback", stderr.String())
	}
}

// Print mode reports the same diagnostics as RPC: the model fallback and the
// resource warnings go to stderr, so stdout stays the answer
// (print-mode.ts, v1.6.0, #12234).
func TestPrintModeReportsDiagnosticsOnStderr(t *testing.T) {
	var buffer bytes.Buffer
	session := &AgentSession{
		promptDiagnostics:    []ResourceDiagnostic{{Type: "warning", Message: "prompt conflict"}},
		modelFallbackMessage: "Could not restore model nope/nope.",
	}
	reportPrintDiagnostics(session, &buffer)
	for _, want := range []string{"prompt conflict", "Could not restore model nope/nope."} {
		if !strings.Contains(buffer.String(), want) {
			t.Fatalf("stderr = %q, want %q", buffer.String(), want)
		}
	}
}
