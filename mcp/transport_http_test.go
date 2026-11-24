package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dat267/pier/ai"
	"github.com/dat267/pier/mcp/protocol"
)

// Port of packages/mcp/test/streamable-http.test.ts: httptest servers play
// the MCP endpoints.

type recordedRequest struct {
	method  string
	headers http.Header
	message json.RawMessage
}

type httpFixture struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []recordedRequest
	url      string
}

// protocolHandler is the fixture handler: GET answers 405, DELETE 200,
// notifications 202, initialize and tools/list JSON, other requests an SSE
// response.
// protocolHandlerBody records the request and answers per the fixture
// protocol; callers that already read the body pass it, others pass nil.
func (f *httpFixture) protocolHandler(w http.ResponseWriter, r *http.Request, body []byte) {
	if body == nil {
		body, _ = io.ReadAll(r.Body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, recordedRequest{method: r.Method, headers: r.Header.Clone(), message: body})
	f.mu.Unlock()
	if r.Method == http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Method == http.MethodDelete {
		w.WriteHeader(http.StatusOK)
		return
	}
	var message struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	_ = json.Unmarshal(body, &message)
	if message.ID == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if message.Method == "initialize" {
		w.Header().Set("content-type", "application/json")
		w.Header().Set("mcp-session-id", "session-1")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":%q,"capabilities":{"tools":{}},"serverInfo":{"name":"http-fixture","version":"1.0.0"}}}`, message.ID, protocol.LatestProtocolVersion)
		return
	}
	if message.Method == "tools/list" {
		w.Header().Set("content-type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"tools":[{"name":"echo","inputSchema":{"type":"object"}}]}}`, message.ID)
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	fmt.Fprintf(w, "id: tool-result\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"hello\"}]}}\n\n", message.ID)
}

func newHTTPFixture(handler func(w http.ResponseWriter, r *http.Request)) *httpFixture {
	fixture := &httpFixture{}
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler(w, r)
	})
	fixture.server = httptest.NewServer(wrapped)
	fixture.url = fixture.server.URL + "/mcp"
	return fixture
}

// isToolCall serves every request but a tools/call POST through the protocol
// handler and reports whether the test's own handler should answer it.
func (f *httpFixture) isToolCall(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		f.protocolHandler(w, r, nil)
		return false
	}
	body, _ := io.ReadAll(r.Body)
	var message struct {
		Method string `json:"method"`
	}
	_ = json.Unmarshal(body, &message)
	if message.Method != "tools/call" {
		f.protocolHandler(w, r, body)
		return false
	}
	return true
}

func (f *httpFixture) close(*testing.T) {
	f.server.Close()
}

func (f *httpFixture) countMethod(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, request := range f.requests {
		if request.method == method {
			count++
		}
	}
	return count
}

