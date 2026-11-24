package coding

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// Port of the "MCP connections" cases of upstream
// packages/coding-agent/test/mcp-extension.test.ts, plus the tool definition
// from createMcpToolDefinition.

func newMCPFakeServer(t *testing.T, noTools bool) (*mcpFakeServer, *mcp.InMemoryTransport) {
	t.Helper()
	client, server := mcp.CreateInMemoryTransportPair()
	fake := &mcpFakeServer{noTools: noTools, transport: server}
	server.OnMessage(func(message mcp.Message) {
		request, ok := message.(*protocol.JsonRpcRequest)
		if !ok {
			return
		}
		fake.mu.Lock()
		fake.methods = append(fake.methods, request.Method)
		fake.mu.Unlock()
		go func() {
			var result any
			var failure *protocol.McpError
			switch request.Method {
			case "initialize":
				capabilities := map[string]any{"tools": map[string]any{}}
				if fake.noTools {
					capabilities = map[string]any{"prompts": map[string]any{}}
				}
				if fake.hasResources {
					capabilities["resources"] = map[string]any{}
				}
				result = map[string]any{
					"protocolVersion": protocol.LatestProtocolVersion,
					"capabilities":    capabilities,
					"serverInfo":      map[string]any{"name": "fake", "version": "1.0.0"},
					"instructions":    fake.instructions,
				}
			case "tools/list":
				if fake.noTools {
					failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: "Method not found"}
				} else {
					result = map[string]any{"tools": []any{
						map[string]any{"name": "echo", "description": "Echo", "inputSchema": map[string]any{"type": "object"}},
					}}
				}
			case "tools/call":
				fake.mu.Lock()
				fake.calls++
				onCall := fake.onCall
				fake.mu.Unlock()
				if onCall != nil {
					onCall()
				}
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}
			default:
				failure = &protocol.McpError{Code: protocol.JSONRPCErrorCode.MethodNotFound, Msg: "Method not found"}
			}
			replyJSONRPC(server, request.ID, result, failure)
		}()
	})
	_ = server.Start(context.Background())
	return fake, client
}

func (t *closeTrackingTransport) Close(ctx context.Context) error {
	t.once.Do(func() { close(t.closed) })
	return t.Transport.Close(ctx)
}

func (t *failingTransport) Send(ctx context.Context, message mcp.Message) error {
	request, ok := message.(*protocol.JsonRpcRequest)
	if !ok {
		return t.Transport.Send(ctx, message)
	}
	if t.method != "" && request.Method != t.method {
		return t.Transport.Send(ctx, message)
	}
	t.mu.Lock()
	already := t.failed
	t.failed = true
	t.mu.Unlock()
	if !already {
		return t.err
	}
	return t.Transport.Send(ctx, message)
}

