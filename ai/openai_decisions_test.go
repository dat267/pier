package ai

// OpenAI Decisions behavior follows the pinned API implementation and its
// tests in pi/packages/ai/src/api/openai-decisions.ts and test/openai-decisions.test.ts.

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Reference: pi v1.4.0 packages/ai/test/openai-decisions.test.ts maps the
// choice, score and bool question variants to Decisions and parses answers by name.
func TestOpenAIDecisionsRejectsOtherAPIsAndMissingKeys(t *testing.T) {
	requests := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	context := ClassifierContext{State: json.RawMessage(`{}`)}
	otherAPI := *model
	otherAPI.API = "other-classifier-api"
	unsupported := (OpenAIDecisions{}).Classify(&otherAPI, context, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	missingKey := (OpenAIDecisions{}).Classify(model, context, nil)
	if len(requests) != 0 || unsupported.ErrorMessage == nil || !strings.Contains(*unsupported.ErrorMessage, "Unsupported classifier API: other-classifier-api") ||
		missingKey.ErrorMessage == nil || !strings.Contains(*missingKey.ErrorMessage, "No API key for provider: openai") {
		t.Fatalf("unsupported=%+v missing-key=%+v requests=%d", unsupported, missingKey, len(requests))
	}
}

func TestOpenAIDecisionsIncludesHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"Decision input exceeds the token limit."}}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	zero := 0
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret", MaxRetries: &zero}})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "OpenAI Decisions error (400)") || !strings.Contains(*result.ErrorMessage, "Decision input exceeds the token limit.") {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsPreservesPrototypeSensitiveQuestionIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[{"type":"predicate","name":"__proto__","probability":0.75}]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{
		State: json.RawMessage(`{}`), Questions: []ClassifierQuestion{{Name: "__proto__", Type: "bool"}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	encoded, err := MarshalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != ClassifierStopStop || result.Answers["__proto__"].Probability == nil ||
		!strings.Contains(string(encoded), `"answers":{"__proto__":{"type":"bool","probability":0.75}}`) {
		t.Fatalf("result=%+v json=%s", result, encoded)
	}
}

