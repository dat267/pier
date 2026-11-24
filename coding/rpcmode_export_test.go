package coding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
)

func rpcResponses(records []map[string]any) []map[string]any {
	var responses []map[string]any
	for _, record := range records {
		if record["type"] == "response" {
			responses = append(responses, record)
		}
	}
	return responses
}

// pi v1.4.0 rpc-mode.ts export_html calls
// pi v1.4.0 rpc-mode.ts export_html calls agent-session.exportToHtml with the
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

// pi v1.4.0 core/agent-session-runtime.ts fork settles the old turn before
// mutating an in-memory manager in place. Its aborted tail must not be written
// into the newly selected branch. This exercises the JSONL RPC boundary.
func TestRPCInMemoryForkSettlesBeforeChangingBranch(t *testing.T) {
	session := blockingRPCSession(t)
	firstID := session.Sessions.AppendMessage(&ai.UserMessage{Content: ai.StringOrBlocks{Text: "original"}})
	session.Agent.SetMessages(session.Sessions.Projection().Messages)
	entered := make(chan struct{})
	stream := session.Agent.StreamFunction
	session.Agent.StreamFunction = func(model *ai.Model, transcript ai.TranscriptContext, options *ai.SimpleStreamOptions) *ai.AssistantMessageEventStream {
		close(entered)
		return stream(model, transcript, options)
	}
	client := startRPCTestWith(t, session, RPCModeOptions{ReplaceSession: rpcReplaceFactory()})
	client.send(t, `{"id":"prompt","type":"prompt","message":"still running"}`)
	if response := client.until(t, byResponseID("prompt")); response["success"] != true {
		t.Fatalf("prompt response = %+v", response)
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not enter provider stream")
	}
	client.send(t, `{"id":"fork","type":"fork","entryId":"`+firstID+`"}`)
	if response := client.until(t, byResponseID("fork")); response["success"] != true {
		t.Fatalf("fork response = %+v", response)
	}
	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, byResponseID("messages"))["data"].(map[string]any)["messages"].([]any)
	if len(messages) != 0 {
		t.Fatalf("fork before the first message retained the outgoing turn: %+v", messages)
	}
}

// D235 exposes a Go host with no replacement factory, unlike pi v1.4.0's
// modes/rpc/rpc-mode.ts runtime. Its explicit unavailable error must not mutate
// the current manager or create a branch before discovering the missing host.
func TestRPCUnavailableForkPreservesInMemorySession(t *testing.T) {
	for _, kind := range []string{"fork", "clone"} {
		t.Run(kind, func(t *testing.T) {
			session := rpcTestSession(t)
			firstID := session.Sessions.AppendMessage(createUserMessage("original"))
			session.Agent.SetMessages(session.Sessions.Projection().Messages)
			originalID := session.SessionID()
			command := `{"id":"change","type":"` + kind + `","entryId":"` + firstID + `"}`
			responses := rpcResponses(runRPCScript(t, session, command+"\n"+
				`{"id":"state","type":"get_state"}`+"\n"+
				`{"id":"entries","type":"get_entries"}`+"\n"))
			if len(responses) != 3 || responses[0]["success"] != false || responses[0]["error"] != "Session replacement is not available in this build" {
				t.Fatalf("replacement responses = %+v", responses)
			}
			state := responses[1]["data"].(map[string]any)
			if state["sessionId"] != originalID {
				t.Errorf("unavailable replacement changed session id: %v, want %s", state["sessionId"], originalID)
			}
			entries := responses[2]["data"].(map[string]any)["entries"].([]any)
			if len(entries) != 1 || entries[0].(map[string]any)["id"] != firstID {
				t.Errorf("unavailable replacement changed entries: %+v", entries)
			}
		})
	}
}

