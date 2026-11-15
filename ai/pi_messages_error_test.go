package ai

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) api/pi-messages.ts formatPiMessagesResponseError
// uses `error.message ?? body`, which keeps an empty message string (a string is
// not nullish) and drops the body. Pier fell back to the body whenever the
// message was empty.
func TestPiMessagesErrorKeepsAnEmptyMessageInsteadOfTheBody(t *testing.T) {
	model := &Model{Provider: "pi", ID: "m", BaseURL: "https://example.test"}
	requestURL, err := url.Parse("https://example.test/messages")
	if err != nil {
		t.Fatal(err)
	}
	response := &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request"}
	body := `{"error":{"message":"","code":"invalid_request"}}`
	created := createPiMessagesResponseError(model, requestURL, response, body)
	want := "400 Bad Request:  (invalid_request)"
	if created.Message != want {
		t.Fatalf("message = %q, want %q", created.Message, want)
	}
	if strings.Contains(created.Message, "error") {
		t.Fatalf("message leaked the body: %q", created.Message)
	}
}
