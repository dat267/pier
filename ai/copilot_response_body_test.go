package ai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

type copilotBodyTransport func(*http.Request) (*http.Response, error)

func (f copilotBodyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type stalledCopilotBody struct {
	ctx    context.Context
	closed bool
}

func (b *stalledCopilotBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledCopilotBody) Close() error {
	b.closed = true
	return nil
}

// pi v1.4.0 (4a080252), auth/oauth/github-copilot.ts cancels a discarded
// rate-limit body before retrying; it must not drain it under the live budget.
func TestCopilotRetryClosesDiscardedResponseWithoutReading(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		previous := http.DefaultClient
		defer func() { http.DefaultClient = previous }()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		attempts := 0
		var discarded *stalledCopilotBody
		http.DefaultClient = &http.Client{Transport: copilotBodyTransport(func(request *http.Request) (*http.Response, error) {
			if err := request.Context().Err(); err != nil {
				return nil, err
			}
			attempts++
			if attempts == 1 {
				discarded = &stalledCopilotBody{ctx: request.Context()}
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"0"}}, Body: discarded}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})}
		const endpoint = "https://fixture.invalid/models"
		response, err := ai.FetchWithCopilotRateLimitRetry(ctx, endpoint, func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		}, ai.CopilotRetryPolicy{MaxRetries: 1})
		if err != nil {
			t.Fatalf("retry waited for discarded body: %v (attempts=%d)", err, attempts)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || string(body) != "ok" || attempts != 2 || !discarded.closed {
			t.Fatalf("body=%q err=%v attempts=%d discarded closed=%v", body, err, attempts, discarded.closed)
		}
	})
}

// pi v1.4.0 (4a080252), auth/oauth/github-copilot.ts
// fetchWithRateLimitRetry keeps its 5s attempt signal live after returning
// response headers; fetchGitHubCopilotModels then consumes the JSON body.
func TestCopilotRetryKeepsReturnedResponseBodyReadable(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"data":[]}`)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	response, err := ai.FetchWithCopilotRateLimitRetry(context.Background(), server.URL, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	}, ai.CopilotRetryPolicy{})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != `{"data":[]}` {
		t.Fatalf("returned response body=%q err=%v; want readable JSON", body, err)
	}
}
