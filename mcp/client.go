package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Client defaults (upstream client.ts).
const (
	DefaultRequestTimeoutMs = 30000
	MaxListPages            = 1000
)

// ClientState is the connection lifecycle (upstream ClientState).
type ClientState = string

// Client states.
const (
	StateIdle       ClientState = "idle"
	StateConnecting ClientState = "connecting"
	StateConnected  ClientState = "connected"
	StateClosed     ClientState = "closed"
)

// ClientOptions configures a Client (upstream McpClientOptions).
type ClientOptions struct {
	Name    string
	Version string
	Title   *string

	Capabilities     *protocol.ClientCapabilities
	ProtocolVersion  string
	RequestTimeoutMs int64
	// Roots are served from the roots/list handler; RootsFunc, when set,
	// is called instead so the answer can change per request.
	Roots     []protocol.Root
	RootsFunc func(ctx context.Context) ([]protocol.Root, error)
}

// RequestOptions mirrors McpRequestOptions. Cancellation is the request's
// context (upstream's AbortSignal).
type RequestOptions struct {
	TimeoutMs  int64
	OnProgress func(*protocol.ProgressNotification)
}

// RequestHandler serves a server-to-client request (upstream RequestHandler).
// The returned value is marshaled as the response result; a nil result is
// sent as `{}`.
type RequestHandler func(ctx context.Context, params json.RawMessage) (any, error)

type pendingResult struct {
	result json.RawMessage
	err    error
}

type pendingRequest struct {
	ch            chan pendingResult
	timeoutMs     int64
	timer         *time.Timer
	cancellable   bool
	onProgress    func(*protocol.ProgressNotification)
	progressToken *int64
}

// Client is one MCP connection (upstream McpClient). Methods are safe for
// concurrent use; listeners are delivered outside the internal lock.
type Client struct {
	options ClientOptions

	mu                    sync.Mutex
	state                 ClientState
	transport             Transport
	nextRequestId         int64
	serverInfo            *protocol.Implementation
	serverCapabilities    *protocol.ServerCapabilities
	instructions          *string
	protocolVersionValue  string
	pending               map[int64]*pendingRequest
	progressRequests      map[int64]int64
	incoming              map[protocol.JsonRpcId]context.CancelFunc
	requestHandlers       map[string]RequestHandler
	notificationListeners map[string][]*listenerEntry[func(json.RawMessage)]
	notificationMu        sync.Mutex
	errorListeners        []listenerEntry[func(error)]
	closeListeners        []listenerEntry[func()]
	transportDisposers    []func()
}

// NewClient builds a client in the idle state.
func NewClient(options ClientOptions) *Client {
	c := &Client{
		options:               options,
		state:                 StateIdle,
		nextRequestId:         1,
		pending:               map[int64]*pendingRequest{},
		progressRequests:      map[int64]int64{},
		incoming:              map[protocol.JsonRpcId]context.CancelFunc{},
		requestHandlers:       map[string]RequestHandler{},
		notificationListeners: map[string][]*listenerEntry[func(json.RawMessage)]{},
	}
	c.requestHandlers["ping"] = func(context.Context, json.RawMessage) (any, error) {
		return map[string]any{}, nil
	}
	if options.Roots != nil || options.RootsFunc != nil {
		c.requestHandlers["roots/list"] = func(ctx context.Context, _ json.RawMessage) (any, error) {
			roots := options.Roots
			if options.RootsFunc != nil {
				resolved, err := options.RootsFunc(ctx)
				if err != nil {
					return nil, err
				}
				roots = resolved
			}
			list := make([]protocol.Root, 0, len(roots))
			list = append(list, roots...)
			return map[string]any{"roots": list}, nil
		}
	}
	return c
}

// ConnectionState returns the lifecycle state.
func (c *Client) ConnectionState() ClientState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

// ServerInfo returns what initialize reported, after Connect.
func (c *Client) ServerInfo() *protocol.Implementation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverInfo
}

