package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

// Streamable-HTTP tuning (upstream streamable-http.ts).
const (
	maxErrorBodyBytes          = 8 * 1024
	errorMessageBodyChars      = 500
	defaultReconnectInitialMs  = 1000
	defaultReconnectMaxMs      = 30000
	defaultReconnectMaxRetries = 5
	sessionDeleteTimeoutMs     = 1000
)

// SseEvent is one parsed server-sent event (upstream SseEvent).
type SseEvent struct {
	Event *string
	Data  string
	ID    *string
}

// ConsumeSseOptions configures the SSE parser (upstream ConsumeSseOptions).
type ConsumeSseOptions struct {
	MaxEventBytes int64
	// OnEvent is called for every complete event.
	OnEvent func(SseEvent)
	// OnID is called for every `id` field, including events without data
	// (for example resumption priming events).
	OnID func(id string)
	// OnRetry is called for every valid `retry` field, in milliseconds.
	OnRetry func(delayMs int64)
}

// ConsumeSseStream parses an SSE body (upstream consumeSseStream). Events
// streamed as many short `data:` lines without a terminating blank line
// cannot grow without bound: the byte budget counts the pending event's
// data including the line joins.
func ConsumeSseStream(body io.Reader, options ConsumeSseOptions) error {
	maxEventBytes := options.MaxEventBytes
	if maxEventBytes == 0 {
		maxEventBytes = DefaultMaxMessageBytes
	}
	reader := bufio.NewReader(body)
	var eventName, eventId *string
	var dataLines []string
	var dataBytes int64

	dispatch := func() {
		if len(dataLines) == 0 {
			eventName, eventId = nil, nil
			return
		}
		data := strings.Join(dataLines, "\n")
		event := SseEvent{Data: data, Event: eventName, ID: eventId}
		options.OnEvent(event)
		eventName, eventId = nil, nil
		dataLines = nil
		dataBytes = 0
	}
	processLine := func(rawLine string) error {
		line := strings.TrimSuffix(rawLine, "\r")
		if line == "" {
			dispatch()
			return nil
		}
		if strings.HasPrefix(line, ":") {
			return nil
		}
		field, value := line, ""
		if colon := strings.Index(line, ":"); colon >= 0 {
			field, value = line[:colon], line[colon+1:]
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		}
		switch field {
		case "data":
			dataBytes += int64(len(value))
			if len(dataLines) > 0 {
				dataBytes++
			}
			if dataBytes > maxEventBytes {
				return fmt.Errorf("MCP SSE event exceeds %d bytes", maxEventBytes)
			}
			dataLines = append(dataLines, value)
		case "event":
			name := value
			eventName = &name
		case "id":
			if !strings.Contains(value, "\x00") {
				id := value
				eventId = &id
				if options.OnID != nil {
					options.OnID(id)
				}
			}
		case "retry":
			if retryDigits.MatchString(value) {
				delayMs, _ := strconv.ParseInt(value, 10, 64)
				if options.OnRetry != nil {
					options.OnRetry(delayMs)
				}
			}
		}
		return nil
	}

	var buffered string
	// Read in chunks and decode as UTF-8; a chunk boundary never splits a
	// byte because the line search works on bytes.
	chunk := make([]byte, 32*1024)
	for {
		n, err := reader.Read(chunk)
		if n > 0 {
			buffered += string(chunk[:n])
		}
		for {
			newline := strings.Index(buffered, "\n")
			if newline < 0 {
				break
			}
			if err := processLine(buffered[:newline]); err != nil {
				return err
			}
			buffered = buffered[newline+1:]
		}
		if int64(len(buffered)) > maxEventBytes {
			return fmt.Errorf("MCP SSE event exceeds %d bytes", maxEventBytes)
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
	}
	if buffered != "" {
		if err := processLine(buffered); err != nil {
			return err
		}
	}
	dispatch()
	return nil
}

var retryDigits = regexp.MustCompile(`^\d+$`)

// StreamableHttpReconnectOptions configures reconnection of dropped SSE
// streams (upstream StreamableHttpReconnectOptions).
type StreamableHttpReconnectOptions struct {
	// InitialDelayMs delays the first reconnection attempt, unless the
	// server sent a `retry` field. Default: 1000.
	InitialDelayMs int64
	// MaxDelayMs bounds the exponential backoff. Default: 30000.
	MaxDelayMs int64
	// MaxRetries is the number of consecutive failed attempts before giving
	// up on a stream. Default: 5.
	MaxRetries int
}

// StreamableHttpTransportOptions configures the transport (upstream
// StreamableHttpTransportOptions).
type StreamableHttpTransportOptions struct {
	URL     string
	Headers map[string]string
	// Fetch replaces the HTTP client (upstream's fetch injection).
	Fetch           func(ctx context.Context, request *http.Request) (*http.Response, error)
	OpenGetStream   *bool
	MaxMessageBytes int64
	AuthProvider    AuthProvider
	Reconnect       *StreamableHttpReconnectOptions
}

// McpHttpError is an HTTP-level transport failure (upstream McpHttpError).
type McpHttpError struct {
	Status int
	Body   string
	Msg    string
}

func (e *McpHttpError) Error() string { return e.Msg }

// McpAuthRequiredError reports a 401 (upstream McpAuthRequiredError).
type McpAuthRequiredError struct {
	McpHttpError
	WWWAuthenticate string
}

// McpSessionExpiredError reports a session the server no longer knows
// (upstream McpSessionExpiredError).
type McpSessionExpiredError struct{ McpHttpError }

// AuthProvider supplies bearer credentials and reacts to a 401 (upstream
// AuthProvider).
type AuthProvider interface {
	// Token returns the current bearer token, if any.
	Token(ctx context.Context) (string, error)
	// OnUnauthorized reacts to a 401 (or step-up 403) once per request;
	// whatever credentials it leaves behind are retried.
	OnUnauthorized(ctx context.Context, info UnauthorizedInfo) error
}

// UnauthorizedInfo is the onUnauthorized payload (upstream's object).
type UnauthorizedInfo struct {
	Response  *http.Response
	ServerURL string
	Fetch     func(ctx context.Context, request *http.Request) (*http.Response, error)
	Token     string
}

// streamCursor tracks one SSE stream's resumption state (upstream
// StreamCursor).
type streamCursor struct {
	lastEventID *string
	retryMs     *int64
	received    bool
}

// StreamableHttpTransport speaks the streamable HTTP MCP wire (upstream
// StreamableHttpTransport): JSON POSTs answered inline or over SSE, plus an
// optional server-to-client GET stream with resumption.
type StreamableHttpTransport struct {
	transportEvents
	options   StreamableHttpTransportOptions
	parsedURL *url.URL
	client    *http.Client

	mu               sync.Mutex
	stopCtx          context.Context
	stop             context.CancelFunc
	started          bool
	closed           bool
	sessionID        string
	protocolVersion  string
	getStreamStarted bool
}

// NewStreamableHttpTransport builds a transport; Start only validates.
func NewStreamableHttpTransport(options StreamableHttpTransportOptions) *StreamableHttpTransport {
	parsed, err := url.Parse(options.URL)
	stopCtx, stop := context.WithCancel(context.Background())
	t := &StreamableHttpTransport{
		options:   options,
		parsedURL: parsed,
		client:    &http.Client{},
		stopCtx:   stopCtx,
		stop:      stop,
	}
	_ = err // URL errors surface at fetch time, like upstream's lazy URL parse
	return t
}

// SessionID returns the server-assigned session, once known.
func (t *StreamableHttpTransport) SessionID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sessionID
}

