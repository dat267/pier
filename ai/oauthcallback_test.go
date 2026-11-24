package ai

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Tests for auth/oauth/{pkce,device-code,oauth-page,anthropic}.ts.

func TestOAuthCallbackServer(t *testing.T) {
	server, err := StartOAuthCallbackServer("expected-state", AnthropicCallbackPort)
	if err != nil {
		t.Skipf("loopback callback port unavailable: %v", err)
	}
	defer server.Close()
	if server.RedirectURI != AnthropicRedirectURI {
		t.Fatalf("redirect uri = %s", server.RedirectURI)
	}

	// A successful callback serves the success page and settles the wait.
	go func() {
		response, err := http.Get(AnthropicRedirectURI + "?code=abc&state=expected-state")
		if err == nil {
			response.Body.Close()
		}
	}()
	result, ok := server.WaitForCode(context.Background())
	if !ok || result.Code != "abc" || result.State != "expected-state" {
		t.Fatalf("result = %+v ok = %v", result, ok)
	}

	// A state mismatch serves the failure page and does not settle.
	server2, err := StartOAuthCallbackServer("expected-2", AnthropicCallbackPort)
	if err != nil {
		t.Skipf("loopback callback port unavailable: %v", err)
	}
	defer server2.Close()
	response, err := http.Get(AnthropicRedirectURI + "?code=abc&state=wrong")
	if err != nil {
		t.Fatal(err)
	}
	body := make([]byte, 4096)
	read, _ := response.Body.Read(body)
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body[:read]), "Authentication failed") {
		t.Fatalf("status = %d body = %q", response.StatusCode, body[:read])
	}
	server2.CancelWait()
	if _, ok := server2.WaitForCode(context.Background()); ok {
		t.Fatal("a cancelled wait must not report a code")
	}
}

