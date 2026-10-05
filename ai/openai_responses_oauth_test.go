package ai

import (
	"encoding/json"
	"testing"
)

// Upstream's exemption is credential- and endpoint-specific, not a model-wide
// compat flag. API keys and OpenAI-compatible proxies keep their cache controls.
func TestResponsesChatGPTFieldOmissionIsCredentialSpecific(t *testing.T) {
	for _, test := range []struct {
		name, provider, baseURL, apiKey string
		explicit, omit                  bool
	}{
		{"oauth explicit cache", "openai", OpenAIChatGPTResource, "oauth-token", true, true},
		{"oauth retention", "openai", OpenAIChatGPTResource, "oauth-token", false, true},
		{"api key explicit cache", "openai", OpenAIChatGPTResource, "sk-test", true, false},
		{"api key retention", "openai", OpenAIChatGPTResource, "sk-test", false, false},
		{"proxy token", "openai", "https://proxy.example/v1", "proxy-token", true, false},
		{"other provider", "custom", OpenAIChatGPTResource, "custom-token", true, false},
		{"no credential", "openai", OpenAIChatGPTResource, "", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := testResponsesModel()
			model.Provider, model.BaseURL = test.provider, test.baseURL
			model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
				SupportsExplicitPromptCacheMode: boolPtrT(test.explicit),
			}}
			maxTokens := 4096
			temperature := 0.2
			params, err := BuildOpenAIResponsesParams(model, TranscriptContext{}, &OpenAIResponsesOptions{
				StreamOptions: StreamOptions{
					APIKey: test.apiKey, CacheRetention: CacheRetentionLong, SessionID: "session",
					MaxTokens: &maxTokens, Temperature: &temperature,
				},
			}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if test.omit {
				if params.PromptCacheOptions != nil || params.PromptCacheRetention != nil {
					t.Fatal("ChatGPT request retained unsupported cache controls")
				}
			} else if test.explicit {
				if params.PromptCacheOptions == nil || params.PromptCacheOptions.TTL != "30m" {
					t.Fatal("non-ChatGPT request lost explicit cache controls")
				}
			} else if params.PromptCacheRetention == nil || *params.PromptCacheRetention != "24h" {
				t.Fatal("non-ChatGPT request lost cache retention")
			}
			if test.omit {
				if params.MaxOutputTokens != nil || params.Temperature != nil {
					t.Fatal("ChatGPT request retained unsupported sampling controls")
				}
			} else if params.MaxOutputTokens == nil || *params.MaxOutputTokens != 4096 ||
				params.Temperature == nil || *params.Temperature != 0.2 {
				t.Fatal("non-ChatGPT request lost sampling controls")
			}
			if params.PromptCacheKey == nil || *params.PromptCacheKey != "session" {
				t.Fatal("session prompt cache key was not preserved")
			}
		})
	}
}

// Upstream api/openai-responses.ts buildParams omits unsupported fields for
// Sign in with ChatGPT. Compaction uses cacheRetention=none; emitting explicit
// prompt_cache_options in that case makes subscription requests fail with 400.
func TestResponsesChatGPTCompactionOmitsUnsupportedFields(t *testing.T) {
	model := testResponsesModel()
	model.Compat = &ModelCompat{OpenAIResponses: &OpenAIResponsesCompat{
		SupportsExplicitPromptCacheMode: boolPtrT(true),
	}}
	maxTokens := 4096
	temperature := 0.2
	params, err := BuildOpenAIResponsesParams(model, TranscriptContext{}, &OpenAIResponsesOptions{
		StreamOptions: StreamOptions{
			APIKey: "oauth-access-token", CacheRetention: CacheRetentionNone,
			MaxTokens: &maxTokens, Temperature: &temperature,
		},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"prompt_cache_options", "prompt_cache_retention", "max_output_tokens", "temperature"} {
		if value, ok := wire[field]; ok {
			t.Errorf("ChatGPT request contains unsupported %s: %s", field, value)
		}
	}
}
