package ai

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
	"testing"
)

// TestOpenAIChatGPTAgentHostID pins the agent-host URI and its validation
// (upstream agentHostId).
func TestOpenAIChatGPTAgentHostID(t *testing.T) {
	hostID, err := openAIChatGPTAgentHostID("0190A1B2-C3D4-7E5F-8A9B-0C1D2E3F4A5B")
	if err != nil || hostID != "urn:uuid:0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b" {
		t.Fatalf("hostID = %q, %v", hostID, err)
	}
	for _, invalid := range []string{"", "not-a-uuid", "0190a1b2c3d47e5f8a9b0c1d2e3f4a5b"} {
		if _, err := openAIChatGPTAgentHostID(invalid); err == nil {
			t.Fatalf("%q must be rejected", invalid)
		}
	}
}

// TestOpenAIChatGPTAuthorizationFlowURL pins the dynamic-client authorize URL.
func TestOpenAIChatGPTAuthorizationFlowURL(t *testing.T) {
	flow, err := CreateOpenAIChatGPTAuthorizationFlow("0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(flow.URL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	if query.Get("client_id") != OpenAIChatGPTDynamicClientID {
		t.Fatalf("client_id = %q", query.Get("client_id"))
	}
	if query.Get("agent_name_hint") != OpenAIChatGPTAgentNameHint {
		t.Fatalf("agent_name_hint = %q", query.Get("agent_name_hint"))
	}
	if query.Get("ext_agent_host_id") != "urn:uuid:0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b" {
		t.Fatalf("ext_agent_host_id = %q", query.Get("ext_agent_host_id"))
	}
	if query.Get("resource") != OpenAIChatGPTResource || query.Get("nonce") != flow.Nonce {
		t.Fatalf("resource/nonce = %q/%q", query.Get("resource"), query.Get("nonce"))
	}
	if !strings.Contains(query.Get("scope"), OpenAIChatGPTDirectTokenScope) {
		t.Fatalf("scope = %q", query.Get("scope"))
	}
	if query.Get("state") != flow.State || query.Get("code_challenge") != "" && !strings.Contains(flow.URL, "code_challenge=") {
		t.Fatalf("state/challenge mismatch: %s", flow.URL)
	}
}

func TestOpenAIChatGPTAuthorizationFlowUsesCustomAgentName(t *testing.T) {
	flow, err := CreateOpenAIChatGPTAuthorizationFlow("0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b", "ExampleApp")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(flow.URL)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("agent_name_hint"); got != "ExampleApp" {
		t.Fatalf("agent_name_hint = %q; want ExampleApp", got)
	}
}

// TestLoginOpenAIChatGPTRequiresDeviceID pins that a missing device id fails
// before the browser opens.
func TestLoginOpenAIChatGPTRequiresDeviceID(t *testing.T) {
	_, err := LoginOpenAIChatGPT(&AuthInteraction{})
	if err == nil || !strings.Contains(err.Error(), "requires a device ID") {
		t.Fatalf("err = %v", err)
	}
}

// TestParseOpenAIChatGPTAuthorizationInput pins the pasted-URL validation.
func TestParseOpenAIChatGPTAuthorizationInput(t *testing.T) {
	const state = "state-1"
	valid := "http://127.0.0.1:1455/auth/callback?code=abc&state=state-1&client_id=oaiapp_issued"
	result, err := ParseOpenAIChatGPTAuthorizationInput(valid, state)
	if err != nil || result.Code != "abc" || result.ClientID != "oaiapp_issued" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	// WHATWG resolves dot segments before comparing, so a pasted URL that names
	// the callback through ".." is the callback URL. Go's url.Parse keeps the "..".
	viaDotDot := "http://127.0.0.1:1455/auth/../auth/callback?code=abc&state=state-1&client_id=oaiapp_issued"
	if result, err := ParseOpenAIChatGPTAuthorizationInput(viaDotDot, state); err != nil || result.Code != "abc" {
		t.Fatalf("dot segments: result = %+v, %v", result, err)
	}
	// Wrong origin or path, missing issued client id, and state mismatch fail.
	for _, invalid := range []string{
		"http://127.0.0.1:9999/auth/callback?code=abc&state=state-1&client_id=x",
		"https://attacker.example/auth/callback?code=abc&state=state-1&client_id=x",
		"http://127.0.0.1:1455/other?code=abc&state=state-1&client_id=x",
		valid[:strings.Index(valid, "&client_id")],
		"http://127.0.0.1:1455/auth/callback?code=abc&state=wrong&client_id=x",
	} {
		if _, err := ParseOpenAIChatGPTAuthorizationInput(invalid, state); err == nil {
			t.Fatalf("%q must be rejected", invalid)
		}
	}
}

