package ai

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/dat267/pier/internal/jsnum"
)

// Port of auth/oauth/openai-codex.ts: the OpenAI Codex (ChatGPT) OAuth flow,
// with both the browser (loopback) and device-code login methods.

const (
	OpenAICodexClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	OpenAICodexAuthBaseURL  = "https://auth.openai.com"
	OpenAICodexRedirectURI  = "http://localhost:1455/auth/callback"
	OpenAICodexScope        = "openid profile email offline_access"
	OpenAICodexJWTPath      = "https://api.openai.com/auth"
	OpenAICodexDeviceTTLSec = 15 * 60
	// OpenAICodexCallbackPort is the fixed loopback port of the browser flow.
	// PI_OAUTH_CALLBACK_PORT overrides it for a caller that needs its own port
	// (oauthCallbackPort).
	OpenAICodexCallbackPort = 1455
	// Login method ids for the select prompt.
	OpenAICodexBrowserLogin    = "browser"
	OpenAICodexDeviceCodeLogin = "device_code"
)

// openAICodexDeviceRedirectURI is the redirect used with device-code logins.
const openAICodexDeviceRedirectURI = OpenAICodexAuthBaseURL + "/deviceauth/callback"

// The auth endpoints are variables so tests can point them at a local server
// (upstream keeps constants and mocks fetch).
var (
	openAICodexAuthorizeURL      = OpenAICodexAuthBaseURL + "/oauth/authorize"
	openAICodexTokenURL          = OpenAICodexAuthBaseURL + "/oauth/token"
	openAICodexDeviceUserCodeURL = OpenAICodexAuthBaseURL + "/api/accounts/deviceauth/usercode"
	openAICodexDeviceTokenURL    = OpenAICodexAuthBaseURL + "/api/accounts/deviceauth/token"
	openAICodexDeviceVerifyURL   = OpenAICodexAuthBaseURL + "/codex/device"
)

// OpenAICodexTokenResponse is one token endpoint response.
type OpenAICodexTokenResponse struct {
	Access  string
	Refresh string
	Expires int64
}

// ParseOpenAICodexAuthorizationInput extracts the code/state from a pasted
// redirect URL, `code#state`, a query string, or a bare code.
func ParseOpenAICodexAuthorizationInput(input string) OAuthAuthorizationInput {
	return ParseAuthorizationInput(input)
}

// decodeJWT decodes a JWT payload using the WHATWG forgiving base64 alphabet
// (upstream atob), returning nil when the token is not a JWT.
func decodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	payload := parts[1]
	// WHATWG forgiving-base64 accepts both alphabets and omits padding.
	normalized := strings.NewReplacer("-", "+", "_", "/").Replace(payload)
	if remainder := len(normalized) % 4; remainder != 0 {
		normalized += strings.Repeat("=", 4-remainder)
	}
	decoded, err := base64.StdEncoding.DecodeString(normalized)
	if err != nil {
		return nil
	}
	var parsed map[string]any
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		return nil
	}
	return parsed
}

// GetOpenAICodexAccountID extracts the ChatGPT account id from an access token
// (upstream getAccountId).
func GetOpenAICodexAccountID(accessToken string) string {
	payload := decodeJWT(accessToken)
	if payload == nil {
		return ""
	}
	auth, ok := payload[OpenAICodexJWTPath].(map[string]any)
	if !ok {
		return ""
	}
	accountID, _ := auth["chatgpt_account_id"].(string)
	return accountID
}

// ReadOpenAICodexTokenResponse decodes a token endpoint response.
func ReadOpenAICodexTokenResponse(response *http.Response, operation string) (*OpenAICodexTokenResponse, error) {
	raw, readErr := readCappedDocument(response.Body, oauthDocumentBodyLimit)
	if response.StatusCode >= 400 {
		text := string(raw)
		if text == "" {
			text = response.Status
		}
		return nil, fmt.Errorf("OpenAI Codex token %s failed (%d): %s", operation, response.StatusCode, text)
	}
	if readErr != nil {
		return nil, readErr
	}
	var payload struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		ExpiresIn    *float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil || payload.AccessToken == "" ||
		payload.RefreshToken == "" || payload.ExpiresIn == nil {
		var generic any
		_ = json.Unmarshal(raw, &generic)
		encoded, _ := MarshalJSON(generic)
		return nil, fmt.Errorf("OpenAI Codex token %s response missing fields: %s", operation, encoded)
	}
	return &OpenAICodexTokenResponse{
		Access:  payload.AccessToken,
		Refresh: payload.RefreshToken,
		Expires: oauthExpiryFromLifetime(*payload.ExpiresIn, 0),
	}, nil
}

