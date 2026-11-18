package coding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestFetchWithRetryKeepsBodyReadableWithAttemptTimeout pins the per-attempt
// timeout semantics: the attempt budget must not abort a body the caller reads
// after FetchWithRetry returns. Canceling the attempt context right after Do
// aborted every body read under AttemptTimeoutMS.
func TestFetchWithRetryKeepsBodyReadableWithAttemptTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		time.Sleep(50 * time.Millisecond)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()

	attemptTimeout := int64(2000)
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := FetchWithRetry(context.Background(), request, http.DefaultClient, FetchRetryOptions{AttemptTimeoutMS: &attemptTimeout})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("body read after FetchWithRetry: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body = %q", body)
	}
}

// TestFetchWithRetryRetriesOnAttemptTimeout keeps the per-attempt abort
// observable: a hung attempt is retried and the second response is returned.
func TestFetchWithRetryRetriesOnAttemptTimeout(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			// Hang until the client abandons the attempt (closing the server
			// would otherwise wait out a fixed sleep).
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))
	defer server.Close()

	attemptTimeout := int64(50)
	maxRetries := 1
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := FetchWithRetry(context.Background(), request, http.DefaultClient, FetchRetryOptions{
		AttemptTimeoutMS: &attemptTimeout,
		MaxRetries:       &maxRetries,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("body read: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

type stalledManagementBody struct {
	ctx    context.Context
	closed bool
}

func (b *stalledManagementBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stalledManagementBody) Close() error {
	b.closed = true
	return nil
}

// pi v1.4.0 (4a080252), utils/management-http.ts fetchWithRetry cancels
// abandoned response bodies without reading them. A 503 body can stall even
// though retrying immediately would succeed within the overall budget.
func TestFetchWithRetryDoesNotDrainStalledRetryBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		attempts := 0
		var abandoned *stalledManagementBody
		client := &http.Client{Transport: managementTimeoutTransport(func(request *http.Request) (*http.Response, error) {
			attempts++
			if attempts == 1 {
				abandoned = &stalledManagementBody{ctx: request.Context()}
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: abandoned}, nil
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
		})}
		request, err := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
		if err != nil {
			t.Fatal(err)
		}
		timeout := int64(1000)
		response, err := FetchWithRetry(context.Background(), request, client, FetchRetryOptions{TimeoutMS: &timeout})
		if err != nil {
			t.Fatalf("retry waited for abandoned response body: %v (attempts=%d)", err, attempts)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || string(body) != "ok" || attempts != 2 {
			t.Fatalf("body=%q err=%v attempts=%d; want ok, nil, 2", body, err, attempts)
		}
		if !abandoned.closed {
			t.Fatal("abandoned response body not closed")
		}
	})
}
