package coding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/agent"
	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp"
	"github.com/dat267/pier/mcp/oauth"
	"github.com/dat267/pier/mcp/protocol"
)

// Port of extensions/mcp/runtime.ts: the part that talks to servers
// (connections, transports, OAuth sign-in) plus createMcpToolDefinition from
// tools.ts.
//
// Divergences (D185): upstream reads a provider token and persists OAuth
// credentials in auth.json; the port keeps OAuth credentials in a per-process
// store (a later `/mcp` sign-in can add file backing), the tool definition
// carries no renderers (the interactive layer's generic fallback draws MCP
// calls) and no output schema/annotations, and an `isError` result is returned
// as a Go error since agent.AgentToolResult has no error flag.

// McpDefaultTimeoutSeconds is the per-request timeout (upstream
// DEFAULT_TIMEOUT_SECONDS).
const McpDefaultTimeoutSeconds = 60

// mcpStderrTailChars bounds the stderr a failed stdio connect reports.
const mcpStderrTailChars = 2000

// mcpConnectRetryDelaysMs are the delays between HTTP connect attempts.
var mcpConnectRetryDelaysMs = []int64{250, 1000}

// McpServerState is a connection's lifecycle state (upstream ServerState).
type McpServerState string

// The connection states.
const (
	McpServerConnecting   McpServerState = "connecting"
	McpServerConnected    McpServerState = "connected"
	McpServerDisconnected McpServerState = "disconnected"
	McpServerNeedsAuth    McpServerState = "needs-auth"
	McpServerFailed       McpServerState = "failed"
	McpServerClosed       McpServerState = "closed"
)

// McpTransportFactory builds a transport for one entry (upstream
// McpTransportFactory).
type McpTransportFactory func(entry McpServerEntry, cwd string, authProvider mcp.AuthProvider) (mcp.Transport, error)

// McpOAuthCredentialStore hands out the OAuth state store of one server URL
// (upstream McpOAuthCredentialStore).
type McpOAuthCredentialStore interface {
	ForServer(name string, url string) oauth.McpOAuthStateStore
}

// MemoryMcpOAuthCredentialStore keeps credentials for one process. Upstream
// persists them in auth.json; a later `/mcp` sign-in can add that backing.
type MemoryMcpOAuthCredentialStore struct {
	mu     sync.Mutex
	stores map[string]*oauth.MemoryOAuthStateStore
}

// NewMemoryMcpOAuthCredentialStore creates an empty store.
func NewMemoryMcpOAuthCredentialStore() *MemoryMcpOAuthCredentialStore {
	return &MemoryMcpOAuthCredentialStore{stores: map[string]*oauth.MemoryOAuthStateStore{}}
}

// ForServer returns the store for one server URL.
func (s *MemoryMcpOAuthCredentialStore) ForServer(name string, serverURL string) oauth.McpOAuthStateStore {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := name + "\x00" + serverURL
	store, ok := s.stores[key]
	if !ok {
		store = &oauth.MemoryOAuthStateStore{}
		s.stores[key] = store
	}
	return store
}

// McpServerConnectionOptions are McpServerConnection inputs.
type McpServerConnectionOptions struct {
	Entry           McpServerEntry
	Cwd             string
	CreateTransport McpTransportFactory
	Credentials     McpOAuthCredentialStore
	// ProviderToken returns the current token of a `/login` provider, for
	// servers with auth.provider.
	ProviderToken func(ctx context.Context, provider string) (string, error)
	// OnTools is called when the tool list changed.
	OnTools func(connection *McpServerConnection)
	// OnChange is called when state, error, or tools changed.
	OnChange func(connection *McpServerConnection)
}

