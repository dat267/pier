package rpcclient

// Subprocess tests. The fake server is this test binary re-executed with
// PIER_RPC_CLIENT_FAKE=1, mirroring upstream's child-script tests
// (rpc-client-process-exit.test.ts).

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/cmd"
)

// TestRPCClientFakeServer is the child process for the subprocess tests. It
// speaks the RPC JSONL protocol on stdin/stdout.
func TestRPCClientFakeServer(t *testing.T) {
	if os.Getenv("PIER_RPC_CLIENT_FAKE") != "1" {
		t.Skip("fake server only runs as a child process")
	}
	reader := bufio.NewReader(os.Stdin)
	if os.Getenv("PIER_RPC_CLIENT_FAKE_EXIT") == "1" {
		_, _ = reader.ReadBytes('\n')
		os.Exit(43)
	}
	write := func(value string) { _, _ = os.Stdout.WriteString(value + "\n") }
	reply := func(id string, command string, data any) {
		response := map[string]any{"id": id, "type": "response", "command": command, "success": true}
		if data != nil {
			response["data"] = data
		}
		encoded, _ := json.Marshal(response)
		write(string(encoded))
	}
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var command map[string]any
			if json.Unmarshal(line, &command) == nil {
				id, _ := command["id"].(string)
				name, _ := command["type"].(string)
				switch name {
				case "prompt":
					write(`{"type":"turn_start"}`)
					reply(id, name, map[string]any{"disposition": "started"})
					write(`{"type":"agent_settled"}`)
				case "get_state":
					write(`{"type":"custom_event","n":1}`)
					reply(id, name, map[string]any{"sessionId": "fake", "thinkingLevel": "off"})
				case "clear_queue":
					reply(id, name, map[string]any{"steering": []string{"a"}, "followUp": []string{"b"}})
				case "clone":
					reply(id, name, map[string]any{"cancelled": false})
				case "export_html":
					reply(id, name, map[string]any{"path": "/tmp/fake.html"})
				case "get_commands":
					reply(id, name, map[string]any{"commands": []any{}})
				default:
					encoded, _ := json.Marshal(map[string]any{
						"id": id, "type": "response", "command": name, "success": false,
						"error": "Unknown command: " + name,
					})
					write(string(encoded))
				}
			}
		}
		if err != nil {
			return
		}
	}
}

func startFakeClient(t *testing.T, env map[string]string) *RpcClient {
	t.Helper()
	variables := map[string]string{"PIER_RPC_CLIENT_FAKE": "1"}
	for key, value := range env {
		variables[key] = value
	}
	client := New(Options{
		CLIPath:        os.Args[0],
		PeerArgs:       []string{"-test.run=^TestRPCClientFakeServer$"},
		Env:            variables,
		RequestTimeout: 10 * time.Second,
	})
	t.Cleanup(client.Stop)
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	return client
}

func TestClientSubprocessRoundTrip(t *testing.T) {
	client := startFakeClient(t, nil)
	var events []Event
	client.OnEvent(func(event Event) { events = append(events, event) })
	state, err := client.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "fake" || state.ThinkingLevel != "off" {
		t.Fatalf("state = %+v", state)
	}
	if len(events) != 1 || events[0].Type() != "custom_event" {
		t.Fatalf("events = %+v", events)
	}
	if client.Stderr() != "" {
		t.Fatalf("stderr = %q", client.Stderr())
	}
}

