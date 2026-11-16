package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/openai-chatgpt.ts races the
// callback against the manual prompt and aborts the prompt in its finally
// (`manualAbort.abort()`), so a login completed in the browser leaves no
// prompt waiting. The port waited on the callback server and never cancelled
// the prompt, which kept a goroutine blocked forever and left a stale prompt
// on the terminal.
func TestOpenAIChatGPTLoginCancelsTheManualPromptWhenTheCallbackWins(t *testing.T) {
	useFreeOAuthCallbackPort(t)
	token := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		_, _ = writer.Write([]byte(`{"access_token":"a","refresh_token":"r","id_token":"i",` +
			`"scope":"openid profile chatgpt.tokens.use.direct","expires_in":3600}`))
	}))
	defer token.Close()
	previous := OpenAIChatGPTTokenURL
	OpenAIChatGPTTokenURL = token.URL
	defer func() { OpenAIChatGPTTokenURL = previous }()

	promptStarted := make(chan struct{})
	promptCancelled := make(chan struct{})
	var authorizeURL string
	var authorizeMu chan struct{} = make(chan struct{}, 1)
	identified := make(chan string, 1)

	type loginResult struct {
		credential *OAuthCredential
		err        error
	}
	done := make(chan loginResult, 1)
	go func() {
		credential, err := LoginOpenAIChatGPT(&AuthInteraction{
			Ctx:         context.Background(),
			GetDeviceID: func() string { return "11111111-2222-3333-4444-555555555555" },
			Prompt: func(prompt AuthPrompt) (string, error) {
				close(promptStarted)
				if prompt.Ctx == nil {
					// The old flow passed no context, so nothing can cancel it.
					select {}
				}
				<-prompt.Ctx.Done()
				close(promptCancelled)
				return "", prompt.Ctx.Err()
			},
			Notify: func(event AuthEvent) {
				if event.Type == AuthEventAuthURL {
					authorizeURL = event.URL
					authorizeMu <- struct{}{}
				}
			},
		})
		done <- loginResult{credential: credential, err: err}
	}()

	select {
	case <-promptStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the manual prompt never started")
	}
	select {
	case <-authorizeMu:
	case <-time.After(5 * time.Second):
		t.Fatal("no authorize URL")
	}
	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	if state == "" {
		t.Fatal("authorize URL has no state")
	}
	callback := OpenAIChatGPTRedirectURI() + "?code=the-code&state=" + url.QueryEscape(state) + "&client_id=oaiapp_x"
	response, err := http.Get(callback)
	if err != nil {
		t.Fatalf("callback: %v", err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("callback status = %d", response.StatusCode)
	}

	var result loginResult
	select {
	case result = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the login did not finish after the callback")
	}
	if result.err != nil || result.credential == nil || result.credential.Access != "a" {
		t.Fatalf("login = %+v, %v", result.credential, result.err)
	}
	select {
	case <-promptCancelled:
		identified <- "ok"
	case <-time.After(5 * time.Second):
		t.Fatal("the manual prompt was not cancelled when the callback completed the login")
	}
}