// waitForMethod waits until the fixture recorded a request with the given
// HTTP method. The server-to-client GET stream opens on its own goroutine
// (upstream dispatches it asynchronously too), and a Close aborts a GET that
// has not reached the server yet, so the assertions must wait for it instead
// of racing it.
func waitForMethod(t *testing.T, fixture *httpFixture, method string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for fixture.countMethod(method) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("no %s request recorded", method)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// headerOf returns the header of the first request whose JSON-RPC message
// has the given method.
func (f *httpFixture) headerOf(messageMethod string, header string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, request := range f.requests {
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(request.message, &message)
		if message.Method == messageMethod {
			return request.headers.Get(header)
		}
	}
	return ""
}

func TestConsumeSseStreamParsesChunkedCRLFEvents(t *testing.T) {
	body := ": keepalive\r\nid: 7\r\ndata: {\"one\":\r\ndata: 1}\r\n\r\n"
	var events []SseEvent
	if err := ConsumeSseStream(strings.NewReader(body), ConsumeSseOptions{
		OnEvent: func(event SseEvent) { events = append(events, event) },
	}); err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	if events[0].ID == nil || *events[0].ID != "7" || events[0].Data != `{"one":`+"\n"+`1}` || events[0].Event != nil {
		t.Fatalf("event = %+v", events[0])
	}
}

// streamable-http.ts decodes SSE bytes with TextDecoder, replacing malformed UTF-8.
func TestConsumeSseStreamReplacesMalformedUTF8(t *testing.T) {
	body := string([]byte("data: {\"x\":\"\xff\"}\n\n"))
	var got string
	if err := ConsumeSseStream(strings.NewReader(body), ConsumeSseOptions{
		OnEvent: func(event SseEvent) { got = event.Data },
	}); err != nil {
		t.Fatal(err)
	}
	if got != `{"x":"�"}` {
		t.Fatalf("data = %q; want replacement-decoded UTF-8", got)
	}
}

func TestConsumeSseStreamRejectsOversizedPendingEvent(t *testing.T) {
	body := strings.Repeat("data: xxxxxxxxxxxxxxxx\n", 40)
	err := ConsumeSseStream(strings.NewReader(body), ConsumeSseOptions{
		MaxEventBytes: 256,
		OnEvent:       func(SseEvent) {},
	})
	if err == nil || err.Error() != "MCP SSE event exceeds 256 bytes" {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamableHTTPHandlesJSONAndSSEResponses(t *testing.T) {
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) { fixture.protocolHandler(w, r, nil) })
	defer fixture.close(t)
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	if transport.SessionID() != "session-1" {
		t.Fatalf("sessionId = %q", transport.SessionID())
	}
	tools, err := client.ListTools(context.Background(), RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %+v", tools)
	}
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"}, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("result = %+v", result.Content)
	}
	waitForMethod(t, fixture, "GET")
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fixture.headerOf("tools/list", "Mcp-Session-Id"); got != "session-1" {
		t.Fatalf("session header = %q", got)
	}
	if got := fixture.headerOf("tools/list", "MCP-Protocol-Version"); got != protocol.LatestProtocolVersion {
		t.Fatalf("protocol header = %q", got)
	}
	if fixture.countMethod("GET") == 0 {
		t.Fatal("no GET stream opened")
	}
	if fixture.countMethod("DELETE") == 0 {
		t.Fatal("no DELETE on close")
	}
}

func TestStreamableHTTPRejectsEmptyJSONBody(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	_, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url}))
	if err == nil {
		t.Fatal("connect succeeded with an empty JSON body")
	}
}

func TestStreamableHTTPHandlesWhitespacePrefixedBatch(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("content-type", "application/json")
		w.Header().Set("mcp-session-id", "session-1")
		// Upstream parses the body with response.json(), so leading
		// whitespace before a batch array is invisible.
		fmt.Fprintf(w, "\n  [{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":%q,\"capabilities\":{},\"serverInfo\":{\"name\":\"http-fixture\",\"version\":\"1.0.0\"}}}]", message.ID, protocol.LatestProtocolVersion)
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})); err != nil {
		t.Fatalf("connect: %v", err)
	}
	_ = client.Close(context.Background())
}

func TestStreamableHTTPRejectsRequestWithoutResponse(t *testing.T) {
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if !fixture.isToolCall(w, r) {
			return
		}
		w.WriteHeader(http.StatusAccepted)
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	openGetStream := false
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream,
	})); err != nil {
		t.Fatal(err)
	}
	_, err := client.CallTool(context.Background(), "echo", nil, RequestOptions{})
	if err == nil || !strings.Contains(err.Error(), "without a response") {
		t.Fatalf("err = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTPIncludesBodyInHTTPErrors(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Invalid Accept header"))
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	_, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url}))
	if err == nil || err.Error() != "MCP HTTP request failed with status 400: Invalid Accept header" {
		t.Fatalf("err = %v", err)
	}
}