// Start marks the transport open (the HTTP layer needs no setup).
func (t *StreamableHttpTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return fmt.Errorf("MCP Streamable HTTP transport already started")
	}
	if t.closed {
		return &protocol.McpConnectionClosedError{}
	}
	t.started = true
	return nil
}

// SetProtocolVersion stores the negotiated version for the header.
func (t *StreamableHttpTransport) SetProtocolVersion(version string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.protocolVersion = version
}

// Send posts one message. A request is answered either inline (JSON body)
// or over an SSE response stream consumed in the background; notifications
// and responses are acknowledged with 202 and carry no reply.
func (t *StreamableHttpTransport) Send(ctx context.Context, message Message) error {
	t.mu.Lock()
	started, closed := t.started, t.closed
	t.mu.Unlock()
	if !started || closed {
		return &protocol.McpConnectionClosedError{}
	}
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	response, err := t.authorizedFetch(ctx, http.MethodPost, map[string]string{
		"accept":       "application/json, text/event-stream",
		"content-type": "application/json",
	}, body)
	if err != nil {
		return err
	}
	if err := t.checkResponse(response); err != nil {
		discardBody(response)
		return err
	}
	t.captureSession(response)

	request, isRequest := message.(*protocol.JsonRpcRequest)
	if !isRequest {
		discardBody(response)
		// The server-to-client stream may only open once the session is
		// initialized.
		if notification, ok := message.(*protocol.JsonRpcNotification); ok && notification.Method == "notifications/initialized" {
			t.startGetStream()
		}
		return nil
	}
	if response.StatusCode == http.StatusAccepted || response.StatusCode == http.StatusNoContent {
		discardBody(response)
		return &McpHttpError{Status: response.StatusCode, Msg: fmt.Sprintf("MCP server accepted request %s without a response", request.Method)}
	}
	switch contentType(response) {
	case "application/json":
		data, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return readErr
		}
		var items []json.RawMessage
		if data[0] == '[' {
			if err := json.Unmarshal(data, &items); err != nil {
				return err
			}
		} else {
			items = []json.RawMessage{data}
		}
		for _, item := range items {
			decoded, err := decodeJSONValue(item)
			if err != nil {
				return err
			}
			if err := protocol.ParseJsonRpcMessage(decoded); err != nil {
				return err
			}
			concrete, err := protocol.DecodeMessage(item)
			if err != nil {
				return err
			}
			t.emitMessage(Message(concrete))
		}
		return nil
	case "text/event-stream":
		if response.Body != nil {
			go t.consumeResponseStream(response.Body, request.ID)
			return nil
		}
	}
	discardBody(response)
	missing := "missing"
	typeName := contentType(response)
	if typeName == "" {
		typeName = missing
	}
	return &McpHttpError{Status: response.StatusCode, Msg: "Unsupported MCP response content type: " + typeName}
}