// ServerCapabilities returns the server's capabilities, after Connect.
func (c *Client) ServerCapabilities() *protocol.ServerCapabilities {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.serverCapabilities
}

// Instructions returns the server's instructions, after Connect.
func (c *Client) Instructions() *string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.instructions
}

// ProtocolVersion returns the negotiated version, after Connect.
func (c *Client) ProtocolVersion() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.protocolVersionValue
}

// InitializeResult is the connect result.
func (c *Client) Connect(ctx context.Context, transport Transport) (*protocol.InitializeResult, error) {
	c.mu.Lock()
	if c.state != StateIdle {
		state := c.state
		c.mu.Unlock()
		return nil, fmt.Errorf("Cannot connect MCP client in %s state", state)
	}
	c.state = StateConnecting
	c.transport = transport
	c.mu.Unlock()

	disposeMessage := transport.OnMessage(func(message Message) { c.handleMessage(message) })
	disposeError := transport.OnError(func(err error) { c.emitError(err) })
	disposeClose := transport.OnClose(func() { c.handleTransportClose() })
	c.mu.Lock()
	c.transportDisposers = []func(){disposeMessage, disposeError, disposeClose}
	c.mu.Unlock()

	result, err := c.connectInitialize(ctx, transport)
	if err != nil {
		_ = c.Close(ctx)
		return nil, err
	}
	return result, nil
}

func (c *Client) connectInitialize(ctx context.Context, transport Transport) (*protocol.InitializeResult, error) {
	if err := transport.Start(ctx); err != nil {
		return nil, err
	}
	capabilities := protocol.ClientCapabilities{}
	if c.options.Capabilities != nil {
		capabilities = *c.options.Capabilities
	}
	c.mu.Lock()
	hasRoots := c.options.Roots != nil || c.options.RootsFunc != nil
	c.mu.Unlock()
	if hasRoots && capabilities.Roots == nil {
		capabilities.Roots = &struct {
			ListChanged bool `json:"listChanged,omitempty"`
		}{}
	}
	clientInfo := protocol.Implementation{Name: c.options.Name, Version: c.options.Version, Title: c.options.Title}
	requested := c.options.ProtocolVersion
	if requested == "" {
		requested = protocol.LatestProtocolVersion
	}
	params := map[string]any{
		"protocolVersion": requested,
		"capabilities":    capabilities,
		"clientInfo":      clientInfo,
	}
	raw, err := c.request(ctx, "initialize", params, RequestOptions{}, true)
	if err != nil {
		return nil, err
	}
	result, err := validateInitializeResult(raw)
	if err != nil {
		return nil, err
	}
	supported := false
	for _, version := range protocol.SupportedProtocolVersions {
		if version == result.ProtocolVersion {
			supported = true
			break
		}
	}
	if !supported {
		return nil, fmt.Errorf("MCP server selected unsupported protocol version %s", result.ProtocolVersion)
	}
	transport.SetProtocolVersion(result.ProtocolVersion)
	if err := c.notifyInternal(ctx, "notifications/initialized", nil, true); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.protocolVersionValue = result.ProtocolVersion
	c.serverInfo = &result.ServerInfo
	c.serverCapabilities = &result.Capabilities
	c.instructions = result.Instructions
	c.state = StateConnected
	c.mu.Unlock()
	return result, nil
}

// Request sends a request and waits for the response.
func (c *Client) Request(ctx context.Context, method string, params any, options RequestOptions) (json.RawMessage, error) {
	return c.request(ctx, method, params, options, false)
}

// Notify sends a notification.
func (c *Client) notify(ctx context.Context, method string, params any) error {
	return c.notifyInternal(ctx, method, params, false)
}