func openAICodexPostForm(ctx context.Context, requestURL string, fields map[string]string) (*http.Response, error) {
	form := url.Values{}
	for key, value := range fields {
		form.Set(key, value)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := doBoundedRequest(request)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, errLoginCancelled
		}
		return nil, err
	}
	return response, nil
}

// ExchangeOpenAICodexAuthorizationCode exchanges a code for tokens.
func ExchangeOpenAICodexAuthorizationCode(ctx context.Context, code, verifier, redirectURI string) (*OpenAICodexTokenResponse, error) {
	response, err := openAICodexPostForm(ctx, openAICodexTokenURL, map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     OpenAICodexClientID,
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  redirectURI,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	return ReadOpenAICodexTokenResponse(response, "exchange")
}

// RefreshOpenAICodexAccessToken refreshes the Codex access token.
func RefreshOpenAICodexAccessToken(ctx context.Context, refreshToken string) (*OpenAICodexTokenResponse, error) {
	response, err := openAICodexPostForm(ctx, openAICodexTokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     OpenAICodexClientID,
	})
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, errors.New("OpenAI Codex token refresh error: Login cancelled")
		}
		return nil, fmt.Errorf("OpenAI Codex token refresh error: %s", err.Error())
	}
	defer func() { _ = response.Body.Close() }()
	return ReadOpenAICodexTokenResponse(response, "refresh")
}

// OpenAICodexDeviceAuthInfo is one device authorization.
type OpenAICodexDeviceAuthInfo struct {
	DeviceAuthID    string
	UserCode        string
	IntervalSeconds int
}

