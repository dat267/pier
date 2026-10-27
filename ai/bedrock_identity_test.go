package ai

import (
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 bedrock-converse-stream.ts custom-header middleware:
// null values are ignored and reserved headers cannot replace SDK headers.
func TestBedrockIdentityHeaderOverrides(t *testing.T) {
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_BEDROCK_SKIP_AUTH", "")
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	for _, tc := range []struct {
		name    string
		headers ProviderHeaders
		want    string
	}{
		{"caller override", ProviderHeaders{"user-agent": new("caller")}, "caller"},
		{"null ignored", ProviderHeaders{"user-agent": nil}, "pi"},
		{"empty override", ProviderHeaders{"user-agent": new("")}, ""},
		{"reserved ignored", ProviderHeaders{"x-amz-user-agent": new("caller")}, "pi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var captured http.Request
			server := bedrockTestServer(t, [][]byte{
				bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
				bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
			}, &captured, nil)
			message, err := StreamBedrockConverseSimple(bedrockTestModel(server.URL), TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}},
			}}, &SimpleStreamOptions{
				Identity: &ClientIdentity{UserAgent: "short"}, Headers: tc.headers,
				Env: ProviderEnv{"AWS_ACCESS_KEY_ID": "AKID", "AWS_SECRET_ACCESS_KEY": "SECRET", "AWS_REGION": "us-west-2"}}).Result(bgCtx())
			if err != nil || message.StopReason != StopStop {
				t.Fatalf("message = %+v; err = %v", message, err)
			}
			if captured.Header.Get("User-Agent") != tc.want || captured.Header.Get("X-Amz-User-Agent") != "pi" {
				t.Fatalf("headers = %v", captured.Header)
			}
			if auth := captured.Header.Get("Authorization"); !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID/") || !strings.Contains(auth, "x-amz-user-agent") {
				t.Fatalf("identity header was not present before signing: %q", auth)
			}
		})
	}
}

// Reference: pi v1.4.0 bedrock-converse-stream.ts bedrockUserAgent and the
// pinned @aws-sdk/core user-agent middleware. Golden values are the escaped
// identity contribution, not fabricated JavaScript SDK/runtime metadata (D266).
// Regenerate: node --experimental-strip-types ai/testdata/bedrock-identity/generate.mjs ./pi > ai/testdata/bedrock-identity/golden.json
func TestBedrockIdentityHeaders(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock-identity/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Identity *ClientIdentity
		Want     string
		AmzWant  string
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for index, tc := range cases {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			frames := [][]byte{
				bedrockEventFrame("messageStart", map[string]any{"role": "assistant"}),
				bedrockEventFrame("messageStop", map[string]any{"stopReason": "end_turn"}),
			}
			var captured http.Request
			server := bedrockTestServer(t, frames, &captured, nil)
			message, err := StreamBedrockConverseSimple(bedrockTestModel(server.URL), TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}},
			}}, &SimpleStreamOptions{APIKey: "token", Identity: tc.Identity}).Result(bgCtx())
			if err != nil || message.StopReason != StopStop {
				t.Fatalf("message = %+v; err = %v", message, err)
			}
			if captured.Header.Get("User-Agent") != tc.Want || captured.Header.Get("X-Amz-User-Agent") != tc.AmzWant {
				t.Fatalf("headers = %v; want agent %q, amz agent %q", captured.Header, tc.Want, tc.AmzWant)
			}
		})
	}
}