// McpServerConnection is one configured server (upstream McpServerConnection).
// It reconnects lazily when a call finds the connection gone.
type McpServerConnection struct {
	mu sync.Mutex

	Entry        McpServerEntry
	State        McpServerState
	Error        string
	Tools        []protocol.Tool
	HasResources bool
	Resources    []protocol.Resource
	// ResourceTemplates are the server's resource templates.
	ResourceTemplates []protocol.ResourceTemplate
	// Instructions is the server's initialize instructions.
	Instructions *string
	// Challenge is the last OAuth challenge, for sign-in.
	Challenge *oauth.OAuthChallenge

	client          *mcp.Client
	opening         chan struct{}
	openingCancel   context.CancelFunc
	openingClient   *mcp.Client
	openingErr      error
	closed          bool
	stderrTail      string
	cwd             string
	createTransport McpTransportFactory
	credentials     McpOAuthCredentialStore
	providerToken   func(ctx context.Context, provider string) (string, error)
	onTools         func(*McpServerConnection)
	onChange        func(*McpServerConnection)
	authProvider    *oauth.McpOAuthProvider
}

// NewMcpServerConnection builds a connection (upstream constructor).
func NewMcpServerConnection(options McpServerConnectionOptions) *McpServerConnection {
	connection := &McpServerConnection{
		Entry:           options.Entry,
		State:           McpServerConnecting,
		cwd:             options.Cwd,
		createTransport: options.CreateTransport,
		credentials:     options.Credentials,
		providerToken:   options.ProviderToken,
		onTools:         options.OnTools,
		onChange:        options.OnChange,
	}
	if url := connection.oauthURL(); url != "" {
		options := connection.oauthSettings()
		connection.authProvider = oauth.NewMcpOAuthProvider(oauth.McpOAuthProviderOptions{
			ServerURL:      url,
			ClientMetadata: oauth.OAuthClientMetadata{ClientName: mcpOptionalString(options.ClientName)},
			ClientID:       options.ClientID,
			ClientSecret:   options.ClientSecret,
			Store:          connection.credentials.ForServer(connection.Entry.Name, url),
			OnRedirect: func(context.Context, string) error {
				// Sign-in is driven by the caller (`/mcp` in a later slice); a
				// redirect without one keeps the challenge for it.
				return nil
			},
		})
		// The OAuth challenge (populated by the sign-in flow, a later slice)
		// stays nil here; the config and transport layers surface the 401 as an
		// McpAuthRequiredError.
	}
	return connection
}

// Name is the server name.
func (c *McpServerConnection) Name() string { return c.Entry.Name }

// TimeoutMs is the request timeout in milliseconds.
func (c *McpServerConnection) TimeoutMs() int64 {
	if c.Entry.Config.TimeoutSeconds != nil {
		return int64(*c.Entry.Config.TimeoutSeconds * 1000)
	}
	return McpDefaultTimeoutSeconds * 1000
}

// oauthURL is the server URL when the server authenticates with OAuth: HTTP
// without an Authorization header and without provider auth (upstream
// usesOAuth).
func (c *McpServerConnection) oauthURL() string {
	if !c.Entry.Config.IsHTTP() || c.Entry.Config.Auth != nil {
		return ""
	}
	for header := range c.Entry.Config.Headers {
		if strings.EqualFold(header, "authorization") {
			return ""
		}
	}
	return c.Entry.Config.URL
}

// oauthSettings maps the config's oauth section (upstream oauthSettings).
func (c *McpServerConnection) oauthSettings() McpOAuthSettings {
	oauthConfig := c.Entry.Config.OAuth
	if oauthConfig == nil {
		return McpOAuthSettings{}
	}
	settings := McpOAuthSettings{}
	if oauthConfig.ClientID != nil {
		settings.ClientID = *oauthConfig.ClientID
	}
	if oauthConfig.ClientSecret != nil {
		settings.ClientSecret = resolveMcpConfigValue(*oauthConfig.ClientSecret, "MCP server \""+c.Entry.Name+"\" oauth.clientSecret")
	}
	if oauthConfig.CallbackPort != nil {
		settings.CallbackPort = oauthConfig.CallbackPort
	}
	if oauthConfig.CallbackURL != nil {
		settings.CallbackURL = oauthConfig.CallbackURL
	}
	if oauthConfig.Scope != nil {
		settings.Scope = oauthConfig.Scope
	}
	if oauthConfig.ClientName != nil {
		settings.ClientName = *oauthConfig.ClientName
	}
	if oauthConfig.AuthServerMetadataURL != nil {
		if parsed, err := url.Parse(*oauthConfig.AuthServerMetadataURL); err == nil {
			settings.AuthServerMetadataURL = parsed
		}
	}
	return settings
}

