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

func (f *httpFixture) close(t *testing.T) {
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

// headerOf returns the header of the first request whose JSON-RPC message
// has the given method.
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

func TestStreamableHTTPClassifiesAuthenticationFailures(t *testing.T) {
	fixture := newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("www-authenticate", `Bearer resource_metadata="https://example.com/meta"`)
		_, _ = w.Write([]byte("login required"))
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	_, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url}))
	if err == nil {
		t.Fatal("connect did not fail")
	}
	var authErr *McpAuthRequiredError
	if !errors.As(err, &authErr) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if authErr.Status != 401 || authErr.Body != "login required" {
		t.Fatalf("authErr = %+v", authErr)
	}
}

func TestStreamableHTTPFailsOnlyTheBrokenStream(t *testing.T) {
	releaseSlow := make(chan struct{})
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fixture.protocolHandler(w, r, nil)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var message struct {
			Params struct {
				Name string `json:"name"`
			} `json:"params"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Params.Name == "broken" {
			w.Header().Set("content-type", "text/event-stream")
			_, _ = w.Write([]byte("data: not json\n\n"))
			return
		}
		if message.Params.Name == "slow" {
			<-releaseSlow
		}
		fixture.protocolHandler(w, r, body)
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	errorsSeen := make(chan string, 4)
	client.OnError(func(err error) { errorsSeen <- err.Error() })
	openGetStream := false
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream,
	})); err != nil {
		t.Fatal(err)
	}
	slowDone := make(chan error, 1)
	go func() {
		_, err := client.CallTool(context.Background(), "slow", nil, RequestOptions{})
		slowDone <- err
	}()
	time.Sleep(50 * time.Millisecond)
	if _, err := client.CallTool(context.Background(), "broken", nil, RequestOptions{}); err == nil ||
		!strings.Contains(err.Error(), "MCP response stream failed") {
		t.Fatalf("broken err = %v", err)
	}
	close(releaseSlow)
	select {
	case err := <-slowDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow call never finished")
	}
	select {
	case message := <-errorsSeen:
		if message != "Invalid JSON" {
			t.Fatalf("transport error = %q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the broken stream's parse error was never reported")
	}
	select {
	case message := <-errorsSeen:
		t.Fatalf("extra transport error: %s", message)
	case <-time.After(100 * time.Millisecond):
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTPOpensGetStreamAfterInitialization(t *testing.T) {
	var mu sync.Mutex
	var order []string
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		fixture.protocolHandler(w, r, nil)
		mu.Lock()
		order = append(order, r.Method)
		mu.Unlock()
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CallTool(context.Background(), "echo", nil, RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListTools(context.Background(), RequestOptions{}); err != nil {
		t.Fatal(err)
	}
	waitForMethod(t, fixture, "GET")
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	initializedAt, getAt := -1, -1
	// order: POST(initialize), POST(initialized), GET, ...; GET must come
	// after the initialized notification.
	if len(order) < 3 {
		t.Fatalf("order = %v", order)
	}
	postsSeen := 0
	for i, step := range order {
		if step == "POST" {
			postsSeen++
			if postsSeen == 2 {
				initializedAt = i
			}
		}
		if step == "GET" && getAt == -1 {
			getAt = i
		}
	}
	if initializedAt == -1 || getAt == -1 || getAt <= initializedAt {
		t.Fatalf("GET not after initialized: order=%v", order)
	}
	if got := fixture.headerOf("GET", "Last-Event-ID"); got != "" {
		t.Fatalf("last-event-id on fresh GET = %q", got)
	}
}

func TestStreamableHTTPResumesClosedResponseStream(t *testing.T) {
	var resumeHeaders []string
	var mu sync.Mutex
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if last := r.Header.Get("Last-Event-ID"); last != "" {
				mu.Lock()
				resumeHeaders = append(resumeHeaders, last)
				mu.Unlock()
				w.Header().Set("content-type", "text/event-stream")
				fmt.Fprintf(w, "id: 2\ndata: {\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"resumed\"}]}}\n\n")
				return
			}
		}
		if r.Method != http.MethodPost {
			fixture.protocolHandler(w, r, nil)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Method != "tools/call" {
			fixture.protocolHandler(w, r, body)
			return
		}
		// Priming event (ID, no data) and a retry hint, then the server
		// drops the stream.
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte("id: 1\nretry: 5\ndata:\n\n"))
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	openGetStream := false
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream,
	})); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "echo", nil, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) != 1 || result.Content[0].Text != "resumed" {
		t.Fatalf("result = %+v", result.Content)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(resumeHeaders) != 1 || resumeHeaders[0] != "1" {
		t.Fatalf("resumeHeaders = %v", resumeHeaders)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTPFailsRequestWhoseStreamEndsWithoutAnswer(t *testing.T) {
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fixture.protocolHandler(w, r, nil)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Method != "tools/call" {
			fixture.protocolHandler(w, r, body)
			return
		}
		w.Header().Set("content-type", "text/event-stream")
		_, _ = w.Write([]byte(": nothing here\n\n"))
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	openGetStream := false
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream,
	})); err != nil {
		t.Fatal(err)
	}
	_, err := client.CallTool(context.Background(), "echo", nil, RequestOptions{TimeoutMs: 5000})
	if err == nil || err.Error() != "MCP response stream failed: stream ended without a response" {
		t.Fatalf("err = %v", err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTPReconnectsGetStream(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	var lastEventIDs []*string
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		last := r.Header.Get("Last-Event-ID")
		if last != "" {
			lastEventIDs = append(lastEventIDs, &last)
		} else {
			lastEventIDs = append(lastEventIDs, nil)
		}
		first := gets == 1
		mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
		notification := `{"jsonrpc":"2.0","method":"notifications/tools/list_changed"}`
		if first {
			fmt.Fprintf(w, "id: g1\ndata: %s\n\n", notification)
			return
		}
		fmt.Fprintf(w, "id: g2\ndata: %s\n\n", notification)
		// The second stream ends immediately (connection close), which
		// triggers one more reconnect that the test does not wait for.
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	changes := make(chan struct{}, 8)
	client.OnNotification("notifications/tools/list_changed", func(json.RawMessage) { changes <- struct{}{} })
	initialDelay := int64(1)
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, Reconnect: &StreamableHttpReconnectOptions{InitialDelayMs: initialDelay},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("first notification never arrived")
	}
	select {
	case <-changes:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect never re-delivered the notification")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lastEventIDs) < 2 || lastEventIDs[0] != nil || lastEventIDs[1] == nil || *lastEventIDs[1] != "g1" {
		t.Fatalf("lastEventIDs = %v", lastEventIDs)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStreamableHTTPRejectsRequestWithoutResponse(t *testing.T) {
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fixture.protocolHandler(w, r, nil)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Method != "tools/call" {
			fixture.protocolHandler(w, r, body)
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

// The auth provider sees the rejected token and the fresh token is retried.
func TestStreamableHTTPHands401And403ToAuthProvider(t *testing.T) {
	var seen []struct {
		status int
		token  string
	}
	token := "old"
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			fixture.protocolHandler(w, r, nil)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if r.Header.Get("Authorization") == "Bearer old" {
			w.Header().Set("www-authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if message.Method == "tools/call" && r.Header.Get("Authorization") == "Bearer new" {
			w.Header().Set("www-authenticate", `Bearer error="insufficient_scope", scope="admin"`)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		fixture.protocolHandler(w, r, body)
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	openGetStream := false
	auth := &replayAuthProvider{token: func() string {
		return token
	}, onUnauthorized: func(status int) {
		seen = append(seen, struct {
			status int
			token  string
		}{status, token})
		switch status {
		case 401:
			token = "new"
		case 403:
			token = "admin"
		}
	}}
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream, AuthProvider: auth,
	})); err != nil {
		t.Fatal(err)
	}
	result, err := client.CallTool(context.Background(), "echo", nil, RequestOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// "echo" without arguments: the fixture answers hello over SSE.
	if len(result.Content) != 1 || result.Content[0].Text != "hello" {
		t.Fatalf("result = %+v", result.Content)
	}
	if len(seen) != 2 || seen[0].status != 401 || seen[0].token != "old" || seen[1].status != 403 || seen[1].token != "new" {
		t.Fatalf("seen = %+v", seen)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type replayAuthProvider struct {
	token          func() string
	onUnauthorized func(status int)
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
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url, Fetch: injected, OpenGetStream: boolPtr(false)})
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

func TestStreamableHTTPClassifiesExpiredSession(t *testing.T) {
	var mu sync.Mutex
	posts := 0
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			mu.Lock()
			// Upstream evaluates `posts++ >= 2` before the increment.
			expired := posts >= 2
			posts++
			mu.Unlock()
			if expired {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("gone"))
				return
			}
		}
		fixture.protocolHandler(w, r, nil)
	})
	defer fixture.close(t)
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	openGetStream := false
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream,
	})); err != nil {
		t.Fatal(err)
	}
	_, err := client.ListTools(context.Background(), RequestOptions{})
	var expired *McpSessionExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func boolPtr(b bool) *bool { return &b }
