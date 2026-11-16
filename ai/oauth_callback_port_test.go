package ai

import (
	"net"
	"strconv"
	"testing"
)

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
