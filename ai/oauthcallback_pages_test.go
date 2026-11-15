package ai

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/callback-server.ts sendPage sets
// cache-control: no-store, answers a claimed or settled sign-in with 409, ends
// the wait with a failure when the provider redirects with ?error=, and writes
// "Signed in to <provider>. You may now close this page." on success. The Codex
// and ChatGPT servers differed on all four.
func TestOAuthCallbackPagesMatchTheSharedServer(t *testing.T) {
	fetch := func(t *testing.T, target string) (*http.Response, string) {
		t.Helper()
		response, err := http.Get(target)
		if err != nil {
			t.Fatalf("GET %s: %v", target, err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		return response, string(body)
	}

	t.Run("codex success", func(t *testing.T) {
		server := StartOpenAICodexCallbackServer("st-success")
		defer server.Close()
		response, body := fetch(t, OpenAICodexRedirectURI+"?code=the-code&state=st-success")
		if response.StatusCode != http.StatusOK ||
			!strings.Contains(body, "Signed in to OpenAI. You may now close this page.") {
			t.Fatalf("status %d body %q", response.StatusCode, body)
		}
		if got := response.Header.Get("cache-control"); got != "no-store" {
			t.Fatalf("cache-control = %q", got)
		}
		if code, ok := server.WaitForCode(); !ok || code != "the-code" {
			t.Fatalf("code = %q ok = %v", code, ok)
		}
	})

	t.Run("codex denied authorization", func(t *testing.T) {
		server := StartOpenAICodexCallbackServer("st-denied")
		defer server.Close()
		response, body := fetch(t, OpenAICodexRedirectURI+
			"?error=access_denied&error_description=The+user+denied+the+request&state=st-denied")
		if response.StatusCode != http.StatusBadRequest ||
			!strings.Contains(body, "OpenAI authorization failed.") ||
			!strings.Contains(body, "The user denied the request") {
			t.Fatalf("status %d body %q", response.StatusCode, body)
		}
		if got := response.Header.Get("cache-control"); got != "no-store" {
			t.Fatalf("cache-control = %q", got)
		}
		if _, ok := server.WaitForCode(); ok {
			t.Fatal("the wait did not end with a failure")
		}
		if err := server.LastError(); err == nil || err.Error() != "OpenAI authorization failed: The user denied the request" {
			t.Fatalf("lastErr = %v", err)
		}
		replay, _ := fetch(t, OpenAICodexRedirectURI+"?code=other&state=st-denied")
		if replay.StatusCode != http.StatusConflict {
			t.Fatalf("replay status = %d", replay.StatusCode)
		}
	})

	t.Run("chatgpt success", func(t *testing.T) {
		server, err := StartOpenAIChatGPTCallbackServer("st-chatgpt")
		if err != nil {
			t.Skipf("the fixed callback port is unavailable: %v", err)
		}
		defer server.Close()
		response, body := fetch(t, OpenAIChatGPTRedirectURI()+"?code=the-code&state=st-chatgpt&client_id=oaiapp_x")
		if response.StatusCode != http.StatusOK ||
			!strings.Contains(body, "Signed in to OpenAI. You may now close this page.") {
			t.Fatalf("status %d body %q", response.StatusCode, body)
		}
		if got := response.Header.Get("cache-control"); got != "no-store" {
			t.Fatalf("cache-control = %q", got)
		}
	})
}

// Reference: openai-codex.ts (b4afdddf) awaits the callback wait, whose
// rejection the login rethrows, so a denied authorization fails the login. The
// port fell through to the paste prompt and waited for input instead.
func TestLoginOpenAICodexFailsWhenTheBrowserDeniesAuthorization(t *testing.T) {
	authorize := make(chan string, 1)
	promptStarted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := LoginOpenAICodex(&AuthInteraction{
			Ctx: context.Background(),
			Prompt: func(prompt AuthPrompt) (string, error) {
				select {
				case <-promptStarted:
				default:
					close(promptStarted)
				}
				<-prompt.Ctx.Done()
				return "", prompt.Ctx.Err()
			},
			Notify: func(event AuthEvent) {
				if event.Type == AuthEventAuthURL {
					authorize <- event.URL
				}
			},
		})
		done <- err
	}()

	var authorizeURL string
	select {
	case authorizeURL = <-authorize:
	case <-time.After(5 * time.Second):
		t.Fatal("no authorize URL")
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	target := OpenAICodexRedirectURI + "?error=access_denied&error_description=Denied&state=" +
		url.QueryEscape(parsed.Query().Get("state"))
	response, getErr := http.Get(target)
	if getErr != nil {
		t.Fatalf("callback: %v", getErr)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()

	select {
	case loginErr := <-done:
		if loginErr == nil || loginErr.Error() != "OpenAI authorization failed: Denied" {
			t.Fatalf("login err = %v", loginErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the login did not fail after the denied authorization")
	}
}
