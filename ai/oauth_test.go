package ai

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Tests for auth/oauth/{pkce,device-code,oauth-page,anthropic}.ts.

func TestGeneratePKCE(t *testing.T) {
	pair, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	// 32 random bytes base64url without padding is 43 characters.
	if len(pair.Verifier) != 43 || strings.ContainsAny(pair.Verifier, "+/=") {
		t.Fatalf("verifier = %q", pair.Verifier)
	}
	sum := sha256.Sum256([]byte(pair.Verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if pair.Challenge != want {
		t.Fatalf("challenge = %q, want %q", pair.Challenge, want)
	}
	second, _ := GeneratePKCE()
	if second.Verifier == pair.Verifier {
		t.Fatal("verifiers must be random")
	}
}

func TestAbortableSleepAndDeviceFlow(t *testing.T) {
	fastDeviceCodeFlows(t)
	// A cancelled context aborts the sleep with the cancel message.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := AbortableSleep(ctx, time.Second, DeviceCodeCancelMessage); err == nil ||
		err.Error() != DeviceCodeCancelMessage {
		t.Fatalf("err = %v", err)
	}
	// A completed sleep returns nil.
	if err := AbortableSleep(context.Background(), time.Millisecond, DeviceCodeCancelMessage); err != nil {
		t.Fatalf("err = %v", err)
	}

	// The poll loop completes when the poll reports complete.
	polls := 0
	pending := 0
	value, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		Ctx: context.Background(),
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			polls++
			if polls < 2 {
				pending++
				return OAuthDeviceCodePollResult[string]{Status: "pending"}, nil
			}
			return OAuthDeviceCodePollResult[string]{Status: "complete", Value: "token"}, nil
		},
	})
	if err != nil || value != "token" || pending != 1 {
		t.Fatalf("value = %q err = %v polls = %d", value, err, polls)
	}

	// A failed poll surfaces its message.
	if _, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		Ctx: context.Background(),
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			return OAuthDeviceCodePollResult[string]{Status: "failed", Message: "denied"}, nil
		},
	}); err == nil || err.Error() != "denied" {
		t.Fatalf("err = %v", err)
	}

	// Expiry reports the timeout message; with a slow_down it reports the
	// clock-drift variant.
	zero := 0
	if _, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		ExpiresInSeconds: &zero,
		Ctx:              context.Background(),
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			return OAuthDeviceCodePollResult[string]{Status: "pending"}, nil
		},
	}); err == nil || err.Error() != DeviceCodeTimeoutMessage {
		t.Fatalf("err = %v", err)
	}
	one := 1
	if _, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		ExpiresInSeconds: &one,
		Ctx:              context.Background(),
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			interval := 1
			return OAuthDeviceCodePollResult[string]{Status: "slow_down", IntervalSeconds: &interval}, nil
		},
	}); err == nil || err.Error() != DeviceCodeSlowDownMessage {
		t.Fatalf("err = %v", err)
	}

	// A cancelled context aborts the loop.
	ctxCancel, cancelLoop := context.WithCancel(context.Background())
	cancelLoop()
	if _, err := PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[string]{
		Ctx: ctxCancel,
		Poll: func() (OAuthDeviceCodePollResult[string], error) {
			return OAuthDeviceCodePollResult[string]{Status: "pending"}, nil
		},
	}); err == nil || err.Error() != DeviceCodeCancelMessage {
		t.Fatalf("err = %v", err)
	}
}

func TestOAuthPageHTML(t *testing.T) {
	success := OAuthSuccessHTML("Done <now>")
	if !strings.Contains(success, "Authentication successful") || !strings.Contains(success, "Done &lt;now&gt;") {
		t.Fatalf("success page = %q", success)
	}
	if !strings.Contains(success, oauthLogoSVG) {
		t.Fatal("logo missing")
	}
	failure := OAuthErrorHTML("Failed & bad", "code=1")
	if !strings.Contains(failure, "Authentication failed") || !strings.Contains(failure, "Failed &amp; bad") ||
		!strings.Contains(failure, `class="details"`) {
		t.Fatalf("failure page = %q", failure)
	}
	// Details are omitted when absent.
	if strings.Contains(OAuthSuccessHTML("x"), `class="details"`) {
		t.Fatal("success page must not carry a details block")
	}
}

