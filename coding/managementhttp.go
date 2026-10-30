package coding

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dat267/pier/ai"
)

// Port of utils/management-http.ts and utils/pi-user-agent.ts.

// retryableStatusCodes are the transient HTTP statuses retried by management
// requests.
var retryableStatusCodes = map[int]bool{408: true, 425: true, 429: true, 500: true, 502: true, 503: true, 504: true}

const (
	maxAbortSignalTimeoutMS int64 = 1<<32 - 1
	maxNodeTimerDelayMS     int64 = 1<<31 - 1
)

func abortSignalTimeoutDuration(timeoutMS int64) (time.Duration, error) {
	if timeoutMS < 0 || timeoutMS > maxAbortSignalTimeoutMS {
		return 0, fmt.Errorf("The value of \"delay\" is out of range. It must be >= 0 && <= %d. Received %d", maxAbortSignalTimeoutMS, timeoutMS)
	}
	if timeoutMS > maxNodeTimerDelayMS {
		timeoutMS = 1
	}
	return time.Duration(timeoutMS) * time.Millisecond, nil
}

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
		maxRetries = max(*options.MaxRetries, 0)
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
	var overallCancel context.CancelFunc
	if options.TimeoutMS != nil && *options.TimeoutMS > 0 {
		timeout, err := abortSignalTimeoutDuration(*options.TimeoutMS)
		if err != nil {
			return nil, err
		}
		baseCtx, overallCancel = context.WithTimeout(baseCtx, timeout)
		defer func() {
			if overallCancel != nil {
				overallCancel()
			}
		}()
	}

	for attempt := 0; ; attempt++ {
		// Caller cancellation and the overall budget are terminal.
		if err := baseCtx.Err(); err != nil {
			return nil, err
		}

		attemptCtx := baseCtx
		var attemptCancel context.CancelFunc
		if options.AttemptTimeoutMS != nil && *options.AttemptTimeoutMS > 0 {
			attemptTimeout, err := abortSignalTimeoutDuration(*options.AttemptTimeoutMS)
			if err != nil {
				return nil, err
			}
			attemptCtx, attemptCancel = context.WithTimeout(baseCtx, attemptTimeout)
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
			if overallCancel != nil && response.Body != nil {
				// Transfer the total budget too: headers do not finish the
				// returned body, and its deadline must remain in effect.
				response.Body = &cancelOnCloseBody{ReadCloser: response.Body, cancel: overallCancel}
				overallCancel = nil
			}
			return response, nil
		}
		// Upstream cancels the abandoned body. Do not drain it: an error
		// response may stall and consume the budget before we can retry.
		if response.Body != nil {
			_ = response.Body.Close()
		}
		if attemptCancel != nil {
			attemptCancel()
		}
	}
}

// PiUserAgent renders the pi user agent (port of getPiUserAgent).
func PiUserAgent(version string) string {
	return ai.FormatUserAgent(&ai.ClientIdentity{Version: version})
}

// cancelOnCloseBody cancels a request timeout context when the caller closes the
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