// mcpAuthProvider adapts the oauth provider to the transport's AuthProvider, or
// reports a provider-token auth for `auth.provider` (upstream's constructor
// branch).
func (c *McpServerConnection) mcpAuthProvider() mcp.AuthProvider {
	if c.Entry.Config.IsHTTP() && c.Entry.Config.Auth != nil {
		provider := c.Entry.Config.Auth.Provider
		return providerTokenAuthProvider{token: func(ctx context.Context) (string, error) {
			if c.providerToken == nil {
				return "", nil
			}
			return c.providerToken(ctx, provider)
		}}
	}
	if c.authProvider != nil {
		return mcp.AdaptOAuthProvider(c.authProvider)
	}
	return nil
}

// providerTokenAuthProvider serves the token of a `/login` provider.
type providerTokenAuthProvider struct {
	token func(ctx context.Context) (string, error)
}

func (p providerTokenAuthProvider) Token(ctx context.Context) (string, error) { return p.token(ctx) }
func (p providerTokenAuthProvider) OnUnauthorized(context.Context, mcp.UnauthorizedInfo) error {
	return nil
}

// GetClient returns the connected client, connecting when needed (upstream
// getClient).
func (c *McpServerConnection) GetClient(ctx context.Context) (*mcp.Client, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("MCP server %q is shut down", c.Entry.Name)
	}
	if c.client != nil && c.client.ConnectionState() == mcp.StateConnected {
		client := c.client
		c.mu.Unlock()
		return client, nil
	}
	if c.opening != nil {
		done := c.opening
		c.mu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		c.mu.Lock()
		client, err := c.client, c.openingErr
		c.mu.Unlock()
		if client != nil {
			return client, nil
		}
		return nil, err
	}
	done := make(chan struct{})
	connectCtx, cancel := context.WithCancel(ctx)
	c.opening = done
	c.openingCancel = cancel
	c.mu.Unlock()

	client, err := c.open(connectCtx)

	c.mu.Lock()
	if c.closed && err == nil {
		client = nil
		err = fmt.Errorf("MCP server %q is shut down", c.Entry.Name)
	}
	c.opening = nil
	c.openingCancel = nil
	c.openingClient = nil
	c.openingErr = err
	c.mu.Unlock()
	cancel()
	close(done)
	return client, err
}

// CallTool runs a tool call, reconnecting when needed (upstream callTool).
func (c *McpServerConnection) CallTool(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error) {
	return c.withClient(ctx, func(client *mcp.Client) (*protocol.CallToolResult, error) {
		return client.CallTool(ctx, name, args, options)
	}, false)
}

