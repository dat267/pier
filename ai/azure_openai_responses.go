package ai

import (
	"bytes"
	ctxpkg "context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Port of api/azure-openai-responses.ts.

// DefaultAzureAPIVersion is the Azure OpenAI API version the adapter uses.
const DefaultAzureAPIVersion = "v1"

// azureToolCallProviders accepts the `call_id|item_id` tool-call id form.
var azureToolCallProviders = map[string]bool{
	"openai": true, "openai-codex": true, "opencode": true, "azure": true,
}

// AzureOpenAIResponsesOptions are the Azure Responses stream options.
type AzureOpenAIResponsesOptions struct {
	OpenAIResponsesOptions
	AzureAPIVersion     string
	AzureResourceName   string
	AzureBaseURL        string
	AzureDeploymentName string
}

// ParseDeploymentNameMap parses AZURE_OPENAI_DEPLOYMENT_NAME_MAP
// ("model=deployment,...").
func ParseDeploymentNameMap(value string) map[string]string {
	result := map[string]string{}
	for entry := range strings.SplitSeq(value, ",") {
		trimmed := JSTrim(entry)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) != 2 || JSTrim(parts[0]) == "" || JSTrim(parts[1]) == "" {
			continue
		}
		result[JSTrim(parts[0])] = JSTrim(parts[1])
	}
	return result
}

// ResolveAzureDeploymentName resolves the deployment name for a model.
func ResolveAzureDeploymentName(model *Model, options *AzureOpenAIResponsesOptions) string {
	if options != nil && options.AzureDeploymentName != "" {
		return options.AzureDeploymentName
	}
	env := ProviderEnv(nil)
	if options != nil {
		env = options.Env
	}
	mapped := ParseDeploymentNameMap(GetProviderEnvValueOr("AZURE_OPENAI_DEPLOYMENT_NAME_MAP", env))
	if deployment, ok := mapped[model.ID]; ok {
		return deployment
	}
	return model.ID
}

