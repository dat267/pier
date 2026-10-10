package ai

// Port of auth/oauth/openai-chatgpt.ts: OpenAI Responses API token sharing
// through "Sign in with ChatGPT". This public-client flow registers a new
// client per login (client_id=dynamic_agent_client) and OpenAI returns the
// issued client id in the callback, which the token exchange and refresh use.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// OpenAIChatGPTDynamicClientID registers a new client on every login;
	// OpenAI returns the issued client id in the callback.
	OpenAIChatGPTDynamicClientID  = "dynamic_agent_client"
	OpenAIChatGPTAgentNameHint    = "Pi"
	OpenAIChatGPTAuthorizeURL     = "https://auth.openai.com/api/accounts/authorize"
	OpenAIChatGPTTokenURL         = "https://auth.openai.com/api/accounts/oauth/token"
	OpenAIChatGPTResource         = "https://api.openai.com/v1"
	OpenAIChatGPTCallbackPort     = 1455
	OpenAIChatGPTCallbackPath     = "/auth/callback"
	OpenAIChatGPTCallbackHost     = "127.0.0.1"
	OpenAIChatGPTCredentialMargin = 3 * 60 * 1000
	// OpenAIChatGPTDirectTokenScope must be granted for the token to be sent
	// directly to api.openai.com.
	OpenAIChatGPTDirectTokenScope = "chatgpt.tokens.use.direct"
	OpenAIChatGPTScope            = "openid profile email offline_access resource.invoke " + OpenAIChatGPTDirectTokenScope
)

// OpenAIChatGPTUsageURL is where a subscription-limit error points
// (upstream CHATGPT_USAGE_URL).
const OpenAIChatGPTUsageURL = "https://chatgpt.com/settings/usage"

// OpenAIChatGPTRedirectURI is the loopback redirect the dynamic client is
// registered against.
func OpenAIChatGPTRedirectURI() string {
	return fmt.Sprintf("http://%s:%d%s", OpenAIChatGPTCallbackHost, OpenAIChatGPTCallbackPort, OpenAIChatGPTCallbackPath)
}

// openAIChatGPTAgentHostID maps an installation UUID to OpenAI's agent host
// URI, rejecting a missing or malformed id (upstream agentHostId).
func openAIChatGPTAgentHostID(deviceID string) (string, error) {
	if !isUUID(deviceID) {
		return "", fmt.Errorf("Sign in with ChatGPT requires a device ID (UUID) for this installation")
	}
	return "urn:uuid:" + strings.ToLower(deviceID), nil
}

// isUUID reports whether value is an 8-4-4-4-12 hex UUID.
func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, char := range value {
		switch index {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') || (char >= 'A' && char <= 'F')) {
				return false
			}
		}
	}
	return true
}

// OpenAIChatGPTAuthorizationFlow is one pending authorization.
type OpenAIChatGPTAuthorizationFlow struct {
	Verifier string
	State    string
	Nonce    string
	URL      string
}

// CreateOpenAIChatGPTAuthorizationFlow builds the authorization URL for a
// device id.
func CreateOpenAIChatGPTAuthorizationFlow(deviceID string, agentName ...string) (*OpenAIChatGPTAuthorizationFlow, error) {
	hostID, err := openAIChatGPTAgentHostID(deviceID)
	if err != nil {
		return nil, err
	}
	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, err
	}
	state, err := randomHexState()
	if err != nil {
		return nil, err
	}
	nonce, err := randomHexState()
	if err != nil {
		return nil, err
	}
	parsed, err := url.Parse(OpenAIChatGPTAuthorizeURL)
	if err != nil {
		return nil, err
	}
	agentNameHint := OpenAIChatGPTAgentNameHint
	if len(agentName) > 0 && agentName[0] != "" {
		agentNameHint = agentName[0]
	}
	query := parsed.Query()
	query.Set("client_id", OpenAIChatGPTDynamicClientID)
	query.Set("agent_name_hint", agentNameHint)
	query.Set("ext_agent_host_id", hostID)
	query.Set("response_type", "code")
	query.Set("redirect_uri", OpenAIChatGPTRedirectURI())
	query.Set("resource", OpenAIChatGPTResource)
	query.Set("scope", OpenAIChatGPTScope)
	query.Set("state", state)
	query.Set("code_challenge", pkce.Challenge)
	query.Set("code_challenge_method", "S256")
	query.Set("nonce", nonce)
	parsed.RawQuery = query.Encode()
	return &OpenAIChatGPTAuthorizationFlow{Verifier: pkce.Verifier, State: state, Nonce: nonce, URL: parsed.String()}, nil
}