func TestClientSubprocessPromptAndWait(t *testing.T) {
	client := startFakeClient(t, nil)
	events, err := client.PromptAndWait("hi", nil, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var types []string
	for _, event := range events {
		types = append(types, event.Type())
	}
	if strings.Join(types, ",") != "turn_start,agent_settled" {
		t.Fatalf("event types = %v", types)
	}
}

// TestPromptAndWaitSubscribesBeforeSending pins upstream's ordering:
// collectEvents builds its subscription inside the Promise executor, before
// prompt() runs, so an event emitted while the prompt is acknowledged is
// still collected. The port used to start collection on a goroutine.
func TestPromptAndWaitSubscribesBeforeSending(t *testing.T) {
	client := New(Options{})
	client.sendFunc = func(command map[string]any) (*response, error) {
		if command["type"] == "prompt" {
			client.mu.Lock()
			listeners := make([]EventListener, 0, len(client.listeners))
			for _, entry := range client.listeners {
				listeners = append(listeners, entry.listener)
			}
			client.mu.Unlock()
			for _, listener := range listeners {
				listener(Event{"type": "agent_settled"})
			}
		}
		return &response{Type: "response", Command: "prompt", Success: true, Data: json.RawMessage(`{"disposition":"started"}`)}, nil
	}
	events, err := client.PromptAndWait("hi", nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type() != "agent_settled" {
		t.Fatalf("events = %+v", events)
	}
}

func TestClientRejectsInFlightOnExit(t *testing.T) {
	client := startFakeClient(t, map[string]string{"PIER_RPC_CLIENT_FAKE_EXIT": "1"})
	_, err := client.Commands()
	if err == nil || !strings.Contains(err.Error(), "Agent process exited (code=43") {
		t.Fatalf("err = %v", err)
	}
	if _, err := client.State(); err == nil || !strings.Contains(err.Error(), "Agent process exited (code=43") {
		t.Fatalf("second err = %v", err)
	}
}

func TestClientStopIsIdempotent(t *testing.T) {
	client := startFakeClient(t, nil)
	if _, err := client.State(); err != nil {
		t.Fatal(err)
	}
	client.Stop()
	client.Stop()
	if _, err := client.State(); err == nil || err.Error() != "Client not started" {
		t.Fatalf("err after stop = %v", err)
	}
}

// Reference: pi v1.4.0 modes/rpc/rpc-client.ts stop clears this.process, so the
// same client can start a fresh subprocess after its previous one is stopped.
func TestClientCanRestartAfterStop(t *testing.T) {
	client := startFakeClient(t, nil)
	if _, err := client.State(); err != nil {
		t.Fatal(err)
	}
	client.Stop()
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("restart: %v", err)
	}
	state, err := client.State()
	if err != nil || state.SessionID != "fake" {
		t.Fatalf("state after restart = %+v, %v", state, err)
	}
}

// TestRPCClientRealServer drives the client against the real CLI: the child is
// this test binary re-executed, and cmd.Execute runs the shipped RPC mode.
func TestRPCClientRealServer(t *testing.T) {
	if os.Getenv("PIER_RPC_CLIENT_REAL") == "1" {
		os.Args = []string{"pier", "--mode", "rpc", "--offline", "--no-mcp", "--no-session", "--no-tools", "--approve"}
		cmd.Execute()
		os.Exit(0)
	}
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	t.Setenv("TMPDIR", t.TempDir())
	client := New(Options{
		CLIPath:        os.Args[0],
		PeerArgs:       []string{"-test.run=^TestRPCClientRealServer$"},
		Env:            map[string]string{"PIER_RPC_CLIENT_REAL": "1"},
		RequestTimeout: 30 * time.Second,
	})
	t.Cleanup(client.Stop)
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	state, err := client.State()
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID == "" || state.IsStreaming {
		t.Fatalf("state = %+v", state)
	}
	result, err := client.Bash("echo hi")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(result.Output) != "hi" || result.ExitCode == nil || *result.ExitCode != 0 || result.Cancelled {
		t.Fatalf("bash = %+v", result)
	}
	if _, err := client.SessionStats(); err != nil {
		t.Fatalf("stats: %v", err)
	}
	if _, err := client.Commands(); err != nil {
		t.Fatalf("commands: %v", err)
	}
	if _, err := client.ExportHTML(""); err == nil || !strings.Contains(err.Error(), "Cannot export in-memory session to HTML") {
		t.Fatalf("export err = %v", err)
	}
}