func (p *replayAuthProvider) Token(context.Context) (string, error) { return p.token(), nil }

func (p *replayAuthProvider) OnUnauthorized(_ context.Context, info UnauthorizedInfo) error {
	p.onUnauthorized(info.Response.StatusCode)
	_ = info.Response.Body.Close()
	return nil
}

// #10188: the platform fetch must not be called with a receiver; Go has no
// receiver binding, so the transport never adds one. The test verifies the
// injected fetch reaches the transport's requests.
func TestStreamableHTTPUsesInjectedFetch(t *testing.T) {
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) { fixture.protocolHandler(w, r, nil) })
	defer fixture.close(t)
	var calls int
	var mu sync.Mutex
	injected := func(ctx context.Context, request *http.Request) (*http.Response, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		// Rewrite the loopback URL the transport parsed at construction
		// time to the live server address.
		request.URL.Host = fixture.server.URL[7:]
		return http.DefaultClient.Do(request)
	}
	// The transport parses the URL at construction; point the option at the
	// live server and route through the injected fetch.
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url, Fetch: injected, OpenGetStream: new(false)})
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(context.Background(), RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	seenCalls := calls
	mu.Unlock()
	if seenCalls < 2 {
		t.Fatalf("injected fetch calls = %d", seenCalls)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// countingAuthProvider counts Token calls so a test can assert that a path does not refresh.
type countingAuthProvider struct {
	token string
	mu    sync.Mutex
	calls int
}

func (p *countingAuthProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	return p.token, nil
}

func (p *countingAuthProvider) OnUnauthorized(ctx context.Context, info UnauthorizedInfo) error {
	return nil
}

func (p *countingAuthProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// streamable-http.ts trims and slices the error body with JavaScript string
// semantics (String.prototype.trim, .length and .slice count UTF-16 units).
func TestDescribeHTTPFailureUsesJavaScriptStringSemantics(t *testing.T) {
	body := strings.Repeat("界", 250)
	if got, want := describeHttpFailure(400, body), "MCP HTTP request failed with status 400: "+body; got != want {
		t.Fatalf("message = %q; want the untruncated 250-unit body", got)
	}
	if got, want := describeHttpFailure(400, "\uFEFFerr"), "MCP HTTP request failed with status 400: err"; got != want {
		t.Fatalf("message = %q; want JavaScript trim to strip the BOM", got)
	}
}

// fetch sets Content-Length for a string body; a request built with a nil body
// and then given a reader sends chunked encoding instead.
func TestStreamableHTTPPostsSetContentLength(t *testing.T) {
	var fixture *httpFixture
	var mu sync.Mutex
	lengths := map[string]int64{}
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		lengths[r.Method] = r.ContentLength
		mu.Unlock()
		fixture.protocolHandler(w, r, nil)
	})
	defer fixture.close(t)
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	defer client.Close(context.Background())
	if _, err := client.ListTools(context.Background(), RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	post := lengths[http.MethodPost]
	mu.Unlock()
	if post <= 0 {
		t.Fatalf("POST content length = %d; want the body length, not chunked encoding", post)
	}
}

func TestStreamableHTTPErrorBodyIsSlicedByUTF16Units(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, strings.Repeat("界", 9000))
	})
	defer fixture.close(t)
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	_, err := client.Connect(context.Background(), transport)
	if err == nil {
		t.Fatal("connect succeeded against a failing endpoint")
	}
	var httpErr *McpHttpError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v; want *McpHttpError", err)
	}
	// The cap is a byte budget (streamable-http.ts MAX_ERROR_BODY_BYTES = 8 * 1024)
	// applied before decoding, so 9000 three-byte characters keep only the 2731
	// units that the first 8192 bytes decode to: 2730 whole characters plus one
	// U+FFFD for the split sequence. The earlier expectation of 8192 units pinned
	// a character-budget reading of the same constant.
	if got := ai.JSLength(httpErr.Body); got != 2731 {
		t.Fatalf("body units = %d; want %d", got, 2731)
	}
}

