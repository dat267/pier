package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/mcp/protocol"
)

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
		if !fixture.isToolCall(w, r) {
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
		if !fixture.isToolCall(w, r) {
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

// pi v1.4.0 (4a080252), packages/mcp/src/transports/streamable-http.ts:
// optional maxRetries defaults to five, but an explicit zero disables retries.
func TestStreamableHTTPMaxRetriesZeroDisablesReconnect(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	dropped := make(chan error, 1)
	client.OnError(func(err error) { dropped <- err })
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL:       fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{InitialDelayMs: 1, MaxRetries: 0, MaxRetriesSet: true},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-dropped:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not report its drop")
	}
	mu.Lock()
	got := gets
	mu.Unlock()
	if got != 1 {
		t.Fatalf("GET attempts = %d, want one when MaxRetries is explicitly zero", got)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// pi v1.4.0 (4a080252), packages/mcp/src/transports/streamable-http.ts:
// an explicit zero initialDelayMs skips backoff; only undefined uses 1000 ms.
func TestStreamableHTTPZeroInitialDelayReconnectsImmediately(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	secondGet := make(chan struct{}, 1)
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		current := gets
		mu.Unlock()
		if current == 2 {
			secondGet <- struct{}{}
		}
		w.Header().Set("content-type", "text/event-stream")
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	zeroRetries := 1
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL:       fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{InitialDelayMs: 0, InitialDelayMsSet: true, MaxRetries: zeroRetries},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondGet:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("explicit zero initial delay did not reconnect promptly")
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// pi v1.4.0 (4a080252), packages/mcp/src/transports/streamable-http.ts:
// maxDelayMs=0 clamps exponential backoff to zero instead of using its default.
func TestStreamableHTTPZeroMaxDelayClampsBackoff(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	secondGet := make(chan struct{}, 1)
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		current := gets
		mu.Unlock()
		if current == 2 {
			secondGet <- struct{}{}
		}
		w.Header().Set("content-type", "text/event-stream")
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL:       fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{InitialDelayMs: 500, MaxDelayMs: 0, MaxDelayMsSet: true, MaxRetries: 1},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondGet:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("explicit zero max delay did not clamp reconnect backoff")
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

func TestStreamableHTTPCloseReusesTheLastToken(t *testing.T) {
	var fixture *httpFixture
	var mu sync.Mutex
	var deleteAuth string
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			mu.Lock()
			deleteAuth = r.Header.Get("Authorization")
			mu.Unlock()
		}
		fixture.protocolHandler(w, r, nil)
	})
	defer fixture.close(t)

	provider := &countingAuthProvider{token: "token-1"}
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, AuthProvider: provider,
	})
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	if _, err := client.Connect(context.Background(), transport); err != nil {
		t.Fatal(err)
	}
	if provider.count() == 0 {
		t.Fatal("the transport never asked for a token, so the test proves nothing")
	}

	// Connect launches the server-to-client GET asynchronously. Wait for its
	// token lookup to finish before measuring Close, otherwise the GET can
	// increment the counter inside the close assertion's observation window.
	// This fixture answers GET with 405, so it cannot trigger a reconnect.
	waitForMethod(t, fixture, http.MethodGet)
	before := provider.count()
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if after := provider.count(); after != before {
		t.Fatalf("closing asked for a token %d more time(s); it must reuse the last one", after-before)
	}
	if fixture.countMethod("DELETE") == 0 {
		t.Fatal("no DELETE on close")
	}
	mu.Lock()
	got := deleteAuth
	mu.Unlock()
	if got != "Bearer token-1" {
		t.Fatalf("DELETE authorization = %q, want the token of the last request", got)
	}
}

// pi v1.4.0 transports/streamable-http.ts dispatches POSTs with its lifetime
// controller.signal. close() aborts that controller, not just the GET stream.
func TestStreamableHTTPCloseCancelsInFlightPost(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		close(started)
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); close(release); server.Close() })
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: server.URL})
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- transport.Send(ctx, &protocol.JsonRpcRequest{
			JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "fixture/held",
		})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("POST did not reach the fixture")
	}
	if err := transport.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("closed transport POST unexpectedly succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not cancel the in-flight POST")
	}
}

type trackedSseBody struct {
	io.Reader
	closed chan struct{}
}

// Reference: pi v1.4.0 packages/mcp/src/transports/streamable-http.ts releases
// its SSE reader in finally. The Go HTTP body must likewise be closed when
// consumption finishes, so its connection and request resources are released.
func TestHttpResponseStreamClosesBodyAfterEOF(t *testing.T) {
	body := &trackedSseBody{
		Reader: strings.NewReader("data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n"),
		closed: make(chan struct{}),
	}
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: "https://mcp.example.test",
		Fetch: func(context.Context, *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: body}, nil
		},
	})
	defer transport.Close(context.Background())
	if err := transport.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(context.Background(), &protocol.JsonRpcRequest{
		JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "ping",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-body.closed:
	case <-time.After(2 * time.Second):
		t.Fatal("completed SSE response body was never closed")
	}
}