// OpenAIChatGPTAuthorizationResult is a callback's code plus the issued client
// id.
type OpenAIChatGPTAuthorizationResult struct {
	Code     string
	ClientID string
}

// OpenAIChatGPTCallbackServer is the loopback callback server for the fixed
// port. A held port fails the login: the browser's callback would otherwise
// reach whatever else holds it (another pending login or the Codex CLI) and be
// rejected as a state mismatch.
type OpenAIChatGPTCallbackServer struct {
	server      *http.Server
	listener    net.Listener
	expectedURL *url.URL

	mu      sync.Mutex
	settled bool
	lastErr error
	result  chan OpenAIChatGPTAuthorizationResult
	failure chan error
	done    chan struct{}
}

// StartOpenAIChatGPTCallbackServer serves the ChatGPT callback on the fixed
// port.
func StartOpenAIChatGPTCallbackServer(expectedState string) (*OpenAIChatGPTCallbackServer, error) {
	expected, _ := url.Parse(OpenAIChatGPTRedirectURI())
	callback := &OpenAIChatGPTCallbackServer{
		expectedURL: expected,
		result:      make(chan OpenAIChatGPTAuthorizationResult, 1),
		failure:     make(chan error, 1),
		done:        make(chan struct{}),
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", oauthCallbackHost(), OpenAIChatGPTCallbackPort))
	if err != nil {
		if isAddressInUseError(err) {
			return nil, fmt.Errorf("Port %d is in use, probably by an unfinished login in another pi session or by the Codex CLI. Cancel that login and try again.", OpenAIChatGPTCallbackPort)
		}
		return nil, err
	}
	callback.listener = listener
	mux := http.NewServeMux()
	mux.HandleFunc(OpenAIChatGPTCallbackPath, func(writer http.ResponseWriter, request *http.Request) {
		writeHTML := func(status int, body string) {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.WriteHeader(status)
			_, _ = writer.Write([]byte(body))
		}
		parsed, err := url.Parse(request.URL.String())
		if err != nil {
			writeHTML(http.StatusBadRequest, OAuthErrorHTML("Invalid callback.", ""))
			return
		}
		if errorCode := parsed.Query().Get("error"); errorCode != "" {
			writeHTML(http.StatusBadRequest, OAuthErrorHTML("ChatGPT was not connected.", "Error: "+errorCode))
			callback.fail(fmt.Errorf("ChatGPT authorization failed: %s", errorCode))
			return
		}
		result, err := authorizationResultFromCallback(parsed, expectedState)
		if err != nil {
			writeHTML(http.StatusBadRequest, OAuthErrorHTML(err.Error(), ""))
			return
		}
		writeHTML(http.StatusOK, OAuthSuccessHTML("ChatGPT authentication completed. You can close this window."))
		callback.settle(result)
	})
	callback.server = &http.Server{Handler: mux}
	go func() { _ = callback.server.Serve(listener) }()
	return callback, nil
}

func authorizationResultFromCallback(parsed *url.URL, expectedState string) (OpenAIChatGPTAuthorizationResult, error) {
	code := parsed.Query().Get("code")
	if code == "" {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("Missing authorization code")
	}
	state := parsed.Query().Get("state")
	if state == "" {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("Missing OAuth state")
	}
	if state != expectedState {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("OAuth state mismatch")
	}
	clientID := strings.TrimSpace(parsed.Query().Get("client_id"))
	if clientID == "" {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("OpenAI OAuth registration callback did not contain an issued client ID")
	}
	return OpenAIChatGPTAuthorizationResult{Code: code, ClientID: clientID}, nil
}

// ParseOpenAIChatGPTAuthorizationInput extracts the result from a pasted
// callback URL, rejecting a URL that is not this flow's redirect URI.
func ParseOpenAIChatGPTAuthorizationInput(input string, expectedState string) (OpenAIChatGPTAuthorizationResult, error) {
	parsed, err := url.Parse(strings.TrimSpace(input))
	if err != nil {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("Paste the full callback URL from the browser")
	}
	expected, _ := url.Parse(OpenAIChatGPTRedirectURI())
	if parsed.Scheme != expected.Scheme || parsed.Host != expected.Host || parsed.Path != expected.Path {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("The pasted callback URL must start with %s", OpenAIChatGPTRedirectURI())
	}
	if errorCode := parsed.Query().Get("error"); errorCode != "" {
		return OpenAIChatGPTAuthorizationResult{}, fmt.Errorf("ChatGPT authorization failed: %s", errorCode)
	}
	return authorizationResultFromCallback(parsed, expectedState)
}

func (s *OpenAIChatGPTCallbackServer) settle(result OpenAIChatGPTAuthorizationResult) {
	s.mu.Lock()
	if s.settled {
		s.mu.Unlock()
		return
	}
	s.settled = true
	s.mu.Unlock()
	s.result <- result
	close(s.done)
}

func (s *OpenAIChatGPTCallbackServer) fail(err error) {
	s.mu.Lock()
	if s.settled {
		s.mu.Unlock()
		return
	}
	s.settled = true
	s.mu.Unlock()
	s.failure <- err
	close(s.done)
}

// CancelWait hands the login over to manual code entry.
func (s *OpenAIChatGPTCallbackServer) CancelWait() {
	s.mu.Lock()
	if s.settled {
		s.mu.Unlock()
		return
	}
	s.settled = true
	s.mu.Unlock()
	close(s.done)
}

// Wait returns the callback result, a failure, or ok=false when cancelled.
func (s *OpenAIChatGPTCallbackServer) Wait() (OpenAIChatGPTAuthorizationResult, bool) {
	<-s.done
	select {
	case result := <-s.result:
		return result, true
	case err := <-s.failure:
		s.mu.Lock()
		s.lastErr = err
		s.mu.Unlock()
		return OpenAIChatGPTAuthorizationResult{}, false
	default:
		return OpenAIChatGPTAuthorizationResult{}, false
	}
}

// LastError returns the callback failure, when the wait ended because of one.
func (s *OpenAIChatGPTCallbackServer) LastError() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastErr
}