// StartOpenAICodexDeviceAuth starts the Codex device authorization.
func StartOpenAICodexDeviceAuth(ctx context.Context) (*OpenAICodexDeviceAuthInfo, error) {
	body := strings.NewReader(`{"client_id":"` + OpenAICodexClientID + `"}`)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICodexDeviceUserCodeURL, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := doBoundedRequest(request)
	if err != nil {
		if ctx != nil && ctx.Err() != nil {
			return nil, errLoginCancelled
		}
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	raw, readErr := readCappedDocument(response.Body, oauthDocumentBodyLimit)
	if response.StatusCode >= 400 {
		if response.StatusCode == http.StatusNotFound {
			return nil, errors.New("OpenAI Codex device code login is not enabled for this server. " +
				"Use browser login or verify the server URL.")
		}
		message := fmt.Sprintf("OpenAI Codex device code request failed with status %d", response.StatusCode)
		if len(raw) > 0 {
			message += ": " + string(raw)
		}
		return nil, errors.New(message)
	}
	if readErr != nil {
		return nil, readErr
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		var generic any
		_ = json.Unmarshal(raw, &generic)
		encoded, _ := MarshalJSON(generic)
		return nil, fmt.Errorf("Invalid OpenAI Codex device code response: %s", encoded)
	}
	deviceAuthID, _ := payload["device_auth_id"].(string)
	userCode, _ := payload["user_code"].(string)
	// The interval may arrive as a string.
	var intervalSeconds float64
	hasInterval := false
	switch typed := payload["interval"].(type) {
	case float64:
		intervalSeconds, hasInterval = typed, true
	case string:
		// The reference uses Number(value.trim()), which accepts a radix prefix
		// and rejects a numeric separator; strconv.ParseFloat is the reverse.
		if parsed, ok := jsnum.Number(JSTrim(typed)); ok {
			intervalSeconds, hasInterval = parsed, true
		}
	}
	if deviceAuthID == "" || userCode == "" || !hasInterval || intervalSeconds < 0 {
		encoded, _ := MarshalJSON(payload)
		return nil, fmt.Errorf("Invalid OpenAI Codex device code response: %s", encoded)
	}
	return &OpenAICodexDeviceAuthInfo{
		DeviceAuthID: deviceAuthID, UserCode: userCode, IntervalSeconds: jsnum.ToInt(intervalSeconds),
	}, nil
}

// OpenAICodexDeviceTokenSuccess is the device authorization result.
type OpenAICodexDeviceTokenSuccess struct {
	AuthorizationCode string
	CodeVerifier      string
}

// PollOpenAICodexDeviceAuth polls the device token endpoint.
func PollOpenAICodexDeviceAuth(ctx context.Context, device *OpenAICodexDeviceAuthInfo) (*OpenAICodexDeviceTokenSuccess, error) {
	intervalSeconds := device.IntervalSeconds
	expiresIn := OpenAICodexDeviceTTLSec
	return PollOAuthDeviceCodeFlow(OAuthDeviceCodePollOptions[*OpenAICodexDeviceTokenSuccess]{
		IntervalSeconds:  &intervalSeconds,
		ExpiresInSeconds: &expiresIn,
		Ctx:              ctx,
		Poll: func() (OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess], error) {
			requestBody, _ := MarshalJSON(map[string]string{
				"device_auth_id": device.DeviceAuthID,
				"user_code":      device.UserCode,
			})
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, openAICodexDeviceTokenURL, strings.NewReader(string(requestBody)))
			if err != nil {
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "failed", Message: err.Error()}, nil
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := doBoundedRequest(request)
			if err != nil {
				if ctx != nil && ctx.Err() != nil {
					return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{
						Status: "failed", Message: "Login cancelled",
					}, nil
				}
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "failed", Message: err.Error()}, nil
			}
			defer func() { _ = response.Body.Close() }()
			raw, readErr := readCappedDocument(response.Body, oauthDocumentBodyLimit)
			if response.StatusCode < 400 && readErr != nil {
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "failed", Message: readErr.Error()}, nil
			}

			if response.StatusCode < 400 {
				var payload struct {
					AuthorizationCode string `json:"authorization_code"`
					CodeVerifier      string `json:"code_verifier"`
				}
				if err := json.Unmarshal(raw, &payload); err != nil || payload.AuthorizationCode == "" || payload.CodeVerifier == "" {
					var generic any
					_ = json.Unmarshal(raw, &generic)
					encoded, _ := MarshalJSON(generic)
					return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{
						Status:  "failed",
						Message: fmt.Sprintf("Invalid OpenAI Codex device auth token response: %s", encoded),
					}, nil
				}
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{
					Status: "complete",
					Value:  &OpenAICodexDeviceTokenSuccess{AuthorizationCode: payload.AuthorizationCode, CodeVerifier: payload.CodeVerifier},
				}, nil
			}
			if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound {
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "pending"}, nil
			}

			// The error code may be a string or nested under `error.code`.
			var errorCode string
			var parsedError map[string]any
			if err := json.Unmarshal(raw, &parsedError); err == nil {
				switch typed := parsedError["error"].(type) {
				case string:
					errorCode = typed
				case map[string]any:
					errorCode, _ = typed["code"].(string)
				}
			}
			switch errorCode {
			case "deviceauth_authorization_pending":
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "pending"}, nil
			case "slow_down":
				return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "slow_down"}, nil
			}
			message := fmt.Sprintf("OpenAI Codex device auth failed with status %d", response.StatusCode)
			if len(raw) > 0 {
				message += ": " + string(raw)
			}
			return OAuthDeviceCodePollResult[*OpenAICodexDeviceTokenSuccess]{Status: "failed", Message: message}, nil
		},
	})
}

// OpenAICodexAuthorizationFlow is the browser flow's PKCE state and URL.
type OpenAICodexAuthorizationFlow struct {
	Verifier string
	State    string
	URL      string
}