// Close aborts in-flight work, deletes the session, and emits close.
func (t *StreamableHttpTransport) Close(ctx context.Context) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	sessionID := t.sessionID
	t.mu.Unlock()
	t.stop()
	if t.started && sessionID != "" {
		deleteCtx, cancel := context.WithTimeout(context.Background(), sessionDeleteTimeoutMs*time.Millisecond)
		headers, _, err := t.headers(nil, "")
		if err == nil {
			request, buildErr := http.NewRequestWithContext(deleteCtx, http.MethodDelete, t.parsedURL.String(), nil)
			if buildErr == nil {
				for key, value := range headers {
					request.Header.Set(key, value)
				}
				response, err := t.doFetch(deleteCtx, request)
				if err == nil {
					discardBody(response)
				}
			}
		}
		cancel()
	}
	t.emitClose()
	return nil
}

// authorizedFetch fetches with auth headers; a 401 (or a 403 asking for
// more scope) is handed to the auth provider once, and the request is
// retried with whatever credentials it left behind.
func (t *StreamableHttpTransport) authorizedFetch(ctx context.Context, method string, extra map[string]string, body []byte) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		headers, token, err := t.headers(extra, "")
		if err != nil {
			return nil, err
		}
		request, err := http.NewRequestWithContext(t.stopCtx, method, t.parsedURL.String(), nil)
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		if body != nil {
			request.Body = io.NopCloser(bytes.NewReader(body))
		}
		response, err := t.doFetch(t.stopCtx, request)
		if err != nil {
			return nil, err
		}
		if attempt > 0 || t.options.AuthProvider == nil || !needsAuthorization(response) {
			return response, nil
		}
		info := UnauthorizedInfo{Response: response, ServerURL: t.parsedURL.String(), Fetch: t.doFetch, Token: token}
		err = t.options.AuthProvider.OnUnauthorized(t.stopCtx, info)
		if err == nil {
			continue
		}
		discardBody(response)
		return nil, err
	}
}