func (c *Client) notifyInternal(ctx context.Context, method string, params any, allowConnecting bool) error {
	transport, err := c.requireTransport(allowConnecting)
	if err != nil {
		return err
	}
	message := protocol.JsonRpcNotification{JSONRPC: protocol.JSONRPCVersion, Method: method}
	if params != nil {
		enc, err := json.Marshal(params)
		if err != nil {
			return err
		}
		message.Params = enc
	}
	return transport.Send(ctx, message)
}

// Notify is the exported notification send.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	return c.notify(ctx, method, params)
}

// SetRequestHandler registers a server-to-client request handler.
func (c *Client) SetRequestHandler(method string, handler RequestHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestHandlers[method] = handler
}

// OnNotification registers a notification listener; the returned func
// removes it.
func (c *Client) OnNotification(method string, listener func(json.RawMessage)) func() {
	c.notificationMu.Lock()
	defer c.notificationMu.Unlock()
	entry := &listenerEntry[func(json.RawMessage)]{fn: listener}
	c.notificationListeners[method] = append(c.notificationListeners[method], entry)
	return func() {
		c.notificationMu.Lock()
		defer c.notificationMu.Unlock()
		entry.removed = true
	}
}

// OnError registers an error listener; the returned func removes it.
func (c *Client) OnError(listener func(error)) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errorListeners = append(c.errorListeners, listenerEntry[func(error)]{fn: listener})
	token := &c.errorListeners[len(c.errorListeners)-1]
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		token.removed = true
	}
}

// OnClose registers a listener called once when the connection closes,
// whether the transport dropped or Close was called.
func (c *Client) OnClose(listener func()) func() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeListeners = append(c.closeListeners, listenerEntry[func()]{fn: listener})
	token := &c.closeListeners[len(c.closeListeners)-1]
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		token.removed = true
	}
}

// Ping sends a ping request.
func (c *Client) Ping(ctx context.Context, options RequestOptions) error {
	_, err := c.Request(ctx, "ping", nil, options)
	return err
}

// ListTools returns every tool, following nextCursor through all pages.
func (c *Client) ListTools(ctx context.Context, options RequestOptions) ([]protocol.Tool, error) {
	items, err := c.listAll(ctx, "tools/list", "tools", isTool, options)
	if err != nil {
		return nil, err
	}
	return decodeItems[protocol.Tool](items)
}

// ListResources returns every resource (upstream listResources).
func (c *Client) ListResources(ctx context.Context, options RequestOptions) ([]protocol.Resource, error) {
	items, err := c.listAll(ctx, "resources/list", "resources", isResource, options)
	if err != nil {
		return nil, err
	}
	pages := make([]protocol.Resource, 0, len(items))
	for _, item := range items {
		resource, err := decodeResource(item)
		if err != nil {
			return nil, err
		}
		pages = append(pages, resource)
	}
	return pages, nil
}

// ListResourcesPage returns one page of resources.
func (c *Client) ListResourcesPage(ctx context.Context, cursor *string, options RequestOptions) (*protocol.ListResourcesResult, error) {
	items, next, err := c.listPage(ctx, "resources/list", "resources", isResource, cursor, options)
	if err != nil {
		return nil, err
	}
	result := &protocol.ListResourcesResult{NextCursor: next}
	for _, item := range items {
		resource, err := decodeResource(item)
		if err != nil {
			return nil, err
		}
		result.Resources = append(result.Resources, resource)
	}
	return result, nil
}

