package coding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
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
			time.Sleep(500 * time.Millisecond)
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
