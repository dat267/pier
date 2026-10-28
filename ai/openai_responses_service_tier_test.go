package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// pi v1.4.0 (4a080252), api/openai-responses.ts getServiceTierCostMultiplier
// and openai-responses-shared.ts finalizeResponse price the returned tier even
// without a request override. Each token class contributes one million tokens
// at rates 1/2/3/4, so the unscaled cost is exactly 10.
func TestResponsesPricesTheReturnedServiceTier(t *testing.T) {
	for _, c := range []struct {
		name, request, response string
		want                    float64
		modelID                 string
	}{
		{"response priority", "", `,"service_tier":"priority"`, 20, ""},
		{"response flex", "", `,"service_tier":"flex"`, 5, ""},
		{"response overrides request", "flex", `,"service_tier":"priority"`, 20, ""},
		{"response default overrides request", "priority", `,"service_tier":"default"`, 10, ""},
		{"request fallback", "priority", "", 20, ""},
		{"default", "", "", 10, ""},
		{"response fast", "", `,"service_tier":"fast"`, 20, ""},
		{"response fast gpt-5.5", "", `,"service_tier":"fast"`, 25, "gpt-5.5"},
		{"request fast fallback", "fast", "", 20, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":3000000,\"output_tokens\":1000000,\"total_tokens\":4000000,\"input_tokens_details\":{\"cached_tokens\":1000000,\"cache_write_tokens\":1000000}}%s}}\n\n", c.response)
			}))
			defer server.Close()
			model := testResponsesModel()
			if c.modelID != "" {
				model.ID = c.modelID
			}
			model.BaseURL = server.URL
			model.Cost = ModelCost{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}
			result, err := StreamOpenAIResponses(model, TranscriptContext{Messages: []Message{
				&UserMessage{Content: StringOrBlocks{Text: "hello"}, Timestamp: 1},
			}}, &OpenAIResponsesOptions{StreamOptions: StreamOptions{APIKey: "test-key"}, ServiceTier: c.request}).Result(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if result.StopReason != StopStop {
				t.Fatalf("stop = %s, error = %v", result.StopReason, result.ErrorMessage)
			}
			if result.Usage.Cost.Total != c.want {
				t.Fatalf("cost = %+v, want total %v", result.Usage.Cost, c.want)
			}
		})
	}
}
