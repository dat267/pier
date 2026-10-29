package coding

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
)

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
