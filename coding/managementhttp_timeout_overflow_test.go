package coding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// pi v1.4.0 management-http.ts passes positive timeoutMs to AbortSignal.timeout;
// Node's timer clamps values above 2^31-1 to 1ms.
func TestFetchWithRetryRejectsOutOfRangeAbortSignalTimeout(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	timeoutMS := int64(4_294_967_296)
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := FetchWithRetry(context.Background(), request, server.Client(), FetchRetryOptions{TimeoutMS: &timeoutMS})
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "out of range") {
		t.Fatalf("request error = %v, want AbortSignal timeout range error", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("fetch requests = %d, want timeout validation before fetch", got)
	}
}

func TestFetchWithRetryRejectsOutOfRangeAttemptTimeout(t *testing.T) {
	var requests atomic.Int32
	client := &http.Client{Transport: managementTimeoutTransport(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header), Request: request}, nil
	})}
	timeoutMS := int64(4_294_967_296)
	request, err := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := FetchWithRetry(context.Background(), request, client, FetchRetryOptions{AttemptTimeoutMS: &timeoutMS})
	if response != nil {
		response.Body.Close()
	}
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "out of range") {
		t.Fatalf("request error = %v, want AbortSignal timeout range error", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("fetch requests = %d, want timeout validation before fetch", got)
	}
}

func TestFetchWithRetryClampsLargeAttemptTimeout(t *testing.T) {
	client := &http.Client{Transport: managementTimeoutTransport(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	timeoutMS := int64(2_147_483_648)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://fixture.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = FetchWithRetry(ctx, request, client, FetchRetryOptions{AttemptTimeoutMS: &timeoutMS})
	if err == nil {
		t.Fatal("request succeeded; want the attempt timeout to abort it")
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("request ended after %s, want JavaScript's 1ms timer clamp", elapsed)
	}
}

func TestFetchWithRetryClampsLargeAbortSignalTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-release
	}))
	defer server.Close()
	defer close(release)

	timeoutMS := int64(2_147_483_648)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	response, err := FetchWithRetry(ctx, request, server.Client(), FetchRetryOptions{TimeoutMS: &timeoutMS})
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("request succeeded; want the configured timeout to abort it")
	}
	if elapsed := time.Since(started); elapsed > 150*time.Millisecond {
		t.Fatalf("request ended after %s, want JavaScript's 1ms timer clamp", elapsed)
	}
}
