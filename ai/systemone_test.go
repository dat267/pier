package ai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 (4a080252) packages/ai/src/api/system-one-shared.ts
// serializes OpenRouter image_url state parts with the declaration order
// {type, image_url: {url}}.
func TestTypeSafeSystemOneOpenRouterImagePayloadOrder(t *testing.T) {
	var requestBody string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		requestBody = string(body)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"answers":{"approved":{"type":"noul","noul":0.95}}}`)
	}))
	defer server.Close()

	model := &Model{
		ID: "typesafe/jev-1.13", Name: "Jev", API: APITypeSafeSystemOne,
		Provider: "openrouter", BaseURL: server.URL + "/v1", Type: "classifier",
		Input: []string{"text", "image"},
	}
	result := (TypeSafeSystemOne{}).Classify(model, ClassifierContext{
		State:  []byte(`{"text":"hello"}`),
		Images: []ImageContent{{MimeType: "image/png", Data: "cG5n"}},
		Questions: []ClassifierQuestion{{
			Name: "approved", Type: "bool", Instructions: "Approve?",
			BoolCriteria: ClassifierBoolCriteria{True: "yes", False: "no"},
		}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("classification = %+v", result)
	}
	want := `{"model":"typesafe/jev-1.13","state":[{"type":"image_url","image_url":{"url":"data:image/png;base64,cG5n"}},{"text":"hello"}],"questions":{"approved":{"type":"noul","instructions":"Approve?","criteria":{"true":"yes","false":"no"}}}}`
	if strings.TrimSpace(requestBody) != want {
		t.Fatalf("request body = %s\nwant         = %s", requestBody, want)
	}
}
