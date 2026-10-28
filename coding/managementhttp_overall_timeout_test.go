package coding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

type managementTimeoutTransport func(*http.Request) (*http.Response, error)

func (f managementTimeoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFetchWithRetryReturnedBodyRetainsAndReleasesOverallBudget(t *testing.T) {
	for _, closeEarly := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var requestCtx context.Context
			client := &http.Client{Transport: managementTimeoutTransport(func(request *http.Request) (*http.Response, error) {
				requestCtx = request.Context()
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("body"))}, nil
			})}
			request, _ := http.NewRequest(http.MethodGet, "https://fixture.invalid", nil)
			timeout := int64(1000)
			response, err := FetchWithRetry(context.Background(), request, client, FetchRetryOptions{TimeoutMS: &timeout})
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if requestCtx.Err() != nil {
				t.Fatal("overall budget ended at response headers")
			}
			if closeEarly {
				_ = response.Body.Close()
				if requestCtx.Err() != context.Canceled {
					t.Fatalf("body close did not release overall budget: %v", requestCtx.Err())
				}
			} else {
				time.Sleep(time.Second)
				synctest.Wait()
				if requestCtx.Err() != context.DeadlineExceeded {
					t.Fatalf("returned body lost overall deadline: %v", requestCtx.Err())
				}
			}
		})
	}
}

// pi v1.4.0 (4a080252), utils/management-http.ts keeps its overall abort
// signal alive after returning fetch's response. Receiving headers is not
// completion of the body: a caller must still be able to consume it.
func TestFetchWithRetryOverallTimeoutKeepsReturnedBodyReadable(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-release:
			_, _ = io.WriteString(w, "body-after-headers")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	timeout := int64(2000)
	response, err := FetchWithRetry(context.Background(), request, server.Client(), FetchRetryOptions{TimeoutMS: &timeout})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "body-after-headers" {
		t.Fatalf("returned response body was cancelled at headers: %q, %v", body, err)
	}
}