func TestOpenAIDecisionsPreservesEmptyProbabilityValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[{"type":"choice","name":"category","choice":"","probabilities":[{"value":"","probability":1}],"confidence":1}]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{
		State: json.RawMessage(`{}`), Questions: []ClassifierQuestion{{Name: "category", Type: "choice"}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop || result.Answers["category"].Probabilities[""] != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsAcceptsEmptyChoiceValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[{"type":"choice","name":"category","choice":"","probabilities":[],"confidence":0.5}]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{
		State: json.RawMessage(`{}`), Questions: []ClassifierQuestion{{Name: "category", Type: "choice"}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop || result.Answers["category"].Choice != "" {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsRejectsNullRequiredNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[{"type":"choice","name":"category","choice":"ok","probabilities":[{"value":"ok","probability":1}],"confidence":null}]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{
		State: json.RawMessage(`{}`), Questions: []ClassifierQuestion{{Name: "category", Type: "choice"}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "invalid confidence for category") {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsRetriesOtherServerErrors(t *testing.T) {
	requests := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		if len(requests) == 1 {
			w.Header().Set("Retry-After-Ms", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("busy"))
			return
		}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if len(requests) != 2 || result.StopReason != ClassifierStopStop {
		t.Fatalf("requests = %d; result = %+v", len(requests), result)
	}
}

func TestOpenAIDecisionsUsesLongContextPricing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[],"usage":{"input_tokens":300000,"output_tokens":0}}`))
	}))
	defer server.Close()
	model := &Model{
		ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier",
		Input: []string{"text"}, Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 0.1}, Tiers: []ModelCostTier{{ModelCostRates: ModelCostRates{Input: 0.2}, InputTokensAbove: 272000}}},
	}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop || result.Usage == nil || math.Abs(result.Usage.Cost.Total-0.06) > 1e-12 {
		t.Fatalf("result usage = %+v; result = %+v", result.Usage, result)
	}
}

func TestOpenAIDecisionsSerializesImagesWithState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []struct {
				Role    string `json:"role"`
				Content []struct {
					Type     string `json:"type"`
					Text     string `json:"text"`
					ImageURL string `json:"image_url"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(request.Input) != 1 || request.Input[0].Role != "user" || len(request.Input[0].Content) != 3 ||
			request.Input[0].Content[0].Type != "input_text" || request.Input[0].Content[0].Text != `{"text":"hi"}` ||
			request.Input[0].Content[1].ImageURL != "data:image/png;base64,aW1hZ2U=" || request.Input[0].Content[2].ImageURL != "data:image/jpeg;base64,aW1hZ2U=" {
			t.Errorf("input = %+v", request.Input)
		}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text", "image"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{
		State: json.RawMessage(`{"text":"hi"}`), Images: []ImageContent{{Data: "aW1hZ2U=", MimeType: "image/png"}, {Data: "aW1hZ2U=", MimeType: "image/jpeg"}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsDoesNotRetryGatewayTimeout(t *testing.T) {
	requests := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct{}{}
		w.Header().Set("Retry-After-Ms", "0")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("<html>Gateway time-out</html>"))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if len(requests) != 1 {
		t.Fatalf("requests = %d, want one", len(requests))
	}
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "request timed out at the gateway") || strings.Contains(*result.ErrorMessage, "<html>") {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsStringifiesEmptyStringStateWithImages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input []struct {
				Content []struct {
					Type     string  `json:"type"`
					Text     *string `json:"text"`
					ImageURL string  `json:"image_url"`
				} `json:"content"`
			} `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(request.Input) != 1 || len(request.Input[0].Content) != 2 || request.Input[0].Content[0].Text == nil || *request.Input[0].Content[0].Text != `""` || request.Input[0].Content[1].ImageURL != "data:;base64," {
			t.Errorf("input = %+v", request.Input)
		}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text", "image"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`""`), Images: []ImageContent{{}}}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsKeepsEmptyCriteriaArrays(t *testing.T) {
	model := &Model{ID: "classifier", API: APIOpenAIDecisions}
	payload, err := buildOpenAIDecisionsPayload(model, ClassifierContext{
		State: json.RawMessage(`{}`),
		Questions: []ClassifierQuestion{
			{Name: "choice", Type: "choice"},
			{Name: "score", Type: "score"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"model":"classifier","input":"{}","questions":[{"type":"choice","name":"choice","instructions":"","choices":[]},{"type":"score","name":"score","instructions":"","levels":[]}]}`
	if string(encoded) != want {
		t.Fatalf("payload = %s, want %s", encoded, want)
	}
}

func TestOpenAIDecisionsStringifiesStateNumbersLikeJavaScript(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Input != `{"float":1,"negativeZero":0}` {
			t.Errorf("input = %q", request.Input)
		}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	state := json.RawMessage(`{"float":1.0,"negativeZero":-0}`)
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: state}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsStringifiesNonObjectJSONState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Input != `["x"]` {
			t.Errorf("input = %q", request.Input)
		}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`["x"]`)}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsAcceptsWhitespaceAroundJSONState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Input string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Input != `{"text":1}` {
			t.Errorf("input = %q", request.Input)
		}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(" \n { \"text\": 1 }  ")}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsRejectsInvalidPayloadTransformBeforeRequest(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{
		APIKey:    "secret",
		OnPayload: func(json.RawMessage, *Model) json.RawMessage { return json.RawMessage(`{`) },
	}})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || len(requests) != 0 {
		t.Fatalf("result = %+v, requests = %d", result, len(requests))
	}
}

func TestOpenAIDecisionsIgnoresNonObjectAnswerEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[42]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{
		State: json.RawMessage(`{}`), Questions: []ClassifierQuestion{{Name: "approved", Type: "bool"}},
	}, &ClassifierOptions{StreamOptions: StreamOptions{APIKey: "secret"}})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || *result.ErrorMessage != "OpenAI Decisions did not return an answer for approved" {
		t.Fatalf("result = %+v", result)
	}
}