// TestCreateMcpToolDefinition covers the tool definition's name, label,
// description fallback, schema normalization, execution and error result.
func TestCreateMcpToolDefinition(t *testing.T) {
	description := "Echo"
	tool := protocol.Tool{Name: "echo", Description: &description, InputSchema: json.RawMessage(`{"properties":{"text":{"type":"string"}}}`)}
	var called map[string]any
	toolDefinition := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: tool, Name: "mcp__docs__echo", Exposure: McpExposureDirect, TimeoutMs: 1000,
		GetClient: func(context.Context) (McpToolCaller, error) {
			return mcpToolCallerFunc(func(_ context.Context, name string, args map[string]any, _ mcp.RequestOptions) (*protocol.CallToolResult, error) {
				called = args
				return &protocol.CallToolResult{Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "ok"}}}, nil
			}), nil
		},
	})
	if toolDefinition.Name != "mcp__docs__echo" || toolDefinition.Label != "docs/echo" {
		t.Fatalf("definition = %+v", toolDefinition)
	}
	if toolDefinition.Description != "Echo" {
		t.Fatalf("description = %q", toolDefinition.Description)
	}
	var parameters map[string]any
	if err := json.Unmarshal(toolDefinition.Parameters, &parameters); err != nil {
		t.Fatal(err)
	}
	if parameters["type"] != "object" {
		t.Fatalf("parameters = %v", parameters)
	}
	result, err := toolDefinition.Execute(context.Background(), "call-1", json.RawMessage(`{"text":"hi"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if called["text"] != "hi" {
		t.Fatalf("args = %v", called)
	}
	if len(result.Content) != 1 || result.Content[0].(ai.TextContent).Text != "ok" {
		t.Fatalf("result = %+v", result.Content)
	}

	// Annotations.title is the fallback when the top-level title is absent.
	annotationTitle := "Annotated tool"
	annotated := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: protocol.Tool{Name: "annotated", Annotations: &protocol.ToolAnnotations{Title: &annotationTitle}}, Name: "mcp__docs__annotated",
	})
	if annotated.Description != annotationTitle {
		t.Fatalf("annotated description = %q; want %q", annotated.Description, annotationTitle)
	}

	// A description without text falls back to the server/tool label.
	unnamed := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: protocol.Tool{Name: "mystery"}, Name: "mcp__docs__mystery",
		GetClient: func(context.Context) (McpToolCaller, error) { return nil, errors.New("unused") },
	})
	if unnamed.Description != "MCP tool mystery from server docs" {
		t.Fatalf("description = %q", unnamed.Description)
	}

	// An isError result keeps its converted content and details and flags the
	// result (upstream convertMcpResult returns them with isError: true), instead
	// of replacing both with an error string.
	erroring := CreateMcpToolDefinition(McpToolDefinitionOptions{
		Server: "docs", Tool: tool, Name: "mcp__docs__echo",
		GetClient: func(context.Context) (McpToolCaller, error) {
			return mcpToolCallerFunc(func(context.Context, string, map[string]any, mcp.RequestOptions) (*protocol.CallToolResult, error) {
				return &protocol.CallToolResult{
					Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "nope"}},
					IsError: true,
				}, nil
			}), nil
		},
	})
	errorResult, err := erroring.Execute(context.Background(), "call-2", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("err = %v; want the isError result, not a Go error", err)
	}
	if !errorResult.IsError {
		t.Fatal("result.IsError = false; want true")
	}
	if len(errorResult.Content) != 1 || errorResult.Content[0].(ai.TextContent).Text != "nope" {
		t.Fatalf("content = %+v", errorResult.Content)
	}
	if details := string(errorResult.Details); !strings.Contains(details, `"tool":"echo"`) || !strings.Contains(details, `"server":"docs"`) {
		t.Fatalf("details = %s; want the MCP tool details preserved", details)
	}
}

// mcpToolCallerFunc adapts a function to McpToolCaller.
type mcpToolCallerFunc func(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error)

// Upstream renders the progress notification with a JavaScript template string
// (`Progress ${progress.progress}${total}`, extensions/mcp/tools.ts), so the
// number is formatted with String(number): full precision, exponent notation
// below 1e-6 and above 1e21. Pier formatted with %f and an int64 fast path,
// which rounded fractional values and printed small or huge ones as 0 or a
// digit run.
func TestMcpToolProgressTextFormatsNumbersLikeJavaScript(t *testing.T) {
	cases := []struct {
		progress float64
		total    *float64
		want     string
	}{
		{progress: 3, want: "Progress 3"},
		{progress: 2.5, total: new(10.0), want: "Progress 2.5/10"},
		{progress: 1.2345678, want: "Progress 1.2345678"},
		{progress: 1e-7, want: "Progress 1e-7"},
		{progress: 0.000001, want: "Progress 0.000001"},
		{progress: 1e21, want: "Progress 1e+21"},
		{progress: 1234567890123456789, want: "Progress 1234567890123456800"},
		{progress: 0, total: new(0.0), want: "Progress 0/0"},
	}
	for _, testCase := range cases {
		progress, total := testCase.progress, testCase.total
		toolDefinition := CreateMcpToolDefinition(McpToolDefinitionOptions{
			Server: "docs", Tool: protocol.Tool{Name: "slow"}, Name: "mcp__docs__slow",
			GetClient: func(context.Context) (McpToolCaller, error) {
				return mcpToolCallerFunc(func(_ context.Context, _ string, _ map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error) {
					if options.OnProgress != nil {
						options.OnProgress(&protocol.ProgressNotification{Progress: progress, Total: total})
					}
					return &protocol.CallToolResult{Content: []protocol.ContentBlock{{Type: protocol.ContentTypeText, Text: "ok"}}}, nil
				}), nil
			},
		})
		var updates []string
		if _, err := toolDefinition.Execute(context.Background(), "call-1", nil, func(result agent.AgentToolResult) {
			for _, content := range result.Content {
				if text, ok := content.(ai.TextContent); ok {
					updates = append(updates, text.Text)
				}
			}
		}); err != nil {
			t.Fatalf("%v: %v", testCase.want, err)
		}
		if len(updates) != 1 || updates[0] != testCase.want {
			t.Fatalf("progress %v: updates = %v, want %q", testCase.progress, updates, testCase.want)
		}
	}
}

// pi v1.4.0 (4a080252), extensions/mcp/runtime.ts connectOnce registers
// refreshTools without an opening-request signal. A completed connect must not
// cancel later notifications/tools/list_changed discovery on its live client.
func TestMcpToolRefreshSurvivesInitialConnectCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, transport := newMCPFakeServer(t, false)
		defer server.transport.Close(context.Background())
		published := make(chan struct{}, 2)
		connection := NewMcpServerConnection(McpServerConnectionOptions{
			Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
			CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
				return transport, nil
			},
			OnTools: func(*McpServerConnection) { published <- struct{}{} },
		})
		defer connection.Close(context.Background())
		if _, err := connection.GetClient(context.Background()); err != nil {
			t.Fatal(err)
		}
		<-published // Initial tool discovery has been published.
		if err := server.transport.Send(context.Background(), &protocol.JsonRpcNotification{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "notifications/tools/list_changed",
		}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-published:
		default:
			connection.mu.Lock()
			err := connection.Error
			connection.mu.Unlock()
			t.Fatalf("live connection did not publish refreshed tools: %s", err)
		}
	})
}

// pi v1.4.0 (4a080252), extensions/mcp/runtime.ts starts refreshTools with
// `void`: awaiting tools/list inside the notification handler would prevent
// that same transport reader from delivering the response. Hold initial
// publication open so its context is still live, isolating reader reentrancy
// from the separate post-connect context-lifetime regression.
func TestMcpToolRefreshDoesNotBlockNotificationDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server, transport := newMCPFakeServer(t, false)
		defer server.transport.Close(context.Background())
		initial := make(chan struct{})
		releaseInitial := make(chan struct{})
		refreshed := make(chan struct{}, 1)
		var publications atomic.Int32
		connection := NewMcpServerConnection(McpServerConnectionOptions{
			Entry: McpServerEntry{Name: "fake", Config: &McpServerConfig{Command: "unused"}},
			CreateTransport: func(McpServerEntry, string, mcp.AuthProvider) (mcp.Transport, error) {
				return transport, nil
			},
			OnTools: func(*McpServerConnection) {
				if publications.Add(1) == 1 {
					close(initial)
					<-releaseInitial
				} else {
					refreshed <- struct{}{}
				}
			},
		})
		defer connection.Close(context.Background())
		defer close(releaseInitial)
		go func() { _, _ = connection.GetClient(context.Background()) }()
		<-initial
		if err := server.transport.Send(context.Background(), &protocol.JsonRpcNotification{
			JSONRPC: protocol.JSONRPCVersion,
			Method:  "notifications/tools/list_changed",
		}); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		select {
		case <-refreshed:
		default:
			t.Fatal("tool refresh blocked the reader that must deliver its response")
		}
	})
}

// pi v1.4.0 coding-agent/extensions/mcp/runtime.ts converts timeout seconds to
// milliseconds; mcp/client.ts keeps positive deadlines and the timer clamps
// sub-millisecond or oversized delays rather than disabling them.
func TestMcpServerTimeoutMsClampsConfiguredSeconds(t *testing.T) {
	for _, test := range []struct {
		name    string
		seconds float64
		want    int64
	}{
		{name: "sub-millisecond", seconds: 0.000001, want: 1},
		{name: "ordinary fractional", seconds: 1.5, want: 1500},
		{name: "above int64 range", seconds: 1e16, want: math.MaxInt64},
		{name: "non-finite milliseconds from finite seconds", seconds: 1e308, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			connection := NewMcpServerConnection(McpServerConnectionOptions{Entry: McpServerEntry{
				Name: "test",
				Config: &McpServerConfig{
					TimeoutSeconds: &test.seconds,
				},
			}})
			if got := connection.TimeoutMs(); got != test.want {
				t.Fatalf("TimeoutMs() = %d, want %d", got, test.want)
			}
		})
	}
}