// ListResourceTemplates returns every resource template.
func (c *Client) ListResourceTemplates(ctx context.Context, options RequestOptions) ([]protocol.ResourceTemplate, error) {
	items, err := c.listAll(ctx, "resources/templates/list", "resourceTemplates", isResourceTemplate, options)
	if err != nil {
		return nil, err
	}
	templates := make([]protocol.ResourceTemplate, 0, len(items))
	for _, item := range items {
		template, err := decodeResourceTemplate(item)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return templates, nil
}

// ListResourceTemplatesPage returns one page of resource templates.
func (c *Client) ListResourceTemplatesPage(ctx context.Context, cursor *string, options RequestOptions) (*protocol.ListResourceTemplatesResult, error) {
	items, next, err := c.listPage(ctx, "resources/templates/list", "resourceTemplates", isResourceTemplate, cursor, options)
	if err != nil {
		return nil, err
	}
	result := &protocol.ListResourceTemplatesResult{NextCursor: next}
	for _, item := range items {
		template, err := decodeResourceTemplate(item)
		if err != nil {
			return nil, err
		}
		result.ResourceTemplates = append(result.ResourceTemplates, template)
	}
	return result, nil
}

// ReadResource reads one resource.
func (c *Client) ReadResource(ctx context.Context, uri string, options RequestOptions) (*protocol.ReadResourceResult, error) {
	raw, err := c.Request(ctx, "resources/read", map[string]any{"uri": uri}, options)
	if err != nil {
		return nil, err
	}
	if err := validateReadResourceResult(raw); err != nil {
		return nil, err
	}
	var result protocol.ReadResourceResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CallTool invokes a tool.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any, options RequestOptions) (*protocol.CallToolResult, error) {
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	raw, err := c.Request(ctx, "tools/call", params, options)
	if err != nil {
		return nil, err
	}
	result, err := validateCallToolResult(raw)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Close rejects in-flight requests, aborts served server requests, and
// closes the transport.
func (c *Client) Close(ctx context.Context) error {
	c.mu.Lock()
	transport := c.transport
	c.transport = nil
	disposers := c.transportDisposers
	c.transportDisposers = nil
	c.mu.Unlock()
	for _, dispose := range disposers {
		dispose()
	}
	c.markClosed(&protocol.McpConnectionClosedError{})
	if transport != nil {
		return transport.Close(ctx)
	}
	return nil
}

// request is requestInternal (upstream requestInternal).
func (c *Client) request(ctx context.Context, method string, params any, options RequestOptions, allowConnecting bool) (json.RawMessage, error) {
	transport, err := c.requireTransport(allowConnecting)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, &protocol.McpAbortError{}
	}
	c.mu.Lock()
	id := c.nextRequestId
	c.nextRequestId++
	timeoutMs := options.TimeoutMs
	if timeoutMs == 0 {
		timeoutMs = c.options.RequestTimeoutMs
	}
	if timeoutMs == 0 {
		timeoutMs = DefaultRequestTimeoutMs
	}
	entry := &pendingRequest{
		ch:          make(chan pendingResult, 1),
		timeoutMs:   timeoutMs,
		cancellable: method != "initialize",
		onProgress:  options.OnProgress,
	}
	c.pending[id] = entry
	if options.OnProgress != nil {
		entry.progressToken = &id
		c.progressRequests[id] = id
	}
	c.mu.Unlock()

	c.armTimeout(id, entry)

	var message protocol.JsonRpcRequest
	message.JSONRPC = protocol.JSONRPCVersion
	message.ID = protocol.NumberId(float64(id))
	message.Method = method
	if entry.progressToken != nil {
		message.Params = withProgressToken(params, id)
	} else if params != nil {
		enc, marshalErr := json.Marshal(params)
		if marshalErr != nil {
			c.cancelPending(id, marshalErr, false, "")
			return nil, marshalErr
		}
		message.Params = enc
	}
	if sendErr := transport.Send(ctx, message); sendErr != nil {
		c.cancelPending(id, sendErr, false, "")
		<-entry.ch
		return nil, sendErr
	}
	select {
	case r := <-entry.ch:
		return r.result, r.err
	case <-ctx.Done():
		reason := "Aborted"
		if cause := context.Cause(ctx); cause != nil {
			reason = cause.Error()
		}
		c.cancelPending(id, &protocol.McpAbortError{}, entry.cancellable, reason)
		r := <-entry.ch
		return r.result, r.err
	}
}

// withProgressToken injects `_meta.progressToken` into the params,
// preserving an existing `_meta` object (upstream requestInternal).
func withProgressToken(params any, token int64) json.RawMessage {
	var merged map[string]any
	if object, ok := params.(map[string]any); ok && object != nil {
		merged = make(map[string]any, len(object)+1)
		for key, value := range object {
			merged[key] = value
		}
	} else if params != nil {
		var decoded map[string]any
		if enc, err := json.Marshal(params); err == nil && json.Unmarshal(enc, &decoded) == nil && decoded != nil {
			merged = decoded
		}
	}
	meta := map[string]any{"progressToken": token}
	if merged == nil {
		enc, _ := json.Marshal(map[string]any{"_meta": meta})
		return enc
	}
	if existing, ok := merged["_meta"].(map[string]any); ok && existing != nil {
		for key, value := range existing {
			meta[key] = value
		}
	}
	merged["_meta"] = meta
	enc, _ := json.Marshal(merged)
	return enc
}

func (c *Client) requireTransport(allowConnecting bool) (Transport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport != nil && (c.state == StateConnected || (allowConnecting && c.state == StateConnecting)) {
		return c.transport, nil
	}
	return nil, &protocol.McpConnectionClosedError{Msg: fmt.Sprintf("MCP client is %s", c.state)}
}

// handleMessage dispatches an inbound message (upstream handleMessage).
func (c *Client) handleMessage(message Message) {
	switch m := message.(type) {
	case *protocol.JsonRpcResponse:
		c.handleResponse(m)
	case *protocol.JsonRpcRequest:
		c.handleRequest(m)
	case *protocol.JsonRpcNotification:
		c.handleNotification(m)
	default:
		c.emitError(&protocol.McpError{Code: protocol.JSONRPCErrorCode.InvalidRequest, Msg: "Received invalid JSON-RPC message"})
	}
}

// handleResponse resolves or rejects the pending request (upstream
// handleResponse).
func (c *Client) handleResponse(message *protocol.JsonRpcResponse) {
	c.mu.Lock()
	id := int64(message.ID.Num)
	entry := c.pending[id]
	if entry == nil {
		c.mu.Unlock()
		c.emitError(fmt.Errorf("Received response for unknown MCP request %s", message.ID.String()))
		return
	}
	c.removePendingLocked(id, entry)
	c.mu.Unlock()
	if message.Error != nil {
		entry.ch <- pendingResult{err: &protocol.McpError{Code: message.Error.Code, Msg: message.Error.Message, Data: message.Error.Data}}
		return
	}
	entry.ch <- pendingResult{result: message.Result}
}

// handleRequest serves a server-to-client request (upstream handleRequest).
func (c *Client) handleRequest(message *protocol.JsonRpcRequest) {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return
	}
	c.mu.Lock()
	handler := c.requestHandlers[message.Method]
	c.mu.Unlock()
	if handler == nil {
		errorResponse := protocol.JsonRpcResponse{
			JSONRPC: protocol.JSONRPCVersion,
			ID:      message.ID,
			Error:   &protocol.JsonRpcErrorObject{Code: protocol.JSONRPCErrorCode.MethodNotFound, Message: fmt.Sprintf("Method not found: %s", message.Method)},
		}
		if err := transport.Send(context.Background(), errorResponse); err != nil {
			c.emitError(err)
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.incoming[message.ID] = cancel
	c.mu.Unlock()
	defer func() {
		cancel()
		c.mu.Lock()
		delete(c.incoming, message.ID)
		c.mu.Unlock()
	}()
	result, err := handler(ctx, message.Params)
	if err == nil {
		if result == nil {
			result = map[string]any{}
		}
		enc, marshalErr := json.Marshal(result)
		if marshalErr == nil {
			err = transport.Send(context.Background(), protocol.JsonRpcResponse{
				JSONRPC: protocol.JSONRPCVersion, ID: message.ID, Result: enc,
			})
		} else {
			err = marshalErr
		}
	}
	if err != nil {
		responseError := &protocol.JsonRpcErrorObject{Code: protocol.JSONRPCErrorCode.InternalError}
		if mcpErr, ok := err.(*protocol.McpError); ok {
			responseError.Code = mcpErr.Code
			responseError.Message = mcpErr.Msg
			if raw, ok := mcpErr.Data.(json.RawMessage); ok {
				responseError.Data = raw
			}
		} else {
			responseError.Message = err.Error()
		}
		if sendErr := transport.Send(context.Background(), protocol.JsonRpcResponse{
			JSONRPC: protocol.JSONRPCVersion, ID: message.ID, Error: responseError,
		}); sendErr != nil {
			c.emitError(sendErr)
		}
	}
}

// handleNotification dispatches notifications (upstream handleNotification).
func (c *Client) handleNotification(message *protocol.JsonRpcNotification) {
	if message.Method == "notifications/progress" {
		c.handleProgress(message.Params)
	} else if message.Method == "notifications/cancelled" {
		c.handleCancelled(message.Params)
	}
	c.notificationMu.Lock()
	entries := make([]*listenerEntry[func(json.RawMessage)], 0, len(c.notificationListeners[message.Method]))
	for _, entry := range c.notificationListeners[message.Method] {
		if !entry.removed {
			entries = append(entries, entry)
		}
	}
	c.notificationMu.Unlock()
	for _, entry := range entries {
		c.safeListener(entry.fn, message.Params)
	}
}

// safeListener runs a notification listener; a panic becomes an error
// report, matching upstream's try/catch.
func (c *Client) safeListener(fn func(json.RawMessage), params json.RawMessage) (panicked bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			c.emitError(protocol.ToError(recovered))
			panicked = true
		}
	}()
	fn(params)
	return false
}