// MCP sends messages with JSON.stringify, which does not HTML-escape <, > and &.
func TestStreamableHTTPDoesNotHTMLEscapePayloads(t *testing.T) {
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) { fixture.protocolHandler(w, r, nil) })
	defer fixture.close(t)
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "a < b & c > d"}, RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	fixture.mu.Lock()
	var body string
	for _, request := range fixture.requests {
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(request.message, &message)
		if message.Method == "tools/call" {
			body = string(request.message)
		}
	}
	fixture.mu.Unlock()
	if body == "" {
		t.Fatal("no tools/call request recorded")
	}
	if strings.Contains(body, `\u003c`) || strings.Contains(body, `\u0026`) || strings.Contains(body, `\u003e`) {
		t.Fatalf("payload HTML-escaped: %s", body)
	}
	if !strings.Contains(body, "a < b & c > d") {
		t.Fatalf("payload = %s", body)
	}
	_ = client.Close(context.Background())
}

// pi v1.4.0 (4a080252), transports/streamable-http.ts constructs
// `this.url = new URL(options.url)` in the constructor, so a malformed URL is a
// caller-visible error before any request. Start is the constructor's
// validating half in Go; a malformed URL must never reach a nil dereference.
func TestStreamableHTTPRejectsMalformedURLAtStart(t *testing.T) {
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: "http://[::1"})
	err := transport.Start(context.Background())
	if err == nil {
		t.Fatal("Start accepted a malformed URL")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("Start reported a cancellation for a malformed URL: %v", err)
	}
	sendErr := transport.Send(context.Background(), &protocol.JsonRpcNotification{JSONRPC: protocol.JSONRPCVersion, Method: "notifications/initialized"})
	if sendErr == nil {
		t.Fatal("Send after a rejected Start succeeded")
	}
}

// pi v1.4.0 streamable-http.ts uses ?? for both maxEventBytes and
// maxMessageBytes, so explicit zero rejects any nonempty SSE data.
func TestConsumeSseStreamExplicitZeroEventLimit(t *testing.T) {
	err := ConsumeSseStream(strings.NewReader("data: x\n\n"), ConsumeSseOptions{
		MaxEventBytes:    0,
		MaxEventBytesSet: true,
		OnEvent:          func(SseEvent) {},
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds 0 bytes") {
		t.Fatalf("SSE parse error = %v, want zero-byte limit", err)
	}
}

func TestHttpTransportExplicitZeroMessageLimit(t *testing.T) {
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: "http://example.test/mcp", MaxMessageBytes: 0, MaxMessageBytesSet: true})
	err := transport.consumeSse(io.NopCloser(strings.NewReader("data: x\n\n")), &streamCursor{}, func(Message) {})
	if err == nil || !strings.Contains(err.Error(), "exceeds 0 bytes") {
		t.Fatalf("SSE transport error = %v, want zero-byte limit", err)
	}
}

// Reference: pi v1.4.0 (4a080252),
// packages/mcp/src/transports/streamable-http.ts needsAuthorization:
// /(?:^|[\s,])error="?insufficient_scope"?/i. JavaScript's \s includes the
// Unicode space separators, so a challenge separated by one is still a
// step-up authorization request.
func TestNeedsAuthorizationAcceptsJavaScriptWhitespace(t *testing.T) {
	for _, separator := range []struct {
		name  string
		value string
	}{
		{"ASCII space", " "},
		{"tab", "\t"},
		{"no-break space", "\u00a0"},
		{"ideographic space", "\u3000"},
		{"comma", ","},
	} {
		response := &http.Response{
			StatusCode: http.StatusForbidden,
			Header: http.Header{"Www-Authenticate": []string{
				`Bearer realm="mcp"` + separator.value + `error="insufficient_scope"`,
			}},
		}
		if !needsAuthorization(response) {
			t.Errorf("%s: needsAuthorization = false, want true", separator.name)
		}
	}
	// An unrelated 403 challenge is not a step-up request.
	other := &http.Response{
		StatusCode: http.StatusForbidden,
		Header:     http.Header{"Www-Authenticate": []string{`Bearer realm="mcp"`}},
	}
	if needsAuthorization(other) {
		t.Error("needsAuthorization(unrelated 403) = true, want false")
	}
}

