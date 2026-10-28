package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Keep the configured redirect hook and Go's default ten-hop limit while
// enforcing fetch's credential policy, for both idle-guarded and plain calls.
func TestProviderRequestRedirectPolicy(t *testing.T) {
	for _, guarded := range []bool{false, true} {
		t.Run(fmt.Sprintf("guarded=%v", guarded), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Redirect(w, r, "/again", http.StatusTemporaryRedirect)
			}))
			defer server.Close()
			var timeout *int
			if guarded {
				timeout = new(5000)
			}
			request, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			if response, err := DoProviderRequest(request, timeout); err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
				if response != nil {
					response.Body.Close()
				}
				t.Fatalf("redirect limit error = %v", err)
			}
			if calls.Load() != 10 {
				t.Fatalf("redirect requests = %d; want 10", calls.Load())
			}
			original := http.DefaultClient
			client := *original
			hookCalls := 0
			client.CheckRedirect = func(*http.Request, []*http.Request) error {
				hookCalls++
				return http.ErrUseLastResponse
			}
			http.DefaultClient = &client
			defer func() { http.DefaultClient = original }()
			response, err := DoProviderRequest(request, timeout)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != http.StatusTemporaryRedirect || hookCalls != 1 {
				t.Fatalf("configured hook: status=%d calls=%d", response.StatusCode, hookCalls)
			}
		})
	}
}

// pi v1.4.0 (4a080252), api/openai-responses.ts uses fetch (via the SDK),
// which removes bearer authorization when a redirect changes origins, including
// a same-host port change. Only local servers and synthetic credentials are used.
func TestResponsesRedirectCredentials(t *testing.T) {
	for _, route := range []string{"same-origin", "cross-port", "cross-port-chain", "origin-roundtrip"} {
		t.Run(route, func(t *testing.T) {
			authorized := make(chan http.Header, 1)
			finish := func(w http.ResponseWriter, r *http.Request) {
				authorized <- r.Header.Clone()
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[]}}\n\n")
			}
			var origin, target *httptest.Server
			target = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case route == "origin-roundtrip":
					http.Redirect(w, r, origin.URL+"/done", http.StatusTemporaryRedirect)
				case route == "cross-port-chain" && r.URL.Path != "/done":
					http.Redirect(w, r, target.URL+"/done", http.StatusTemporaryRedirect)
				default:
					finish(w, r)
				}
			}))
			defer target.Close()
			origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/done" {
					finish(w, r)
					return
				}
				if r.Header.Get("Authorization") == "" {
					t.Error("origin received no authorization")
				}
				destination := target.URL + "/responses"
				if route == "same-origin" {
					destination = origin.URL + "/done"
				}
				http.Redirect(w, r, destination, http.StatusTemporaryRedirect)
			}))
			defer origin.Close()
			model := testResponsesModel()
			model.BaseURL = origin.URL + "/v1"
			result, err := StreamOpenAIResponses(model, NormalizeContext(Context{Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}}}}), &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "synthetic-test", Headers: ProviderHeaders{"Cookie": new("synthetic-cookie"), "Proxy-Authorization": new("synthetic-proxy"), "X-Trace": new("keep")}}}).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != StopStop {
				t.Fatalf("stream failed: %s %v", result.StopReason, result.ErrorMessage)
			}
			headers := <-authorized
			for _, name := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
				if got, want := headers.Get(name) != "", route == "same-origin"; got != want {
					t.Errorf("final %s present=%v; want %v", name, got, want)
				}
			}
			if headers.Get("X-Trace") != "keep" {
				t.Error("redirect lost nonsensitive header")
			}
		})
	}
}
