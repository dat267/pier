package ai

import (
	"encoding/base64"
	"net/url"
	"testing"
)

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