// Matches pinned pi/packages/ai/src/auth/oauth/callback-server.ts, which aborts
// the concurrent manual prompt when a browser callback wins.
func TestLoginAnthropicCallbackUnblocksManualPrompt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"access_token":"callback-access","refresh_token":"callback-refresh","expires_in":60}`))
	}))
	defer server.Close()

	promptStarted := make(chan struct{})
	promptRelease := make(chan struct{})
	promptReturned := make(chan struct{})
	authURL := make(chan string, 1)
	loginDone := make(chan error, 1)
	withTokenURL(t, server.URL, func() {
		go func() {
			_, err := LoginAnthropic(&AuthInteraction{
				Ctx: context.Background(),
				Prompt: func(prompt AuthPrompt) (string, error) {
					close(promptStarted)
					select {
					case <-prompt.Ctx.Done():
						close(promptReturned)
						return "", errors.New("prompt cancelled after callback")
					case <-promptRelease:
						close(promptReturned)
						return "", errors.New("prompt was not cancelled")
					}
				},
				Notify: func(event AuthEvent) {
					if event.Type == AuthEventAuthURL {
						authURL <- event.URL
					}
				},
			})
			loginDone <- err
		}()

		select {
		case <-promptStarted:
		case <-time.After(2 * time.Second):
			close(promptRelease)
			t.Fatal("manual prompt did not start")
		}
		loginURL := <-authURL
		parsed, err := url.Parse(loginURL)
		if err != nil {
			t.Fatal(err)
		}
		redirectURI, err := url.Parse(parsed.Query().Get("redirect_uri"))
		if err != nil {
			t.Fatal(err)
		}
		callbackURL := redirectURI.String() + "?code=browser-code&state=" + url.QueryEscape(parsed.Query().Get("state"))
		response, err := http.Get(callbackURL)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if err := <-loginDone; err != nil {
			t.Fatalf("LoginAnthropic: %v", err)
		}
		select {
		case <-promptReturned:
		case <-time.After(100 * time.Millisecond):
			close(promptRelease)
			<-promptReturned
			t.Fatal("browser callback returned without unblocking the manual prompt")
		}
	})
}

// withTokenURL points the Anthropic token endpoint at a test server.
func withTokenURL(t *testing.T, url string, run func()) {
	t.Helper()
	original := anthropicTokenURLValue
	anthropicTokenURLValue = url
	defer func() { anthropicTokenURLValue = original }()
	run()
}

// A server bound to port 0 has to advertise the port it actually got: that URI is
// what the browser is sent back to.
func TestOAuthCallbackServerAdvertisesTheBoundPort(t *testing.T) {
	server, err := StartOAuthCallbackServer("state", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	parsed, err := url.Parse(server.RedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hostname() != "localhost" || parsed.Path != "/callback" {
		t.Fatalf("redirect uri = %s", server.RedirectURI)
	}
	port := parsed.Port()
	if port == "" || port == "0" {
		t.Fatalf("redirect uri names no bound port: %s", server.RedirectURI)
	}

	// The advertised URI is the one that answers.
	go func() {
		response, err := http.Get(server.RedirectURI + "?code=abc&state=state")
		if err == nil {
			response.Body.Close()
		}
	}()
	result, ok := server.WaitForCode(context.Background())
	if !ok || result.Code != "abc" {
		t.Fatalf("result = %+v ok = %v", result, ok)
	}
}

// The preferred port keeps the callback forwardable into a container or over SSH, so
// the login only moves to a free port when it cannot be bound (#10571). The browser
// then comes back to the free port, which is why the redirect URI follows it.
func TestAnthropicCallbackServerFallsBackToAFreePort(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:"+itoaAnthropicPort())
	if err != nil {
		t.Skipf("port %d is already in use, so this cannot be set up: %v", AnthropicCallbackPort, err)
	}
	defer blocker.Close()

	server := startAnthropicCallbackServer("state")
	if server == nil {
		t.Fatal("no callback server was started")
	}
	defer server.Close()
	if server.RedirectURI == AnthropicRedirectURI {
		t.Fatalf("the occupied port was advertised anyway: %s", server.RedirectURI)
	}
	go func() {
		response, err := http.Get(server.RedirectURI + "?code=fallback&state=state")
		if err == nil {
			response.Body.Close()
		}
	}()
	result, ok := server.WaitForCode(context.Background())
	if !ok || result.Code != "fallback" {
		t.Fatalf("result = %+v ok = %v", result, ok)
	}
}

func itoaAnthropicPort() string { return fmt.Sprintf("%d", AnthropicCallbackPort) }

// The callback servers listen on loopback, but any local process can connect and
// send nothing; without ReadHeaderTimeout that connection holds a handler
// goroutine and a file descriptor for as long as it likes (gosec G112).
func TestOAuthCallbackServerDropsAStalledConnection(t *testing.T) {
	previous := oauthCallbackReadHeaderTimeout
	oauthCallbackReadHeaderTimeout = 50 * time.Millisecond
	t.Cleanup(func() { oauthCallbackReadHeaderTimeout = previous })

	server, err := StartOAuthCallbackServer("state", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	parsed, err := url.Parse(server.RedirectURI)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", net.JoinHostPort(oauthCallbackHost(), parsed.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("the server answered a connection that sent no request")
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("the server kept a stalled connection open for %v", 2*time.Second)
		}
	}
}

// useFreeOAuthCallbackPort gives one test its own loopback port through
// PI_OAUTH_CALLBACK_PORT. The isolated gate runs each test in its own process,
// several at a time, and the providers' fixed port (1455, shared by the Codex and
// ChatGPT flows) cannot be bound twice: without an override those processes open
// the port, take each other's callbacks and fail as if the flow had hung.
func useFreeOAuthCallbackPort(t *testing.T) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find a free callback port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("release the callback port: %v", err)
	}
	t.Setenv("PI_OAUTH_CALLBACK_PORT", strconv.Itoa(port))
}

// The override changes both the bound port and the advertised redirect URI, which
// the provider echoes back.
func TestOAuthCallbackPortOverride(t *testing.T) {
	t.Setenv("PI_OAUTH_CALLBACK_PORT", "23456")
	if got := oauthCallbackPort(OpenAICodexCallbackPort); got != 23456 {
		t.Fatalf("port = %d, want the override", got)
	}
	if got := openAICodexRedirectURI(); got != "http://localhost:23456/auth/callback" {
		t.Fatalf("codex redirect = %q", got)
	}
	if got := OpenAIChatGPTRedirectURI(); got != "http://"+OpenAIChatGPTCallbackHost+":23456"+OpenAIChatGPTCallbackPath {
		t.Fatalf("chatgpt redirect = %q", got)
	}
	t.Setenv("PI_OAUTH_CALLBACK_PORT", "not-a-port")
	if got := oauthCallbackPort(OpenAICodexCallbackPort); got != OpenAICodexCallbackPort {
		t.Fatalf("port = %d, want the fixed port for an invalid override", got)
	}
	t.Setenv("PI_OAUTH_CALLBACK_PORT", "")
	if got := oauthCallbackPort(OpenAICodexCallbackPort); got != OpenAICodexCallbackPort {
		t.Fatalf("port = %d, want the fixed port when unset", got)
	}
}