// headers builds the request headers: static options, per-request extras,
// session and protocol headers, then the bearer token.
func (t *StreamableHttpTransport) headers(extra map[string]string, _ string) (map[string]string, string, error) {
	headers := map[string]string{}
	for key, value := range t.options.Headers {
		headers[key] = value
	}
	for key, value := range extra {
		headers[key] = value
	}
	t.mu.Lock()
	if t.sessionID != "" {
		headers["Mcp-Session-Id"] = t.sessionID
	}
	if t.protocolVersion != "" {
		headers["MCP-Protocol-Version"] = t.protocolVersion
	}
	t.mu.Unlock()
	token := ""
	if t.options.AuthProvider != nil {
		var err error
		token, err = t.options.AuthProvider.Token(context.Background())
		if err != nil {
			return nil, "", err
		}
		if token != "" {
			headers["Authorization"] = "Bearer " + token
		}
	}
	return headers, token, nil
}

func (t *StreamableHttpTransport) doFetch(ctx context.Context, request *http.Request) (*http.Response, error) {
	if t.options.Fetch != nil {
		return t.options.Fetch(ctx, request)
	}
	return t.client.Do(request)
}

func (t *StreamableHttpTransport) captureSession(response *http.Response) {
	sessionID := response.Header.Get("mcp-session-id")
	if sessionID != "" {
		t.mu.Lock()
		t.sessionID = sessionID
		t.mu.Unlock()
	}
}

func (t *StreamableHttpTransport) checkResponse(response *http.Response) error {
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxErrorBodyBytes))
	_ = response.Body.Close()
	if err != nil {
		body = nil
	}
	text := string(body)
	if response.StatusCode == http.StatusUnauthorized {
		return &McpAuthRequiredError{
			McpHttpError:    McpHttpError{Status: 401, Body: text, Msg: "MCP server requires authentication"},
			WWWAuthenticate: response.Header.Get("www-authenticate"),
		}
	}
	t.mu.Lock()
	hasSession := t.sessionID != ""
	t.mu.Unlock()
	if response.StatusCode == http.StatusNotFound && hasSession {
		return &McpSessionExpiredError{McpHttpError: McpHttpError{Status: 404, Body: text, Msg: "MCP session expired"}}
	}
	return &McpHttpError{Status: response.StatusCode, Body: text, Msg: describeHttpFailure(response.StatusCode, text)}
}

func describeHttpFailure(status int, body string) string {
	text := strings.TrimSpace(body)
	snippet := text
	if len(text) > errorMessageBodyChars {
		snippet = text[:errorMessageBodyChars-3] + "..."
	}
	message := fmt.Sprintf("MCP HTTP request failed with status %d", status)
	if snippet != "" {
		message += ": " + snippet
	}
	return message
}

func contentType(response *http.Response) string {
	value := response.Header.Get("content-type")
	if value == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.SplitN(value, ";", 2)[0]))
}

// needsAuthorization: 401, or 403 with an `insufficient_scope` bearer
// challenge (step-up authorization).
func needsAuthorization(response *http.Response) bool {
	if response.StatusCode == http.StatusUnauthorized {
		return true
	}
	if response.StatusCode != http.StatusForbidden {
		return false
	}
	return insufficientScope.MatchString(response.Header.Get("www-authenticate"))
}

var insufficientScope = regexp.MustCompile(`(?i)(?:^|[\s,])error="?insufficient_scope"?`)

// isTransientStatus: statuses worth retrying when a stream fails to
// (re)open.
func isTransientStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func discardBody(response *http.Response) {
	if response == nil || response.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
}