// Close shuts the server down.
func (s *OpenAIChatGPTCallbackServer) Close() {
	if s.server != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = s.server.Shutdown(shutdownCtx)
	}
}

// openAIChatGPTTokenResponse is the raw token endpoint payload.
type openAIChatGPTTokenResponse struct {
	AccessToken      string
	RefreshToken     string
	IDToken          string
	Scope            string
	ExpiresIn        float64
	HasExpiresInFlag bool
}

func requestOpenAIChatGPTToken(ctx context.Context, body url.Values) (*openAIChatGPTTokenResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, OpenAIChatGPTTokenURL, strings.NewReader(body.Encode()))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 400 {
		return nil, fmt.Errorf("OpenAI OAuth token request failed (%d): %s", response.StatusCode, strings.TrimSpace(string(raw)))
	}
	var payload struct {
		AccessToken  string   `json:"access_token"`
		RefreshToken string   `json:"refresh_token"`
		IDToken      string   `json:"id_token"`
		Scope        string   `json:"scope"`
		ExpiresIn    *float64 `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("OpenAI OAuth token response must be an object")
	}
	out := &openAIChatGPTTokenResponse{
		AccessToken: payload.AccessToken, RefreshToken: payload.RefreshToken,
		IDToken: payload.IDToken, Scope: payload.Scope,
	}
	if payload.ExpiresIn != nil {
		out.ExpiresIn = *payload.ExpiresIn
		out.HasExpiresInFlag = true
	}
	return out, nil
}

// openAIChatGPTCredentialFromToken validates the token response and builds the
// credential, storing the issued client id and scopes.
func openAIChatGPTCredentialFromToken(token *openAIChatGPTTokenResponse, clientID string) (*OAuthCredential, error) {
	if token.AccessToken == "" {
		return nil, fmt.Errorf("OpenAI OAuth token response has invalid access_token")
	}
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("OpenAI OAuth token response has invalid refresh_token")
	}
	if token.Scope == "" {
		return nil, fmt.Errorf("OpenAI OAuth token response has invalid scope")
	}
	if !token.HasExpiresInFlag || token.ExpiresIn <= 0 {
		return nil, fmt.Errorf("OpenAI OAuth token response has invalid expires_in")
	}
	scopes := strings.Fields(token.Scope)
	if !containsString(scopes, OpenAIChatGPTDirectTokenScope) {
		return nil, fmt.Errorf("OpenAI OAuth grant did not include %s", OpenAIChatGPTDirectTokenScope)
	}
	encodedScopes, err := MarshalJSON(scopes)
	if err != nil {
		return nil, err
	}
	return &OAuthCredential{OAuthCredentials: OAuthCredentials{
		Access:  token.AccessToken,
		Refresh: token.RefreshToken,
		Expires: time.Now().UnixMilli() + int64(token.ExpiresIn*1000) - OpenAIChatGPTCredentialMargin,
		Extra: map[string]json.RawMessage{
			"clientId": jsonRawString(clientID),
			"scopes":   encodedScopes,
		},
	}}, nil
}

// ExchangeOpenAIChatGPTAuthorizationCode exchanges a code for tokens with the
// issued client id.
func ExchangeOpenAIChatGPTAuthorizationCode(ctx context.Context, code, verifier, clientID string) (*OAuthCredential, error) {
	token, err := requestOpenAIChatGPTToken(ctx, url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {OpenAIChatGPTRedirectURI()},
		"resource":      {OpenAIChatGPTResource},
	})
	if err != nil {
		return nil, err
	}
	// Pi does not use the ID token to identify the user, but its presence is
	// part of the token-response contract.
	if token.IDToken == "" {
		return nil, fmt.Errorf("OpenAI OAuth token response did not contain an ID token")
	}
	return openAIChatGPTCredentialFromToken(token, clientID)
}

// RefreshOpenAIChatGPTToken refreshes a ChatGPT credential with its stored
// issued client id.
func RefreshOpenAIChatGPTToken(credential *OAuthCredential, ctx context.Context) (*OAuthCredential, error) {
	clientID := ""
	if credential != nil {
		if raw, ok := credential.Extra["clientId"]; ok {
			var value string
			if err := json.Unmarshal(raw, &value); err == nil {
				clientID = value
			}
		}
	}
	if strings.TrimSpace(clientID) == "" {
		return nil, fmt.Errorf("Stored OpenAI OAuth credential does not contain an issued client ID; reconnect ChatGPT")
	}
	token, err := requestOpenAIChatGPTToken(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {credential.Refresh},
		"resource":      {OpenAIChatGPTResource},
	})
	if err != nil {
		return nil, err
	}
	return openAIChatGPTCredentialFromToken(token, clientID)
}

// LoginOpenAIChatGPT runs the browser login path with the manual paste race.
func LoginOpenAIChatGPT(interaction *AuthInteraction) (*OAuthCredential, error) {
	if interaction == nil {
		return nil, fmt.Errorf("auth interaction is required")
	}
	deviceID := ""
	if interaction.GetDeviceID != nil {
		deviceID = interaction.GetDeviceID()
	}
	flow, err := CreateOpenAIChatGPTAuthorizationFlow(deviceID, interaction.AgentName)
	if err != nil {
		return nil, err
	}
	ctx := interaction.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	server, err := StartOpenAIChatGPTCallbackServer(flow.State)
	if err != nil {
		return nil, err
	}
	defer server.Close()

	type manualResult struct {
		result OpenAIChatGPTAuthorizationResult
		err    error
	}
	manualCh := make(chan manualResult, 1)
	go func() {
		input, perr := interaction.Prompt(AuthPrompt{
			Type:        AuthPromptManualCode,
			Message:     "Complete login in your browser, or paste the final redirect URL here:",
			Placeholder: OpenAIChatGPTRedirectURI(),
		})
		if perr != nil {
			manualCh <- manualResult{err: perr}
			server.CancelWait()
			return
		}
		result, rerr := ParseOpenAIChatGPTAuthorizationInput(input, flow.State)
		manualCh <- manualResult{result: result, err: rerr}
		server.CancelWait()
	}()

	if interaction.Notify != nil {
		interaction.Notify(AuthEvent{
			Type:         AuthEventAuthURL,
			URL:          flow.URL,
			Instructions: "Complete sign-in in your browser. If the callback does not complete, paste the final redirect URL here.",
		})
	}

	result, ok := server.Wait()
	if !ok {
		if lastErr := server.LastError(); lastErr != nil {
			return nil, lastErr
		}
		manual := <-manualCh
		if manual.err != nil {
			return nil, manual.err
		}
		result = manual.result
	}
	if interaction.Notify != nil {
		interaction.Notify(AuthEvent{Type: AuthEventProgress, Message: "Exchanging authorization code for tokens..."})
	}
	credential, err := ExchangeOpenAIChatGPTAuthorizationCode(ctx, result.Code, flow.Verifier, result.ClientID)
	if err != nil {
		return nil, err
	}
	return credential, nil
}

// OpenAIChatGPTOAuth is the ChatGPT-subscription OAuth auth definition.
func OpenAIChatGPTOAuth() *OAuthAuth {
	return &OAuthAuth{
		Name:           "OpenAI (ChatGPT subscription)",
		IsSubscription: true,
		LoginLabel:     "Sign in with ChatGPT",
		Login:          LoginOpenAIChatGPT,
		Refresh: func(credential *OAuthCredential, ctx context.Context) (*OAuthCredential, error) {
			return RefreshOpenAIChatGPTToken(credential, ctx)
		},
		ToAuth: func(credential *OAuthCredential) (*ModelAuth, error) {
			return &ModelAuth{APIKey: credential.Access}, nil
		},
	}
}
