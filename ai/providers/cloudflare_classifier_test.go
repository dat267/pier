package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dat267/pier/ai"
)

// Reference: pi v1.4.0 (4a080252) providers/cloudflare-workers-ai.ts,
// providers/cloudflare-stream.ts, api/cloudflare-workers-ai-system-one.ts, and
// api/system-one-shared.ts.
func TestCloudflareWorkersAIClassifierProvider(t *testing.T) {
	provider := CloudflareWorkersAIProvider()
	models := ai.CreateModels(&ai.CreateModelsOptions{AuthContext: cloudflareEnv{
		CloudflareAccountIDEnv: "account-id",
	}})
	models.SetProvider(provider)
	if model := models.GetModel("cloudflare-workers-ai", "typesafe/jev"); model != nil {
		t.Fatalf("classifier unexpectedly appears in chat model lookup: %+v", model)
	}
	classifierModels := provider.GetClassifierModels()
	if len(classifierModels) != 3 {
		t.Fatalf("classifier models = %d, want 3: %+v", len(classifierModels), classifierModels)
	}
	var model *ai.Model
	for _, candidate := range classifierModels {
		if candidate.ID == "typesafe/jev" {
			model = candidate
			break
		}
	}
	if model == nil || model.API != ai.APICloudflareWorkersAISystemOne {
		t.Fatalf("Cloudflare Jev classifier = %+v", model)
	}
	if provider.GetClassifierImplementations()[model.API] == nil {
		t.Fatalf("classifier implementation for %q is missing", model.API)
	}

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/accounts/account-id/ai/run" {
			t.Errorf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer cf-key" {
			t.Errorf("authorization = %q", request.Header.Get("Authorization"))
		}
		var payload struct {
			Model string `json:"model"`
			Input struct {
				Model     string                    `json:"model"`
				Questions map[string]map[string]any `json:"questions"`
			} `json:"input"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		} else if payload.Model != model.ID || payload.Input.Model != "" || payload.Input.Questions["urgent"]["type"] != "noul" {
			t.Errorf("payload = %+v, Cloudflare input must not repeat model", payload)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"success":true,"result":{"state":"Completed","result":{"answers":{"urgent":{"type":"noul","noul":0.95}},"usage":{"input_tokens":426,"output_tokens":73}}}}`)
	}))
	defer server.Close()

	requestModel := *model
	requestModel.BaseURL = server.URL + "/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai"
	result := models.Classify(&requestModel, ai.ClassifierContext{
		State: []byte(`{"message":"Help!"}`),
		Questions: []ai.ClassifierQuestion{{
			Name: "urgent", Type: "bool", Instructions: "Is it urgent?",
			BoolCriteria: ai.ClassifierBoolCriteria{True: "yes", False: "no"},
		}},
	}, &ai.ModelsClassifierOptions{ClassifierOptions: ai.ClassifierOptions{StreamOptions: ai.StreamOptions{
		APIKey: "cf-key",
	}}})
	if result.StopReason != ai.ClassifierStopStop || result.Answers["urgent"].Probability == nil || *result.Answers["urgent"].Probability != 0.95 {
		t.Fatalf("classification = %+v", result)
	}
	if result.Usage == nil || result.Usage.Input != 426 || result.Usage.Output != 73 || result.Usage.TotalTokens != 499 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestCloudflareWorkersAIClassifierDirectResult(t *testing.T) {
	provider := CloudflareWorkersAIProvider()
	model := ai.GetBuiltinClassifierModel("cloudflare-workers-ai", "@cf/cloudflare/clef")
	if model == nil {
		t.Fatal("Cloudflare Clef classifier is missing")
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{"success":true,"result":{"model":"clef","answers":{"approved":{"type":"noul","noul":0.9912}},"usage":{"input_tokens":222,"output_tokens":0}}}`)
	}))
	defer server.Close()
	requestModel := *model
	requestModel.BaseURL = server.URL + "/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai"
	result := provider.Classify(&requestModel, ai.ClassifierContext{
		State:     []byte(`{"message":"hello"}`),
		Questions: []ai.ClassifierQuestion{{Name: "approved", Type: "bool", BoolCriteria: ai.ClassifierBoolCriteria{True: "yes", False: "no"}}},
	}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{
		APIKey: "cf-key", Env: ai.ProviderEnv{CloudflareAccountIDEnv: "account-id"},
	}})
	if result.StopReason != ai.ClassifierStopStop || result.Answers["approved"].Probability == nil || *result.Answers["approved"].Probability != 0.9912 {
		t.Fatalf("classification = %+v", result)
	}
	if result.Usage == nil || result.Usage.Input != 222 || result.Usage.TotalTokens != 222 || result.Usage.Cost.Input <= 0 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestCloudflareWorkersAIClassifierErrors(t *testing.T) {
	provider := CloudflareWorkersAIProvider()
	model := ai.GetBuiltinClassifierModel("cloudflare-workers-ai", "typesafe/jev")
	if model == nil {
		t.Fatal("Cloudflare Jev classifier is missing")
	}
	for _, test := range []struct {
		name, response, wantError string
	}{
		{
			name:      "REST error envelope",
			response:  `{"success":false,"errors":[{"code":5007,"message":"No such model"}],"result":null}`,
			wantError: "Cloudflare Workers AI error: No such model",
		},
		{
			name:      "incomplete run",
			response:  `{"success":true,"result":{"state":"Queued","result":null}}`,
			wantError: "Cloudflare Workers AI run did not complete (state: Queued)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, test.response)
			}))
			defer server.Close()
			requestModel := *model
			requestModel.BaseURL = server.URL + "/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai"
			result := provider.Classify(&requestModel, ai.ClassifierContext{}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{
				APIKey: "cf-key", Env: ai.ProviderEnv{CloudflareAccountIDEnv: "account-id"},
			}})
			if result.StopReason != ai.ClassifierStopError || result.ErrorMessage == nil || *result.ErrorMessage != test.wantError {
				t.Fatalf("result = %+v, want error %q", result, test.wantError)
			}
		})
	}
}

func TestCloudflareWorkersAIClassifierImageForms(t *testing.T) {
	provider := CloudflareWorkersAIProvider()
	for _, test := range []struct {
		id      string
		wantKey string
	}{
		{id: "@cf/cloudflare/clef", wantKey: "images"},
		{id: "typesafe/jev", wantKey: "assets"},
	} {
		t.Run(test.id, func(t *testing.T) {
			model := ai.GetBuiltinClassifierModel("cloudflare-workers-ai", test.id)
			if model == nil {
				t.Fatalf("classifier %q is missing", test.id)
			}
			requestModel := *model
			requestModel.Input = []string{"text", "image"}
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				var payload struct {
					Input map[string]json.RawMessage `json:"input"`
				}
				if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
					t.Errorf("decode request: %v", err)
				} else if test.wantKey == "images" {
					var images []string
					if err := json.Unmarshal(payload.Input["images"], &images); err != nil || len(images) != 1 || images[0] != "data:image/png;base64,cG5n" {
						t.Errorf("images = %v, err = %v", images, err)
					}
				} else {
					var assets map[string]struct {
						Type       string `json:"type"`
						Format     string `json:"format"`
						DataBase64 string `json:"data_base64"`
					}
					if err := json.Unmarshal(payload.Input["assets"], &assets); err != nil || assets["<image-1>"].Type != "image" || assets["<image-1>"].Format != "png" || assets["<image-1>"].DataBase64 != "cG5n" {
						t.Errorf("assets = %+v, err = %v", assets, err)
					}
					var state []json.RawMessage
					if err := json.Unmarshal(payload.Input["state"], &state); err != nil || len(state) != 2 || string(state[0]) != `"<image-1>"` {
						t.Errorf("state = %s, err = %v", payload.Input["state"], err)
					}
				}
				if request.Header.Get("TypeSafe-Danger-Zone-Images") != "" {
					t.Errorf("unexpected TypeSafe image opt-in header")
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(writer, `{"success":true,"result":{"answers":{"approved":{"type":"noul","noul":0.7}}}}`)
			}))
			defer server.Close()
			requestModel.BaseURL = server.URL + "/accounts/{CLOUDFLARE_ACCOUNT_ID}/ai"
			result := provider.Classify(&requestModel, ai.ClassifierContext{
				State:     []byte(`{"message":"hello"}`),
				Images:    []ai.ImageContent{{MimeType: "image/png", Data: "cG5n"}},
				Questions: []ai.ClassifierQuestion{{Name: "approved", Type: "bool", BoolCriteria: ai.ClassifierBoolCriteria{True: "yes", False: "no"}}},
			}, &ai.ClassifierOptions{StreamOptions: ai.StreamOptions{
				APIKey: "cf-key", Env: ai.ProviderEnv{CloudflareAccountIDEnv: "account-id"},
			}})
			if result.StopReason != ai.ClassifierStopStop {
				t.Fatalf("classification = %+v", result)
			}
		})
	}
}
