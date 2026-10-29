package ai_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/dat267/pier/ai"
)

// The same upstream retryBudgetSignal must survive a completed 429 attempt;
// resetting the overall budget for the next attempt would exceed the bound.
func TestCopilotRetrySharesOverallBudgetAcrossAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		previous := http.DefaultClient
		defer func() { http.DefaultClient = previous }()
		attempts := 0
		http.DefaultClient = &http.Client{Transport: copilotBodyTransport(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				time.Sleep(80 * time.Millisecond)
				return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"0"}}, Body: http.NoBody}, nil
			}
			<-request.Context().Done()
			return nil, request.Context().Err()
		})}
		start := time.Now()
		const endpoint = "https://fixture.invalid/models"
		_, err := ai.FetchWithCopilotRateLimitRetry(context.Background(), endpoint, func(ctx context.Context) (*http.Request, error) {
			return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		}, ai.CopilotRetryPolicy{MaxRetries: 1, MaxElapsedMS: 100})
		if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != 100*time.Millisecond || attempts != 2 {
			t.Fatalf("elapsed=%v error=%v attempts=%d; want 100ms deadline after two attempts", time.Since(start), err, attempts)
		}
	})
}

// pi v1.4.0 (4a080252), auth/oauth/github-copilot.ts
// fetchWithRateLimitRetry shares retryBudgetSignal across all attempts and
// returned response consumption; a caller's earlier abort still wins.
func TestCopilotRetryOverallBudgetBoundsRequestsAndBodies(t *testing.T) {
	for _, test := range []struct {
		name          string
		policy        ai.CopilotRetryPolicy
		callerTimeout time.Duration
		want          time.Duration
	}{
		{"overall budget", ai.CopilotRetryPolicy{MaxRetries: 1, MaxElapsedMS: 100}, 0, 100 * time.Millisecond},
		{"no retries control", ai.CopilotRetryPolicy{MaxElapsedMS: 100}, 0, 5 * time.Second},
		{"no budget control", ai.CopilotRetryPolicy{MaxRetries: 1}, 0, 5 * time.Second},
		{"earlier caller deadline", ai.CopilotRetryPolicy{MaxRetries: 1, MaxElapsedMS: 100}, 50 * time.Millisecond, 50 * time.Millisecond},
	} {
		for _, returnHeaders := range []bool{false, true} {
			boundary := "headers"
			if returnHeaders {
				boundary = "body"
			}
			t.Run(test.name+"/"+boundary, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					previous := http.DefaultClient
					defer func() { http.DefaultClient = previous }()
					ctx := context.Background()
					if test.callerTimeout > 0 {
						var cancel context.CancelFunc
						ctx, cancel = context.WithTimeout(ctx, test.callerTimeout)
						defer cancel()
					}
					http.DefaultClient = &http.Client{Transport: copilotBodyTransport(func(request *http.Request) (*http.Response, error) {
						if returnHeaders {
							return &http.Response{StatusCode: http.StatusOK, Body: &stalledCopilotBody{ctx: request.Context()}}, nil
						}
						<-request.Context().Done()
						return nil, request.Context().Err()
					})}
					start := time.Now()
					const endpoint = "https://fixture.invalid/models"
					response, err := ai.FetchWithCopilotRateLimitRetry(ctx, endpoint, func(ctx context.Context) (*http.Request, error) {
						return http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
					}, test.policy)
					if response != nil {
						defer response.Body.Close()
						_, err = io.ReadAll(response.Body)
					}
					if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != test.want {
						t.Fatalf("elapsed=%v error=%v; want %v and deadline exceeded", time.Since(start), err, test.want)
					}
				})
			})
		}
	}
}