func TestOpenAIDecisionsConvertsResponseHookPanicsToErrorResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"answers":[]}`))
	}))
	defer server.Close()
	model := &Model{ID: "classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	result := (OpenAIDecisions{}).Classify(model, ClassifierContext{State: json.RawMessage(`{}`)}, &ClassifierOptions{StreamOptions: StreamOptions{
		APIKey:     "secret",
		OnResponse: func(ProviderResponse, *Model) { panic("response hook failed") },
	}})
	if result.StopReason != ClassifierStopError || result.ErrorMessage == nil || !strings.Contains(*result.ErrorMessage, "response hook failed") {
		t.Fatalf("result = %+v", result)
	}
}

func TestModelsClassifyAppliesResolvedAuthAndHeaderTransform(t *testing.T) {
	responseCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer explicit" {
			t.Errorf("authorization = %q", got)
		}
		if got := r.Header.Get("X-Classifier-Test"); got != "transformed" {
			t.Errorf("transformed header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[{"type":"predicate","name":"approved","probability":0.9}]}`))
	}))
	defer server.Close()
	model := &Model{ID: "test-classifier", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1", Type: "classifier", Input: []string{"text"}}
	provider := CreateProvider(CreateProviderOptions{
		ID: "openai", Auth: ProviderAuth{APIKey: EnvApiKeyAuth("OpenAI API key", []string{"OPENAI_API_KEY"})},
		Models: []*Model{model}, Classifiers: map[string]ProviderClassifier{APIOpenAIDecisions: OpenAIDecisions{}},
	})
	models := CreateModels(nil)
	models.SetProvider(provider)
	result := models.Classify(model, ClassifierContext{
		State: json.RawMessage(`{}`), Questions: []ClassifierQuestion{{Name: "approved", Type: "bool"}},
	}, &ModelsClassifierOptions{ClassifierOptions: ClassifierOptions{StreamOptions: StreamOptions{
		APIKey: "explicit",
		TransformHeaders: func(headers ProviderHeaders) ProviderHeaders {
			if headers == nil {
				headers = ProviderHeaders{}
			}
			value := "transformed"
			headers["X-Classifier-Test"] = &value
			return headers
		},
		OnResponse: func(response ProviderResponse, _ *Model) {
			responseCalled = response.Status == http.StatusOK && response.Headers["content-type"] == "application/json"
		},
	}}})
	if result.StopReason != ClassifierStopStop || !responseCalled {
		t.Fatalf("result = %+v; response callback = %v", result, responseCalled)
	}
}