func TestParseAuthorizationInput(t *testing.T) {
	cases := []struct {
		input string
		code  string
		state string
	}{
		{"", "", ""},
		{"http://localhost:53692/callback?code=abc&state=verifier", "abc", "verifier"},
		{"http:///localhost:53692/callback?code=abc&state=verifier", "abc", "verifier"},
		{"abc#verifier", "abc", "verifier"},
		{"\uFEFFabc#verifier\uFEFF", "abc", "verifier"},
		{"code=abc&state=verifier", "abc", "verifier"},
		{"raw-code", "raw-code", ""},
		{"  http://localhost:53692/callback?code=abc  ", "abc", ""},
		// The reference parses any absolute URL, not only http(s), and reads the
		// query off it whatever the scheme.
		{"localhost:53692/callback?code=abc", "abc", ""},
		{"ftp://example.com/cb?code=abc", "abc", ""},
		// new URL() needs a ":" to read a scheme, so this reaches the code= branch;
		// URLSearchParams then reads "anything?code" as the KEY and code is absent.
		// Go's url.Parse instead took "anything" as a scheme, which also yielded no
		// code: same result, different reason.
		{"anything?code=abc", "", ""},
		{"1abc:x?code=1", "", ""},
		{"anything:?code=abc", "abc", ""},
		{"javascript:alert(1)", "", ""},
		{"mailto:x@y.z", "", ""},
		// JS `split("#", 2)` stops after the second part, so the rest is dropped.
		{"a#b#c", "a", "b"},
		// URLSearchParams tolerates a leading "?", which url.ParseQuery does not.
		{"?code=abc", "abc", ""},
		{"?code=abc&state=v", "abc", "v"},
	}
	for _, testCase := range cases {
		parsed := ParseAuthorizationInput(testCase.input)
		if parsed.Code != testCase.code || parsed.State != testCase.state {
			t.Errorf("ParseAuthorizationInput(%q) = %+v, want code=%q state=%q", testCase.input, parsed, testCase.code, testCase.state)
		}
	}
}

func TestAnthropicAuthorizeURL(t *testing.T) {
	authURL := AnthropicAuthorizeURL("challenge-value", "verifier-value", AnthropicRedirectURI)
	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "claude.ai" || parsed.Path != "/oauth/authorize" {
		t.Fatalf("url = %s", authURL)
	}
	query := parsed.Query()
	expectations := map[string]string{
		"code":                  "true",
		"client_id":             AnthropicOAuthClientID,
		"response_type":         "code",
		"redirect_uri":          AnthropicRedirectURI,
		"code_challenge":        "challenge-value",
		"code_challenge_method": "S256",
		"state":                 "verifier-value",
	}
	for key, want := range expectations {
		if got := query.Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(query.Get("scope"), "user:inference") {
		t.Fatalf("scope = %q", query.Get("scope"))
	}
}

func TestExchangeAndRefreshAnthropicToken(t *testing.T) {
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		raw, _ := readAll(request)
		_ = json.Unmarshal(raw, &captured)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`))
	}))
	defer server.Close()

	withTokenURL(t, server.URL, func() {
		credential, err := ExchangeAnthropicAuthorizationCode(context.Background(), "code-1", "state-1", "verifier-1", AnthropicRedirectURI)
		if err != nil {
			t.Fatal(err)
		}
		if credential.Access != "access-1" || credential.Refresh != "refresh-1" {
			t.Fatalf("credential = %+v", credential)
		}
		// The expiry is five minutes inside the server-provided lifetime.
		expected := time.Now().UnixMilli() + 3600*1000 - 5*60*1000
		if diff := credential.Expires - expected; diff > 5000 || diff < -5000 {
			t.Fatalf("expires off by %d ms", diff)
		}
		if captured["grant_type"] != "authorization_code" || captured["code_verifier"] != "verifier-1" ||
			captured["client_id"] != AnthropicOAuthClientID || captured["redirect_uri"] != AnthropicRedirectURI {
			t.Fatalf("body = %#v", captured)
		}

		refreshed, err := RefreshAnthropicToken(context.Background(), "refresh-1")
		if err != nil || refreshed.Access != "access-1" {
			t.Fatalf("refreshed = %+v err = %v", refreshed, err)
		}
		if captured["grant_type"] != "refresh_token" || captured["refresh_token"] != "refresh-1" {
			t.Fatalf("body = %#v", captured)
		}
	})

	// HTTP failures carry upstream's message shape.
	errorServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusBadRequest)
		_, _ = writer.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer errorServer.Close()
	withTokenURL(t, errorServer.URL, func() {
		_, err := ExchangeAnthropicAuthorizationCode(context.Background(), "c", "s", "v", AnthropicRedirectURI)
		if err == nil || !strings.Contains(err.Error(), "Token exchange request failed.") ||
			!strings.Contains(err.Error(), "invalid_grant") {
			t.Fatalf("err = %v", err)
		}
		_, err = RefreshAnthropicToken(context.Background(), "r")
		if err == nil || !strings.Contains(err.Error(), "Anthropic token refresh request failed.") {
			t.Fatalf("err = %v", err)
		}
	})

	// Invalid JSON is reported.
	badJSON := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte("not json"))
	}))
	defer badJSON.Close()
	withTokenURL(t, badJSON.URL, func() {
		_, err := ExchangeAnthropicAuthorizationCode(context.Background(), "c", "s", "v", AnthropicRedirectURI)
		if err == nil || !strings.Contains(err.Error(), "Token exchange returned invalid JSON.") {
			t.Fatalf("err = %v", err)
		}
		_, err = RefreshAnthropicToken(context.Background(), "r")
		if err == nil || !strings.Contains(err.Error(), "Anthropic token refresh returned invalid JSON.") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestLoginAnthropicManualCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"access_token":"manual-access","refresh_token":"manual-refresh","expires_in":60}`))
	}))
	defer server.Close()

	var notified []AuthEvent
	withTokenURL(t, server.URL, func() {
		credential, err := LoginAnthropic(&AuthInteraction{
			Ctx: context.Background(),
			Prompt: func(prompt AuthPrompt) (string, error) {
				if prompt.Type != AuthPromptManualCode {
					t.Errorf("prompt type = %s", prompt.Type)
				}
				// A bare code is accepted; the state defaults to the PKCE
				// verifier (upstream's behavior).
				return "manual-code", nil
			},
			Notify: func(event AuthEvent) { notified = append(notified, event) },
		})
		if err != nil {
			t.Fatal(err)
		}
		if credential.Access != "manual-access" {
			t.Fatalf("credential = %+v", credential)
		}
	})
	if len(notified) != 2 || notified[0].Type != AuthEventAuthURL ||
		!strings.Contains(notified[0].URL, "claude.ai/oauth/authorize") ||
		notified[1].Type != AuthEventProgress {
		t.Fatalf("notifications = %+v", notified)
	}
}

