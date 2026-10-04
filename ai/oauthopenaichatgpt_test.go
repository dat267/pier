package ai

import (
	"encoding/json"
	"net/url"
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
	if err := json.Unmarshal(probe["scopes"], &scopes); err != nil || !containsString(scopes, OpenAIChatGPTDirectTokenScope) {
		t.Fatalf("scopes = %s, %v", probe["scopes"], err)
	}
	// A grant without the direct-token scope is rejected.
	token.Scope = "openid profile"
	if _, err := openAIChatGPTCredentialFromToken(token, "x"); err == nil {
		t.Fatal("a grant without the direct-token scope must fail")
	}
}