// CreateOpenAICodexAuthorizationFlow builds the authorize URL and state.
func CreateOpenAICodexAuthorizationFlow(originator string) (*OpenAICodexAuthorizationFlow, error) {
	if originator == "" {
		originator = "pi"
	}
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomHexState()
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(openAICodexAuthorizeURL)
	if err != nil {
		return nil, err
	}
	query := parsed.Query()
	query.Set("response_type", "code")
	query.Set("client_id", OpenAICodexClientID)
	query.Set("redirect_uri", openAICodexRedirectURI())
	query.Set("scope", OpenAICodexScope)
	query.Set("code_challenge", pkce.Challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("id_token_add_organizations", "true")
	query.Set("codex_cli_simplified_flow", "true")
	query.Set("originator", originator)
	parsed.RawQuery = query.Encode()
	return &OpenAICodexAuthorizationFlow{Verifier: pkce.Verifier, State: state, URL: parsed.String()}, nil
}

// OpenAICodexCallbackServer is the loopback callback server. When the fixed
// port is unavailable the server reports a null wait so the manual paste path
// takes over (upstream's listen-error handling).
type OpenAICodexCallbackServer struct {
	server      *http.Server
	unavailable bool
	oauthCallbackOutcome[string]
}

// StartOpenAICodexCallbackServer serves /auth/callback on the fixed port.
func StartOpenAICodexCallbackServer(expectedState string) *OpenAICodexCallbackServer {
	callback := &OpenAICodexCallbackServer{
		oauthCallbackOutcome: newOAuthCallbackOutcome[string](),
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", oauthCallbackHost(), oauthCallbackPort(OpenAICodexCallbackPort)))
	if err != nil {
		// Settled already: CancelWait (the manual-entry handoff) must not
		// close done a second time.
		callback.unavailable = true
		callback.settled = true
		close(callback.done)
		return callback
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(writer http.ResponseWriter, request *http.Request) {
		writeHTML := func(status int, body string) {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.Header().Set("cache-control", "no-store")
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(body))
		}
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.URL.Path != "/auth/callback" {
			writeHTML(http.StatusNotFound, OAuthErrorHTML("Callback route not found.", ""))
			return
		}
		if query.Get("state") != expectedState {
			writeHTML(http.StatusBadRequest, OAuthErrorHTML("State mismatch.", ""))
			return
		}
		if callback.isSettled() {
			writeHTML(http.StatusConflict, OAuthErrorHTML("This sign-in has already been handled.", ""))
			return
		}
		// A denied authorization ends the wait with a failure (shared
		// callback-server.ts), instead of leaving the login on the manual path.
		if errorCode := query.Get("error"); errorCode != "" {
			description := cmp.Or(query.Get("error_description"), errorCode)
			writeHTML(http.StatusBadRequest, OAuthErrorHTML("OpenAI authorization failed.", description))
			callback.fail(fmt.Errorf("OpenAI authorization failed: %s", description))
			return
		}
		code := query.Get("code")
		if code == "" {
			writeHTML(http.StatusBadRequest, OAuthErrorHTML("Missing authorization code.", ""))
			return
		}
		writeHTML(http.StatusOK, OAuthSuccessHTML("Signed in to OpenAI. You may now close this page."))
		callback.settle(code)
	})
	callback.server = newOAuthCallbackHTTPServer(mux)
	go func() { _ = callback.server.Serve(listener) }()
	return callback
}

// WaitForCode returns the callback code; ok is false when the wait was
// cancelled or the listener was unavailable.
func (s *OpenAICodexCallbackServer) WaitForCode() (string, bool) {
	if s.unavailable {
		return "", false
	}
	return s.wait()
}

// Close shuts the server down.
func (s *OpenAICodexCallbackServer) Close() { shutdownCallbackServer(s.server) }

// OpenAICodexCredentialsFromToken builds a credential, requiring the ChatGPT
// account id claim (upstream credentialsFromToken).
func OpenAICodexCredentialsFromToken(token *OpenAICodexTokenResponse) (*OAuthCredential, error) {
	accountID := GetOpenAICodexAccountID(token.Access)
	if accountID == "" {
		return nil, errCodexAccountID
	}
	return &OAuthCredential{
		Access: token.Access, Refresh: token.Refresh, Expires: token.Expires,
		Extra: map[string]json.RawMessage{"accountId": jsonRawString(accountID)}}, nil
}

// LoginOpenAICodexDeviceCode runs the device-code login path.
func LoginOpenAICodexDeviceCode(interaction *AuthInteraction) (*OAuthCredential, error) {
	if interaction == nil {
		return nil, errAuthInteractionRequired
	}
	ctx := interaction.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	device, err := StartOpenAICodexDeviceAuth(ctx)
	if err != nil {
		return nil, err
	}
	if interaction.Notify != nil {
		interaction.Notify(AuthEvent{
			Type: AuthEventDeviceCode, UserCode: device.UserCode,
			VerificationURI: openAICodexDeviceVerifyURL,
			IntervalSeconds: device.IntervalSeconds, ExpiresSeconds: OpenAICodexDeviceTTLSec,
		})
	}
	result, err := PollOpenAICodexDeviceAuth(ctx, device)
	if err != nil {
		return nil, err
	}
	token, err := ExchangeOpenAICodexAuthorizationCode(ctx, result.AuthorizationCode, result.CodeVerifier, openAICodexDeviceRedirectURI)
	if err != nil {
		return nil, err
	}
	return OpenAICodexCredentialsFromToken(token)
}

// LoginOpenAICodex runs the browser login path with the manual paste race.
func LoginOpenAICodex(interaction *AuthInteraction) (*OAuthCredential, error) {
	if interaction == nil {
		return nil, errAuthInteractionRequired
	}
	flow, err := CreateOpenAICodexAuthorizationFlow(interaction.AgentName)
	if err != nil {
		return nil, err
	}
	ctx := interaction.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	server := StartOpenAICodexCallbackServer(flow.State)
	defer server.Close()
	manualCtx, cancelManual := context.WithCancel(ctx)
	defer cancelManual()

	type manualResult struct {
		input string
		err   error
	}
	manualCh := make(chan manualResult, 1)
	go func() {
		input, perr := interaction.Prompt(AuthPrompt{
			Type:        AuthPromptManualCode,
			Ctx:         manualCtx,
			Message:     "Complete login in your browser, or paste the authorization code / redirect URL here:",
			Placeholder: openAICodexRedirectURI(),
		})
		manualCh <- manualResult{input: input, err: perr}
		server.CancelWait()
	}()

	if interaction.Notify != nil {
		interaction.Notify(AuthEvent{
			Type:         AuthEventAuthURL,
			URL:          flow.URL,
			Instructions: "A browser window should open. Complete login to finish.",
		})
	}

	var code string
	callbackCode, ok := server.WaitForCode()
	if ok && callbackCode != "" {
		cancelManual()
		code = callbackCode
	} else if lastErr := server.LastError(); lastErr != nil {
		return nil, lastErr
	} else {
		manual := <-manualCh
		if manual.err != nil {
			return nil, manual.err
		}
		parsed := ParseOpenAICodexAuthorizationInput(manual.input)
		if parsed.State != "" && parsed.State != flow.State {
			return nil, errors.New("State mismatch")
		}
		code = parsed.Code
	}
	if code == "" {
		return nil, errMissingAuthorizationCode
	}
	token, err := ExchangeOpenAICodexAuthorizationCode(ctx, code, flow.Verifier, openAICodexRedirectURI())
	if err != nil {
		return nil, err
	}
	return OpenAICodexCredentialsFromToken(token)
}

// RefreshOpenAICodexToken refreshes a Codex credential.
func RefreshOpenAICodexToken(ctx context.Context, refreshToken string) (*OAuthCredential, error) {
	token, err := RefreshOpenAICodexAccessToken(ctx, refreshToken)
	if err != nil {
		return nil, err
	}
	return OpenAICodexCredentialsFromToken(token)
}

// OpenAICodexOAuth is the Codex OAuth auth definition (upstream
// openaiCodexOAuth).
func OpenAICodexOAuth() *OAuthAuth {
	return &OAuthAuth{
		Name:           "OpenAI (ChatGPT Plus/Pro)",
		IsSubscription: true,
		LoginLabel:     "Sign in with ChatGPT",
		Login: func(interaction *AuthInteraction) (*OAuthCredential, error) {
			if interaction == nil {
				return nil, errAuthInteractionRequired
			}
			method, err := interaction.Prompt(AuthPrompt{
				Type:    AuthPromptSelect,
				Message: "Select OpenAI Codex login method:",
				SelectOptions: []AuthSelectOption{
					{ID: OpenAICodexBrowserLogin, Label: "Browser login (default)"},
					{ID: OpenAICodexDeviceCodeLogin, Label: "Device code login (headless)"},
				},
			})
			if err != nil {
				return nil, err
			}
			switch method {
			case OpenAICodexDeviceCodeLogin:
				return LoginOpenAICodexDeviceCode(interaction)
			case OpenAICodexBrowserLogin:
				return LoginOpenAICodex(interaction)
			default:
				return nil, fmt.Errorf("Unknown OpenAI Codex login method: %s", method)
			}
		},
		Refresh: refreshWithToken(RefreshOpenAICodexToken),
		ToAuth:  accessTokenAuth,
	}
}

// randomHexState returns 16 random bytes as hex (upstream createState).
func randomHexState() (string, error) {
	return randomHex(16)
}

// openAICodexRedirectURI is the loopback redirect the flow advertises, on the
// effective callback port (oauthCallbackPort).
func openAICodexRedirectURI() string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", oauthCallbackPort(OpenAICodexCallbackPort))
}