// withClient runs a request, reconnecting on a dropped or expired session
// (upstream withClient).
func (c *McpServerConnection) withClient(
	ctx context.Context,
	run func(client *mcp.Client) (*protocol.CallToolResult, error),
	readOnly bool,
) (*protocol.CallToolResult, error) {
	for attempt := 1; ; attempt++ {
		client, err := c.GetClient(ctx)
		if err != nil {
			return nil, err
		}
		result, err := run(client)
		if err == nil {
			return result, nil
		}
		var httpError *mcp.McpHttpError
		if readOnly && attempt == 1 && errors.As(err, &httpError) && isMcpTransientError(err) {
			select {
			case <-time.After(time.Duration(mcpConnectRetryDelaysMs[0]) * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		var expired *mcp.McpSessionExpiredError
		if attempt == 1 && errors.As(err, &expired) {
			// The server no longer knows the session (restart, deploy), so it
			// did not run the request. Retry once on a new session.
			c.mu.Lock()
			if c.client == client {
				c.client = nil
			}
			c.mu.Unlock()
			continue
		}
		if !c.needsSignIn(err) {
			return nil, err
		}
		c.dropClient(ctx, client)
		c.markNeedsAuth()
		return nil, errors.New(c.signInRequiredMessage())
	}
}

// needsSignIn reports whether the error requires a sign-in (upstream
// needsSignIn).
func (c *McpServerConnection) needsSignIn(err error) bool {
	var authorizationRequired *oauth.McpOAuthAuthorizationRequiredError
	if errors.As(err, &authorizationRequired) {
		return true
	}
	var authRequired *mcp.McpAuthRequiredError
	return c.oAuthProviderActive() && errors.As(err, &authRequired)
}

func (c *McpServerConnection) oAuthProviderActive() bool { return c.authProvider != nil }

// signInRequiredMessage is the user-facing sign-in hint (upstream
// signInRequiredMessage).
func (c *McpServerConnection) signInRequiredMessage() string {
	provider := ""
	if c.Entry.Config.Auth != nil {
		provider = c.Entry.Config.Auth.Provider
	}
	if provider != "" {
		return fmt.Sprintf("MCP server %q requires sign-in. Run /login %s to sign in.", c.Entry.Name, provider)
	}
	return fmt.Sprintf("MCP server %q requires sign-in. Run /mcp to sign in.", c.Entry.Name)
}

// markNeedsAuth moves the connection into the sign-in state.
func (c *McpServerConnection) markNeedsAuth() {
	c.mu.Lock()
	c.State = McpServerNeedsAuth
	c.Error = ""
	c.mu.Unlock()
	c.changed()
}

func (c *McpServerConnection) changed() {
	if c.onChange != nil {
		c.onChange(c)
	}
}

func (c *McpServerConnection) dropClient(ctx context.Context, client *mcp.Client) {
	c.mu.Lock()
	if c.client == client {
		c.client = nil
	}
	c.mu.Unlock()
	_ = client.Close(ctx)
}

// mcpOpenResult carries the client and the error of one open.
// open connects with retries for transient HTTP failures (upstream open).
func (c *McpServerConnection) open(ctx context.Context) (*mcp.Client, error) {
	c.mu.Lock()
	c.State = McpServerConnecting
	c.mu.Unlock()
	c.changed()

	retries := []int64(nil)
	if c.Entry.Config.IsHTTP() {
		retries = mcpConnectRetryDelaysMs
	}
	for attempt := 0; ; attempt++ {
		c.mu.Lock()
		c.stderrTail = ""
		c.mu.Unlock()
		client, err := c.connectOnce(ctx)
		if err == nil {
			return client, nil
		}
		if c.isClosed() || attempt >= len(retries) || !isMcpTransientError(err) {
			return nil, c.connectFailed(err)
		}
		select {
		case <-time.After(time.Duration(retries[attempt]) * time.Millisecond):
		case <-ctx.Done():
			return nil, c.connectFailed(ctx.Err())
		}
		if c.isClosed() {
			return nil, c.connectFailed(err)
		}
	}
}

// connectOnce builds the client, connects and lists the server's tools and
// resources (upstream connectOnce).
func (c *McpServerConnection) connectOnce(ctx context.Context) (*mcp.Client, error) {
	roots := []protocol.Root{}
	if c.cwd != "" {
		name := filepath.Base(c.cwd)
		roots = append(roots, protocol.Root{URI: "file://" + c.cwd, Name: &name})
	}
	client := mcp.NewClient(mcp.ClientOptions{
		Name:             "pi",
		Version:          Version,
		RequestTimeoutMs: c.TimeoutMs(),
		Roots:            roots,
	})
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, errors.New("shut down while connecting")
	}
	c.openingClient = client
	c.mu.Unlock()
	if c.createTransport == nil {
		return nil, errors.New("no transport factory")
	}
	transport, err := c.createTransport(c.Entry, c.cwd, c.mcpAuthProvider())
	if err != nil {
		return nil, err
	}
	if c.isClosed() || ctx.Err() != nil {
		_ = transport.Close(context.Background())
		return nil, errors.New("shut down while connecting")
	}
	if _, err := client.Connect(ctx, transport); err != nil {
		_ = client.Close(ctx)
		if tail := mcpStderrTail(transport); tail != "" {
			c.mu.Lock()
			c.stderrTail = tail
			c.mu.Unlock()
		}
		return nil, err
	}
	client.OnNotification("notifications/tools/list_changed", func(json.RawMessage) {
		c.refreshTools(ctx, client)
	})
	client.OnNotification("notifications/resources/list_changed", func(json.RawMessage) {
		c.refreshResources(ctx, client)
	})
	client.OnClose(func() {
		c.handleClientClose(client, transport)
	})

	capabilities := client.ServerCapabilities()
	hasResources := capabilities != nil && capabilities.Resources != nil
	tools := []protocol.Tool{}
	if capabilities != nil && capabilities.Tools != nil {
		listed, err := client.ListTools(ctx, mcp.RequestOptions{})
		if err != nil {
			_ = client.Close(ctx)
			return nil, err
		}
		tools = listed
	}
	resources, templates := []protocol.Resource{}, []protocol.ResourceTemplate{}
	if hasResources {
		resources, templates = fetchMcpResources(ctx, client)
	}
	if c.isClosed() {
		_ = client.Close(ctx)
		return nil, errors.New("shut down while connecting")
	}
	if client.ConnectionState() != mcp.StateConnected {
		_ = client.Close(ctx)
		return nil, errors.New("connection closed during setup")
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = client.Close(ctx)
		return nil, errors.New("shut down while connecting")
	}
	c.client = client
	c.Tools = tools
	c.HasResources = hasResources
	c.Resources = resources
	c.ResourceTemplates = templates
	instructions := client.Instructions()
	if instructions != nil {
		trimmed := strings.TrimSpace(*instructions)
		if trimmed == "" {
			instructions = nil
		} else {
			instructions = &trimmed
		}
	}
	c.Instructions = instructions
	c.State = McpServerConnected
	c.Error = ""
	c.mu.Unlock()
	if c.onTools != nil {
		c.onTools(c)
	}
	c.changed()
	return client, nil
}

// fetchMcpResources lists resources and templates, tolerating servers that do
// not implement them (upstream fetchResources).
func fetchMcpResources(ctx context.Context, client *mcp.Client) ([]protocol.Resource, []protocol.ResourceTemplate) {
	resources := []protocol.Resource{}
	if listed, err := client.ListResources(ctx, mcp.RequestOptions{}); err == nil {
		resources = listed
	}
	templates := []protocol.ResourceTemplate{}
	if listed, err := client.ListResourceTemplates(ctx, mcp.RequestOptions{}); err == nil {
		templates = listed
	}
	return resources, templates
}

// connectFailed records the failure and returns the user-facing error
// (upstream connectFailed).
func (c *McpServerConnection) connectFailed(err error) error {
	if c.needsSignIn(err) && !c.isClosed() {
		c.markNeedsAuth()
		return errors.New(c.signInRequiredMessage())
	}
	c.mu.Lock()
	if c.closed {
		c.State = McpServerClosed
	} else {
		c.State = McpServerFailed
	}
	message := err.Error()
	if c.stderrTail != "" {
		message = message + "\n" + c.stderrTail
	}
	c.Error = message
	c.mu.Unlock()
	c.changed()
	return fmt.Errorf("MCP server %q failed to connect: %s", c.Entry.Name, message)
}

// handleClientClose records a dropped connection; the next call reconnects
// (upstream handleClientClose).
func (c *McpServerConnection) handleClientClose(client *mcp.Client, transport mcp.Transport) {
	c.mu.Lock()
	if c.client != client || c.closed {
		c.mu.Unlock()
		return
	}
	c.client = nil
	c.State = McpServerDisconnected
	message := "Connection closed"
	if tail := mcpStderrTail(transport); tail != "" {
		message = "Connection closed\n" + tail
	}
	c.Error = message
	c.mu.Unlock()
	c.changed()
}

// refreshTools re-lists the tools after a list_changed notification (upstream
// refreshTools).
func (c *McpServerConnection) refreshTools(ctx context.Context, client *mcp.Client) {
	tools, err := client.ListTools(ctx, mcp.RequestOptions{})
	c.mu.Lock()
	if c.client != client || c.closed {
		c.mu.Unlock()
		return
	}
	if err != nil {
		c.Error = "Failed to refresh tools: " + err.Error()
	} else {
		c.Tools = tools
	}
	c.mu.Unlock()
	if err == nil && c.onTools != nil {
		c.onTools(c)
	}
	c.changed()
}

// refreshResources re-lists the resources after a list_changed notification.
func (c *McpServerConnection) refreshResources(ctx context.Context, client *mcp.Client) {
	resources, templates := fetchMcpResources(ctx, client)
	c.mu.Lock()
	if c.client != client || c.closed {
		c.mu.Unlock()
		return
	}
	c.Resources = resources
	c.ResourceTemplates = templates
	c.mu.Unlock()
	if c.onTools != nil {
		c.onTools(c)
	}
	c.changed()
}

// Close stops the connection (upstream close).
func (c *McpServerConnection) Close(ctx context.Context) error {
	c.mu.Lock()
	c.closed = true
	c.State = McpServerClosed
	client := c.client
	openingClient := c.openingClient
	opening := c.opening
	cancelOpening := c.openingCancel
	c.client = nil
	c.mu.Unlock()
	c.changed()
	if cancelOpening != nil {
		cancelOpening()
	}
	if client != nil {
		_ = client.Close(ctx)
	}
	if openingClient != nil && openingClient != client {
		_ = openingClient.Close(ctx)
	}
	if opening != nil {
		<-opening
	}
	return nil
}

func (c *McpServerConnection) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// isMcpTransientError reports network failures and overloaded or restarting
// servers (upstream isTransientError).
func isMcpTransientError(err error) bool {
	var httpError *mcp.McpHttpError
	if errors.As(err, &httpError) {
		status := httpError.Status
		return status == 408 || status == 429 || (status >= 500 && status != 501)
	}
	// Go reports network failures as *url.Error or net.Error; upstream checks
	// for a fetch TypeError (D183).
	var urlError *url.Error
	if errors.As(err, &urlError) {
		return true
	}
	var networkError interface{ Timeout() bool }
	return errors.As(err, &networkError)
}

// mcpStderrTail extracts the stderr tail of a stdio transport.
func mcpStderrTail(transport mcp.Transport) string {
	stdio, ok := transport.(*mcp.StdioTransport)
	if !ok {
		return ""
	}
	tail := strings.TrimSpace(stdio.Stderr())
	if len(tail) > mcpStderrTailChars {
		tail = tail[len(tail)-mcpStderrTailChars:]
	}
	return tail
}

// McpOAuthSettings are the settings a sign-in uses (upstream McpOAuthSettings).
type McpOAuthSettings struct {
	ClientID              string
	ClientSecret          string
	CallbackPort          *int
	CallbackURL           *string
	Scope                 *string
	ClientName            string
	AuthServerMetadataURL *url.URL
}

// resolveMcpConfigValue expands a config value: `${NAME}` reads an environment
// variable, `!cmd` runs a command (upstream resolveConfigValue).
func resolveMcpConfigValue(value string, what string) string {
	resolved, err := ResolveMcpConfigValue(value)
	if err != nil {
		return ""
	}
	_ = what
	return resolved
}

// ResolveMcpConfigValue expands `${NAME}` (environment) and `!command`
// (command substitution) values, like a shell (upstream
// resolveConfigValueOrThrow).
func ResolveMcpConfigValue(value string) (string, error) {
	return resolveConfigValueOrThrow(value)
}

// runMcpConfigCommand runs a `!command` config value through the platform
// shell (upstream execSync(cmd, { shell: true })).
func runMcpConfigCommand(command string) (string, error) {
	if command == "" {
		return "", nil
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", command)
	} else {
		cmd = exec.Command("sh", "-c", command)
	}
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

// McpConfigValueEnv is the environment lookup (a test seam).
var McpConfigValueEnv = os.Getenv

// resolveConfigValueOrThrow expands one config value.
func resolveConfigValueOrThrow(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if strings.HasPrefix(trimmed, "!") {
		output, err := runMcpConfigCommand(strings.TrimSpace(trimmed[1:]))
		if err != nil {
			return "", err
		}
		return strings.TrimRight(output, "\r\n"), nil
	}
	return expandMcpEnv(trimmed)
}

// expandMcpEnv substitutes `${NAME}` with the environment value.
func expandMcpEnv(value string) (string, error) {
	var builder strings.Builder
	for index := 0; index < len(value); {
		if value[index] == '$' && index+1 < len(value) && value[index+1] == '{' {
			end := strings.IndexByte(value[index+2:], '}')
			if end < 0 {
				builder.WriteByte(value[index])
				index++
				continue
			}
			name := value[index+2 : index+2+end]
			builder.WriteString(McpConfigValueEnv(name))
			index += 2 + end + 1
			continue
		}
		builder.WriteByte(value[index])
		index++
	}
	return builder.String(), nil
}

// resolveHeadersOrThrow expands every header value (upstream
// resolveHeadersOrThrow), dropping a header whose expansion fails.
func resolveHeadersOrThrow(headers map[string]string, what string) map[string]string {
	resolved := make(map[string]string, len(headers))
	for name, value := range headers {
		expanded, err := resolveConfigValueOrThrow(value)
		if err != nil {
			continue
		}
		resolved[name] = expanded
	}
	_ = what
	return resolved
}

// McpDefaultTransport builds the transport of one entry (upstream
// createDefaultTransport).
func McpDefaultTransport(entry McpServerEntry, cwd string, authProvider mcp.AuthProvider) (mcp.Transport, error) {
	if entry.Config.IsHTTP() {
		return mcp.NewStreamableHttpTransport(mcp.StreamableHttpTransportOptions{
			URL:          entry.Config.URL,
			Headers:      resolveHeadersOrThrow(entry.Config.Headers, "MCP server \""+entry.Name+"\""),
			AuthProvider: authProvider,
		}), nil
	}
	env := map[string]string{}
	for key, value := range entry.Config.Env {
		expanded, err := resolveConfigValueOrThrow(value)
		if err != nil {
			return nil, err
		}
		env[key] = expanded
	}
	resolvedCwd := cwd
	if entry.Config.Cwd != "" {
		resolvedCwd = filepath.Join(cwd, expandMcpHome(entry.Config.Cwd))
	}
	args := make([]string, 0, len(entry.Config.Args))
	for _, arg := range entry.Config.Args {
		args = append(args, expandMcpHome(arg))
	}
	return mcp.NewStdioTransport(mcp.StdioTransportOptions{
		Command: expandMcpHome(entry.Config.Command),
		Args:    args,
		Cwd:     resolvedCwd,
		Env:     env,
		Stderr:  "pipe",
	}), nil
}

// expandMcpHome expands `~` and `~/…` (upstream expandHome).
func expandMcpHome(value string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return value
	}
	if value == "~" {
		return home
	}
	if strings.HasPrefix(value, "~/") {
		return filepath.Join(home, value[2:])
	}
	return value
}

// --- createMcpToolDefinition (tools.ts) ---

// McpToolCaller is the connection surface a tool definition calls (upstream
// McpToolCaller).
type McpToolCaller interface {
	CallTool(ctx context.Context, name string, args map[string]any, options mcp.RequestOptions) (*protocol.CallToolResult, error)
}

// McpToolDefinitionOptions are CreateMcpToolDefinition inputs.
type McpToolDefinitionOptions struct {
	Server    string
	Tool      protocol.Tool
	Name      string
	Exposure  McpExposure
	TimeoutMs int64
	GetClient func(ctx context.Context) (McpToolCaller, error)
	// ReadableResources reports whether read_mcp_resource can read the server's
	// resources.
	ReadableResources func() bool
}

// CreateMcpToolDefinition converts one MCP tool into an agent tool (upstream
// createMcpToolDefinition, minus the renderers the port's AgentTool does not
// carry).
func CreateMcpToolDefinition(options McpToolDefinitionOptions) agent.AgentTool {
	label := options.Server + "/" + options.Tool.Name
	description := ""
	if options.Tool.Description != nil {
		description = strings.TrimSpace(*options.Tool.Description)
	}
	if description == "" && options.Tool.Title != nil {
		description = *options.Tool.Title
	}
	if description == "" {
		description = "MCP tool " + options.Tool.Name + " from server " + options.Server
	}
	timeoutMs := options.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = McpDefaultTimeoutSeconds * 1000
	}
	return agent.AgentTool{
		Name:        options.Name,
		Label:       label,
		Description: description,
		Parameters:  mcpToolParameters(options.Tool.InputSchema),
		Execute: func(_ string, params json.RawMessage, ctx context.Context, onUpdate func(agent.AgentToolResult)) (agent.AgentToolResult, error) {
			client, err := options.GetClient(ctx)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			args := map[string]any{}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &args); err != nil {
					return agent.AgentToolResult{}, err
				}
			}
			requestOptions := mcp.RequestOptions{TimeoutMs: timeoutMs}
			if onUpdate != nil {
				requestOptions.OnProgress = func(progress *protocol.ProgressNotification) {
					total := ""
					if progress.Total != nil {
						total = "/" + formatMcpNumber(*progress.Total)
					}
					message := "Progress " + formatMcpNumber(progress.Progress) + total
					if progress.Message != nil {
						message = *progress.Message
					}
					onUpdate(agent.AgentToolResult{Content: []ai.Content{ai.TextContent{Text: message}}})
				}
			}
			result, err := client.CallTool(ctx, options.Tool.Name, args, requestOptions)
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			converted, isError, err := ConvertMcpResult(options.Server, options.Tool.Name, result, ConvertMcpResultOptions{
				ReadableResources: options.ReadableResources != nil && options.ReadableResources(),
			})
			if err != nil {
				return agent.AgentToolResult{}, err
			}
			if isError {
				return converted, McpToolError(converted)
			}
			return converted, nil
		},
	}
}

// mcpToolParameters normalizes a tool input schema: objects need a `type` and
// `properties` for some providers (upstream toParameters).
func mcpToolParameters(schema json.RawMessage) json.RawMessage {
	object := map[string]any{}
	if len(schema) > 0 {
		if err := json.Unmarshal(schema, &object); err != nil {
			object = map[string]any{}
		}
	}
	if _, ok := object["type"]; !ok {
		object["type"] = "object"
	}
	if _, ok := object["properties"]; !ok {
		object["properties"] = map[string]any{}
	}
	encoded, err := ai.MarshalJSON(object)
	if err != nil {
		return schema
	}
	return encoded
}

// formatMcpNumber renders a JSON number without a trailing `.0`.
func formatMcpNumber(value float64) string {
	if value == float64(int64(value)) {
		return itoaInt(int64(value))
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", value), "0"), ".")
}

func itoaInt(value int64) string { return fmt.Sprintf("%d", value) }

// mcpOptionalString takes the address of a non-empty string.
func mcpOptionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
