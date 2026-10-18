package coding

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"
)

// Port of utils/management-http.ts and utils/pi-user-agent.ts.

// retryableStatusCodes are the transient HTTP statuses retried by management
// requests.
var retryableStatusCodes = map[int]bool{408: true, 425: true, 429: true, 500: true, 502: true, 503: true, 504: true}

// FetchRetryOptions bound the retry behavior.
type FetchRetryOptions struct {
	// MaxRetries is the number of additional attempts after the initial
	// request; nil defaults to 2.
	MaxRetries *int
	// RetryOnStatus retries transient HTTP responses as well as transport
	// failures; nil defaults to true.
	RetryOnStatus *bool
	// TimeoutMS is the overall time budget shared by all attempts.
	TimeoutMS *int64
	// AttemptTimeoutMS is the per-attempt timeout; a hung attempt is retried
	// with a fresh timeout.
	AttemptTimeoutMS *int64
}

// FetchWithRetry performs a management HTTP request with a bounded immediate
// retry. Transport-level only: model requests are retried by their semantic
// caller instead.
func FetchWithRetry(ctx context.Context, request *http.Request, client *http.Client, options FetchRetryOptions) (*http.Response, error) {
	maxRetries := 2
	if options.MaxRetries != nil {
		maxRetries = *options.MaxRetries
		if maxRetries < 0 {
			maxRetries = 0
		}
	}
	retryOnStatus := true
	if options.RetryOnStatus != nil {
		retryOnStatus = *options.RetryOnStatus
	}
	if client == nil {
		client = http.DefaultClient
	}

	baseCtx := ctx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	if options.TimeoutMS != nil && *options.TimeoutMS > 0 {
		var cancel context.CancelFunc
		baseCtx, cancel = context.WithTimeout(baseCtx, time.Duration(*options.TimeoutMS)*time.Millisecond)
		defer cancel()
	}

	for attempt := 0; ; attempt++ {
		// Caller cancellation and the overall budget are terminal.
		if err := baseCtx.Err(); err != nil {
			return nil, err
		}

		attemptCtx := baseCtx
		var attemptCancel context.CancelFunc
		if options.AttemptTimeoutMS != nil && *options.AttemptTimeoutMS > 0 {
			attemptCtx, attemptCancel = context.WithTimeout(baseCtx, time.Duration(*options.AttemptTimeoutMS)*time.Millisecond)
		}
		attemptRequest := request.WithContext(attemptCtx)

		response, err := client.Do(attemptRequest)
		if err != nil {
			if attemptCancel != nil {
				attemptCancel()
			}
			// Caller cancellation and the overall budget are terminal.
			if baseCtx.Err() != nil {
				return nil, err
			}
			if attempt >= maxRetries {
				return nil, err
			}
			// A transport failure or a per-attempt timeout retries with a fresh
			// per-attempt budget.
			continue
		}

		shouldRetry := retryOnStatus && retryableStatusCodes[response.StatusCode] && attempt < maxRetries
		if !shouldRetry {
			if attemptCancel != nil && response.Body != nil {
				// Keep the per-attempt budget alive while the caller reads the
				// body: canceling here would abort the read. Release it when the
				// body closes, as upstream leaves the attempt signal alone.
				response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: attemptCancel}
			}
			return response, nil
		}
		// Discard the body before retrying; the response is being abandoned.
		if response.Body != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
		}
		if attemptCancel != nil {
			attemptCancel()
		}
	}
}

// PiUserAgent renders the pi user agent (port of getPiUserAgent; the JS runtime
// token becomes the Go runtime version).
func PiUserAgent(version string) string {
	return fmt.Sprintf("pi/%s (%s; %s; %s)", version, runtime.GOOS, runtime.Version(), runtime.GOARCH)
}

// cancelOnCloseBody cancels the per-attempt context when the caller closes the
// response body. Canceling it inside FetchWithRetry (immediately after Do)
// aborts the body read, so the attempt budget is released on close instead.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}