// handleProgress renews the timeout and reports progress (upstream
// handleProgress).
func (c *Client) handleProgress(params json.RawMessage) {
	var decoded struct {
		ProgressToken any      `json:"progressToken"`
		Progress      *float64 `json:"progress"`
		Total         *float64 `json:"total"`
		Message       *string  `json:"message"`
	}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return
	}
	token, ok := jsonRpcIdNumber(decoded.ProgressToken)
	if !ok || decoded.Progress == nil {
		return
	}
	c.mu.Lock()
	requestId := c.progressRequests[token]
	entry := c.pending[requestId]
	if requestId == 0 || entry == nil {
		c.mu.Unlock()
		return
	}
	c.armTimeoutLocked(requestId, entry)
	c.mu.Unlock()
	if entry.onProgress != nil {
		notification := &protocol.ProgressNotification{
			ProgressToken: formatId(token),
			Progress:      *decoded.Progress,
			Total:         decoded.Total,
			Message:       decoded.Message,
		}
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					c.emitError(protocol.ToError(recovered))
				}
			}()
			entry.onProgress(notification)
		}()
	}
}

func formatId(token int64) string {
	return fmt.Sprintf("%d", token)
}

// jsonRpcIdNumber narrows a decoded progressToken to a finite number.
func jsonRpcIdNumber(value any) (int64, bool) {
	switch v := value.(type) {
	case json.Number:
		n, err := v.Float64()
		if err != nil {
			return 0, false
		}
		return int64(n), true
	case float64:
		return int64(v), true
	}
	return 0, false
}

