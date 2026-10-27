package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Public exchangeAuthorizationCode cases from pi v1.4.0 oauth/flow.ts.
// Regenerate: node --experimental-transform-types mcp/oauth/testdata/token-auth/generate.mjs ./pi > mcp/oauth/testdata/token-auth/fixtures.json
func TestTokenClientAuthenticationAgainstUpstream(t *testing.T) {
	data, err := os.ReadFile("testdata/token-auth/fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name      string
		Client    OAuthClientInformationFull
		Supported []string
		Resource  *string
		Request   *struct {
			Authorization *string
			Body          string
			Params        map[string]string
		}
		Error string
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			fetched := false
			options := ExchangeOptions{
				ClientInformation: &fixture.Client, Resource: fixture.Resource,
				Fetch: func(_ context.Context, request *http.Request) (*http.Response, error) {
					fetched = true
					if fixture.Request == nil {
						t.Fatal("unexpected token request")
					}
					if err := request.ParseForm(); err != nil {
						t.Fatal(err)
					}
					params := map[string]string{}
					for key := range request.PostForm {
						params[key] = request.PostForm.Get(key)
					}
					if !reflect.DeepEqual(params, fixture.Request.Params) {
						t.Errorf("params = %v, upstream = %v", params, fixture.Request.Params)
					}
					wantAuthorization := ""
					if fixture.Request.Authorization != nil {
						wantAuthorization = *fixture.Request.Authorization
					}
					if got := request.Header.Get("Authorization"); got != wantAuthorization {
						t.Error("Authorization differs from the source-generated fixture")
					}
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"access_token":"fixture-token","token_type":"Bearer"}`))}, nil
				},
				Code: "fixture-code", CodeVerifier: "fixture-verifier", RedirectURL: "http://127.0.0.1/callback",
			}
			if fixture.Supported != nil {
				options.Metadata = &AuthorizationServerMetadata{
					TokenEndpoint: "https://auth.example/token", TokenEndpointAuthMethodsSupported: fixture.Supported,
				}
			}
			_, err := ExchangeAuthorizationCode(context.Background(), "https://auth.example", options)
			if fixture.Error != "" {
				if err == nil || err.Error() != fixture.Error {
					t.Fatalf("error = %v, upstream = %q", err, fixture.Error)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if fetched != (fixture.Request != nil) {
				t.Fatalf("request sent = %v, upstream = %v", fetched, fixture.Request != nil)
			}
		})
	}
}
