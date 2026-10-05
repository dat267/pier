package coding

import (
	"context"
	"sync"
	"time"

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
// (D185). Connections start without blocking the boot (NewMcpManagerAsync);
// the first prompt waits for the direct-tool servers, bounded, and late tools
// attach to the live session through the CLI wiring (D195).

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
	// OnToolsChange fires after every tool-set rebuild (the CLI wiring attaches
	// late tools to the live session with it).
	OnToolsChange func()
}

// McpManager is the per-session MCP server set.
type McpManager struct {
	mu          sync.Mutex
	connections []*McpServerConnection
	tools       []agent.AgentTool
	options     McpManagerOptions
	// onToolsChange fires after every tool-set rebuild (the CLI wiring uses it
	// to attach late tools to the live session).
	onToolsChange func()
}

// NewMcpManager connects every enabled server (in configuration order), waits
// for the connections to settle, and builds the direct tool set. Connections
// are attempted concurrently; a server that fails stays in the set with its
// failed state and error.
func NewMcpManager(ctx context.Context, options McpManagerOptions) *McpManager {
	manager, wait := newMcpManager(ctx, options)
	wait.Wait()
	manager.rebuildTools()
	return manager
}

// NewMcpManagerAsync starts every enabled connection (in configuration order)
// and returns before they settle: the CLI boots the UI while the connects run
// in the background, the first prompt waits for the direct-tool servers, and
// late tools attach live (upstream extensions/mcp: the connections start
// asynchronously and the ready promises are awaited, bounded, at
// before_agent_start).
func NewMcpManagerAsync(ctx context.Context, options McpManagerOptions) *McpManager {
	manager, _ := newMcpManager(ctx, options)
	manager.rebuildTools()
	return manager
}

// newMcpManager builds the manager and starts every connect, returning the
// WaitGroup the synchronous variant waits on.
func newMcpManager(ctx context.Context, options McpManagerOptions) (*McpManager, *sync.WaitGroup) {
	manager := &McpManager{options: options, onToolsChange: options.OnToolsChange}
	if manager.options.CreateTransport == nil {
		manager.options.CreateTransport = McpDefaultTransport
	}
	if manager.options.Credentials == nil {
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
	// The connections publish before any can settle, so both variants see the
	// full set immediately (the sync variant then waits them out).
	manager.connections = connections
	return manager, &wait
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

// McpStartupWaitMs bounds the first prompt's wait for the direct-tool servers
// (upstream DEFAULT_STARTUP_WAIT_MS = 10_000).
const McpStartupWaitMs = 10_000

// SetOnToolsChange installs (or clears) the tool-set-change callback.
func (m *McpManager) SetOnToolsChange(onToolsChange func()) {
	m.mu.Lock()
	m.onToolsChange = onToolsChange
	m.mu.Unlock()
}

// Errors reports the config errors plus every failed connection's message.
func (m *McpManager) Errors() []string {
	errors := append([]string{}, m.options.Config.Errors...)
	return append(errors, m.ConnectionErrors()...)
}

// ConnectionErrors reports only the failed connections' messages (the config
// errors surface at boot, before any connection can have settled).
func (m *McpManager) ConnectionErrors() []string {
	var errors []string
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
	changed := m.onToolsChange
	m.mu.Unlock()
	// Delivered outside the lock: the callback re-enters the manager via
	// DirectTools (the no-user-code-under-a-lock rule).
	if changed != nil {
		changed()
	}
}

// HasMcpDirectTools reports whether any of the entry's tools are declared to
// the model, so the first prompt waits for the connection (upstream
// hasDirectTools: the configured exposures contain "direct" — the server-level
// exposure or any toolExposure value, including a `*` pattern).
func HasMcpDirectTools(entry McpServerEntry) bool {
	if entry.Config == nil {
		return false
	}
	if entry.Config.Exposure != nil && *entry.Config.Exposure == McpExposureDirect {
		return true
	}
	for _, exposure := range entry.Config.ToolExposure {
		if exposure == McpExposureDirect {
			return true
		}
	}
	return false
}

// WaitForDirectTools waits until every connection whose configured exposures
// contain `direct` has settled (connected, failed, needs-auth or closed —
// upstream awaits the ready promises, which settle on connect or failure).
// It reports false when the timeout expires or ctx is canceled first; the
// still-connecting servers keep connecting in the background either way.
func (m *McpManager) WaitForDirectTools(ctx context.Context, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		pending := false
		for _, connection := range m.Connections() {
			if !HasMcpDirectTools(connection.Entry) {
				continue
			}
			connection.mu.Lock()
			state := connection.State
			connection.mu.Unlock()
			switch state {
			case McpServerConnecting, McpServerDisconnected:
				pending = true
			}
		}
		if !pending {
			return true
		}
		if ctx.Err() != nil {
			return false
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// mcpClientCaller adapts a connected mcp.Client to McpToolCaller.
type mcpClientCaller struct {
	client *mcp.Client
}

// CallTool runs one tool call on the client.
func (c mcpClientCaller) CallTool(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error) {
	return c.client.CallTool(ctx, name, args, options)
}
