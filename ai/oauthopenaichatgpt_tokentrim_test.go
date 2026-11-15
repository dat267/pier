package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/openai-chatgpt.ts requireTokenString
// rejects a value whose trim() is empty, and exchangeAuthorizationCode does the
// same for id_token. Pierce only compared against "", so a token endpoint that
// answered " " passed validation and was stored.
func TestOpenAIChatGPTTokenValidationRejectsBlankStrings(t *testing.T) {
	valid := func() *openAIChatGPTTokenResponse {
		return &openAIChatGPTTokenResponse{
			AccessToken: "a", RefreshToken: "r", IDToken: "i",
			Scope: "openid " + OpenAIChatGPTDirectTokenScope, ExpiresIn: 3600, HasExpiresInFlag: true,
		}
	}
	for _, test := range []struct {
		name   string
		mutate func(*openAIChatGPTTokenResponse)
		want   string
	}{
		{"access_token", func(token *openAIChatGPTTokenResponse) { token.AccessToken = " \t" }, "invalid access_token"},
		{"refresh_token", func(token *openAIChatGPTTokenResponse) { token.RefreshToken = " " }, "invalid refresh_token"},
		{"scope", func(token *openAIChatGPTTokenResponse) { token.Scope = "  " }, "invalid scope"},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := valid()
			test.mutate(token)
			_, err := openAIChatGPTCredentialFromToken(token, "oaiapp_x")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = io.Copy(io.Discard, request.Body)
		_, _ = writer.Write([]byte(`{"access_token":"a","refresh_token":"r","id_token":"  ",` +
			`"scope":"openid chatgpt.tokens.use.direct","expires_in":3600}`))
	}))
	defer server.Close()
	previous := OpenAIChatGPTTokenURL
	OpenAIChatGPTTokenURL = server.URL
	defer func() { OpenAIChatGPTTokenURL = previous }()
	_, err := ExchangeOpenAIChatGPTAuthorizationCode(context.Background(), "code", "verifier", "oaiapp_x")
	if err == nil || !strings.Contains(err.Error(), "did not contain an ID token") {
		t.Fatalf("err = %v, want the ID token error", err)
	}
}