func (b *trackedSseBody) Close() error {
	close(b.closed)
	return nil
}

type stalledAcknowledgmentBody struct {
	ctx    context.Context
	closed bool
}

func (b *stalledAcknowledgmentBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledAcknowledgmentBody) Close() error {
	b.closed = true
	return nil
}

type recoveryBody struct {
	io.Reader
	closes int
}

func (b *recoveryBody) Close() error { b.closes++; return nil }

type recoveryAuthProvider struct{ err error }

func (*recoveryAuthProvider) Token(context.Context) (string, error)                    { return "synthetic-token", nil }
func (p *recoveryAuthProvider) OnUnauthorized(context.Context, UnauthorizedInfo) error { return p.err }

type heldTokenProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (p *heldTokenProvider) Token(context.Context) (string, error) {
	close(p.entered)
	<-p.release
	return "synthetic-token", nil
}

func (*heldTokenProvider) OnUnauthorized(context.Context, UnauthorizedInfo) error { return nil }

// M35: an MCP server is untrusted network input. A successful JSON-RPC
// response over the Streamable HTTP transport must respect the same per-message
// cap the stdio transport and SSE stream already enforce.
func TestStreamableHTTPRejectsOversizedJSONResponse(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var request struct {
			ID json.RawMessage `json:"id"`
		}
		_ = json.Unmarshal(body, &request)
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":`))
		_, _ = w.Write(request.ID)
		_, _ = w.Write([]byte(`,"result":{"protocolVersion":"2025-06-18","capabilities":{},"serverInfo":{"name":"x","version":"1"},"padding":"`))
		_, _ = w.Write([]byte(strings.Repeat("x", DefaultMaxMessageBytes+1024)))
		_, _ = w.Write([]byte(`"}}`))
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("oversized JSON response accepted: err=%v", err)
	}
}

// Reference: pi v1.6.0 transports/streamable-http.ts checkResponse reads a failed
// response with `readBodyText(response, MAX_ERROR_BODY_BYTES)`, which keeps the
// first 8192 **bytes** and decodes only those. pier read up to four times that and
// sliced 8192 UTF-16 units after decoding, so a multi-byte body kept far more
// text than the reference: 4000 CJK characters are 12000 bytes, and upstream
// keeps 2731 units where the port kept all 4000.
func TestMcpErrorBodyKeepsTheBytePrefix(t *testing.T) {
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: "https://example.test"})
	for _, tc := range []struct {
		name, body string
		wantKept   int
	}{
		{"ascii under the cap", strings.Repeat("a", 100), 100},
		{"ascii over the cap", strings.Repeat("a", 9000), 8192},
		// 12000 bytes: the first 8192 bytes decode to 2731 whole characters
		// (8192/3 = 2730.67, so 2730 full characters plus one split rune).
		{"cjk over the cap", "日" + strings.Repeat("本", 3999), 2731},
	} {
		response := &http.Response{
			StatusCode: 500, Header: http.Header{},
			Body: io.NopCloser(strings.NewReader(tc.body)),
		}
		err := transport.checkResponse(response)
		httpErr, ok := err.(*McpHttpError)
		if !ok {
			t.Fatalf("%s: err = %T", tc.name, err)
		}
		if got := ai.JSLength(httpErr.Body); got != tc.wantKept {
			t.Fatalf("%s: kept %d units, want %d", tc.name, got, tc.wantKept)
		}
	}
}
