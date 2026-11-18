package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/dat267/pier/mcp/protocol"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

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

func TestStreamableHTTPDoesNotRetryCanceledRequestAfterAuthRecovery(t *testing.T) {
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	retried := make(chan struct{}, 1)
	var fixture *httpFixture
	fixture = newHTTPFixture(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var message struct {
			Method string `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		if message.Method == "tools/call" && r.Header.Get("Authorization") == "Bearer old" {
			w.Header().Set("www-authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if message.Method == "tools/call" {
			retried <- struct{}{}
		}
		fixture.protocolHandler(w, r, body)
	})
	defer fixture.close(t)

	token := "old"
	var tokenMu sync.Mutex
	auth := &replayAuthProvider{
		token: func() string {
			tokenMu.Lock()
			defer tokenMu.Unlock()
			return token
		},
		onUnauthorized: func(status int) {
			if status != http.StatusUnauthorized {
				return
			}
			close(refreshStarted)
			<-releaseRefresh
			tokenMu.Lock()
			token = "new"
			tokenMu.Unlock()
		},
	}
	client := NewClient(ClientOptions{Name: "http-test", Version: "1.0.0"})
	openGetStream := false
	if _, err := client.Connect(context.Background(), NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: fixture.url, OpenGetStream: &openGetStream, AuthProvider: auth,
	})); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	callDone := make(chan error, 1)
	go func() {
		_, err := client.CallTool(ctx, "echo", nil, RequestOptions{})
		callDone <- err
	}()
	select {
	case <-refreshStarted:
	case <-time.After(time.Second):
		t.Fatal("authorization recovery did not start")
	}
	cancel()
	select {
	case err := <-callDone:
		if err == nil {
			t.Fatal("canceled tool call succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("canceled tool call did not return")
	}
	close(releaseRefresh)
	select {
	case <-retried:
		t.Fatal("canceled tool call was sent after authorization recovered")
	case <-time.After(50 * time.Millisecond):
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type replayAuthProvider struct {
	token          func() string
	onUnauthorized func(status int)
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
	if _, ok := errors.AsType[*McpSessionExpiredError](err); !ok {
		t.Fatalf("err = %v (%T)", err, err)
	}
	if err := client.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// #10565: closing a session must not ask the auth provider for a token, because that call can
// refresh over the network before a DELETE for a session that is going away anyway. The DELETE
// reuses the token of the last request instead.
// streamable-http.ts buildHeaders starts from `new Headers(options.headers)` and
// `.set`s the session, protocol and auth headers, so those override configured
// ones case-insensitively instead of being sent twice.
func TestStreamableHTTPHeaderOverridesAreCaseInsensitive(t *testing.T) {
	transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
		URL: "https://example.test/mcp",
		Headers: map[string]string{
			"mcp-session-id":       "stale-session",
			"MCP-PROTOCOL-VERSION": "stale-version",
			"authorization":        "Bearer stale-token",
			"accept":               "text/plain",
		},
		AuthProvider: &countingAuthProvider{token: "fresh-token"},
	})
	transport.sessionID = "server-session"
	transport.protocolVersion = "2025-11-25"
	headers, _, err := transport.headers(map[string]string{"accept": "application/json"}, "")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"mcp-session-id":       "server-session",
		"mcp-protocol-version": "2025-11-25",
		"authorization":        "Bearer fresh-token",
		"accept":               "application/json",
	}
	for name, value := range want {
		count := 0
		for key, got := range headers {
			if strings.EqualFold(key, name) {
				count++
				if got != value {
					t.Fatalf("header %q = %q; want %q", key, got, value)
				}
			}
		}
		if count != 1 {
			t.Fatalf("header %q appears %d times; want exactly one case-insensitive entry", name, count)
		}
	}
}

// pi v1.4.0 (4a080252), transports/streamable-http.ts authorizedFetch discards
// the unauthorized response in finally, before retrying, even when recovery
// succeeds. AuthProvider does not own the transport's response-body cleanup.
func TestStreamableHTTPClosesUnauthorizedBodyBeforeRetry(t *testing.T) {
	for _, recoveryFails := range []bool{false, true} {
		name := "recovered"
		if recoveryFails {
			name = "failed-control"
		}
		t.Run(name, func(t *testing.T) {
			authErr := errors.New("auth failed")
			auth := &recoveryAuthProvider{}
			if recoveryFails {
				auth.err = authErr
			}
			body := &recoveryBody{Reader: strings.NewReader("authentication required")}
			calls := 0
			closedBeforeRetry := false
			transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
				URL: "https://invalid.test", AuthProvider: auth, OpenGetStream: new(false),
				Fetch: func(context.Context, *http.Request) (*http.Response, error) {
					calls++
					if calls == 1 {
						return &http.Response{StatusCode: http.StatusUnauthorized, Header: http.Header{}, Body: body}, nil
					}
					closedBeforeRetry = body.closes > 0
					return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
				},
			})
			if err := transport.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer transport.Close(context.Background())
			err := transport.Send(context.Background(), &protocol.JsonRpcNotification{JSONRPC: protocol.JSONRPCVersion, Method: "notifications/fixture"})
			if recoveryFails {
				if !errors.Is(err, authErr) || calls != 1 {
					t.Fatalf("failed recovery = %v, calls = %d", err, calls)
				}
			} else {
				if err != nil || calls != 2 {
					t.Fatalf("recovered Send = %v, calls = %d", err, calls)
				}
				if !closedBeforeRetry {
					t.Error("unauthorized response body remained open when retry dispatched")
				}
			}
			if body.closes != 1 {
				t.Fatalf("unauthorized body closes = %d, want 1", body.closes)
			}
		})
	}
}

// pi v1.4.0 (4a080252), transports/streamable-http.ts authorizedFetch checks
// the sender's signal after token acquisition and before calling fetch. Shared
// token acquisition is not canceled, but an abandoned call must not dispatch.
func TestStreamableHTTPCanceledTokenWaitDoesNotDispatch(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		name := "control"
		if cancelRequest {
			name = "canceled"
		}
		t.Run(name, func(t *testing.T) {
			auth := &heldTokenProvider{entered: make(chan struct{}), release: make(chan struct{})}
			calls := 0
			transport := NewStreamableHttpTransport(StreamableHttpTransportOptions{
				URL: "https://invalid.test", AuthProvider: auth, OpenGetStream: new(false),
				Fetch: func(context.Context, *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"jsonrpc":"2.0","id":1,"result":{}}`))}, nil
				},
			})
			if err := transport.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = transport.Close(context.Background()) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				done <- transport.Send(ctx, &protocol.JsonRpcRequest{JSONRPC: protocol.JSONRPCVersion, ID: protocol.NumberId(1), Method: "tools/call"})
			}()
			select {
			case <-auth.entered:
			case <-time.After(3 * time.Second):
				close(auth.release)
				t.Fatal("token lookup did not start")
			}
			if cancelRequest {
				cancel()
			}
			close(auth.release)
			select {
			case err := <-done:
				if cancelRequest && !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled Send = %v", err)
				}
				if !cancelRequest && err != nil {
					t.Fatalf("control Send = %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("Send did not settle after token release")
			}
			wantCalls := 1
			if cancelRequest {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatalf("fetch dispatches = %d, want %d", calls, wantCalls)
			}
		})
	}
}