// consumeSse parses one SSE stream, tracking resumption state.
func (t *StreamableHttpTransport) consumeSse(body io.Reader, cursor *streamCursor, onMessage func(Message)) error {
	maxEventBytes := t.options.MaxMessageBytes
	if maxEventBytes == 0 {
		maxEventBytes = DefaultMaxMessageBytes
	}
	return ConsumeSseStream(body, ConsumeSseOptions{
		MaxEventBytes: maxEventBytes,
		OnID: func(id string) {
			cursor.lastEventID = &id
		},
		OnRetry: func(delayMs int64) {
			retry := delayMs
			cursor.retryMs = &retry
		},
		OnEvent: func(event SseEvent) {
			cursor.received = true
			// Events without data prime resumption; other event types are
			// not JSON-RPC.
			if strings.TrimSpace(event.Data) == "" || (event.Event != nil && *event.Event != "message") {
				return
			}
			concrete, err := protocol.DecodeMessage([]byte(event.Data))
			if err != nil {
				t.emitError(err)
				return
			}
			if onMessage != nil {
				onMessage(concrete)
			}
			t.emitMessage(Message(concrete))
		},
	})
}

// consumeResponseStream reads the SSE stream answering one request. When
// the stream ends or breaks before the response arrives and the server
// assigned event IDs, resume it with GET and `Last-Event-ID`, as the server
// may close response streams at will. Otherwise only this request fails.
func (t *StreamableHttpTransport) consumeResponseStream(body io.ReadCloser, requestID protocol.JsonRpcId) {
	cursor := &streamCursor{}
	var answered bool
	onMessage := func(message Message) {
		if response, ok := message.(*protocol.JsonRpcResponse); ok && response.ID.String() == requestID.String() {
			answered = true
		}
	}
	var stream io.Reader = body
	var failure error
	for attempt := 0; ; {
		if stream != nil {
			if err := t.consumeSse(stream, cursor, onMessage); err != nil {
				failure = err
			} else {
				failure = nil
			}
		}
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if answered || closed {
			return
		}
		if failure != nil && !t.isRetryable(failure) {
			break
		}
		if cursor.lastEventID == nil || attempt >= t.maxRetries() {
			break
		}
		if cursor.received {
			attempt = 0
		}
		cursor.received = false
		if !t.sleep(t.reconnectDelay(attempt, cursor.retryMs)) {
			return
		}
		attempt++
		opened, err := t.openSseStream(cursor.lastEventID)
		if err != nil {
			failure = err
			if !t.isRetryable(err) {
				break
			}
			stream = nil
			continue
		}
		stream = opened
	}
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return
	}
	reason := "stream ended without a response"
	if failure != nil {
		reason = failure.Error()
	}
	t.emitMessage(Message(&protocol.JsonRpcResponse{
		JSONRPC: protocol.JSONRPCVersion,
		ID:      requestID,
		Error:   &protocol.JsonRpcErrorObject{Code: protocol.JSONRPCErrorCode.InternalError, Message: fmt.Sprintf("MCP response stream failed: %s", reason)},
	}))
}

// startGetStream opens the server-to-client GET stream once.
func (t *StreamableHttpTransport) startGetStream() {
	t.mu.Lock()
	if (t.options.OpenGetStream != nil && !*t.options.OpenGetStream) || t.getStreamStarted || t.closed {
		t.mu.Unlock()
		return
	}
	t.getStreamStarted = true
	t.mu.Unlock()
	go t.runGetStream()
}

