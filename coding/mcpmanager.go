package coding

import (
	"context"
	"strings"
	"sync"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/protocol"
)

// McpManager owns the configured MCP servers for one session: it connects the
// enabled ones, exposes their `direct` tools as agent tools, and closes every
// connection.
//
// Upstream's extension runtime manages the same connections and additionally
// gives tools to codemode and to `tool_search`; the port has neither, so
// `codemode`, `deferred` and `hidden` tools are registered but not exposed
// (D185). A tool list change refreshes DirectTools; a running session keeps the
// tools it was created with, so new tools appear after a session reload.

// McpManagerOptions are McpManager inputs.
type McpManagerOptions struct {
	// Config is the merged mcp.json configuration.
	Config LoadedMcpConfig
	Cwd    string
	// CreateTransport builds a server's transport (default: McpDefaultTransport).
	CreateTransport McpTransportFactory
	Credentials     McpOAuthCredentialStore
	// ProviderToken returns the token of a `/login` provider.
	ProviderToken func(ctx context.Context, provider string) (string, error)
}

// McpManager is the per-session MCP server set.
type McpManager struct {
	mu          sync.Mutex
	connections []*McpServerConnection
	tools       []agent.AgentTool
	options     McpManagerOptions
}

// NewMcpManager connects every enabled server (in configuration order) and
// builds the direct tool set. Connections are attempted concurrently; a server
// that fails stays in the set with its failed state and error.
func NewMcpManager(ctx context.Context, options McpManagerOptions) *McpManager {
	manager := &McpManager{options: options}
	if options.CreateTransport == nil {
		manager.options.CreateTransport = McpDefaultTransport
	}
	if options.Credentials == nil {
		manager.options.Credentials = NewMemoryMcpOAuthCredentialStore()
	}
	entries := make([]McpServerEntry, 0, len(options.Config.Servers))
	for _, entry := range options.Config.Servers {
		if entry.Config.Enabled != nil && !*entry.Config.Enabled {
			continue
		}
		entries = append(entries, entry)
	}
	connections := make([]*McpServerConnection, len(entries))
	var wait sync.WaitGroup
	for index, entry := range entries {
		entry := entry
		index := index
		connection := NewMcpServerConnection(McpServerConnectionOptions{
			Entry:           entry,
			Cwd:             options.Cwd,
			CreateTransport: manager.options.CreateTransport,
			Credentials:     manager.options.Credentials,
			ProviderToken:   options.ProviderToken,
			OnTools:         func(*McpServerConnection) { manager.rebuildTools() },
		})
		connections[index] = connection
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, _ = connection.GetClient(ctx)
		}()
	}
	wait.Wait()
	manager.connections = connections
	manager.rebuildTools()
	return manager
}

// Connections are the configured connections, in mcp.json order.
func (m *McpManager) Connections() []*McpServerConnection {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*McpServerConnection{}, m.connections...)
}

// DirectTools are the tools exposed to the model (exposure `direct`).
func (m *McpManager) DirectTools() []agent.AgentTool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]agent.AgentTool{}, m.tools...)
}

// Errors reports the config errors plus every failed connection's message.
func (m *McpManager) Errors() []string {
	errors := append([]string{}, m.options.Config.Errors...)
	for _, connection := range m.Connections() {
		if connection.State == McpServerFailed && connection.Error != "" {
			errors = append(errors, "MCP server \""+connection.Name()+"\" failed to connect: "+connection.Error)
		}
	}
	return errors
}

// Close stops every connection.
func (m *McpManager) Close(ctx context.Context) error {
	for _, connection := range m.Connections() {
		_ = connection.Close(ctx)
	}
	return nil
}

// rebuildTools recomputes the direct tool set from the current tool lists
// (upstream registers a definition per tool and exposes the direct ones).
func (m *McpManager) rebuildTools() {
	m.mu.Lock()
	connections := append([]*McpServerConnection{}, m.connections...)
	m.mu.Unlock()
	taken := map[string]bool{}
	tools := []agent.AgentTool{}
	for _, connection := range connections {
		if connection.State != McpServerConnected {
			continue
		}
		name := connection.Name()
		config := connection.Entry.Config
		connection.mu.Lock()
		serverTools := append([]protocol.Tool{}, connection.Tools...)
		timeoutMs := connection.TimeoutMs()
		connection.mu.Unlock()
		for _, tool := range serverTools {
			toolName := CreateMcpToolName(name, tool.Name, func(candidate string) bool { return taken[candidate] })
			taken[toolName] = true
			if McpToolExposure(config, tool.Name) != McpExposureDirect {
				continue
			}
			connection := connection
			tool := tool
			tools = append(tools, CreateMcpToolDefinition(McpToolDefinitionOptions{
				Server:    name,
				Tool:      tool,
				Name:      toolName,
				TimeoutMs: timeoutMs,
				GetClient: func(ctx context.Context) (McpToolCaller, error) {
					client, err := connection.GetClient(ctx)
					if err != nil {
						return nil, err
					}
					return mcpClientCaller{client: client}, nil
				},
				ReadableResources: func() bool {
					connection.mu.Lock()
					defer connection.mu.Unlock()
					return connection.HasResources
				},
			}))
		}
	}
	m.mu.Lock()
	m.tools = tools
	m.mu.Unlock()
}

// mcpClientCaller adapts a connected mcp.Client to McpToolCaller.
type mcpClientCaller struct {
	client *mcp.Client
}

// CallTool runs one tool call on the client.
func (c mcpClientCaller) CallTool(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error) {
	return c.client.CallTool(ctx, name, args, options)
}

// McpNamespaceLabel renders a server's tool namespace for status output.
func McpNamespaceLabel(server string) string {
	return strings.TrimPrefix(McpNamespace(server), "mcp__")
}