func TestOpenAIDecisionsMapsQuestionVariantsAndPricesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/decisions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("authorization = %q", got)
		}
		var request struct {
			Model     string `json:"model"`
			Input     string `json:"input"`
			Questions []struct {
				Type         string `json:"type"`
				Name         string `json:"name"`
				Instructions string `json:"instructions"`
				Choices      []struct {
					Value       string `json:"value"`
					Description string `json:"description"`
				} `json:"choices"`
				Levels []struct {
					Label string `json:"label"`
				} `json:"levels"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Model != "gpt-6-luna" || request.Input != `{"text":"hi"}` || len(request.Questions) != 3 {
			t.Errorf("request = %+v", request)
		} else {
			choice, score, predicate := request.Questions[0], request.Questions[1], request.Questions[2]
			if choice.Type != "choice" || choice.Name != "category" || len(choice.Choices) != 2 || choice.Choices[1].Description != "" {
				t.Errorf("choice question = %+v", choice)
			}
			if score.Type != "score" || len(score.Levels) != 3 || score.Levels[2].Label != "high" {
				t.Errorf("score question = %+v", score)
			}
			if predicate.Type != "predicate" || predicate.Instructions != "Does the user approve?\n\nTrue means: yes\nFalse means: no" {
				t.Errorf("predicate question = %+v", predicate)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"answers":[{"type":"predicate","name":"approved","probability":0.95},{"type":"score","name":"satisfaction","score":1.8,"confidence":0.7},{"type":"choice","name":"category","choice":"success","probabilities":[{"value":"success","probability":0.9},{"value":"failure","probability":0.1}],"confidence":0.8}],"usage":{"input_tokens":164,"output_tokens":0}}`))
	}))
	defer server.Close()

	model := &Model{
		ID: "gpt-6-luna", API: APIOpenAIDecisions, Provider: "openai", BaseURL: server.URL + "/v1",
		Type: "classifier", Input: []string{"text", "image"}, ContextWindow: 922000,
		Cost: ModelCost{ModelCostRates: ModelCostRates{Input: 0.1}},
	}
	classifierContext := ClassifierContext{
		State: json.RawMessage(`{"text":"hi"}`),
		Questions: []ClassifierQuestion{
			{Name: "category", Type: "choice", Instructions: "Classify", Choices: []ClassifierChoice{{Value: "success", Description: "Successful"}, {Value: "failure"}}},
			{Name: "satisfaction", Type: "score", Instructions: "Score", Levels: []string{"low", "neutral", "high"}},
			{Name: "approved", Type: "bool", Instructions: "Does the user approve?", BoolCriteria: ClassifierBoolCriteria{True: "yes", False: "no"}},
		},
	}
	result := (OpenAIDecisions{}).Classify(model, classifierContext, &ClassifierOptions{StreamOptions: StreamOptions{Ctx: context.Background(), APIKey: "secret"}})
	if result.StopReason != ClassifierStopStop {
		t.Fatalf("result = %+v", result)
	}
	if got := result.Answers["category"]; got.Type != "choice" || got.Choice != "success" || got.Probabilities["failure"] != 0.1 || got.Confidence == nil || *got.Confidence != 0.8 {
		t.Fatalf("choice = %+v", got)
	}
	if got := result.Answers["satisfaction"]; got.Type != "score" || got.Score == nil || *got.Score != 1.8 || got.Confidence == nil || *got.Confidence != 0.7 {
		t.Fatalf("score = %+v", got)
	}
	if got := result.Answers["approved"]; got.Type != "bool" || got.Probability == nil || *got.Probability != 0.95 {
		t.Fatalf("bool = %+v", got)
	}
	if result.Usage == nil || result.Usage.Input != 164 || result.Usage.Output != 0 || math.Abs(result.Usage.Cost.Total-0.0000164) > 1e-18 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	encoded, err := MarshalJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(encoded)
	category, satisfaction, approved := strings.Index(serialized, `"category":`), strings.Index(serialized, `"satisfaction":`), strings.Index(serialized, `"approved":`)
	if category < 0 || category >= satisfaction || satisfaction >= approved {
		t.Fatalf("answer order = %s", serialized)
	}
	success, failure := strings.Index(serialized, `"success":0.9`), strings.Index(serialized, `"failure":0.1`)
	if success < 0 || success >= failure {
		t.Fatalf("probability order = %s", serialized)
	}
	if !strings.Contains(serialized, `"satisfaction":{"type":"score","score":1.8,"confidence":0.7}`) {
		t.Fatalf("score answer order = %s", serialized)
	}
	stop, timestamp, usage := strings.Index(serialized, `"stopReason":"stop"`), strings.Index(serialized, `"timestamp":`), strings.Index(serialized, `"usage":`)
	if stop < 0 || stop >= timestamp || timestamp >= usage {
		t.Fatalf("result field order = %s", serialized)
	}
}
