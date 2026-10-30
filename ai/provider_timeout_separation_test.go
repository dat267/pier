package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// Upstream's SDK timeout covers fetch headers, while httpIdleTimeoutMs remains
// the independent Undici bodyTimeout for the stream after response headers.
func TestSDKTimeoutDoesNotReplaceDispatcherBodyIdleTimeout(t *testing.T) {
	previous := http.DefaultClient
	defer func() { http.DefaultClient = previous }()
	for _, test := range []struct {
		name          string
		idleTimeoutMS int
	}{
		{name: "disabled", idleTimeoutMS: 0},
		{name: "longer global timeout", idleTimeoutMS: 250},
	} {
		t.Run(test.name, func(t *testing.T) {
			http.DefaultClient = &http.Client{Transport: timeoutSeparationTransport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header),
					Body: &waitingProviderBody{ctx: request.Context()}, Request: request,
				}, nil
			})}
			sdkTimeoutMS := 10
			ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://fixture.invalid", nil)
			if err != nil {
				t.Fatal(err)
			}
			idleTimeout := test.idleTimeoutMS
			response, err := DoProviderRequestWithSDKTimeoutAndIdle(request, &sdkTimeoutMS, &idleTimeout)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			_, err = io.ReadAll(response.Body)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("body error = %v; per-request %dms timeout must not replace dispatcher idle timeout %dms", err, sdkTimeoutMS, test.idleTimeoutMS)
			}
		})
	}
}

func TestMistralSDKTimeoutDoesNotReplaceDispatcherBodyIdleTimeout(t *testing.T) {
	server := stallServer(t, true, "")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	providerTimeoutMS := 10
	idleTimeoutMS := 250
	response, err := requestMistralStream(ctx, mistralModel(server.URL), []byte(`{}`), &MistralOptions{
		APIKey: "test", TimeoutMs: &providerTimeoutMS, HTTPIdleTimeoutMs: &idleTimeoutMS,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, err = io.ReadAll(response.Body)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("body error = %v; Mistral's 10ms request timeout must not replace the 250ms dispatcher timeout", err)
	}
}

func TestDispatcherOnlyTimeoutIgnoresProviderOverride(t *testing.T) {
	previous := http.DefaultClient
	defer func() { http.DefaultClient = previous }()
	http.DefaultClient = &http.Client{Transport: timeoutSeparationTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	providerTimeoutMS := 10
	idleTimeoutMS := 250
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err = DoProviderRequestWithIdleTimeout(request, &providerTimeoutMS, &idleTimeoutMS)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("elapsed=%v error=%v; non-SDK provider timeout must not replace dispatcher timeout", time.Since(start), err)
	}
}

type timeoutSeparationTransport func(*http.Request) (*http.Response, error)

func (transport timeoutSeparationTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type waitingProviderBody struct {
	ctx context.Context
}

func (body *waitingProviderBody) Read([]byte) (int, error) {
	<-body.ctx.Done()
	return 0, body.ctx.Err()
}

func (*waitingProviderBody) Close() error { return nil }