func TestParseOpenAIChatGPTAuthorizationUsesJavaScriptTrim(t *testing.T) {
	const state = "state-1"
	callback := "http://127.0.0.1:1455/auth/callback?code=abc&state=state-1&client_id=%EF%BB%BFoaiapp_issued%EF%BB%BF"
	result, err := ParseOpenAIChatGPTAuthorizationInput("\uFEFF"+callback+"\uFEFF", state)
	if err != nil || result.Code != "abc" || result.ClientID != "oaiapp_issued" {
		t.Fatalf("result = %+v, %v; want BOM-trimmed OAuth values", result, err)
	}
}

// openai-chatgpt.ts rejects a stored clientId that is empty after String.trim.
func TestOpenAIChatGPTClientIDUsesJavaScriptTrim(t *testing.T) {
	if got := normalizeOpenAIChatGPTClientID("\uFEFF"); got != "" {
		t.Fatalf("BOM-only client ID = %q; want empty", got)
	}
	if got := normalizeOpenAIChatGPTClientID("\uFEFF client-id \uFEFF"); got != "client-id" {
		t.Fatalf("client ID = %q; want trimmed value", got)
	}
}

// openai-chatgpt.ts parses pasted callbacks with the WHATWG URL constructor.
func TestParseOpenAIChatGPTAuthorizationNormalizesSpecialSchemeURL(t *testing.T) {
	input := "http:///127.0.0.1:1455/auth/callback?code=abc&state=state-1&client_id=oaiapp_issued"
	result, err := ParseOpenAIChatGPTAuthorizationInput(input, "state-1")
	if err != nil || result.Code != "abc" || result.ClientID != "oaiapp_issued" {
		t.Fatalf("result = %+v, %v; want parsed WHATWG URL", result, err)
	}
}

// TestOpenAIChatGPTCredentialStoresClientIDAndScopes pins the credential's
// extra keys and their JSON round-trip.
func TestOpenAIChatGPTCredentialStoresClientIDAndScopes(t *testing.T) {
	expires := float64(3600)
	token := &openAIChatGPTTokenResponse{
		AccessToken: "access", RefreshToken: "refresh", IDToken: "id",
		Scope: OpenAIChatGPTScope, ExpiresIn: expires, HasExpiresInFlag: true,
	}
	credential, err := openAIChatGPTCredentialFromToken(token, "oaiapp_issued")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(&Credential{Type: CredentialOAuth, OAuth: credential})
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &probe); err != nil {
		t.Fatal(err)
	}
	var clientID string
	if err := json.Unmarshal(probe["clientId"], &clientID); err != nil || clientID != "oaiapp_issued" {
		t.Fatalf("clientId = %s, %v", probe["clientId"], err)
	}
	var scopes []string
	if err := json.Unmarshal(probe["scopes"], &scopes); err != nil || !slices.Contains(scopes, OpenAIChatGPTDirectTokenScope) {
		t.Fatalf("scopes = %s, %v", probe["scopes"], err)
	}
	// A grant without the direct-token scope is rejected.
	token.Scope = "openid profile"
	if _, err := openAIChatGPTCredentialFromToken(token, "x"); err == nil {
		t.Fatal("a grant without the direct-token scope must fail")
	}
}

// Reference: pi v1.5.2 (b4afdddf) auth/oauth/openai-chatgpt.ts randomValue()
// returns randomBytes(32).toString("base64url") for both state and nonce; the
// port borrowed the Codex helper and sent 16 bytes as hex.
func TestOpenAIChatGPTStateAndNonceAreBase64URLOf32Bytes(t *testing.T) {
	flow, err := CreateOpenAIChatGPTAuthorizationFlow("11111111-2222-3333-4444-555555555555")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(flow.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"state", "nonce"} {
		value := parsed.Query().Get(name)
		decoded, decodeErr := base64.RawURLEncoding.DecodeString(value)
		if decodeErr != nil || len(decoded) != 32 {
			t.Fatalf("%s = %q decodes to %d bytes (err %v), want 32 bytes base64url", name, value, len(decoded), decodeErr)
		}
	}
	if parsed.Query().Get("state") != flow.State {
		t.Fatal("the flow's state does not match the authorize URL")
	}
}

// pi v1.5.2 (b4afdddf) auth/oauth/openai-chatgpt.ts startCallbackServer: a bind
// failure for an address already in use is reported as the port being taken by
// an unfinished login, not as a raw listen error, because the port is fixed. The
// predicate must recognize the real error rather than any bind failure.
func TestIsAddressInUseErrorRecognizesOnlyAnAddressInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot bind a loopback listener: %v", err)
	}
	address := listener.Addr().String()
	defer listener.Close()

	_, busyErr := net.Listen("tcp", address)
	if busyErr == nil {
		t.Fatalf("second bind of %s unexpectedly succeeded", address)
	}
	if !isAddressInUseError(busyErr) {
		t.Fatalf("isAddressInUseError(%v) = false, want true for %s", busyErr, address)
	}
	if isAddressInUseError(nil) {
		t.Fatal("isAddressInUseError(nil) = true")
	}
	if isAddressInUseError(errors.New("some other bind failure")) {
		t.Fatal("a plain error is not an address-in-use failure")
	}
}