// D382: pi v1.5.0 (f9a26365) core/sdk.ts builds the agent with `tools: []` and
// core/agent-session.ts installs the loadout per prompt, so an untouched RPC
// session reports `messageCount: 0`, `get_messages` returns `[]`, and `compact`
// reaches "Nothing to compact (session too small)". Pier installs the tools at
// construction, which places the leading system declaration message, and treats
// the placeholder model as absent, so `compact` fails with "No model selected".
func TestRPCFreshSessionHoldsTheToolDeclaration(t *testing.T) {
	persist := false
	cwd := t.TempDir()
	session, err := NewAgentSession(&SessionConfig{
		Cwd: cwd, StreamFn: stubStreamFn, Tools: CreateCodingTools(cwd, nil),
		Sessions: NewSessionManager(t.TempDir(), &SessionManagerOptions{Persist: &persist}),
	})
	if err != nil {
		t.Fatal(err)
	}
	client := startRPCTest(t, session)
	client.send(t, `{"id":"state","type":"get_state"}`)
	state := client.until(t, func(record map[string]any) bool { return record["id"] == "state" })
	data := state["data"].(map[string]any)
	if data["messageCount"] != float64(1) {
		t.Errorf("messageCount = %v, want 1 (upstream 0)", data["messageCount"])
	}
	if _, ok := data["model"]; ok {
		t.Errorf("get_state sent the placeholder model: %v", data["model"])
	}

	client.send(t, `{"id":"messages","type":"get_messages"}`)
	messages := client.until(t, func(record map[string]any) bool { return record["id"] == "messages" })
	list := messages["data"].(map[string]any)["messages"].([]any)
	if len(list) != 1 {
		t.Fatalf("get_messages = %d messages, want the declaration only (upstream [])", len(list))
	}
	declaration := list[0].(map[string]any)
	if declaration["role"] != "system" || declaration["content"] != "" || len(declaration["toolsAdded"].([]any)) == 0 {
		t.Errorf("first message = %v, want the empty system tool declaration", declaration)
	}

	client.send(t, `{"id":"compact","type":"compact"}`)
	compact := client.until(t, func(record map[string]any) bool { return record["id"] == "compact" })
	if compact["success"] != false || !strings.Contains(compact["error"].(string), "No model selected") {
		t.Errorf("compact = %v, want No model selected (upstream: Nothing to compact)", compact)
	}
}

// D185: pier has no codemode or tool_search tool, so `codemode` and `deferred`
// MCP tools register but can never be called.
//
// Reference: pi v1.5.0 extensions/mcp/index.ts `ensureDiscoveryActive` warns
// from the config, before any server connects, when an enabled server's
// `configuredExposures` (its exposure, default codemode, plus its
// toolExposure values) include `codemode` or `deferred` and no tool reaches
// them. Pier's warning keeps that condition and names the servers and the fix.
func TestMcpUnreachableToolsWarningFollowsConfiguredExposures(t *testing.T) {
	load := func(t *testing.T, body string) LoadedMcpConfig {
		t.Helper()
		agentDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(agentDir, "mcp.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		config := LoadMcpConfig(McpConfigLoadOptions{AgentDir: agentDir, Cwd: t.TempDir()})
		if len(config.Errors) != 0 {
			t.Fatalf("config errors: %v", config.Errors)
		}
		return config
	}
	cases := []struct {
		name string
		body string
		want string // a fragment of the warning, or "" for none
	}{
		{"default exposure is codemode", `{"mcpServers":{"gopls":{"command":"gopls"}}}`, `MCP server "gopls" has tools`},
		{"explicit codemode", `{"mcpServers":{"a":{"command":"x","exposure":"codemode"}}}`, `MCP server "a" has`},
		{"deferred", `{"mcpServers":{"a":{"command":"x","exposure":"deferred"}}}`, `MCP server "a" has`},
		{"direct", `{"mcpServers":{"a":{"command":"x","exposure":"direct"}}}`, ""},
		{"hidden is intentional", `{"mcpServers":{"a":{"command":"x","exposure":"hidden"}}}`, ""},
		{"direct with one hidden tool", `{"mcpServers":{"a":{"command":"x","exposure":"direct","toolExposure":{"t":"hidden"}}}}`, ""},
		{"direct with one codemode tool", `{"mcpServers":{"a":{"command":"x","exposure":"direct","toolExposure":{"t":"codemode"}}}}`, `MCP server "a" has`},
		{"disabled server", `{"mcpServers":{"a":{"command":"x","enabled":false}}}`, ""},
		{"no servers", `{"mcpServers":{}}`, ""},
		{"servers listed in config order", `{"mcpServers":{"b":{"command":"x"},"ok":{"command":"x","exposure":"direct"},"a":{"command":"x","exposure":"deferred"}}}`, `MCP servers "b", "a" have tools`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := McpUnreachableToolsWarning(load(t, tc.body))
			if tc.want == "" {
				if got != "" {
					t.Fatalf("warning = %q, want none", got)
				}
				return
			}
			if !strings.HasPrefix(got, tc.want) || !strings.Contains(got, `"exposure": "direct"`) {
				t.Fatalf("warning = %q, want prefix %q and the direct-exposure fix", got, tc.want)
			}
		})
	}
}
