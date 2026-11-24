package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dat267/pier/internal/jsnum"
)

// pi v1.4.0 OpenAI and Anthropic SDK fetch wrappers use Node setTimeout
// (openai/src/client.ts, @anthropic-ai/sdk/src/client.ts), while
// core/http-dispatcher.ts independently uses Undici FastTimers for bodyTimeout.
// These timers must not share one duration.
func TestProviderHeaderTimerClampsLargeDelayToOneMillisecond(t *testing.T) {
	if int64(int(jsnum.MaxTimeoutMS+1)) != jsnum.MaxTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	server := stallServer(t, false, "")
	timeoutMS := int(jsnum.MaxTimeoutMS + 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = DoProviderRequestWithSDKTimeout(request, &timeoutMS)
	var timeoutErr *idleTimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.afterHeaders || timeoutErr.timeout != time.Millisecond {
		t.Fatalf("elapsed=%v error=%v; want the 1ms Node header timer", time.Since(start), err)
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("header timeout took %v; want Node's 1ms clamp", elapsed)
	}
}

func TestOpenAICompletionsUsesNodeHeaderTimeoutClamp(t *testing.T) {
	if int64(int(jsnum.MaxTimeoutMS+1)) != jsnum.MaxTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	server := stallServer(t, false, "")
	model := testOpenAIModel()
	model.BaseURL = server.URL
	timeoutMS := int(jsnum.MaxTimeoutMS + 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	stream := StreamOpenAICompletions(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICompletionsOptions{APIKey: "test", TimeoutMs: &timeoutMS})
	start := time.Now()
	message, err := stream.Result(ctx)
	if err != nil || ctx.Err() != nil || message == nil || message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(*message.ErrorMessage, "headers idle for 1ms") {
		t.Fatalf("elapsed=%v message=%+v error=%v ctx=%v; want the clamped SDK header timeout", time.Since(start), message, err, ctx.Err())
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("OpenAI header timeout took %v; want Node's 1ms clamp", elapsed)
	}
}

// SDK-backed requests apply Node's timer to headers but keep the raw Undici
// body timeout after the response arrives.
func TestProviderRequestSeparatesHeaderAndBodyTimerSemantics(t *testing.T) {
	if int64(int(jsnum.MaxTimeoutMS+1)) != jsnum.MaxTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	previous := http.DefaultClient
	defer func() { http.DefaultClient = previous }()
	http.DefaultClient = &http.Client{Transport: providerTimeoutTransport(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	timeoutMS := int(jsnum.MaxTimeoutMS + 1)
	request, err := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := DoProviderRequestWithSDKTimeout(request, &timeoutMS)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	guarded, ok := response.Body.(*idleStreamBody)
	if !ok {
		t.Fatalf("body type = %T; want *idleStreamBody", response.Body)
	}
	if guarded.headerTimeout != time.Millisecond {
		t.Fatalf("header timeout = %v; want 1ms", guarded.headerTimeout)
	}
	wantBodyTimeout := time.Duration(timeoutMS) * time.Millisecond
	if guarded.timeout != wantBodyTimeout {
		t.Fatalf("body timeout = %v; want raw Undici delay %v", guarded.timeout, wantBodyTimeout)
	}
}

func TestCodexAbortSignalHeaderTimerClampsLargeDelay(t *testing.T) {
	if int64(int(jsnum.MaxTimeoutMS+1)) != jsnum.MaxTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	server := stallServer(t, false, "")
	timeoutMS := int(jsnum.MaxTimeoutMS + 1)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = DoProviderRequestWithAbortSignalTimeout(request, &timeoutMS)
	var timeoutErr *idleTimeoutError
	if !errors.As(err, &timeoutErr) || timeoutErr.timeout != time.Millisecond || timeoutErr.afterHeaders {
		t.Fatalf("elapsed=%v error=%v; want AbortSignal's 1ms timer clamp", time.Since(start), err)
	}
}

func TestCodexRejectsNegativeTimeoutBeforeFetch(t *testing.T) {
	previous := http.DefaultClient
	defer func() { http.DefaultClient = previous }()
	attempts := 0
	http.DefaultClient = &http.Client{Transport: providerTimeoutTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	})}
	timeoutMS := -1
	maxRetries := 0
	model := codexModel("https://fixture.invalid")
	stream := StreamOpenAICodexResponses(model, NormalizeContext(Context{
		Messages: []Message{&UserMessage{Content: StringOrBlocks{Text: "hi"}, Timestamp: 1}},
	}), &OpenAICodexResponsesOptions{
		APIKey: codexToken(t, "acct-1"), TimeoutMs: &timeoutMS, MaxRetries: &maxRetries,
		Transport: "sse",
	})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	message, err := stream.Result(ctx)
	if err != nil || message == nil || message.StopReason != StopError || message.ErrorMessage == nil ||
		!strings.Contains(strings.ToLower(*message.ErrorMessage), "invalid timeoutms") || attempts != 0 {
		t.Fatalf("message=%+v error=%v attempts=%d; want invalid timeout before fetch", message, err, attempts)
	}
}

func TestCodexAbortSignalRejectsOutOfRangeDelayBeforeFetch(t *testing.T) {
	if int64(int(jsnum.MaxAbortSignalTimeoutMS+1)) != jsnum.MaxAbortSignalTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	previous := http.DefaultClient
	defer func() { http.DefaultClient = previous }()
	attempts := 0
	http.DefaultClient = &http.Client{Transport: providerTimeoutTransport(func(request *http.Request) (*http.Response, error) {
		attempts++
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: http.NoBody, Request: request}, nil
	})}
	timeoutMS := int(jsnum.MaxAbortSignalTimeoutMS + 1)
	request, err := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := DoProviderRequestWithAbortSignalTimeout(request, &timeoutMS)
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "out of range") || attempts != 0 {
		t.Fatalf("error=%v attempts=%d; want AbortSignal range error before fetch", err, attempts)
	}
}