// handleCancelled aborts a served server request (upstream handleCancelled).
func (c *Client) handleCancelled(params json.RawMessage) {
	var decoded struct {
		RequestID any    `json:"requestId"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(params, &decoded); err != nil {
		return
	}
	var id protocol.JsonRpcId
	switch v := decoded.RequestID.(type) {
	case string:
		id = protocol.StringId(v)
	case float64, json.Number:
		n, _ := v.(json.Number).Float64()
		id = protocol.NumberId(n)
	default:
		return
	}
	c.mu.Lock()
	cancel := c.incoming[id]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// armTimeout (re)starts the request timer (upstream armTimeout).
func (c *Client) armTimeout(id int64, entry *pendingRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.armTimeoutLocked(id, entry)
}

func (c *Client) armTimeoutLocked(id int64, entry *pendingRequest) {
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.timeoutMs <= 0 || entry.timeoutMs > 1<<53 {
		return
	}
	entry.timer = time.AfterFunc(time.Duration(entry.timeoutMs)*time.Millisecond, func() {
		c.cancelPending(id, &protocol.McpTimeoutError{TimeoutMs: entry.timeoutMs}, entry.cancellable, "Request timed out")
	})
}

// cancelPending rejects one pending request and notifies the server when
// the request was cancellable (upstream cancelPending).
func (c *Client) cancelPending(id int64, err error, notifyServer bool, reason string) {
	c.mu.Lock()
	entry := c.pending[id]
	if entry == nil {
		c.mu.Unlock()
		return
	}
	c.removePendingLocked(id, entry)
	c.mu.Unlock()
	entry.ch <- pendingResult{err: err}
	if notifyServer {
		c.mu.Lock()
		transport := c.transport
		c.mu.Unlock()
		if transport != nil {
			params := map[string]any{"requestId": id}
			if reason != "" {
				params["reason"] = reason
			}
			enc, _ := json.Marshal(params)
			notification := protocol.JsonRpcNotification{
				JSONRPC: protocol.JSONRPCVersion,
				Method:  "notifications/cancelled",
				Params:  enc,
			}
			go func() {
				if sendErr := transport.Send(context.Background(), notification); sendErr != nil {
					c.emitError(sendErr)
				}
			}()
		}
	}
}

// removePendingLocked unregisters the entry; the caller holds c.mu.
func (c *Client) removePendingLocked(id int64, entry *pendingRequest) {
	delete(c.pending, id)
	if entry.timer != nil {
		entry.timer.Stop()
	}
	if entry.progressToken != nil {
		delete(c.progressRequests, *entry.progressToken)
	}
}

// markClosed rejects in-flight requests, aborts served server requests,
// and flips the state (upstream markClosed).
func (c *Client) markClosed(err error) {
	c.mu.Lock()
	wasClosed := c.state == StateClosed
	c.state = StateClosed
	for id, entry := range c.pending {
		c.removePendingLocked(id, entry)
		entry.ch <- pendingResult{err: err}
	}
	incoming := c.incoming
	c.incoming = map[protocol.JsonRpcId]context.CancelFunc{}
	c.mu.Unlock()
	for _, cancel := range incoming {
		cancel()
	}
	if wasClosed {
		return
	}
	c.mu.Lock()
	listeners := make([]func(), 0, len(c.closeListeners))
	for _, entry := range c.closeListeners {
		if !entry.removed {
			listeners = append(listeners, entry.fn)
		}
	}
	c.mu.Unlock()
	for _, listener := range listeners {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					c.emitError(protocol.ToError(recovered))
				}
			}()
			listener()
		}()
	}
}

// handleTransportClose mirrors upstream handleTransportClose.
func (c *Client) handleTransportClose() {
	c.markClosed(&protocol.McpConnectionClosedError{})
}

// emitError fans out to the error listeners.
func (c *Client) emitError(err error) {
	c.mu.Lock()
	listeners := make([]func(error), 0, len(c.errorListeners))
	for _, entry := range c.errorListeners {
		if !entry.removed {
			listeners = append(listeners, entry.fn)
		}
	}
	c.mu.Unlock()
	for _, listener := range listeners {
		listener(err)
	}
}

// decodeItems turns validated page objects into typed results.
func decodeItems[T any](items []map[string]any) ([]T, error) {
	out := make([]T, 0, len(items))
	for _, item := range items {
		enc, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		var decoded T
		if err := json.Unmarshal(enc, &decoded); err != nil {
			return nil, err
		}
		out = append(out, decoded)
	}
	return out, nil
}