// NormalizeAzureBaseURL normalizes an Azure endpoint so the v1 surface can
// append `/responses` (upstream normalizeAzureBaseUrl).
func NormalizeAzureBaseURL(baseURL string) (string, error) {
	trimmed := strings.TrimRight(JSTrim(baseURL), "/")
	parsed, err := url.Parse(trimmed)
	// Go's url.Parse accepts relative input, so require an absolute http(s)
	// URL the way the upstream URL constructor does.
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("Invalid Azure OpenAI base URL: %s", baseURL)
	}
	host := strings.ToLower(parsed.Hostname())
	isAzureHost := strings.HasSuffix(host, ".openai.azure.com") ||
		strings.HasSuffix(host, ".cognitiveservices.azure.com") ||
		strings.HasSuffix(host, ".ai.azure.com")
	normalizedPath := strings.TrimRight(parsed.Path, "/")

	// Azure hosts get /openai/v1 as the base path so the SDK-style request
	// layout (`{base}/responses?api-version=v1`) works.
	if isAzureHost && (normalizedPath == "" || normalizedPath == "/" ||
		normalizedPath == "/openai" || normalizedPath == "/openai/v1/responses") {
		parsed.Path = "/openai/v1"
		parsed.RawQuery = ""
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// BuildDefaultAzureBaseURL builds the resource-name base URL.
func BuildDefaultAzureBaseURL(resourceName string) string {
	return fmt.Sprintf("https://%s.openai.azure.com/openai/v1", resourceName)
}

// ResolveAzureConfig resolves the Azure base URL and API version
// (upstream resolveAzureConfig).
func ResolveAzureConfig(model *Model, options *AzureOpenAIResponsesOptions) (string, string, error) {
	env := ProviderEnv(nil)
	if options != nil {
		env = options.Env
	}
	apiVersion := DefaultAzureAPIVersion
	if options != nil && options.AzureAPIVersion != "" {
		apiVersion = options.AzureAPIVersion
	} else if value := GetProviderEnvValueOr("AZURE_OPENAI_API_VERSION", env); value != "" {
		apiVersion = value
	}

	baseURL := ""
	if options != nil {
		baseURL = JSTrim(options.AzureBaseURL)
	}
	if baseURL == "" {
		baseURL = JSTrim(GetProviderEnvValueOr("AZURE_OPENAI_BASE_URL", env))
	}
	resourceName := ""
	if options != nil {
		resourceName = options.AzureResourceName
	}
	if resourceName == "" {
		resourceName = GetProviderEnvValueOr("AZURE_OPENAI_RESOURCE_NAME", env)
	}

	resolved := baseURL
	if resolved == "" && resourceName != "" {
		resolved = BuildDefaultAzureBaseURL(resourceName)
	}
	if resolved == "" {
		resolved = model.BaseURL
	}
	if resolved == "" {
		return "", "", fmt.Errorf("Azure OpenAI base URL is required. Set AZURE_OPENAI_BASE_URL or " +
			"AZURE_OPENAI_RESOURCE_NAME, or pass azureBaseUrl, azureResourceName, or model.baseUrl.")
	}
	normalized, err := NormalizeAzureBaseURL(resolved)
	if err != nil {
		return "", "", err
	}
	return normalized, apiVersion, nil
}

// StreamAzureOpenAIResponses streams an Azure OpenAI Responses call
// (upstream stream).
func StreamAzureOpenAIResponses(model *Model, context TranscriptContext, options *AzureOpenAIResponsesOptions) *AssistantMessageEventStream {
	responsesOptions := &OpenAIResponsesOptions{}
	if options != nil {
		responsesOptions = &options.OpenAIResponsesOptions
	}
	config := &openAIResponsesStreamConfig{
		errorPrefix:         "Azure OpenAI API error",
		noStopReasonMessage: "Azure OpenAI Responses stream ended without a stop reason",
		toolCallProviders:   azureToolCallProviders,
		modelName: func(model *Model, _ *OpenAIResponsesOptions) string {
			return ResolveAzureDeploymentName(model, options)
		},
		buildRequest: func(
			ctx ctxpkg.Context, model *Model, body []byte, apiKey string,
			requestOptions *OpenAIResponsesOptions, compat ResolvedOpenAIResponsesCompat,
			cacheSessionID string, messages []Message,
		) (*http.Request, error) {
			azureOptions := &AzureOpenAIResponsesOptions{}
			if options != nil {
				*azureOptions = *options
			}
			if requestOptions != nil {
				azureOptions.OpenAIResponsesOptions = *requestOptions
			}
			baseURL, apiVersion, err := ResolveAzureConfig(model, azureOptions)
			if err != nil {
				return nil, err
			}
			requestURL := baseURL + "/responses?api-version=" + url.QueryEscape(apiVersion)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
			if err != nil {
				return nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			ApplyIdentityHeaders(req.Header, model, requestOptions.Identity)
			// The Azure OpenAI SDK authenticates with the api-key header.
			if apiKey != "" {
				req.Header.Set("api-key", apiKey)
			}
			if cacheSessionID != "" {
				if compat.SessionAffinityFormat == SessionAffinityOpenRouter {
					req.Header.Set("x-session-id", cacheSessionID)
				} else {
					if compat.SessionAffinityFormat == SessionAffinityOpenAI {
						req.Header.Set("session_id", cacheSessionID)
					}
					req.Header.Set("x-client-request-id", cacheSessionID)
				}
			}
			for name, value := range model.Headers {
				req.Header.Set(name, value)
			}
			applyProviderHeaders(req.Header, requestOptions.Headers)
			return req, nil
		},
	}
	return streamOpenAIResponses(model, context, responsesOptions, config)
}

// StreamAzureOpenAIResponsesSimple maps reasoning levels onto the Responses
// reasoning effort (upstream streamSimple).
func StreamAzureOpenAIResponsesSimple(model *Model, context TranscriptContext, options *SimpleStreamOptions) *AssistantMessageEventStream {
	if options == nil {
		options = &SimpleStreamOptions{}
	}
	stream := NewAssistantMessageEventStream()
	if options.APIKey == "" {
		go func() {
			message := fmt.Sprintf("No API key for provider: %s", model.Provider)
			msg := &AssistantMessage{
				API: model.API, Provider: model.Provider, Model: model.ID,
				Usage: Usage{Cost: UsageCost{}}, StopReason: StopError,
				ErrorMessage: &message, Timestamp: time.Now().UnixMilli(),
			}
			stream.Push(AssistantMessageEvent{Type: EventError, Reason: StopError, Error: msg})
			stream.End(&msg)
		}()
		return stream
	}
	go func() {
		responsesOptions := &OpenAIResponsesOptions{StreamOptions: withSimpleMaxTokens(model, context, options.StreamOptions)}
		if options.ToolChoice != nil {
			responsesOptions.ToolChoice = mustMarshalJSON(*options.ToolChoice)
		}
		if options.Reasoning != "" {
			clamped := ClampThinkingLevel(model, options.Reasoning)
			if clamped != ThinkOff {
				responsesOptions.ReasoningEffort = clamped
			}
		}
		azureOptions := &AzureOpenAIResponsesOptions{OpenAIResponsesOptions: *responsesOptions}
		forwardStream(stream, StreamAzureOpenAIResponses(model, context, azureOptions))
	}()
	return stream
}