type providerTimeoutTransport func(*http.Request) (*http.Response, error)

func TestProviderDispatcherHeaderTimerKeepsLargeFastTimerDelay(t *testing.T) {
	if int64(int(jsnum.MaxTimeoutMS+1)) != jsnum.MaxTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	previous := http.DefaultClient
	defer func() { http.DefaultClient = previous }()
	http.DefaultClient = &http.Client{Transport: providerTimeoutTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	timeoutMS := int(jsnum.MaxTimeoutMS + 1)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = DoProviderRequest(request, &timeoutMS)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("elapsed=%v error=%v; want the caller deadline, not a clamped SDK timer", time.Since(start), err)
	}
	if elapsed := time.Since(start); elapsed < 10*time.Millisecond {
		t.Fatalf("dispatcher header timeout fired after %v; want its raw FastTimer delay", elapsed)
	}
}

func (transport providerTimeoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

// Undici's bodyTimeout keeps the configured millisecond delay, unlike Node
// setTimeout. Convert it without clamping and saturate only where Go's
// time.Duration cannot represent the setting.
func TestGuardIdleBodyPreservesFastTimerDelayAndSaturatesDuration(t *testing.T) {
	if int64(int(jsnum.MaxTimeoutMS+1)) != jsnum.MaxTimeoutMS+1 {
		t.Skip("requires 64-bit int")
	}
	cases := []struct {
		name      string
		timeoutMS int
		want      time.Duration
	}{
		{
			name:      "above Node timer range",
			timeoutMS: int(jsnum.MaxTimeoutMS + 1),
			want:      time.Duration(jsnum.MaxTimeoutMS+1) * time.Millisecond,
		},
	}
	maxDuration := time.Duration(1<<63 - 1)
	largeTimeoutMS := int64(maxDuration/time.Millisecond) + 1
	if largeTimeoutMS <= int64(^uint(0)>>1) {
		cases = append(cases, struct {
			name      string
			timeoutMS int
			want      time.Duration
		}{"above Go duration range", int(largeTimeoutMS), maxDuration})
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			body := guardIdleBody(ctx, cancel, io.NopCloser(strings.NewReader("")), &test.timeoutMS)
			defer body.Close()
			guarded, ok := body.(*idleStreamBody)
			if !ok {
				t.Fatalf("body type = %T; want *idleStreamBody", body)
			}
			if guarded.timeout != test.want {
				t.Fatalf("body timeout = %v; want %v", guarded.timeout, test.want)
			}
		})
	}
}

// Reference: pi v1.5.2 core/http-dispatcher.ts keeps the dispatcher idle
// timeout separate from the fetch deadline, so a classifier request with no
// idle setting must report a connection failure rather than touch an absent
// idle timer (D254).
func TestClassifierRequestWithoutIdleTimeoutReportsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()
	request, err := http.NewRequest(http.MethodPost, address, strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	timeoutMS := 5000
	response, err := doClassifierRequest(request, &timeoutMS, nil)
	if response != nil || err == nil {
		t.Fatalf("response = %+v, err = %v; want a request failure", response, err)
	}
}

// Reference: pi v1.5.2 core/http-dispatcher.ts arms bodyTimeout independently
// of headersTimeout, so a body-only idle timeout still fires.
func TestIdleBodyArmsABodyOnlyTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body := newIdleBodyWithTimeouts(ctx, cancel, 0, 20*time.Millisecond)
	body.gotHeaders.Store(true)
	// A read is in flight, so the timer counts the wait for bytes.
	body.blocked.Store(true)
	body.rc = io.NopCloser(strings.NewReader(""))
	defer body.Close()
	if body.timer == nil {
		t.Fatal("body-only idle timeout armed no timer")
	}
	deadline := time.Now().Add(time.Second)
	for !body.fired.Load() {
		if time.Now().After(deadline) {
			t.Fatal("body-only idle timeout never fired")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