func TestAnthropicOAuthDefinition(t *testing.T) {
	auth := AnthropicOAuth()
	if auth.Name != "Anthropic (Claude Pro/Max)" || !auth.IsSubscription {
		t.Fatalf("auth = %+v", auth)
	}
	modelAuth, err := auth.ToAuth(&OAuthCredential{Access: "access-token"})
	if err != nil || modelAuth.APIKey != "access-token" {
		t.Fatalf("model auth = %+v err = %v", modelAuth, err)
	}
	if _, err := auth.ToAuth(nil); err == nil {
		t.Fatal("nil credentials must be rejected")
	}
	if _, err := auth.Refresh(context.Background(), nil); err == nil {
		t.Fatal("nil credentials must be rejected on refresh")
	}
}

func readAll(request *http.Request) ([]byte, error) {
	buffer := make([]byte, 0, 1024)
	chunk := make([]byte, 512)
	for {
		read, err := request.Body.Read(chunk)
		buffer = append(buffer, chunk[:read]...)
		if err != nil {
			return buffer, nil
		}
	}
}

// fastDeviceCodeFlows replaces the device-code poll sleep with a millisecond, so
// the flow tests do not spend their assertions' worth in RFC 8628's one-second
// minimum interval. What the tests below assert is which polls happen (pending,
// slow_down, complete, failures) and what the loop returns; the polling cadence
// itself is covered by TestAbortableSleepAndDeviceFlow, which keeps the real
// sleep. Tests in this package do not run in parallel, so the swap is safe.
func fastDeviceCodeFlows(t *testing.T) {
	t.Helper()
	previous, previousNow := deviceCodeSleep, deviceCodeNow
	// The fake clock advances by each requested sleep, so expiry is reached
	// on schedule without waiting for it.
	var slept atomic.Int64
	start := time.Now()
	deviceCodeNow = func() time.Time { return start.Add(time.Duration(slept.Load())) }
	deviceCodeSleep = func(ctx context.Context, wait time.Duration, cancelMessage string) error {
		if err := AbortableSleep(ctx, time.Millisecond, cancelMessage); err != nil {
			return err
		}
		slept.Add(int64(wait))
		return nil
	}
	t.Cleanup(func() { deviceCodeSleep, deviceCodeNow = previous, previousNow })
}

// fastOAuthRetries records the delays the OAuth backoff loops request and
// returns immediately, so a retry test asserts the schedule instead of
// sleeping it. The expected values are the production backoff formulas
// (Copilot 500ms*(1<<retry), Kimi 1000ms*(1<<(attempt-1))).
func fastOAuthRetries(t *testing.T) *[]time.Duration {
	t.Helper()
	previous := oauthRetrySleep
	delays := &[]time.Duration{}
	oauthRetrySleep = func(_ context.Context, duration time.Duration, _ string) error {
		*delays = append(*delays, duration)
		return nil
	}
	t.Cleanup(func() { oauthRetrySleep = previous })
	return delays
}