// pi v1.4.0 streamable-http.ts sleeps with setTimeout(ms), whose runtime clamps
// delays above 2^31-1 to 1ms. An SSE retry value just above that range must not
// become a multi-day wait in the Go transport.
func TestStreamableHTTPRetryDelayOverflowDoesNotBlockReconnect(t *testing.T) {
	var fixture *httpFixture
	var gets atomic.Int64
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets.Add(1)
			w.Header().Set("content-type", "text/event-stream")
			_, _ = io.WriteString(w, "retry: 2147483648\n\n")
			return
		}
		fixture.protocolHandler(w, r, nil)
	})
	defer fixture.close(t)

	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{URL: fixture.url})
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	ctx := context.Background()
	if err := transport.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := transport.Send(ctx, &protocol.JsonRpcNotification{
		JSONRPC: protocol.JSONRPCVersion,
		Method:  "notifications/initialized",
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.NewTimer(500 * time.Millisecond)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for gets.Load() < 2 {
		select {
		case <-deadline.C:
			t.Fatalf("GET stream did not reconnect after overflowing retry delay; requests=%d", gets.Load())
		case <-tick.C:
		}
	}
}

// pi v1.4.0 (4a080252), transports/streamable-http.ts send/discard:
// notification acknowledgments ignore and cancel any response body.
func TestStreamableHTTPDoesNotDrainNotificationAcknowledgment(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var body *stalledAcknowledgmentBody
		openGetStream := false
		transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
			URL: "https://fixture.invalid/mcp", OpenGetStream: &openGetStream,
			Fetch: func(ctx context.Context, _ *http.Request) (*http.Response, error) {
				body = &stalledAcknowledgmentBody{ctx: ctx}
				return &http.Response{StatusCode: http.StatusAccepted, Body: body}, nil
			},
		})
		if err := transport.Start(ctx); err != nil {
			t.Fatal(err)
		}
		defer transport.Close(context.Background())
		if err := transport.Send(ctx, &protocol.JsonRpcNotification{JSONRPC: "2.0", Method: "probe"}); err != nil {
			t.Fatal(err)
		}
		if ctx.Err() != nil {
			t.Fatalf("ignored 202 body exhausted sender budget: %v", ctx.Err())
		}
		if body == nil || !body.closed {
			t.Fatal("acknowledgment body not closed")
		}
	})
}

// pi v1.5.0 streamable-http.ts runGetStream compares elapsed milliseconds with
// maxDelayMs. A huge maxDelay must keep the retry budget intact, not overflow a
// time.Duration into a negative "healthy" threshold.
func TestStreamableHTTPLargeMaxDelayDoesNotResetRetryBudget(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	dropped := make(chan error, 1)
	client.OnError(func(err error) { dropped <- err })
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{
			InitialDelayMs: 1, MaxDelayMs: math.MaxInt64, MaxDelayMsSet: true, MaxRetries: 2,
		},
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-dropped:
		if err.Error() != "MCP server-to-client stream dropped and could not be reopened" {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("large maxDelay prevented retry exhaustion")
	}
	mu.Lock()
	attempts := gets
	mu.Unlock()
	if attempts != 3 {
		t.Fatalf("GET attempts = %d, want 3", attempts)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// pi v1.4.0 (4a080252), packages/mcp/src/transports/streamable-http.ts
// runGetStream: the "stayed up for a while counts as healthy" reset runs only
// after consumeSse resolves. A stream that breaks mid-flight skips it, so the
// attempt counter reaches maxRetries and the drop is reported instead of
// reconnecting forever.
func TestStreamableHTTPAbortedGetStreamsExhaustRetries(t *testing.T) {
	var mu sync.Mutex
	gets := 0
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			fixture.protocolHandler(w, r, nil)
			return
		}
		mu.Lock()
		gets++
		mu.Unlock()
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprint(w, "id: g1\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/tools/list_changed\"}\n\n")
		w.(http.Flusher).Flush()
		// Stay open past maxDelayMs so a completed stream would count as
		// healthy, then abort mid-chunk: the reader sees an error, not EOF.
		time.Sleep(25 * time.Millisecond)
		panic(http.ErrAbortHandler)
	})
	defer fixture.close(t)

	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	dropped := make(chan error, 16)
	client.OnError(func(err error) { dropped <- err })
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url,
		Reconnect: &StreamableHttpReconnectOptions{
			InitialDelayMs: 1, MaxDelayMs: 10, MaxDelayMsSet: true, MaxRetries: 2,
		},
	})); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-dropped:
		if err.Error() != "MCP server-to-client stream dropped and could not be reopened" {
			t.Fatalf("error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aborted GET streams never exhausted their retries")
	}
	mu.Lock()
	attempts := gets
	mu.Unlock()
	// Three failed attempts (the initial one plus MaxRetries) then give up.
	if attempts != 3 {
		t.Fatalf("GET attempts = %d, want 3", attempts)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	after := gets
	mu.Unlock()
	if after != attempts {
		t.Fatalf("GET attempts kept growing after the drop was reported: %d -> %d", attempts, after)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
