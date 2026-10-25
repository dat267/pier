package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"
)

type callbackFixture struct {
	Name    string `json:"name"`
	Options struct {
		Host       string   `json:"host"`
		Path       string   `json:"path"`
		ExtraPaths []string `json:"extraPaths"`
	} `json:"options"`
	RequestPath  string `json:"requestPath"`
	ExpectedPath string `json:"expectedPath"`
	Expected     struct {
		Status   int            `json:"status"`
		Body     string         `json:"body"`
		Callback *OAuthCallback `json:"callback"`
		Error    string         `json:"error"`
	} `json:"expected"`
}

func loadCallbackFixtures(t *testing.T) []callbackFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/callback/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []callbackFixture
	if err := json.Unmarshal(raw, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("empty callback fixtures")
	}
	return fixtures
}

// pi v1.4.0 packages/mcp/src/oauth/callback.ts handle compares URL.pathname,
// which retains escaped path separators and letters, with allowed paths and
// the pending waiter's expected path. The corpus includes ordinary controls.
func TestCallbackPathsMatchUpstream(t *testing.T) {
	for _, test := range loadCallbackFixtures(t) {
		if test.Name == "ipv4" || test.Name == "ipv6" {
			continue
		}
		t.Run(test.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server, err := ListenCallbackServer(ctx, OAuthCallbackServerOptions{
				Path: test.Options.Path, ExtraPaths: test.Options.ExtraPaths,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close(context.Background()) })
			type outcome struct {
				callback OAuthCallback
				err      error
			}
			var pending chan outcome
			if test.ExpectedPath != "" {
				pending = make(chan outcome, 1)
				go func() {
					callback, err := server.WaitForCallback("fixture-state", test.ExpectedPath)
					pending <- outcome{callback: callback, err: err}
				}()
				for !server.Pending("fixture-state") {
					if ctx.Err() != nil {
						t.Fatal("waiter did not register:", ctx.Err())
					}
					runtime.Gosched()
				}
			}
			base, err := url.Parse(server.RedirectURL)
			if err != nil {
				t.Fatal(err)
			}
			path, err := url.Parse(test.RequestPath)
			if err != nil {
				t.Fatal(err)
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, base.ResolveReference(path).String(), nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			response, err := (&http.Client{Transport: transport}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.Expected.Status || string(body) != test.Expected.Body {
				t.Fatalf("response = %d %q, want %d %q", response.StatusCode, body, test.Expected.Status, test.Expected.Body)
			}
			if pending != nil {
				select {
				case result := <-pending:
					if test.Expected.Error != "" {
						if result.err == nil || result.err.Error() != test.Expected.Error {
							t.Fatalf("callback error = %v, want %q", result.err, test.Expected.Error)
						}
					} else if result.err != nil || !reflect.DeepEqual(&result.callback, test.Expected.Callback) {
						t.Fatalf("callback = %+v, %v; want %+v", result.callback, result.err, test.Expected.Callback)
					}
				case <-ctx.Done():
					t.Fatal("callback did not complete:", ctx.Err())
				}
			}
		})
	}
}

// pi v1.4.0 packages/mcp/src/oauth/callback.ts listen accepts an IPv6 host
// separately from its port and brackets the redirect URL. Regenerate with:
// node mcp/oauth/testdata/callback/generate.mjs ./pi > mcp/oauth/testdata/callback/golden.json
func TestCallbackIPv6MatchesUpstream(t *testing.T) {
	probe, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback unavailable: %v", err)
	}
	_ = probe.Close()
	for _, test := range loadCallbackFixtures(t) {
		if test.Name != "ipv4" && test.Name != "ipv6" {
			continue
		}
		t.Run(test.Name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			server, err := ListenCallbackServer(ctx, OAuthCallbackServerOptions{Host: test.Options.Host})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = server.Close(context.Background()) })
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.RedirectURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			transport := &http.Transport{}
			defer transport.CloseIdleConnections()
			response, err := (&http.Client{Transport: transport}).Do(request)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != test.Expected.Status || string(body) != test.Expected.Body {
				t.Fatalf("response = %d %q, want %d %q", response.StatusCode, body, test.Expected.Status, test.Expected.Body)
			}
		})
	}
}