// Copilot's 429 retry parses retry-after with Number.parseFloat and gives up
// when the resulting delay is not finite (auth/oauth/github-copilot.ts).
func TestCopilotRetryAfterUsesJavaScriptParseFloat(t *testing.T) {
	delays := fastOAuthRetries(t)
	respond := func(header string) (*httptest.Server, *int) {
		calls := new(int)
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			*calls++
			writer.Header().Set("retry-after", header)
			writer.WriteHeader(http.StatusTooManyRequests)
		}))
		return server, calls
	}
	buildRequest := func(url string) func(context.Context) (*http.Request, error) {
		return func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		}
	}

	// A non-finite delay returns the 429 instead of retrying.
	for _, value := range []string{"Infinity", "-Infinity", "NaN"} {
		server, calls := respond(value)
		response, err := FetchWithCopilotRateLimitRetry(context.Background(), server.URL, buildRequest(server.URL),
			CopilotRetryPolicy{MaxRetries: 2, MaxElapsedMS: 60_000})
		if err != nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("retry-after %q: response = %v err = %v", value, response, err)
		}
		response.Body.Close()
		if *calls != 1 {
			t.Fatalf("retry-after %q: calls = %d, want the first 429 returned", value, *calls)
		}
		server.Close()
	}
	if len(*delays) != 0 {
		t.Fatalf("non-finite retry-after slept: %v", *delays)
	}

	// parseFloat takes a numeric prefix: "5abc" is 5 seconds.
	server, calls := respond("5abc")
	response, err := FetchWithCopilotRateLimitRetry(context.Background(), server.URL, buildRequest(server.URL),
		CopilotRetryPolicy{MaxRetries: 1, MaxElapsedMS: 60_000})
	if err != nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("prefix retry-after: response = %v err = %v", response, err)
	}
	response.Body.Close()
	server.Close()
	if *calls != 2 {
		t.Fatalf("prefix retry-after calls = %d, want one retry", *calls)
	}
	if len(*delays) != 1 || (*delays)[0] != 5*time.Second {
		t.Fatalf("prefix retry-after delays = %v, want 5s", *delays)
	}
}

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/github-copilot.ts builds the
// credential as { ..., enterpriseUrl: enterpriseDomain }: for github.com the
// domain is undefined and JSON.stringify drops the key. parseGitHubCopilotModelCatalog
// returns availableModelIds as an array, so an account without picker models
// stores [] rather than null. Pier wrote "enterpriseUrl": "" and stored null.
func TestGitHubCopilotCredentialExtraFieldsMatchTheReference(t *testing.T) {
	fastDeviceCodeFlows(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/login/device/code":
			_, _ = writer.Write([]byte(`{"device_code":"dev","user_code":"CODE","verification_uri":"https://github.com/login/device","interval":0,"expires_in":900}`))
		case "/login/oauth/access_token":
			_, _ = writer.Write([]byte(`{"access_token":"gho_token"}`))
		case "/copilot_internal/v2/token":
			_, _ = writer.Write([]byte(`{"token":"tid=1;proxy-ep=proxy.individual.githubcopilot.com;","expires_at":1000000}`))
		case "/models":
			_, _ = writer.Write([]byte(`{"data":[]}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	withGitHubDomain(t, server.URL, func(domain string) {
		credential, err := LoginGitHubCopilot(&AuthInteraction{
			Ctx:    context.Background(),
			Prompt: func(AuthPrompt) (string, error) { return "", nil },
		})
		if err != nil {
			t.Fatal(err)
		}
		if raw, ok := credential.Extra[CopilotEnterpriseURLKey]; ok {
			t.Fatalf("github.com login stored enterpriseUrl %s", raw)
		}
		if got := string(credential.Extra[CopilotAvailableModelKey]); got != "[]" {
			t.Fatalf("login availableModelIds = %s", got)
		}

		refreshed, err := RefreshGitHubCopilotToken(context.Background(), "gho_token", "")
		if err != nil {
			t.Fatal(err)
		}
		if raw, ok := refreshed.Extra[CopilotEnterpriseURLKey]; ok {
			t.Fatalf("github.com refresh stored enterpriseUrl %s", raw)
		}
		if got := string(refreshed.Extra[CopilotAvailableModelKey]); got != "[]" {
			t.Fatalf("refresh availableModelIds = %s", got)
		}
	})
}

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