// runGetStream keeps the server-to-client stream open, reconnecting with
// backoff when it drops.
func (t *StreamableHttpTransport) runGetStream() {
	cursor := &streamCursor{}
	for attempt := 0; ; {
		t.mu.Lock()
		closed := t.closed
		t.mu.Unlock()
		if closed {
			return
		}
		stream, err := t.openSseStream(cursor.lastEventID)
		if err != nil {
			t.mu.Lock()
			closed = t.closed
			t.mu.Unlock()
			if closed {
				return
			}
			if !t.isRetryable(err) {
				t.emitError(err)
				return
			}
		} else if stream == nil {
			// The server does not offer a GET stream.
			return
		} else {
			openedAt := time.Now()
			if err := t.consumeSse(stream, cursor, nil); err != nil {
				t.mu.Lock()
				closed = t.closed
				t.mu.Unlock()
				if closed {
					return
				}
				if !t.isRetryable(err) {
					t.emitError(err)
					return
				}
			}
			// A stream that stayed up for a while counts as healthy, even if
			// it was idle.
			if cursor.received || time.Since(openedAt) > time.Duration(t.maxDelay())*time.Millisecond {
				attempt = 0
			}
		}
		cursor.received = false
		if attempt >= t.maxRetries() {
			t.emitError(fmt.Errorf("MCP server-to-client stream dropped and could not be reopened"))
			return
		}
		if !t.sleep(t.reconnectDelay(attempt, cursor.retryMs)) {
			return
		}
		attempt++
	}
}

// openSseStream opens a GET SSE stream; nil means the server answered 405
// (no GET stream).
func (t *StreamableHttpTransport) openSseStream(lastEventID *string) (io.ReadCloser, error) {
	extra := map[string]string{"accept": "text/event-stream"}
	if lastEventID != nil {
		extra["last-event-id"] = *lastEventID
	}
	response, err := t.authorizedFetch(t.stopCtx, http.MethodGet, extra, nil)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == http.StatusMethodNotAllowed {
		discardBody(response)
		return nil, nil
	}
	if err := t.checkResponse(response); err != nil {
		discardBody(response)
		return nil, err
	}
	t.captureSession(response)
	if contentType(response) != "text/event-stream" || response.Body == nil {
		discardBody(response)
		typeName := contentType(response)
		if typeName == "" {
			typeName = "missing"
		}
		return nil, &McpHttpError{Status: response.StatusCode, Msg: "Unsupported MCP GET response content type: " + typeName}
	}
	return response.Body, nil
}

// isRetryable: network failures and transient statuses are retried; auth,
// session, and protocol errors are not. Upstream treats fetch TypeErrors as
// network failures; in Go any non-McpHttpError from the HTTP client is one
// (D183).
func (t *StreamableHttpTransport) isRetryable(err error) bool {
	if httpErr, ok := err.(*McpHttpError); ok {
		return isTransientStatus(httpErr.Status)
	}
	if authErr, ok := err.(*McpAuthRequiredError); ok {
		return isTransientStatus(authErr.Status)
	}
	if sessionErr, ok := err.(*McpSessionExpiredError); ok {
		return isTransientStatus(sessionErr.Status)
	}
	return true
}

func (t *StreamableHttpTransport) reconnectDelay(attempt int, serverDelayMs *int64) int64 {
	if serverDelayMs != nil {
		return *serverDelayMs
	}
	initial := int64(defaultReconnectInitialMs)
	if t.options.Reconnect != nil && t.options.Reconnect.InitialDelayMs != 0 {
		initial = t.options.Reconnect.InitialDelayMs
	}
	delay := initial
	for i := 0; i < attempt; i++ {
		if delay*2 > t.maxDelay() {
			delay = t.maxDelay()
			break
		}
		delay *= 2
	}
	return delay
}

func (t *StreamableHttpTransport) maxDelay() int64 {
	if t.options.Reconnect != nil && t.options.Reconnect.MaxDelayMs != 0 {
		return t.options.Reconnect.MaxDelayMs
	}
	return defaultReconnectMaxMs
}

func (t *StreamableHttpTransport) maxRetries() int {
	if t.options.Reconnect != nil && t.options.Reconnect.MaxRetries != 0 {
		return t.options.Reconnect.MaxRetries
	}
	return defaultReconnectMaxRetries
}

// sleep waits; false means the transport closed while waiting.
func (t *StreamableHttpTransport) sleep(ms int64) bool {
	timer := time.NewTimer(time.Duration(ms) * time.Millisecond)
	select {
	case <-timer.C:
		return true
	case <-t.stopCtx.Done():
		timer.Stop()
		return false
	}
}

// decodeJSONValue decodes a JSON payload for shape validation.
func decodeJSONValue(data []byte) (any, error) {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}
